package library

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/tikhonp/shelf/internal/modules/library/tagio"
	"github.com/tikhonp/shelf/internal/platform/config"
	"github.com/tikhonp/shelf/internal/platform/db"
	"github.com/tikhonp/shelf/internal/platform/db/dbtest"
	"github.com/tikhonp/shelf/internal/platform/hub"
	"github.com/tikhonp/shelf/internal/platform/jobs"
	"github.com/tikhonp/shelf/internal/platform/module"
	"github.com/tikhonp/shelf/internal/platform/testwait"
	"github.com/tikhonp/shelf/internal/platform/web"
	"go.senan.xyz/taglib"
)

var bg = context.Background()

// rig is the module on a migrated database and a music folder of its own.
type rig struct {
	t      *testing.T
	root   string
	data   string
	dbPath string
	m      *Module
	hub    *hub.Hub
	// sys runs the module's jobs once startJobs is called.
	sys      *jobs.System
	stopJobs func()
}

func newRig(t *testing.T) *rig {
	t.Helper()
	r := &rig{t: t, root: t.TempDir(), data: t.TempDir(), dbPath: filepath.Join(t.TempDir(), "shelf.db"), hub: hub.New()}
	dbtest.Seed(t, r.dbPath, dbtest.Platform, New())
	r.m = r.module(nil)
	return r
}

// module builds the module on the rig's database, as a start does.
func (r *rig) module(c ConfigReader) *Module {
	r.t.Helper()
	m := New()
	if c != nil {
		m.SetConfig(c)
	}
	d, err := db.Open(r.dbPath)
	if err != nil {
		r.t.Fatal(err)
	}
	r.t.Cleanup(func() { _ = d.Close() })
	r.sys = jobs.New(d, time.UTC, dbtest.Discard)
	r.sys.Poll, r.sys.Grace = 5*time.Millisecond, 200*time.Millisecond
	if err := m.Init(module.Deps{Cfg: &config.Config{MusicDir: r.root, DataDir: r.data}, Log: dbtest.Discard, DB: d, Hub: r.hub, Jobs: r.sys}); err != nil {
		r.t.Fatal(err)
	}
	if err := r.sys.Register(Name, m.JobTypes()...); err != nil {
		r.t.Fatal(err)
	}
	if err := m.ix.load(bg); err != nil {
		r.t.Fatal(err)
	}
	return m
}

// add copies a fixture to rel under the music folder and tags it.
func (r *rig) add(rel, fixture string, tags map[string][]string) string {
	r.t.Helper()
	p := filepath.Join(r.root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		r.t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join("tagio", "testdata", fixture))
	if err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(p, b, 0o644); err != nil {
		r.t.Fatal(err)
	}
	if tags != nil {
		if err := taglib.WriteTags(p, tags, 0); err != nil {
			r.t.Fatal(err)
		}
	}
	return p
}

func (r *rig) write(rel string, b []byte) {
	r.t.Helper()
	p := filepath.Join(r.root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(p, b, 0o644); err != nil {
		r.t.Fatal(err)
	}
}

func (r *rig) walk() Status {
	r.t.Helper()
	if err := r.m.ix.walk(bg); err != nil {
		r.t.Fatal(err)
	}
	return r.m.ix.status()
}

func (r *rig) do(method, path string, hdr ...string) *httptest.ResponseRecorder {
	e := echo.New()
	e.HTTPErrorHandler = web.ErrorHandler
	r.m.Routes(e.Group("/api"))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, nil)
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	e.ServeHTTP(rec, req)
	return rec
}

func (r *rig) getJSON(path string, into any) {
	r.t.Helper()
	rec := r.do(http.MethodGet, path)
	if rec.Code != http.StatusOK {
		r.t.Fatalf("GET %s: %d %s", path, rec.Code, rec.Body)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), into); err != nil {
		r.t.Fatal(err)
	}
}

func (r *rig) albums(query string) listJSON {
	r.t.Helper()
	var l listJSON
	r.getJSON("/api/library/albums"+query, &l)
	return l
}

func blue(track string, over map[string][]string) map[string][]string {
	t := tagged(map[string][]string{"TRACKNUMBER": {track}, "TITLE": {"Track " + track}})
	for k, v := range over {
		t[k] = v
	}
	return t
}

func TestWalkIndexesTheLibrary(t *testing.T) {
	r := newRig(t)
	r.add("folk/Blue/01.flac", "track.flac", blue("1", nil))
	r.add("folk/Blue/02.flac", "track.flac", blue("2", nil))
	r.write("folk/Blue/cover.jpg", jpegOf(t, 10, 10))
	r.add("apple-music/Album/01.m4a", "alac.m4a", map[string][]string{"TITLE": {"T"}, "ARTIST": {"A"}, "ALBUM": {"Album"}, "TRACKNUMBER": {"1"}})
	r.add("folk/.hidden/01.flac", "track.flac", blue("1", nil))
	r.add("folk/Blue/._01.flac", "track.flac", blue("1", nil))
	r.add("folk/@eaDir/01.flac", "track.flac", blue("1", nil))
	r.add("skip/01.flac", "track.flac", blue("1", nil))
	r.write("skip/.ndignore", []byte("# nothing but a comment\n"))
	r.add("...And Justice/01.flac", "track.flac", map[string][]string{"TITLE": {"One"}, "ALBUM": {"...And Justice"}})
	r.write("apple-music/Album/artwork.mp4", []byte("not audio"))
	r.write("broken/01.flac", []byte("not a flac file"))

	st := r.walk()
	if !st.Ready || st.Indexing || st.Albums != 3 || st.Tracks != 4 || st.Unreadable != 1 || st.Read != 5 || st.Rules != "default" {
		t.Fatalf("status %+v", st)
	}
	l := r.albums("?sort=album")
	if l.Total != 3 || l.Albums[0].Title != "...And Justice" || l.Albums[1].Title != "Album" || l.Albums[2].Title != "Blue" {
		t.Fatalf("albums %+v", l)
	}
	b := l.Albums[2]
	if b.Folder != "folk/Blue" || b.Tracks != 2 || b.Artist != "Joni Mitchell" || b.Year != 1971 || b.Lamp ||
		b.Quality != "FLAC 16/44.1" || b.NavidromeID == "" {
		t.Errorf("Blue %+v", b)
	}
	a := l.Albums[1]
	if !a.Lamp || !slices.ContainsFunc(a.Issues, func(i Issue) bool { return i.Kind == NoCover }) {
		t.Errorf("Album %+v", a)
	}

	// Nothing changed, so nothing is read, and the album keeps its row.
	if st := r.walk(); st.Read != 0 || st.Albums != 3 {
		t.Errorf("second walk %+v", st)
	}
	// A retag is read again; a removed file leaves.
	r.add("folk/Blue/02.flac", "track.flac", blue("2", map[string][]string{"LABEL": {"Asylum"}}))
	if err := os.Remove(filepath.Join(r.root, "apple-music/Album/01.m4a")); err != nil {
		t.Fatal(err)
	}
	st = r.walk()
	if st.Read != 1 || st.Albums != 2 || st.Tracks != 3 {
		t.Errorf("after a retag and a removal %+v", st)
	}
	l = r.albums("?filter=values_differ")
	if l.Total != 1 || l.Albums[0].ID != b.ID {
		t.Errorf("values_differ %+v, want Blue under its old ID %d", l, b.ID)
	}

	// A restart loads the index and reads nothing.
	r.m = r.module(nil)
	if st := r.m.ix.status(); !st.Ready || st.Tracks != 3 {
		t.Errorf("loaded %+v", st)
	}
	if st := r.walk(); st.Read != 0 {
		t.Errorf("walk after a restart read %d", st.Read)
	}
}

func TestNavidromeSettingsChangeTheIndex(t *testing.T) {
	r := newRig(t)
	r.add("folk/Blue/01.flac", "track.flac", blue("1", nil))
	r.add("folk/Blue/02.flac", "track.flac", blue("2", map[string][]string{"ALBUMARTIST": {"Joni"}}))
	if st := r.walk(); st.Albums != 2 {
		t.Fatalf("by album artist %+v", st)
	}
	// One album per folder, as the owner may set PID.Album.
	cfg := tagio.NavidromeConfig{AlbumPID: "folder"}
	var fail error
	r.m = r.module(func(context.Context) (tagio.NavidromeConfig, error) { return cfg, fail })
	if st := r.walk(); st.Albums != 1 || st.Read != 2 || st.Rules != "navidrome" {
		t.Errorf("by folder %+v", st)
	}
	// While Navidrome is down, the settings last read stay in force.
	fail = errors.New("down")
	if st := r.walk(); st.Albums != 1 || st.Read != 0 || st.Rules != "cached" {
		t.Errorf("Navidrome down %+v", st)
	}
}

// A format the browser refuses comes through ffmpeg as MP3, and a file ffmpeg
// can't read is refused before any audio.
func TestAudioTranscode(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg is not installed")
	}
	r := newRig(t)
	r.m.FFmpeg = "ffmpeg"
	r.add("qobuz/Blue/01.wma", "track.wma", blue("1", nil))
	r.walk()
	var l listJSON
	r.getJSON("/api/library/albums", &l)
	var d detailJSON
	r.getJSON("/api/library/albums/"+itoa(l.Albums[0].ID), &d)
	url := "/api/library/tracks/" + itoa(d.Tracks[0].ID) + "/audio?transcode=1"
	rec := r.do(http.MethodGet, url)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "audio/mpeg" || rec.Body.Len() == 0 {
		t.Fatalf("transcode: %d %q, %d bytes", rec.Code, rec.Header().Get("Content-Type"), rec.Body.Len())
	}
	if h := rec.Header().Get("Accept-Ranges"); h != "" {
		t.Errorf("a stream with no length offers ranges: %q", h)
	}
	r.write("qobuz/Blue/01.wma", []byte("not audio"))
	if rec := r.do(http.MethodGet, url); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("a file ffmpeg can't read: %d", rec.Code)
	}
}

func TestKeysNavidromeReads(t *testing.T) {
	r := newRig(t)
	var got struct {
		Keys []string `json:"keys"`
	}
	r.getJSON("/api/library/keys", &got)
	for _, k := range []string{"TITLE", "TRACKNUMBER", "RELEASEDATE"} {
		if !slices.Contains(got.Keys, k) {
			t.Errorf("%s is missing from %v", k, got.Keys)
		}
	}
	if slices.Contains(got.Keys, "STUDIO") {
		t.Error("STUDIO is listed, and Navidrome ignores it")
	}
}

func TestListFiltersSortsAndSearches(t *testing.T) {
	r := newRig(t)
	r.add("qobuz/Blue/01.flac", "track.flac", blue("1", nil))
	r.write("qobuz/Blue/cover.jpg", jpegOf(t, 10, 10))
	r.add("qobuz-old/Hejira/01.flac", "track.flac", tagged(map[string][]string{"ALBUM": {"Hejira"}, "DATE": {"1976"}, "RELEASEDATE": nil}))
	r.add("jazz/Kind of Blue/01.flac", "track.flac", tagged(map[string][]string{"ALBUM": {"Kind of Blue"}, "ARTIST": {"Miles Davis"},
		"ALBUMARTIST": {"Miles Davis"}, "ALBUMARTISTS": nil, "DATE": {"1959"}, "MUSICBRAINZ_ALBUMID": {"8d2338a8-5e0e-4e3b-9f3e-6b1e4b8f6c11"}}))
	r.walk()
	titles := func(l listJSON) []string {
		var out []string
		for _, a := range l.Albums {
			out = append(out, a.Title)
		}
		return out
	}
	cases := map[string][]string{
		"":                        {"Blue", "Hejira", "Kind of Blue"},
		"?sort=year":              {"Kind of Blue", "Blue", "Hejira"},
		"?sort=year&order=desc":   {"Hejira", "Blue", "Kind of Blue"},
		"?q=blue":                 {"Blue", "Kind of Blue"},
		"?q=miles%20BLUE":         {"Kind of Blue"},
		"?q=jazz":                 {"Kind of Blue"},
		"?folder=qobuz":           {"Blue"},
		"?filter=issues":          {"Hejira", "Kind of Blue"},
		"?filter=no_releasedate":  {"Hejira"},
		"?filter=no_mbid":         {"Blue", "Hejira"},
		"?filter=guide":           {"Kind of Blue"},
		"?q=100%25":               nil,
		"?limit=1&offset=1":       {"Hejira"},
		"?filter=all&sort=artist": {"Blue", "Hejira", "Kind of Blue"},
	}
	for q, want := range cases {
		if got := titles(r.albums(q)); !slices.Equal(got, want) {
			t.Errorf("%s: %q, want %q", q, got, want)
		}
	}
	for _, q := range []string{"?filter=nope", "?sort=size", "?order=up", "?limit=0", "?limit=501", "?offset=-1"} {
		if rec := r.do(http.MethodGet, "/api/library/albums"+q); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d", q, rec.Code)
		}
	}
	var f filtersJSON
	r.getJSON("/api/library/filters", &f)
	if f.Albums != 3 || f.Issues != 2 || f.NoMBID != 2 || len(f.Kinds) != len(Kinds) ||
		!slices.Equal(f.Folders, []countJSON{{"jazz", 1}, {"qobuz", 1}, {"qobuz-old", 1}}) {
		t.Errorf("filters %+v", f)
	}
}

func TestOpenedAlbumIsReadAgain(t *testing.T) {
	r := newRig(t)
	p := r.add("folk/Blue/01.flac", "track.flac", blue("1", nil))
	r.add("folk/Blue/02.flac", "track.flac", blue("2", nil))
	r.write("folk/Blue/cover.jpg", jpegOf(t, 10, 10))
	r.walk()
	id := r.albums("").Albums[0].ID
	var d detailJSON
	r.getJSON("/api/library/albums/"+itoa(id), &d)
	if d.Stale || len(d.Tracks) != 2 || d.Tracks[0].Title != "Track 1" || d.Tracks[0].Revision == "" || d.Lamp {
		t.Fatalf("detail %+v", d)
	}
	tr := d.Tracks[0]
	if tr.Format != "flac" || !slices.ContainsFunc(tr.Standard, func(k keyJSON) bool {
		return k.Key == "ALBUMARTIST" && slices.Equal(k.Fields, []string{"albumartist"})
	}) {
		t.Errorf("track %+v", tr)
	}

	// After another tool retags a track, the album shows it now and asks for a walk.
	if err := taglib.WriteTags(p, map[string][]string{"TITLE": {"Retitled"}, "ALBUM ARTIST": {"Joni"}}, 0); err != nil {
		t.Fatal(err)
	}
	r.getJSON("/api/library/albums/"+itoa(id), &d)
	if !d.Stale || d.Tracks[0].Title != "Retitled" || d.Tracks[0].Revision == tr.Revision ||
		!slices.ContainsFunc(d.Issues, func(i Issue) bool { return i.Kind == TagsDisagree }) {
		t.Errorf("after a retag %+v", d)
	}
	select {
	case <-r.m.kick:
	default:
		t.Error("no walk asked for")
	}

	for _, path := range []string{"/api/library/albums/999", "/api/library/albums/x", "/api/library/albums/-1"} {
		if rec := r.do(http.MethodGet, path); rec.Code != http.StatusNotFound {
			t.Errorf("%s: %d", path, rec.Code)
		}
	}
}

func TestOpenedAlbumSeesItsSplit(t *testing.T) {
	r := newRig(t)
	r.add("folk/Blue/CD 1/01.flac", "track.flac", blue("1", nil))
	r.add("folk/Blue/CD 2/01.flac", "track.flac", blue("1", map[string][]string{"ALBUMARTIST": {"Joni"}, "DISCNUMBER": {"2"}}))
	r.walk()
	for _, a := range r.albums("").Albums {
		var d detailJSON
		r.getJSON("/api/library/albums/"+itoa(a.ID), &d)
		if !slices.ContainsFunc(d.Issues, func(i Issue) bool { return i.Kind == SplitAlbum }) || d.Stale {
			t.Errorf("%s: %+v", a.Artist, d.Issues)
		}
	}
}

func TestAudio(t *testing.T) {
	r := newRig(t)
	p := r.add("folk/Blue/01.flac", "track.flac", blue("1", nil))
	r.walk()
	var d detailJSON
	r.getJSON("/api/library/albums/"+itoa(r.albums("").Albums[0].ID), &d)
	url := "/api/library/tracks/" + itoa(d.Tracks[0].ID) + "/audio"
	want, _ := os.ReadFile(p)
	rec := r.do(http.MethodGet, url)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "audio/flac" || rec.Body.Len() != len(want) {
		t.Fatalf("audio: %d %q %d bytes", rec.Code, rec.Header().Get("Content-Type"), rec.Body.Len())
	}
	rec = r.do(http.MethodGet, url, "Range", "bytes=0-3")
	if rec.Code != http.StatusPartialContent || rec.Body.String() != "fLaC" {
		t.Errorf("range: %d %q", rec.Code, rec.Body)
	}
	// A file swapped for a symlink is not served.
	if err := os.Rename(p, p+".moved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(p+".moved", p); err != nil {
		t.Fatal(err)
	}
	if rec := r.do(http.MethodGet, url); rec.Code != http.StatusNotFound {
		t.Errorf("symlink: %d", rec.Code)
	}
	if rec := r.do(http.MethodGet, "/api/library/tracks/999/audio"); rec.Code != http.StatusNotFound {
		t.Errorf("unknown track: %d", rec.Code)
	}
}

func TestRunSweepsWalksAndHearsDownloads(t *testing.T) {
	r := newRig(t)
	tmp := filepath.Join(r.root, "qobuz", "Album", ".shelf-tmp-0123456789abcdef")
	r.write("qobuz/Album/.shelf-tmp-0123456789abcdef", nil)
	r.add("qobuz/Album/01.flac", "track.flac", blue("1", nil))
	events, stop := r.hub.Subscribe(Topic)
	defer stop()
	ctx, cancel := context.WithCancel(bg)
	done := make(chan error, 1)
	r.m.Every = time.Hour
	go func() { done <- r.m.Run(ctx) }()
	ready := func() Status {
		t.Helper()
		deadline := time.After(testwait.For(10 * time.Second))
		for {
			select {
			case e := <-events:
				if st := e.Data.(Status); !st.Indexing && st.LastIndex != nil {
					return st
				}
			case <-deadline:
				t.Fatal("no walk")
			}
		}
	}
	if st := ready(); st.Tracks != 1 {
		t.Errorf("first walk %+v", st)
	}
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Error("the temp file is still there")
	}
	r.add("qobuz/Album/02.flac", "track.flac", blue("2", nil))
	if err := r.m.RequestScan(bg); err != nil {
		t.Fatal(err)
	}
	if st := ready(); st.Tracks != 2 {
		t.Errorf("after a download %+v", st)
	}
	if rec := r.do(http.MethodPost, "/api/library/index"); rec.Code != http.StatusAccepted {
		t.Errorf("POST index: %d", rec.Code)
	}
	ready()
	cancel()
	if err := <-done; err != nil {
		t.Errorf("Run: %v", err)
	}

	// A music folder that isn't there is a walk's error, not Run's.
	r = newRig(t)
	r.root = filepath.Join(r.root, "missing")
	r.m = r.module(nil)
	if err := r.m.ix.walk(bg); err == nil || r.m.ix.status().Error == "" {
		t.Errorf("walk of a missing folder: %v", err)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func TestWalkWithNothingNewWritesNothing(t *testing.T) {
	r := newRig(t)
	r.add("folk/Blue/01.flac", "track.flac", blue("1", nil))
	r.walk()
	d, err := db.Open(r.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	if _, err := d.W.Exec(`UPDATE library_albums SET title = 'marked'`); err != nil {
		t.Fatal(err)
	}
	r.walk()
	if got := r.albums("").Albums[0].Title; got != "marked" {
		t.Errorf("a walk with nothing new rewrote the albums: %q", got)
	}
	r.add("folk/Blue/02.flac", "track.flac", blue("2", nil))
	r.walk()
	if got := r.albums("").Albums[0].Title; got != "Blue" {
		t.Errorf("a new track left the album as %q", got)
	}
}
