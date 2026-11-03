package main

import (
	"bytes"
	"context"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/tikhonp/shelf/internal/platform/config"
	"github.com/tikhonp/shelf/internal/platform/db"
	"github.com/tikhonp/shelf/internal/platform/httpx"
	"github.com/tikhonp/shelf/internal/platform/module"
	"github.com/tikhonp/shelf/internal/platform/obs"
)

// syncBuffer is written by serve's logger while the test reads it.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func env(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) {
		v, ok := m[k]
		return v, ok
	}
}

// local is the environment of a test serve: defaults, except a temporary
// data directory and a free port on loopback.
func local(t *testing.T, extra map[string]string) map[string]string {
	t.Helper()
	m := map[string]string{"SHELF_DATA_DIR": filepath.Join(t.TempDir(), "data"), "SHELF_HTTP_ADDR": "127.0.0.1:0"}
	for k, v := range extra {
		m[k] = v
	}
	return m
}

func testCLI(vars map[string]string, out io.Writer, modules ...module.Module) cli {
	return cli{lookup: env(vars), stdout: out, stderr: out, modules: func(*config.Config) []module.Module { return modules }}
}

type src struct {
	name  string
	files fstest.MapFS
}

func (s src) Name() string      { return s.name }
func (s src) Migrations() fs.FS { return s.files }

func migration(up, down string) *fstest.MapFile {
	return &fstest.MapFile{Data: []byte("-- +goose Up\n" + up + "\n-- +goose Down\n" + down + "\n")}
}

func TestRunUsage(t *testing.T) {
	for args, want := range map[string]int{"": 2, "nope": 2, "serve now": 2, "healthcheck please": 2, "help": 0, "--help": 0} {
		var out bytes.Buffer
		if got := testCLI(nil, &out).run(context.Background(), strings.Fields(args)); got != want {
			t.Errorf("run(%q) = %d, want %d", args, got, want)
		}
	}
}

func TestVersionCommand(t *testing.T) {
	defer func(v string) { obs.AppVersion = v }(obs.AppVersion)
	obs.AppVersion = "main-abc1234"
	for _, arg := range []string{"version", "--version", "-v"} {
		var out bytes.Buffer
		if code := testCLI(nil, &out).run(context.Background(), []string{arg}); code != 0 || out.String() != "main-abc1234\n" {
			t.Errorf("%s: exit %d, %q", arg, code, out.String())
		}
	}
}

func TestHealthcheckCommand(t *testing.T) {
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(status)
	}))
	defer srv.Close()
	addr := srv.Listener.Addr().String() // 127.0.0.1:port
	check := func(vars map[string]string) int {
		var out bytes.Buffer
		return testCLI(vars, &out).run(context.Background(), []string{"healthcheck"})
	}

	if got := check(map[string]string{"SHELF_HTTP_ADDR": addr}); got != 0 {
		t.Fatalf("healthy server: exit %d", got)
	}
	// The ":port" form, as SHELF_HTTP_ADDR is written in compose.
	if got := check(map[string]string{"SHELF_HTTP_ADDR": addr[strings.LastIndex(addr, ":"):]}); got != 0 {
		t.Fatalf(":port form: exit %d", got)
	}
	// A broken configuration elsewhere doesn't fail the check.
	if got := check(map[string]string{"SHELF_HTTP_ADDR": addr, "SHELF_TIMEZONE": "Mars/Olympus"}); got != 0 {
		t.Fatalf("other bad variable: exit %d", got)
	}
	status = http.StatusServiceUnavailable
	if got := check(map[string]string{"SHELF_HTTP_ADDR": addr}); got != 1 {
		t.Fatalf("unhealthy server: exit %d", got)
	}
	srv.Close()
	if got := check(map[string]string{"SHELF_HTTP_ADDR": addr}); got != 1 {
		t.Fatalf("nothing listening: exit %d", got)
	}
	if got := check(map[string]string{"SHELF_HTTP_ADDR": "not an address"}); got != 1 {
		t.Fatalf("bad SHELF_HTTP_ADDR: exit %d", got)
	}
}

func TestServeStops(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var out syncBuffer
	addrs := make(chan net.Addr, 1)
	c := testCLI(local(t, nil), &out)
	c.listening = func(a net.Addr) { addrs <- a }
	done := make(chan int, 1)
	go func() { done <- c.run(ctx, []string{"serve"}) }()

	var addr net.Addr
	select {
	case addr = <-addrs:
	case code := <-done:
		t.Fatalf("serve exited %d:\n%s", code, out.String())
	case <-time.After(10 * time.Second):
		t.Fatalf("serve did not start:\n%s", out.String())
	}
	// Without keep-alive the client never dials a spare connection that
	// sends no request, which Shutdown would wait 5 seconds for.
	client := &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}
	for _, p := range []string{"/healthz", "/api/version", "/"} {
		resp, err := client.Get("http://" + addr.String() + p)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("%s: %s", p, resp.Status)
		}
	}

	cancel()
	start := time.Now()
	select {
	case code := <-done:
		if code != 0 {
			t.Errorf("exit %d:\n%s", code, out.String())
		}
		if d := time.Since(start); d > httpx.ShutdownGrace {
			t.Errorf("stopped after %s", d)
		}
	case <-time.After(httpx.ShutdownGrace + 5*time.Second):
		t.Fatal("serve still runs after the grace period")
	}
	if !strings.Contains(out.String(), `"msg":"stopped"`) {
		t.Errorf("no stop line:\n%s", out.String())
	}
}

func TestServeRefusesBadConfig(t *testing.T) {
	var out bytes.Buffer
	code := testCLI(local(t, map[string]string{"SHELF_TIMEZONE": "Mars/Olympus", "SHELF_LOG_LEVEL": "loud"}), &out).
		run(context.Background(), []string{"serve"})
	if code != 1 {
		t.Fatalf("exit %d", code)
	}
	for _, want := range []string{"invalid configuration", "SHELF_TIMEZONE", "SHELF_LOG_LEVEL"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output misses %s:\n%s", want, out.String())
		}
	}
}

func TestServeExitsOnFailingMigration(t *testing.T) {
	vars := local(t, nil)
	bad := src{"broken", fstest.MapFS{
		"00001_ok.sql":  migration("CREATE TABLE broken_ok (id INTEGER PRIMARY KEY) STRICT;", "DROP TABLE broken_ok;"),
		"00002_bad.sql": migration("SELECT * FROM no_such_table;", "SELECT 1;"),
	}}
	var out syncBuffer
	c := testCLI(vars, &out, bad)
	c.listening = func(net.Addr) { t.Error("serve listened despite the failed migration") }
	if code := c.run(context.Background(), []string{"serve"}); code != 1 {
		t.Fatalf("exit %d:\n%s", code, out.String())
	}
	if !strings.Contains(out.String(), "00002_bad.sql") {
		t.Errorf("the failing file is not named:\n%s", out.String())
	}

	d, err := db.Open(filepath.Join(vars["SHELF_DATA_DIR"], "shelf.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	var n int
	if err := d.R.Get(&n, `SELECT count(*) FROM sqlite_schema WHERE name = 'broken_ok'`); err != nil || n != 1 {
		t.Errorf("the first migration was not kept: %d, %v", n, err)
	}
}

func TestManageMigrate(t *testing.T) {
	vars := local(t, nil)
	things := src{"things", fstest.MapFS{
		"00001_a.sql": migration("CREATE TABLE things_a (id INTEGER PRIMARY KEY) STRICT;", "DROP TABLE things_a;"),
		"00002_b.sql": migration("CREATE TABLE things_b (id INTEGER PRIMARY KEY) STRICT;", "DROP TABLE things_b;"),
	}}
	bare := src{"bare", nil}
	manage := func(args ...string) (int, string) {
		var out bytes.Buffer
		code := testCLI(vars, &out, things, bare).run(context.Background(), append([]string{"manage"}, args...))
		return code, out.String()
	}

	if code, out := manage("migrate", "status"); code != 0 || !regexpMatch(out, `platform\s+1\s+pending\s+00001_jobs.sql`) ||
		!regexpMatch(out, `bare\s+-\s+no migrations`) || !regexpMatch(out, `things\s+2\s+pending\s+00002_b.sql`) {
		t.Fatalf("status before up: exit %d\n%s", code, out)
	}
	if code, out := manage("migrate", "up"); code != 0 {
		t.Fatalf("up: exit %d\n%s", code, out)
	}
	if code, out := manage("migrate", "down", "things"); code != 0 {
		t.Fatalf("down: exit %d\n%s", code, out)
	}
	if code, out := manage("migrate", "status"); code != 0 ||
		!regexpMatch(out, `things\s+1\s+applied\s+00001_a.sql`) || !regexpMatch(out, `things\s+2\s+pending\s+00002_b.sql`) {
		t.Fatalf("status after down: exit %d\n%s", code, out)
	}
	if code, out := manage("migrate", "down", "bare"); code != 1 || !strings.Contains(out, "has no migrations") {
		t.Errorf("down bare: exit %d\n%s", code, out)
	}
	if code, out := manage("migrate", "down", "ghost"); code != 1 || !strings.Contains(out, `no module "ghost"`) {
		t.Errorf("down ghost: exit %d\n%s", code, out)
	}
	for _, args := range [][]string{nil, {"migrate"}, {"migrate", "sideways"}, {"create-admin"}} {
		if code, _ := manage(args...); code != 2 {
			t.Errorf("manage %v: exit %d", args, code)
		}
	}

	vars["SHELF_QOBUZ_WORKERS"] = "0"
	if code, out := manage("migrate", "status"); code != 1 || !strings.Contains(out, "SHELF_QOBUZ_WORKERS") {
		t.Errorf("bad config: exit %d\n%s", code, out)
	}
}

func regexpMatch(s, pattern string) bool { return regexp.MustCompile(pattern).MatchString(s) }
