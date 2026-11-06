package platform_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/tikhonp/shelf/internal/platform"
	"github.com/tikhonp/shelf/internal/platform/config"
	"github.com/tikhonp/shelf/internal/platform/module"
	"github.com/tikhonp/shelf/internal/platform/obs"
	"github.com/tikhonp/shelf/internal/platform/web"
)

var (
	ctx     = context.Background()
	discard = slog.New(slog.NewTextHandler(io.Discard, nil))
)

func testConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg, err := config.Load(func(k string) (string, bool) {
		if k == "SHELF_DATA_DIR" {
			return filepath.Join(t.TempDir(), "data"), true
		}
		return "", false
	})
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// fake is a module with a table and every optional interface.
type fake struct {
	inited, migrated bool
	deps             module.Deps
}

func (*fake) Name() string { return "fake" }
func (*fake) Migrations() fs.FS {
	return fstest.MapFS{"00001_init.sql": {Data: []byte("-- +goose Up\nCREATE TABLE fake_things (id INTEGER PRIMARY KEY) STRICT;\n-- +goose Down\nDROP TABLE fake_things;\n")}}
}
func (f *fake) Init(d module.Deps) error { f.inited, f.deps = true, d; return nil }
func (f *fake) AfterMigrate(ctx context.Context) error {
	var n int
	if err := f.deps.DB.R.GetContext(ctx, &n, `SELECT count(*) FROM fake_things`); err != nil {
		return err
	}
	f.migrated = true
	return nil
}
func (*fake) Routes(api *echo.Group) {
	api.GET("/fake", func(c *echo.Context) error { return c.String(http.StatusOK, "fake") })
}

func open(t *testing.T, modules ...module.Module) *platform.App {
	t.Helper()
	a, err := platform.Open(testConfig(t), discard, modules...)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })
	if err := a.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return a
}

func get(a *platform.App, method, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	a.HTTP().ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

func TestOptionalModuleInterfacesArePickedUp(t *testing.T) {
	f := &fake{}
	a := open(t, f)
	if !f.inited || f.deps.DB != a.DB || f.deps.Cfg != a.Cfg || f.deps.Jobs != a.Jobs || f.deps.Hub != a.Hub ||
		f.deps.Sources != a.Sources || f.deps.Closing == nil || !f.migrated {
		t.Errorf("fake = %+v", f)
	}
	st, err := a.MigrationStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(st) != 3 || st[0].Module != platform.Name || st[2].Module != "fake" || !st[2].Applied {
		t.Errorf("status = %+v", st)
	}
	if rec := get(a, http.MethodGet, "/api/fake"); rec.Code != http.StatusOK || rec.Body.String() != "fake" {
		t.Errorf("module route: %d %q", rec.Code, rec.Body)
	}
	if m, ok := a.Module("fake"); !ok || m != f {
		t.Error("Module(fake) not found")
	}
	if _, ok := a.Module(platform.Name); !ok {
		t.Error("the platform is not a module")
	}
}

func TestDuplicateModuleRefused(t *testing.T) {
	if _, err := platform.Open(testConfig(t), discard, &fake{}, &fake{}); err == nil {
		t.Fatal("two modules with one name were accepted")
	}
}

type failingInit struct{ fake }

func (*failingInit) Init(module.Deps) error { return errors.New("no token") }

func TestFailingInitStopsOpen(t *testing.T) {
	_, err := platform.Open(testConfig(t), discard, &failingInit{})
	if err == nil || !strings.Contains(err.Error(), "init fake: no token") {
		t.Fatalf("err = %v", err)
	}
}

func TestUnknownAPIPathIsJSON404(t *testing.T) {
	a := open(t)
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/nope"},
		{http.MethodGet, "/api"},
		{http.MethodGet, "/api/"},
		{http.MethodGet, "/api/version/extra"},
		{http.MethodPost, "/api/nope"},
		{http.MethodDelete, "/api/downloads/1"},
	} {
		rec := get(a, tc.method, tc.path)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s %s: %d %q", tc.method, tc.path, rec.Code, rec.Body)
			continue
		}
		var body web.ErrorBody
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Error == "" {
			t.Errorf("%s %s: body %q", tc.method, tc.path, rec.Body)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			t.Errorf("%s %s: Content-Type %q", tc.method, tc.path, ct)
		}
	}
}

func TestVersionRoute(t *testing.T) {
	a := open(t)
	rec := get(a, http.MethodGet, "/api/version")
	var body platform.VersionBody
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &body) != nil || body.Version != obs.AppVersion {
		t.Fatalf("%d %q", rec.Code, rec.Body)
	}
	if rec.Header().Get("Content-Security-Policy") != web.CSP {
		t.Error("the API answer has no CSP")
	}
	rec = get(a, http.MethodPost, "/api/version")
	if ct := rec.Header().Get("Content-Type"); rec.Code != http.StatusNotFound || !strings.HasPrefix(ct, "application/json") {
		t.Errorf("POST /api/version: %d %q", rec.Code, ct)
	}
}

func TestUIServesEveryOtherGet(t *testing.T) {
	a := open(t)
	a.UI = fstest.MapFS{"index.html": {Data: []byte("<!doctype html>app")}}
	for _, p := range []string{"/", "/downloads", "/status"} {
		rec := get(a, http.MethodGet, p)
		if rec.Code != http.StatusOK || rec.Body.String() != "<!doctype html>app" {
			t.Errorf("%s: %d %q", p, rec.Code, rec.Body)
		}
		if rec.Header().Get("Content-Security-Policy") != web.CSP {
			t.Errorf("%s: no CSP", p)
		}
	}
	for _, p := range []string{"/healthz", "/", "/downloads"} {
		if rec := get(a, http.MethodHead, p); rec.Code != http.StatusOK {
			t.Errorf("HEAD %s: %d", p, rec.Code)
		}
	}
	if rec := get(a, http.MethodPost, "/downloads"); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /downloads: %d", rec.Code)
	}
}

func TestHealthFailsWithoutTheDatabase(t *testing.T) {
	a := open(t)
	if err := a.Health(ctx); err != nil {
		t.Fatalf("healthy: %v", err)
	}
	_ = a.DB.Close()
	if err := a.Health(ctx); err == nil {
		t.Fatal("healthy with a closed database")
	}
	if rec := get(a, http.MethodGet, "/healthz"); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("/healthz: %d", rec.Code)
	}
}

func TestOpenCreatesTheDataDirectory(t *testing.T) {
	cfg := testConfig(t)
	cfg.DataDir = filepath.Join(t.TempDir(), "a", "b")
	a, err := platform.Open(cfg, discard)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = a.Close() }()
	if _, err := os.Stat(filepath.Join(cfg.DataDir, "shelf.db")); err != nil {
		t.Fatal(err)
	}
}

// runner is a module whose Run lasts until the server stops, or fails at
// once.
type runner struct {
	name    string
	fail    bool
	started chan struct{}
	stopped atomic.Bool
}

func (r *runner) Name() string    { return r.name }
func (*runner) Migrations() fs.FS { return nil }
func (r *runner) Run(ctx context.Context) error {
	close(r.started)
	if r.fail {
		return errors.New("no network")
	}
	<-ctx.Done()
	time.Sleep(20 * time.Millisecond)
	r.stopped.Store(true)
	return nil
}

func TestRunnersRunWithServe(t *testing.T) {
	steady := &runner{name: "steady", started: make(chan struct{})}
	broken := &runner{name: "broken", fail: true, started: make(chan struct{})}
	a := open(t, steady, broken)
	a.Cfg.HTTPAddr = "127.0.0.1:0"
	sctx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- a.Serve(sctx, func(net.Addr) {}) }()
	<-steady.started
	<-broken.started
	select {
	case err := <-done:
		t.Fatalf("a failing runner stopped the server: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !steady.stopped.Load() {
		t.Fatal("Serve returned before the runner stopped")
	}
}

// A runner's work is serve's only: Open and Migrate, which manage runs,
// never start it.
func TestManageStartsNoRunner(t *testing.T) {
	r := &runner{name: "steady", started: make(chan struct{})}
	open(t, r)
	select {
	case <-r.started:
		t.Fatal("Run started outside Serve")
	default:
	}
}

func TestSimpleAuthenticationProtectsPlatformAndModuleRoutes(t *testing.T) {
	cfg := testConfig(t)
	cfg.AuthProvider = "simple"
	cfg.PublicURL, _ = url.Parse("https://shelf.example.com")
	a, err := platform.Open(cfg, discard, &fake{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	if err := a.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := a.Auth.Add(ctx, "Alice", "a sufficiently long password"); err != nil {
		t.Fatal(err)
	}
	e := a.HTTP()
	for _, path := range []string{"/api/version", "/api/fake", "/api/library/tracks/1/audio", "/api/downloads/1/log", "/api/events"} {
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != 401 || rec.Header().Get("WWW-Authenticate") != "" {
			t.Fatalf("unprotected %s: %d", path, rec.Code)
		}
	}
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"username":"Alice","password":"a sufficiently long password"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("login: %d %s", rec.Code, rec.Body)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("no session")
	}
	for _, path := range []string{"/api/version", "/api/fake"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		for _, c := range cookies {
			req.AddCookie(c)
		}
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("authenticated %s: %d", path, rec.Code)
		}
	}
}
