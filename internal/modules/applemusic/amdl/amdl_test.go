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

var settings = amdl.Settings{LiteServer: "http://shelf-wrapper:12340", Folder: "/music/apple-music"}

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
		`lite-server: "http://shelf-wrapper:12340" # wrapper-lite HTTP API endpoint`,
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
	// The list is the result, not a line of the log.
	for _, l := range lines {
		if strings.HasPrefix(l, "[{") {
			t.Fatalf("line hears the list: %s", l)
		}
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

func TestBar(t *testing.T) {
	for line, want := range map[string]int64{
		"Decrypting...   0%  (1.0 kB/27 MB, 19 MB/s)":                 1000,
		"Decrypting...  42%  (12/27 MB, 4.6 MB/s)":                    12_000_000,
		"Decrypting...   6%  (1.7/27 MB, 8.9 MB/s) ":                  1_700_000,
		"Decrypting... 100%  (27/27 MB, 24 MB/s) [1s] Decrypted":      27_000_000,
		"Decrypting...   0%  (512 B/27 MB, 1.0 MB/s)":                 512,
		"Downloading...  50%  (1.2/2.4 GB, 30 MB/s)":                  1_200_000_000,
		"Downloading... (3.1 MB, 2.0 MB/s) [2s]":                      3_100_000,
		"Converting -> flac ...":                                      -1,
		"Conversion completed in 917ms: 01. 15 Step.flac":             -1,
		"Decrypting... done":                                          -1,
		"MV Remuxing...":                                              -1,
		"03. Downloading... (Live)":                                   -1,
		"Track 1 of 10: songs":                                        -1,
		"Downloading...  50% |█████     | (1.0/2.0 MB, 4.0 MB/s)":     1_000_000,
		"Decrypting...  50%  (1.0/2.0 MB, 4.0 MB/s)\tand a long tail": 1_000_000,
	} {
		got, ok := amdl.Bar(line)
		if want < 0 {
			if ok {
				t.Errorf("%q gave %d", line, got)
			}
			continue
		}
		if !ok || got != want {
			t.Errorf("%q gave %d %v, want %d", line, got, ok, want)
		}
	}
}

// TestRecordedRuns replays amdl 0.1.4's output for In Rainbows, recorded
// on 2026-11-04 against a signed-in wrapper: a first run, a second run
// over the files the first wrote, and an --atmos run of an album that has
// no Dolby Atmos version.
func TestRecordedRuns(t *testing.T) {
	type run struct {
		lines, bars []string
		started     []int
		tr          *amdl.Tracker
		res         amdl.Result
	}
	replay := func(name string) run {
		t.Helper()
		fixture, err := filepath.Abs(filepath.Join("testdata", "live", name))
		if err != nil {
			t.Fatal(err)
		}
		r := runner(t, "FAKE_AMDL=replay", "FAKE_REPLAY="+fixture)
		out := run{tr: amdl.NewTracker()}
		out.res, err = r.Run(context.Background(), "https://music.apple.com/ru/album/1109714933", nil, func(l string) {
			out.lines = append(out.lines, l)
			if amdl.IsBar(l) {
				out.bars = append(out.bars, l)
			}
			if n := out.tr.Line(l); n > 0 {
				out.started = append(out.started, n)
			}
		})
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	ten := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}

	fresh := replay("in-rainbows-fresh.txt")
	if !fresh.res.Listed || len(fresh.res.Tracks) != 10 || !slices.Equal(fresh.started, ten) {
		t.Fatalf("listed %v, %d tracks, started %v", fresh.res.Listed, len(fresh.res.Tracks), fresh.started)
	}
	first := fresh.res.Tracks[0]
	if first.Path != "/music/apple-music/Radiohead - In Rainbows (2007)[16B-44.1kHz]/01. 15 Step.flac" || first.Song != "15 Step" ||
		first.ArtistID != "657515" || fresh.res.Tracks[3].Song != "Weird Fishes / Arpeggi" ||
		filepath.Base(fresh.res.Tracks[3].Path) != "04. Weird Fishes _ Arpeggi.flac" {
		t.Fatalf("%+v", fresh.res.Tracks)
	}
	// Every redraw is a bar with a count, and each song ends at 100%.
	full := 0
	for _, b := range fresh.bars {
		if _, ok := amdl.Bar(b); !ok {
			t.Errorf("no count in %q", b)
		}
		if strings.Contains(b, "100%") {
			full++
		}
	}
	if len(fresh.bars) < 100 || full != 10 {
		t.Fatalf("%d bars, %d at 100%%", len(fresh.bars), full)
	}
	for _, n := range ten {
		if fresh.tr.Skipped(n) || fresh.tr.Failure(n) != "" {
			t.Fatalf("song %d: skipped %v, failure %q", n, fresh.tr.Skipped(n), fresh.tr.Failure(n))
		}
	}
	// "Repaired 3 affected packets" and the closing tally are not failures.
	if fresh.tr.LastFailure() != "" {
		t.Fatalf("failure %q", fresh.tr.LastFailure())
	}
	for _, l := range fresh.lines {
		if strings.HasPrefix(l, "[{") {
			t.Fatalf("line hears the list: %.60s", l)
		}
	}

	again := replay("in-rainbows-exists.txt")
	if !again.res.Listed || len(again.res.Tracks) != 10 || !slices.Equal(again.started, ten) || len(again.bars) != 0 {
		t.Fatalf("listed %v, %d tracks, started %v, %d bars", again.res.Listed, len(again.res.Tracks), again.started, len(again.bars))
	}
	for _, n := range ten {
		if !again.tr.Skipped(n) {
			t.Fatalf("song %d not skipped", n)
		}
	}

	atmos := replay("in-rainbows-atmos.txt")
	// amdl exits 0 and lists nothing. Only its lines name the failures.
	if !atmos.res.Listed || len(atmos.res.Tracks) != 0 || !slices.Equal(atmos.started, ten) {
		t.Fatalf("listed %v, %d tracks, started %v", atmos.res.Listed, len(atmos.res.Tracks), atmos.started)
	}
	const noCodec = "⚠ Failed to extract info from manifest: no codec found"
	for _, n := range ten {
		if atmos.tr.Failure(n) != noCodec {
			t.Fatalf("song %d: %q", n, atmos.tr.Failure(n))
		}
	}
}

func TestSplitBar(t *testing.T) {
	// From a live run, where Apple's key server refused a song's key.
	const live = "Decrypting...   7%  (1.5/22 MB, 35 MB/s) Failed to run v4: lite-server /key returned code=500 msg=internal error"
	for line, want := range map[string][2]string{
		live: {"Decrypting...   7%  (1.5/22 MB, 35 MB/s)", "Failed to run v4: lite-server /key returned code=500 msg=internal error"},
		"Decrypting... 100%  (27/27 MB, 24 MB/s) [1s] Decrypted": {"Decrypting... 100%  (27/27 MB, 24 MB/s) [1s]", "Decrypted"},
		"Decrypting...  42%  (12/27 MB, 4.6 MB/s) ":              {"Decrypting...  42%  (12/27 MB, 4.6 MB/s)", ""},
		"Downloading... (3.1 MB, 2.0 MB/s) [2s]":                 {"Downloading... (3.1 MB, 2.0 MB/s) [2s]", ""},
		"Converting -> flac ...":                                 {"", "Converting -> flac ..."},
	} {
		bar, rest := amdl.SplitBar(line)
		if bar != want[0] || rest != want[1] {
			t.Errorf("%q gave %q and %q", line, bar, rest)
		}
	}

	tr := amdl.NewTracker()
	tr.Line("Track 11 of 12: songs")
	tr.Line(live)
	if got := tr.Failure(11); got != "Failed to run v4: lite-server /key returned code=500 msg=internal error" {
		t.Fatalf("failure %q", got)
	}
	if got := amdl.ErrorLines([]string{"Track 11 of 12: songs", live, "Error detected, exiting..."}); len(got) != 1 || got[0] != tr.Failure(11) {
		t.Fatalf("error lines %q", got)
	}
	if !amdl.FailedPass([]string{"=======  [✔ ] Completed: 11/12  |  [⚠ ] Warnings: 0  |  [✖ ] Errors: 1  =======", "Error detected, exiting..."}) ||
		amdl.FailedPass([]string{"Failed to get token."}) {
		t.Fatal("FailedPass")
	}
}

func TestTrackerAgain(t *testing.T) {
	tr := amdl.NewTracker()
	for _, l := range []string{
		"Track 1 of 3: songs", "Converted track already exists locally.",
		"Track 2 of 3: songs", "Conversion completed in 1s: 02. B.flac",
		"Track 3 of 3: songs", "Failed to run v4: key retrieval failed",
	} {
		tr.Line(l)
	}
	tr.Again()
	if tr.Failure(3) != "" || tr.LastFailure() != "" {
		t.Fatal("failures kept")
	}
	for _, l := range []string{
		"Track 1 of 3: songs", "Converted track already exists locally.",
		"Track 2 of 3: songs", "Converted track already exists locally.",
		"Track 3 of 3: songs", "Conversion completed in 1s: 03. C.flac",
	} {
		tr.Line(l)
	}
	// Song 1 was there before the first run; the first run wrote song 2.
	if !tr.Skipped(1) || tr.Skipped(2) || tr.Skipped(3) || tr.First() != 1 {
		t.Fatalf("skipped %v %v %v", tr.Skipped(1), tr.Skipped(2), tr.Skipped(3))
	}
}
