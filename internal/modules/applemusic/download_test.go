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
	mu      sync.Mutex
	planned []source.Track
	started []int
	done    map[int]source.TrackResult
	lines   []string
}

func newReporter() *reporter { return &reporter{done: map[int]source.TrackResult{}} }

func (r *reporter) Plan(t []source.Track) { r.mu.Lock(); r.planned = t; r.mu.Unlock() }
func (r *reporter) Start(p int)           { r.mu.Lock(); r.started = append(r.started, p); r.mu.Unlock() }
func (r *reporter) Progress(int, int64)   {}
func (r *reporter) Done(p int, res source.TrackResult) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.done[p] = res
}
func (r *reporter) Log() source.Logger { return logger{r} }

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

func (r *rig) download(mode string, ref source.Ref, format string) (*reporter, source.DownloadResult, string, error) {
	r.t.Helper()
	args := filepath.Join(r.t.TempDir(), "args")
	r.s.runner.Env = []string{"SHELF_FAKE_AMDL=" + mode, "FAKE_ARGS=" + args}
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
	for _, want := range []string{"Running amdl --json for https://music.apple.com/ru/album/1440000000", "Track 1 of 3: songs", "100% |"} {
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
