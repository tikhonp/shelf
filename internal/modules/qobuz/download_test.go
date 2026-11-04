package qobuz

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/tikhonp/qobuz-dl-go/qobuzdltest"
	"github.com/tikhonp/shelf/internal/source"
)

const formatMax = "qobuz:27"

func TestFormats(t *testing.T) {
	r := newRig(t)
	got := r.s.Formats()
	want := []source.Format{
		{ID: "qobuz:27", Label: "Max", Description: "Qobuz 27, FLAC up to 24-bit 192 kHz", Default: true},
		{ID: "qobuz:7", Label: "24/96", Description: "Qobuz 7, FLAC up to 24-bit 96 kHz"},
		{ID: "qobuz:6", Label: "CD", Description: "Qobuz 6, FLAC 16-bit 44.1 kHz"},
		{ID: "qobuz:5", Label: "MP3 320", Description: "Qobuz 5, MP3 320 kbps"},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("%+v", got)
	}
	// SHELF_QOBUZ_QUALITY moves the default and not the order.
	r = newRig(t, func(r *rig) { r.cfg.Qobuz.Quality = 6 })
	if f, ok := source.DefaultFormat(r.s.Formats()); !ok || f.ID != "qobuz:6" || r.s.Formats()[0].ID != "qobuz:27" {
		t.Fatalf("%+v", r.s.Formats())
	}
}

func TestDownloadReports(t *testing.T) {
	r := newRig(t).up()
	rec, res, err := r.download(source.KindAlbum, qobuzdltest.AlbumMultiDisc, formatMax, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.plan) != 16 {
		t.Fatalf("planned %d tracks", len(rec.plan))
	}
	p := rec.plan[15]
	if p.Position != 16 || p.Disc != 2 || p.ID != "47683525" || p.ISRC != "GBN9Y1100043" || p.Artist != "Pink Floyd" || p.Title == "" {
		t.Errorf("plan %+v", p)
	}
	for pos := 1; pos <= 16; pos++ {
		if rec.starts[pos] == 0 || rec.written[pos] == 0 {
			t.Errorf("track %d: %d starts, %d bytes", pos, rec.starts[pos], rec.written[pos])
		}
		d := rec.last(pos)
		if d.Err != nil || d.Skipped || d.Size == 0 || d.Quality != (source.Quality{Codec: "flac", BitDepth: 24, SampleRate: 192000, HiRes: true}) {
			t.Errorf("track %d: %+v", pos, d)
		}
		if strings.Contains(d.Path, "[IN PROGRESS]") || !strings.HasPrefix(d.Path, res.Folders[0]+"/CD 0") || !r.exists(d.Path) {
			t.Errorf("track %d: path %q", pos, d.Path)
		}
	}
	if !slices.ContainsFunc(rec.lines, func(l string) bool { return strings.HasPrefix(l, "info Downloading: Ummagumma") }) {
		t.Errorf("log %q", rec.lines)
	}
	for _, l := range rec.lines {
		if strings.HasSuffix(l, "\n") {
			t.Errorf("line %q ends in a newline", l)
		}
	}
}

func TestDownloadResult(t *testing.T) {
	r := newRig(t).up()
	rec, res, err := r.download(source.KindAlbum, qobuzdltest.AlbumHiRes96, formatMax, false)
	if err != nil {
		t.Fatal(err)
	}
	folder := "qobuz/Norah Jones - Begin Again (2019) [24B-96kHz]"
	if !slices.Equal(res.Folders, []string{folder}) {
		t.Fatalf("folders %q", res.Folders)
	}
	// Tag writes keep out of the work folder and both names it ends with.
	if want := []string{"qobuz/[IN PROGRESS] " + folder[6:], folder, "qobuz/[INCOMPLETE] " + folder[6:]}; !slices.Equal(rec.working, want) {
		t.Fatalf("working %q", rec.working)
	}
	if len(res.Files) != 7 || res.Files[0] != folder+"/01 - My Heart Is Full.flac" {
		t.Fatalf("files %q", res.Files)
	}
	for _, f := range res.Files {
		if !r.exists(f) {
			t.Errorf("%s is missing", f)
		}
	}
	if res.Tool != "qobuz-dl-go v0.1.0 porting qobuz-dl 2.5.9" {
		t.Errorf("tool %q", res.Tool)
	}
	if res.TrackNoun != "" {
		t.Errorf("noun %q", res.TrackNoun)
	}

	_, res, err = r.download(source.KindTrack, qobuzdltest.TrackSingle, formatMax, false)
	if err != nil || len(res.Folders) != 1 || len(res.Files) != 1 || filepath.Dir(res.Files[0]) != res.Folders[0] {
		t.Fatalf("track: %+v, %v", res, err)
	}
}

func TestPlaylistDownloadWritesM3U8(t *testing.T) {
	r := newRig(t).up()
	rec, res, err := r.download(source.KindPlaylist, qobuzdltest.Playlist, formatMax, false)
	if err != nil {
		t.Fatal(err)
	}
	folder := "qobuz/Top 10 Operas"
	m3u := folder + "/Top 10 Operas.m3u8"
	if !slices.Equal(res.Folders, []string{folder}) || !slices.Contains(res.Files, m3u) || len(res.Files) != 11 {
		t.Fatalf("%+v", res)
	}
	body, err := os.ReadFile(filepath.Join(r.cfg.MusicDir, m3u))
	if err != nil || !strings.HasPrefix(string(body), "#EXTM3U") {
		t.Fatalf("%q, %v", body, err)
	}
	if len(rec.plan) != 10 || rec.plan[9].Position != 10 || rec.plan[0].Disc != 0 || rec.plan[0].Artist != "Véronique Gens" {
		t.Fatalf("plan %+v", rec.plan)
	}
	for pos := 1; pos <= 10; pos++ {
		if d := rec.last(pos); d.Err != nil || !strings.HasPrefix(d.Path, folder+"/") {
			t.Errorf("track %d: %+v", pos, d)
		}
	}
}

func TestDownloadUsesItsFormat(t *testing.T) {
	r := newRig(t).up()
	lastFormat := func() string {
		c := r.calls("track/getFileUrl")
		return c[len(c)-1].Query.Get("format_id")
	}
	rec, _, err := r.download(source.KindAlbum, qobuzdltest.AlbumHiRes96, "qobuz:6", false)
	if err != nil {
		t.Fatal(err)
	}
	if got := lastFormat(); got != "6" {
		t.Errorf("asked for %s", got)
	}
	if q := rec.last(1).Quality; q != (source.Quality{Codec: "flac", BitDepth: 16, SampleRate: 44100}) {
		t.Errorf("delivered %+v", q)
	}
	rec, _, err = r.download(source.KindTrack, qobuzdltest.TrackSingle, "qobuz:5", false)
	if err != nil {
		t.Fatal(err)
	}
	if got := lastFormat(); got != "5" {
		t.Errorf("asked for %s", got)
	}
	if q := rec.last(1).Quality; q != (source.Quality{Codec: "mp3"}) || q.Short() != "MP3" {
		t.Errorf("delivered %+v", q)
	}
	// A format asked for twice logs in once.
	logins := len(r.calls("user/get"))
	if _, _, err := r.download(source.KindAlbum, qobuzdltest.AlbumCD, "qobuz:6", false); err != nil {
		t.Fatal(err)
	}
	if n := len(r.calls("user/get")); n != logins {
		t.Errorf("%d logins after %d", n, logins)
	}
}

func TestDownloadRefuses(t *testing.T) {
	r := newRig(t).up()
	if _, _, err := r.download(source.KindAlbum, qobuzdltest.AlbumCD, "qobuz:28", false); err == nil {
		t.Error("an unknown format ran")
	}
	if _, _, err := r.download(source.KindArtist, qobuzdltest.ArtistAlbums, formatMax, false); err == nil {
		t.Error("an artist ran")
	}
	if _, _, err := r.download(source.KindPlaylist, qobuzdltest.PlaylistEmpty, formatMax, false); err == nil {
		t.Error("an empty playlist ran")
	}
	if _, _, err := r.download(source.KindAlbum, "nothing-here", formatMax, false); !errors.Is(err, source.ErrNotFound) {
		t.Errorf("unknown album: %v", err)
	}
}

// A job that reaches Download must run, whatever the history says by then.
// The row may be a copy's, queued next to this one, or its own from before a
// restart, and a skip would report no tracks.
func TestDownloadIgnoresHistoryForAlbumsAndTracks(t *testing.T) {
	r := newRig(t).up()
	album := ref(source.KindAlbum, qobuzdltest.AlbumHiRes96)
	if _, _, err := r.download(album.Kind, album.ID, formatMax, false); err != nil {
		t.Fatal(err)
	}
	if _, ok := r.history.row(album); !ok {
		t.Fatal("the album is not on record")
	}
	rec, res, err := r.download(album.Kind, album.ID, formatMax, false)
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(r.history.asked(), album) {
		t.Error("the download asked the history about the album")
	}
	if len(res.Folders) != 1 || len(res.Files) != 7 {
		t.Fatalf("%+v", res)
	}
	for pos := 1; pos <= 7; pos++ {
		if d := rec.last(pos); !d.Skipped || d.Path == "" || d.Size == 0 || d.Quality.BitDepth != 24 || d.Quality.SampleRate != 96000 {
			t.Errorf("track %d: %+v", pos, d)
		}
	}
}

func TestPlaylistSkipsRecordedTracks(t *testing.T) {
	r := newRig(t).up()
	p := r.item(source.KindPlaylist, qobuzdltest.Playlist)
	r.history.rows[ref(source.KindTrack, p.Tracks[1].ID)] = recorded()
	rec, res, err := r.download(source.KindPlaylist, qobuzdltest.Playlist, formatMax, false)
	if err != nil {
		t.Fatal(err)
	}
	if d := rec.last(2); !d.Skipped || d.Err != nil || d.Path != "" {
		t.Errorf("recorded track: %+v", d)
	}
	if d := rec.last(1); d.Skipped || d.Path == "" {
		t.Errorf("other track: %+v", d)
	}
	if len(res.Files) != 10 {
		t.Errorf("%d files", len(res.Files))
	}
}

// The tracks of the first run are on record by the second, so they come back
// skipped and without a path. They stay in the folder and the m3u8.
func TestResumedPlaylist(t *testing.T) {
	r := newRig(t, func(r *rig) { r.cfg.Qobuz.Workers = 1 }).up()
	ctx, cancel := context.WithCancel(bg)
	defer cancel()
	rec := newRecorder()
	var started atomic.Int32
	rec.onStart = func() {
		if started.Add(1) == 4 {
			cancel()
		}
	}
	req := r.request(source.KindPlaylist, qobuzdltest.Playlist, formatMax, false)
	if _, err := r.s.Download(ctx, req, rec); !errors.Is(err, context.Canceled) {
		t.Fatalf("first run: %v", err)
	}
	rec, res, err := r.download(source.KindPlaylist, qobuzdltest.Playlist, formatMax, false)
	if err != nil {
		t.Fatal(err)
	}
	if d := rec.last(1); !d.Skipped || d.Path != "" {
		t.Errorf("a track of the first run: %+v", d)
	}
	if d := rec.last(10); d.Skipped || d.Path == "" {
		t.Errorf("a track of the second run: %+v", d)
	}
	body, err := os.ReadFile(filepath.Join(r.cfg.MusicDir, res.Folders[0], "Top 10 Operas.m3u8"))
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(body), "#EXTINF"); n != 10 {
		t.Fatalf("%d tracks in the m3u8", n)
	}
}

func TestForcedDownloadPassesNoDatabase(t *testing.T) {
	r := newRig(t).up()
	p := r.item(source.KindPlaylist, qobuzdltest.Playlist)
	track := ref(source.KindTrack, p.Tracks[1].ID)
	r.history.rows[track] = recorded()
	rec, res, err := r.download(source.KindPlaylist, qobuzdltest.Playlist, formatMax, true)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(r.history.asked()); n != 0 {
		t.Errorf("asked the history %d times", n)
	}
	if d := rec.last(2); d.Skipped || d.Path == "" {
		t.Errorf("forced track: %+v", d)
	}
	if len(res.Files) != 11 {
		t.Errorf("%d files", len(res.Files))
	}
	if len(r.history.rows) != 1 {
		t.Errorf("%d rows on record", len(r.history.rows))
	}
}

func TestFailingHistoryStopsTheDownload(t *testing.T) {
	r := newRig(t).up()
	r.history.err = errors.New("disk full")
	_, _, err := r.download(source.KindPlaylist, qobuzdltest.Playlist, formatMax, false)
	if err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("%v", err)
	}
}

func TestFailedTrackGoesToTheReporter(t *testing.T) {
	r := newRig(t).up()
	a := r.item(source.KindAlbum, qobuzdltest.AlbumHiRes96)
	r.srv.Fail404(a.Tracks[2].ID)
	rec, res, err := r.download(source.KindAlbum, qobuzdltest.AlbumHiRes96, formatMax, false)
	if err != nil {
		t.Fatal(err)
	}
	if d := rec.last(3); d.Err == nil || d.Path != "" {
		t.Errorf("failed track: %+v", d)
	}
	if n := len(rec.dones[3]); n != 1 {
		t.Errorf("a failed track ended %d times", n)
	}
	if d := rec.last(1); d.Err != nil || !strings.Contains(d.Path, "[INCOMPLETE] ") || !r.exists(d.Path) {
		t.Errorf("good track: %+v", d)
	}
	if !strings.HasPrefix(res.Folders[0], "qobuz/[INCOMPLETE] ") || len(res.Files) != 6 {
		t.Errorf("%+v", res)
	}
	if _, ok := r.history.row(ref(source.KindAlbum, qobuzdltest.AlbumHiRes96)); ok {
		t.Error("a partial album went on record")
	}
}

func TestEveryTrackFailedIsAnError(t *testing.T) {
	r := newRig(t).up()
	for _, tr := range r.item(source.KindAlbum, qobuzdltest.AlbumHiRes96).Tracks {
		r.srv.Fail404(tr.ID)
	}
	rec, _, err := r.download(source.KindAlbum, qobuzdltest.AlbumHiRes96, formatMax, false)
	if err == nil {
		t.Fatal("no error")
	}
	if rec.last(7).Err == nil {
		t.Error("the tracks did not fail")
	}
}

func TestSkippedReleasesSayWhy(t *testing.T) {
	r := newRig(t, func(r *rig) { r.cfg.Qobuz.Fallback = false }).up()
	// A CD album can't meet 27 without fallback.
	if _, _, err := r.download(source.KindAlbum, qobuzdltest.AlbumCD, formatMax, false); !errors.Is(err, errNoFormat) {
		t.Errorf("quality: %v", err)
	}
	r.srv.EmptyAnswer(qobuzdltest.TrackSingle)
	if _, _, err := r.download(source.KindTrack, qobuzdltest.TrackSingle, formatMax, false); !errors.Is(err, errDemo) {
		t.Errorf("demo: %v", err)
	}
}

func TestCancelledDownload(t *testing.T) {
	r := newRig(t).up()
	ctx, cancel := context.WithCancel(bg)
	defer cancel()
	rec := newRecorder()
	rec.onStart = cancel
	res, err := r.s.Download(ctx, r.request(source.KindAlbum, qobuzdltest.AlbumHiRes96, formatMax, false), rec)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("%v", err)
	}
	if len(res.Folders) != 1 || !strings.HasPrefix(res.Folders[0], "qobuz/[INCOMPLETE] ") {
		t.Fatalf("%+v", res)
	}
	if r.s.Health(bg).OK != true {
		t.Error("a cancel changed the health")
	}
}
