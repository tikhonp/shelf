package applemusic

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/tikhonp/shelf/internal/modules/applemusic/amdl"
	"github.com/tikhonp/shelf/internal/source"
)

// reporter records what a download reports.
type reporter struct {
	mu       sync.Mutex
	planned  []source.Track
	started  []int
	progress map[int][]int64
	done     map[int]source.TrackResult
	lines    []string
	working  []string
}

func newReporter() *reporter {
	return &reporter{done: map[int]source.TrackResult{}, progress: map[int][]int64{}}
}

func (r *reporter) Plan(t []source.Track) { r.mu.Lock(); r.planned = t; r.mu.Unlock() }
func (r *reporter) Start(p int)           { r.mu.Lock(); r.started = append(r.started, p); r.mu.Unlock() }
func (r *reporter) Progress(p int, n int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.progress[p] = append(r.progress[p], n)
}
func (r *reporter) Done(p int, res source.TrackResult) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.done[p] = res
}
func (r *reporter) Log() source.Logger { return logger{r} }
func (r *reporter) Working(f string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.working = append(r.working, f)
}

type logger struct{ r *reporter }

func (l logger) add(format string, a ...any) {
	l.r.mu.Lock()
	defer l.r.mu.Unlock()
	l.r.lines = append(l.r.lines, fmt.Sprintf(format, a...))
}
func (l logger) Debug(f string, a ...any) { l.add(f, a...) }
func (l logger) Info(f string, a ...any)  { l.add(f, a...) }
func (l logger) Warn(f string, a ...any)  { l.add(f, a...) }
func (l logger) Error(f string, a ...any) { l.add(f, a...) }

func (r *rig) download(mode string, ref source.Ref, format string, env ...string) (*reporter, source.DownloadResult, string, error) {
	r.t.Helper()
	args := filepath.Join(r.t.TempDir(), "args")
	r.s.runner.Env = append([]string{"SHELF_FAKE_AMDL=" + mode, "FAKE_ARGS=" + args}, env...)
	rep := newReporter()
	res, err := r.s.Download(bg, source.DownloadRequest{DownloadID: 1, Ref: ref, Format: format, MusicDir: r.music}, rep)
	b, _ := os.ReadFile(args)
	return rep, res, string(b), err
}

var album = source.Ref{Source: ID, Kind: source.KindAlbum, ID: "1440000000"}

const folder = "apple-music/Northern Static - Low Tide Hymns (2019)[24B-96kHz]"

func TestDownloadAlbum(t *testing.T) {
	r := newRig(t)
	rep, res, args, err := r.download("album", album, FormatALAC)
	if err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(r.data, "amdl", "config.yaml")
	if args != "--config "+cfgPath+" --json https://music.apple.com/ru/album/1440000000" {
		t.Fatalf("args %q", args)
	}
	if fi, err := os.Stat(cfgPath); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("%v %v", fi, err)
	}
	if b, _ := os.ReadFile(cfgPath); !strings.Contains(string(b), `alac: "`+filepath.Join(r.music, "apple-music")+`"`) {
		t.Fatalf("config:\n%s", b)
	}
	if !slices.Equal(rep.working, []string{"apple-music"}) {
		t.Fatalf("working %q", rep.working)
	}
	if len(rep.planned) != 3 || !slices.Equal(rep.started, []int{1, 2, 3}) {
		t.Fatalf("planned %d, started %v", len(rep.planned), rep.started)
	}
	d1 := rep.done[1]
	if d1.Err != nil || d1.Path != folder+"/01. Shoreline.flac" || d1.Skipped || d1.Size != 42 ||
		d1.Quality != (source.Quality{Codec: "flac", BitDepth: 24, SampleRate: 96000, HiRes: true}) {
		t.Fatalf("1: %+v", d1)
	}
	if d2 := rep.done[2]; d2.Err == nil || d2.Err.Error() != "Unavailable" {
		t.Fatalf("2: %+v", d2)
	}
	if d3 := rep.done[3]; d3.Err != nil || !d3.Skipped || d3.Path != folder+"/03. Breakwater.flac" {
		t.Fatalf("3: %+v", d3)
	}
	if !slices.Equal(res.Folders, []string{folder}) || len(res.Files) != 2 || res.TrackNoun != "song" ||
		res.Tool != "amdl "+amdl.Version+", wrapper-lite storefront ru" {
		t.Fatalf("%+v", res)
	}
	log := strings.Join(rep.lines, "\n")
	for _, want := range []string{"Running amdl --json for https://music.apple.com/ru/album/1440000000", "Track 1 of 3: songs", "Decrypting... 100%"} {
		if !strings.Contains(log, want) {
			t.Errorf("log lacks %q:\n%s", want, log)
		}
	}
}

func TestDownloadSong(t *testing.T) {
	r := newRig(t)
	rep, res, args, err := r.download("song", source.Ref{Source: ID, Kind: source.KindTrack, ID: "1440000002"}, FormatAtmos)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(args, "--json --atmos https://music.apple.com/ru/song/1440000002") {
		t.Fatalf("args %q", args)
	}
	// amdl numbers the song by its album; it is still the one track.
	if !slices.Equal(rep.started, []int{1}) || rep.done[1].Path != folder+"/02. Undertow.flac" || !slices.Equal(res.Folders, []string{folder}) {
		t.Fatalf("%v %+v %+v", rep.started, rep.done, res)
	}
}

// TestDownloadRecorded replays amdl's recorded runs for In Rainbows against
// Apple's recorded answer for the album.
func TestDownloadRecorded(t *testing.T) {
	r := newRig(t)
	rainbows := source.Ref{Source: ID, Kind: source.KindAlbum, ID: "1109714933"}
	replay := func(name, format string) (*reporter, source.DownloadResult, string, error) {
		t.Helper()
		fixture, err := filepath.Abs(filepath.Join("amdl", "testdata", "live", name))
		if err != nil {
			t.Fatal(err)
		}
		return r.download("replay", rainbows, format, "FAKE_REPLAY="+fixture)
	}
	const dir = "apple-music/Radiohead - In Rainbows (2007)[16B-44.1kHz]"
	ten := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}

	rep, res, _, err := replay("in-rainbows-fresh.txt", FormatALAC)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.planned) != 10 || !slices.Equal(rep.started, ten) || !slices.Equal(res.Folders, []string{dir}) || len(res.Files) != 10 {
		t.Fatalf("planned %d, started %v, %+v", len(rep.planned), rep.started, res)
	}
	for _, n := range ten {
		d := rep.done[n]
		if d.Err != nil || d.Skipped || d.Quality.Codec != "flac" || d.Quality.BitDepth != 16 || d.Quality.SampleRate != 44100 ||
			!strings.HasPrefix(d.Path, dir+"/") || filepath.Base(d.Path)[:3] != fmt.Sprintf("%02d.", n) {
			t.Fatalf("song %d: %+v", n, d)
		}
		// A song's progress ends at its bar's 100%.
		if p := rep.progress[n]; len(p) == 0 || p[len(p)-1] < 10_000_000 {
			t.Fatalf("song %d progress %v", n, p)
		}
	}
	if p := rep.progress[1]; p[len(p)-1] != 27_000_000 {
		t.Fatalf("song 1 progress %v", p)
	}
	if rep.done[4].Path != dir+"/04. Weird Fishes _ Arpeggi.flac" {
		t.Fatalf("%+v", rep.done[4])
	}
	// The replay prints everything at once, so the log keeps only the first
	// redraw and every 100%. It has none of the list.
	bars := 0
	for _, l := range rep.lines {
		if amdl.IsBar(l) {
			bars++
		}
		if strings.HasPrefix(l, "[{") {
			t.Fatalf("log has the list: %.60s", l)
		}
	}
	if bars != 11 {
		t.Fatalf("%d bars in the log", bars)
	}

	// A second run finds every file complete.
	rep, _, _, err = replay("in-rainbows-exists.txt", FormatALAC)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range ten {
		if d := rep.done[n]; d.Err != nil || !d.Skipped || d.Size == 0 || !strings.HasPrefix(d.Path, dir+"/") {
			t.Fatalf("song %d: %+v", n, d)
		}
	}

	// The album has no Dolby Atmos version, which the catalog tells before
	// amdl runs.
	_, _, args, err := replay("in-rainbows-atmos.txt", FormatAtmos)
	if err == nil || err.Error() != "In Rainbows has no Dolby Atmos version" || args != "" {
		t.Fatalf("%v, args %q", err, args)
	}
	// Had amdl run for it, it would have exited 0, listed nothing, and
	// named the failure on every song.
	rep, _, _, err = replay("in-rainbows-atmos.txt", FormatALAC)
	const noCodec = "⚠ Failed to extract info from manifest: no codec found"
	if err == nil || err.Error() != "amdl wrote no songs: "+noCodec {
		t.Fatal(err)
	}
	for _, n := range ten {
		if d := rep.done[n]; d.Err == nil || d.Err.Error() != noCodec {
			t.Fatalf("song %d: %+v", n, d)
		}
	}
}

// TestDownloadRunsAgain checks that a run in which a song failed runs once
// more.
func TestDownloadRunsAgain(t *testing.T) {
	const keyRefused = "Failed to run v4: lite-server /key returned code=500 msg=internal error"
	r := newRig(t)
	dir := filepath.Join(r.music, folder)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "03. Breakwater.flac"), flacHeader(24, 96000), 0o644); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(t.TempDir(), "runs")
	rep, _, _, err := r.download("flaky", album, FormatALAC, "FAKE_STATE="+state, "FAKE_FAILS=1")
	if err != nil {
		t.Fatal(err)
	}
	// Song 1 came in the first run, song 2 in the second; song 3 was there
	// before.
	if d := rep.done[1]; d.Err != nil || d.Skipped {
		t.Fatalf("song 1: %+v", d)
	}
	if d := rep.done[2]; d.Err != nil || d.Skipped || d.Path != folder+"/02. Undertow.flac" {
		t.Fatalf("song 2: %+v", d)
	}
	if d := rep.done[3]; d.Err != nil || !d.Skipped {
		t.Fatalf("song 3: %+v", d)
	}
	if !slices.Equal(rep.started, []int{1, 2, 3, 1, 2, 3}) {
		t.Fatalf("started %v", rep.started)
	}
	log := strings.Join(rep.lines, "\n")
	// The failure printed after a bar reaches the log, though that bar came
	// right after the one before. The redraw before it stays out.
	for _, want := range []string{
		"Decrypting...  20%  (2.0/9.9 MB, 5.0 MB/s) " + keyRefused,
		"A song failed, so amdl runs once more: " + keyRefused,
	} {
		if !strings.Contains(log, want) {
			t.Fatalf("log lacks %q:\n%s", want, log)
		}
	}
	if strings.Contains(log, "Decrypting...  10%") {
		t.Fatalf("log kept a redraw:\n%s", log)
	}

	// A song that fails twice fails the download, after two runs.
	_ = os.Remove(filepath.Join(dir, "02. Undertow.flac"))
	_ = os.Remove(state)
	_, _, _, err = r.download("flaky", album, FormatALAC, "FAKE_STATE="+state, "FAKE_FAILS=2")
	var ee *amdl.ExitError
	if !errors.As(err, &ee) || err.Error() != "amdl exited with code 1: "+keyRefused {
		t.Fatalf("%v", err)
	}
	if b, _ := os.ReadFile(state); string(b) != "2" {
		t.Fatalf("%s runs", b)
	}
}

func TestDownloadFailure(t *testing.T) {
	r := newRig(t)
	rep, _, _, err := r.download("fail", album, FormatALAC)
	var ee *amdl.ExitError
	if !errors.As(err, &ee) || err.Error() != "amdl exited with code 1: Failed to run v4: key retrieval failed" {
		t.Fatalf("%v", err)
	}
	if len(rep.done) != 0 {
		t.Fatalf("%+v", rep.done)
	}

	// A run that writes nothing fails, with each song's reason.
	rep, _, _, err = r.download("none", album, FormatAAC)
	if err == nil || err.Error() != "amdl wrote no songs: Unavailable" {
		t.Fatalf("%v", err)
	}
	if len(rep.done) != 3 || rep.done[3].Err == nil {
		t.Fatalf("%+v", rep.done)
	}

	// amdl that stops before its list fails the download.
	_, _, _, err = r.download("early", album, FormatALAC)
	if err == nil || !strings.Contains(err.Error(), "amdl stopped before it listed its songs") {
		t.Fatalf("%v", err)
	}

	if _, _, _, err := r.download("album", album, "applemusic:flac"); err == nil || !strings.Contains(err.Error(), "unknown format") {
		t.Fatalf("%v", err)
	}
	// A playlist of music videos alone is refused before amdl runs.
	_, _, args, err := r.download("album", source.Ref{Source: ID, Kind: source.KindPlaylist, ID: "pl.videos"}, FormatALAC)
	if err == nil || err.Error() != "Videos has no songs; music videos are not supported yet" || args != "" {
		t.Fatalf("%v, ran %q", err, args)
	}
	if _, _, _, err := r.download("album", source.Ref{Source: ID, Kind: source.KindArtist, ID: "111"}, FormatALAC); err == nil {
		t.Fatal("an artist was downloaded")
	}
}

func TestMatchSongs(t *testing.T) {
	tracks := []source.Track{{Position: 1, Title: "Intro"}, {Position: 2, Title: "Song"}, {Position: 3, Title: "Intro"}, {Position: 4, Title: "Outro"}}
	assigned := map[int]bool{}
	cursor := 0
	var got []int
	for _, song := range []string{"Intro", "intro ", "Outro", "Unknown"} {
		p := match(tracks, assigned, &cursor, song)
		if p > 0 {
			assigned[p] = true
		}
		got = append(got, p)
	}
	if !slices.Equal(got, []int{1, 3, 4, 0}) {
		t.Fatalf("%v", got)
	}
}

func TestFlacInfo(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.flac")
	if err := os.WriteFile(p, flacHeader(16, 44100), 0o644); err != nil {
		t.Fatal(err)
	}
	if bits, rate, ok := flacInfo(p); !ok || bits != 16 || rate != 44100 {
		t.Fatalf("%d %d %v", bits, rate, ok)
	}
	if q, size := fileQuality(p, FormatALAC); q.Short() != "FLAC 16/44.1" || q.HiRes || size != 42 {
		t.Fatalf("%+v %d", q, size)
	}
	m4a := filepath.Join(dir, "a.m4a")
	_ = os.WriteFile(m4a, []byte("not flac"), 0o644)
	if q, _ := fileQuality(m4a, FormatAtmos); q.Codec != "ec-3" || !q.Atmos {
		t.Fatalf("%+v", q)
	}
	if q, _ := fileQuality(filepath.Join(dir, "bad.flac"), FormatALAC); q != (source.Quality{Codec: "flac"}) {
		t.Fatalf("%+v", q)
	}
}
