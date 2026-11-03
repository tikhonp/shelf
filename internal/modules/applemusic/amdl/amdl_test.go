package amdl_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/tikhonp/shelf/internal/modules/applemusic/amdl"
	"github.com/tikhonp/shelf/internal/platform/testwait"
)

const base = `# Base config, as the old bot used it
general:
  lite-server: "http://127.0.0.1:12340"   # wrapper-lite HTTP API endpoint
  media-user-token: "secret-media-token"
  language: "en-GB"
  storefront: "ru"                        # used only for search
  proxy: "http://proxy:3128"
  exit-on-error: false
media:
  alac-max: 192000
paths:
  alac: "/downloads"
  atmos: "/downloads"
  album-folder: "{ArtistName} - {AlbumName} ({ReleaseYear})[{Quality}]"
convert:
  after-download: true
  format: "flac"
`

var settings = amdl.Settings{LiteServer: "http://apple-music-wrapper:12340", Folder: "/music/apple-music"}

func TestRenderConfig(t *testing.T) {
	out, b, err := amdl.Render([]byte(base), settings)
	if err != nil {
		t.Fatal(err)
	}
	if b.Language != "en-GB" || b.Storefront != "ru" {
		t.Fatalf("%+v", b)
	}
	got := string(out)
	for _, want := range []string{
		`lite-server: "http://apple-music-wrapper:12340" # wrapper-lite HTTP API endpoint`,
		`exit-on-error: true`,
		`proxy: ""`,
		`alac: "/music/apple-music"`, `atmos: "/music/apple-music"`, `aac: "/music/apple-music"`, `mv: "/music/apple-music"`,
		// Everything else keeps its value, its place and its comments.
		`# Base config, as the old bot used it`,
		`media-user-token: "secret-media-token"`,
		`storefront: "ru" # used only for search`,
		`album-folder: "{ArtistName} - {AlbumName} ({ReleaseYear})[{Quality}]"`,
		`alac-max: 192000`, `after-download: true`, `format: "flac"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	if strings.Index(got, "media:") > strings.Index(got, "paths:") || strings.Contains(got, "/downloads") || strings.Contains(got, "proxy:3128") {
		t.Fatalf("order or values changed:\n%s", got)
	}

	// A base without the sections gets them.
	out, _, err = amdl.Render([]byte("media:\n  alac-max: 96000\n"), settings)
	if err != nil || !strings.Contains(string(out), "general:\n  lite-server:") || !strings.Contains(string(out), "paths:\n  alac:") {
		t.Fatalf("%v\n%s", err, out)
	}

	// The renderer writes it with mode 0600, and again when the base changes.
	dir := t.TempDir()
	basePath := filepath.Join(dir, "base.yaml")
	if err := os.WriteFile(basePath, []byte(base), 0o644); err != nil {
		t.Fatal(err)
	}
	r := &amdl.Renderer{BasePath: basePath, Path: filepath.Join(dir, "amdl", "config.yaml"), Settings: settings}
	if b, err := r.Ensure(); err != nil || b.Language != "en-GB" {
		t.Fatalf("%+v %v", b, err)
	}
	fi, err := os.Stat(r.Path)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("%v %v", fi, err)
	}
	if err := os.WriteFile(basePath, []byte(strings.Replace(base, "en-GB", "ru-RU", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(basePath, later, later); err != nil {
		t.Fatal(err)
	}
	if b, err := r.Ensure(); err != nil || b.Language != "ru-RU" {
		t.Fatalf("%+v %v", b, err)
	}
	if data, _ := os.ReadFile(r.Path); !strings.Contains(string(data), "ru-RU") {
		t.Fatal("not written again")
	}
	if tmps, _ := filepath.Glob(filepath.Join(dir, "amdl", ".*")); len(tmps) != 0 {
		t.Fatalf("temp files left: %v", tmps)
	}
	// A config that went missing is written again.
	if err := os.Remove(r.Path); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Ensure(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(r.Path); err != nil {
		t.Fatal(err)
	}
}

func TestMissingBaseConfig(t *testing.T) {
	dir := t.TempDir()
	missing := &amdl.Renderer{BasePath: filepath.Join(dir, "none.yaml"), Path: filepath.Join(dir, "out.yaml"), Settings: settings}
	if _, err := missing.Load(); err == nil || !strings.Contains(err.Error(), "none.yaml can't be read") {
		t.Fatalf("%v", err)
	}
	if _, err := missing.Ensure(); err == nil {
		t.Fatal("Ensure wrote a config without a base")
	}
	for name, body := range map[string]string{
		"broken": "general: [unclosed\n",
		"scalar": "just text\n",
		"empty":  "",
		"list":   "general:\n  - a\n",
	} {
		p := filepath.Join(dir, name+".yaml")
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		r := &amdl.Renderer{BasePath: p, Path: filepath.Join(dir, "out.yaml"), Settings: settings}
		if _, err := r.Load(); err == nil || !strings.Contains(err.Error(), "is broken") {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "out.yaml")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("Load wrote a file")
	}
}

func runner(t *testing.T, env ...string) *amdl.Runner {
	t.Helper()
	bin, err := filepath.Abs("testdata/fake-amdl.sh")
	if err != nil {
		t.Fatal(err)
	}
	return &amdl.Runner{
		Bin: bin, Config: "/data/amdl/config.yaml", WorkDir: t.TempDir(),
		Timeout: time.Minute, KillAfter: 5 * time.Second, Env: env,
	}
}

func TestRunnerOutput(t *testing.T) {
	music := t.TempDir()
	r := runner(t, "FAKE_MUSIC="+music)
	tr := amdl.NewTracker()
	var lines, bars []string
	var started []int
	res, err := r.Run(context.Background(), "https://music.apple.com/ru/album/1", nil, func(l string) {
		lines = append(lines, l)
		if amdl.IsBar(l) {
			bars = append(bars, l)
		}
		if n := tr.Line(l); n > 0 {
			started = append(started, n)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Listed || len(res.Tracks) != 2 {
		t.Fatalf("%+v", res)
	}
	want := amdl.Track{
		Path:   music + "/apple-music/Northern Static - Low Tide Hymns (2019)[24B-96kHz]/01. Shoreline.flac",
		Artist: "Northern Static", ArtistID: "111", Album: "Low Tide Hymns", Song: "Shoreline",
	}
	if res.Tracks[0] != want || res.Tracks[1].Song != "Undertow" {
		t.Fatalf("%+v", res.Tracks)
	}
	// \r ends a line too: the bar's three redraws are three lines.
	if len(bars) != 3 || !strings.Contains(bars[2], "100%") {
		t.Fatalf("bars %q", bars)
	}
	if !slices.Equal(started, []int{1, 2, 3}) {
		t.Fatalf("started %v", started)
	}
	if tr.Skipped(1) || !tr.Skipped(2) || tr.Failure(3) != "Unavailable" || tr.Failure(1) != "" {
		t.Fatalf("skipped %v %v, failures %q %q", tr.Skipped(1), tr.Skipped(2), tr.Failure(1), tr.Failure(3))
	}
	if len(res.Tail) == 0 || !strings.HasPrefix(res.Tail[len(res.Tail)-1], "[{") {
		t.Fatalf("tail %q", res.Tail)
	}

	// A run that stops before its summary lists nothing.
	res, err = runner(t, "FAKE_AMDL=early").Run(context.Background(), "x", nil, nil)
	if err != nil || res.Listed || !slices.Equal(amdl.ErrorLines(res.Tail), []string{"Failed to get token."}) {
		t.Fatalf("%+v %v", res, err)
	}
}

func TestRunnerFailure(t *testing.T) {
	_, err := runner(t, "FAKE_AMDL=fail").Run(context.Background(), "x", nil, nil)
	var ee *amdl.ExitError
	if !errors.As(err, &ee) || ee.Code != 1 {
		t.Fatalf("%v", err)
	}
	if !slices.Equal(ee.Lines, []string{"Failed to run v4: key retrieval failed"}) {
		t.Fatalf("%q", ee.Lines)
	}
	if err.Error() != "amdl exited with code 1: Failed to run v4: key retrieval failed" {
		t.Fatal(err)
	}
}

func pidFile(t *testing.T) (string, func() int) {
	p := filepath.Join(t.TempDir(), "pids")
	return p, func() int {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		n, err := strconv.Atoi(strings.TrimSpace(string(b)))
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
}

// gone waits for a process to end; a zombie still answers kill(0) until its
// parent, the dead shell's init, reaps it.
func gone(t *testing.T, pid int) {
	t.Helper()
	deadline := testwait.Deadline(5 * time.Second)
	for {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return
		}
		out, _ := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
		if strings.Contains(string(out), ") Z ") {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("process %d lives on", pid)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestRunnerTimeout(t *testing.T) {
	pids, child := pidFile(t)
	r := runner(t, "FAKE_AMDL=hang", "FAKE_PIDS="+pids)
	r.Timeout = 300 * time.Millisecond
	begin := time.Now()
	_, err := r.Run(context.Background(), "x", nil, nil)
	if !errors.Is(err, amdl.ErrTimeout) || !strings.Contains(err.Error(), "longer than 300ms") {
		t.Fatalf("%v", err)
	}
	if took := time.Since(begin); took > 3*time.Second {
		t.Fatalf("took %s", took)
	}
	gone(t, child())
}

func TestRunnerCancelKillsGroup(t *testing.T) {
	pids, child := pidFile(t)
	r := runner(t, "FAKE_AMDL=stubborn", "FAKE_PIDS="+pids)
	r.KillAfter = 300 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	go func() {
		<-started
		cancel()
	}()
	begin := time.Now()
	_, err := r.Run(ctx, "x", nil, func(l string) {
		if l == "Album" {
			close(started)
		}
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("%v", err)
	}
	// The group ignored SIGTERM, so SIGKILL ended it after KillAfter.
	if took := time.Since(begin); took < 300*time.Millisecond {
		t.Fatalf("ended after %s", took)
	}
	gone(t, child())
}

func TestRunnerEnvironment(t *testing.T) {
	r := runner(t, "FAKE_AMDL=env")
	var lines []string
	res, err := r.Run(context.Background(), "https://music.apple.com/ru/album/1", []string{"--atmos"}, func(l string) { lines = append(lines, l) })
	if err != nil || !res.Listed || len(res.Tracks) != 0 {
		t.Fatalf("%+v %v", res, err)
	}
	wd, _ := filepath.EvalSymlinks(r.WorkDir)
	want := []string{
		"pwd=" + wd,
		"cache=" + filepath.Join(r.WorkDir, ".cache"),
		"tmp=" + filepath.Join(r.WorkDir, "tmp"),
		"args=--config /data/amdl/config.yaml --json --atmos https://music.apple.com/ru/album/1",
	}
	for i, w := range want {
		if i >= len(lines) || lines[i] != w {
			t.Fatalf("line %d: %q, want %q", i, lines, w)
		}
	}
	for _, d := range []string{".cache", "tmp"} {
		if fi, err := os.Stat(filepath.Join(r.WorkDir, d)); err != nil || !fi.IsDir() {
			t.Fatalf("%s: %v", d, err)
		}
	}
}

func TestErrorLines(t *testing.T) {
	tail := []string{"Track 1 of 2: songs", "⚠ Failed to extract info from manifest: x", "Unavailable, trying to dl aac-lc",
		"Failed to run v4: a", "Failed to rip album: b", "Failed to write cover.", "=======  [✔ ] Completed", "Error detected, exiting..."}
	if got := amdl.ErrorLines(tail); !slices.Equal(got, []string{"Failed to run v4: a", "Failed to rip album: b", "Failed to write cover."}) {
		t.Fatalf("%q", got)
	}
}
