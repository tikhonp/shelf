package catalog_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tikhonp/shelf/internal/modules/catalog"
	"github.com/tikhonp/shelf/internal/modules/downloads"
	"github.com/tikhonp/shelf/internal/modules/fakesource"
	"github.com/tikhonp/shelf/internal/platform"
	"github.com/tikhonp/shelf/internal/platform/config"
	"github.com/tikhonp/shelf/internal/source"
)

var bg = context.Background()

// app is the platform with the modules main enables for the fake source.
type app struct {
	t   *testing.T
	a   *platform.App
	dl  *downloads.Module
	src *fakesource.Module
}

func newApp(t *testing.T) *app {
	t.Helper()
	dir := t.TempDir()
	cfg, err := config.Load(func(k string) (string, bool) {
		switch k {
		case "SHELF_DATA_DIR":
			return filepath.Join(dir, "data"), true
		case "SHELF_MUSIC_DIR":
			return filepath.Join(dir, "music"), true
		}
		return "", false
	})
	if err != nil {
		t.Fatal(err)
	}
	dl, src := downloads.New(), fakesource.New()
	off := fakesource.NewSource("dark", "Dark Music")
	off.Off = true
	a, err := platform.Open(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), dl, catalog.New(dl), src, sources{off})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	if err := a.Migrate(bg); err != nil {
		t.Fatal(err)
	}
	return &app{t: t, a: a, dl: dl, src: src}
}

// sources is a module with extra sources.
type sources struct{ s *fakesource.Source }

func (sources) Name() string               { return "extra" }
func (sources) Migrations() fs.FS          { return nil }
func (x sources) Sources() []source.Source { return []source.Source{x.s} }

func (x *app) do(method, path string, body any) *httptest.ResponseRecorder {
	x.t.Helper()
	var b bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&b).Encode(body)
	}
	req := httptest.NewRequest(method, path, &b)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	x.a.HTTP().ServeHTTP(rec, req)
	return rec
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("%d %s: %v", rec.Code, rec.Body, err)
	}
	return v
}

func TestSourcesCapabilities(t *testing.T) {
	x := newApp(t)
	rec := x.do(http.MethodGet, "/api/sources", nil)
	list := decode[[]catalog.SourceJSON](t, rec)
	if rec.Code != http.StatusOK || len(list) != 2 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	f := list[0]
	if f.ID != "fake" || f.Name != "Fake" || !f.Enabled || !f.Capabilities.Links || !f.Capabilities.Resolve || !f.Capabilities.Download ||
		len(f.Capabilities.Search) != 4 || !f.Health.OK {
		t.Fatalf("%+v", f)
	}
	if len(f.Formats) != 2 || f.Formats[0].ID != "fake:flac" || !f.Formats[0].Default || f.Formats[1].ID != "fake:mp3" {
		t.Fatalf("formats %+v", f.Formats)
	}
	d := list[1]
	if d.ID != "dark" || d.Enabled || d.Health.OK || d.Health.Message != "Dark Music is not set up in Shelf" {
		t.Fatalf("%+v", d)
	}
}

func TestSourceErrors(t *testing.T) {
	x := newApp(t)
	search := "/api/search?source=fake&type=album&q=northern"
	// A reason people may read reaches them. Anything else stays in the log.
	x.src.Source().Fail = &source.Unavailable{Reason: "token expired"}
	rec := x.do(http.MethodGet, search, nil)
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "Fake: token expired") {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	x.src.Source().Fail = errors.New("GET https://fake.test/?token=secret: refused")
	rec = x.do(http.MethodGet, search, nil)
	if rec.Code != http.StatusBadGateway || strings.Contains(rec.Body.String(), "secret") {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
}

func TestSearchBadType(t *testing.T) {
	x := newApp(t)
	for path, want := range map[string]int{
		"/api/search?source=fake&type=album&q=northern":    http.StatusOK,
		"/api/search?source=fake&type=albums&q=northern":   http.StatusBadRequest,
		"/api/search?source=fake&type=label&q=northern":    http.StatusBadRequest,
		"/api/search?source=fake&type=album&q=":            http.StatusBadRequest,
		"/api/search?type=album&q=x":                       http.StatusBadRequest,
		"/api/search?source=fake&type=album&q=x&limit=0":   http.StatusBadRequest,
		"/api/search?source=fake&type=album&q=x&offset=-1": http.StatusBadRequest,
		"/api/search?source=nope&type=album&q=x":           http.StatusUnprocessableEntity,
		"/api/search?source=dark&type=album&q=x":           http.StatusUnprocessableEntity,
	} {
		if rec := x.do(http.MethodGet, path, nil); rec.Code != want {
			t.Errorf("%s: %d %s", path, rec.Code, rec.Body)
		}
	}
	rec := x.do(http.MethodGet, "/api/search?source=fake&type=albums&q=x", nil)
	if !strings.Contains(rec.Body.String(), "type must be one of album, track, artist, playlist") {
		t.Fatalf("%s", rec.Body)
	}
	page := decode[source.Page](t, x.do(http.MethodGet, "/api/search?source=fake&type=album&q=northern&limit=1", nil))
	if page.Total != 2 || len(page.Items) != 1 || page.Items[0].Ref.ID != "album-1" {
		t.Fatalf("%+v", page)
	}
}

func TestItemsCarryStatus(t *testing.T) {
	x := newApp(t)
	svc := x.dl.Service()
	q, err := svc.Queue(bg, downloads.QueueRequest{Ref: source.Ref{Source: "fake", Kind: source.KindAlbum, ID: "album-2"}, Origin: "web"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := x.a.DB.W.Exec(`INSERT INTO downloaded_items (source, kind, item_id, path, at) VALUES ('fake', 'album', 'album-1', 'p', '2026-01-01T00:00:00.000Z'),
		('fake', 'track', 'album-3-t2', 'p', '2026-01-01T00:00:00.000Z')`); err != nil {
		t.Fatal(err)
	}

	it := decode[source.Item](t, x.do(http.MethodGet, "/api/items/fake/artist/artist-1", nil))
	secs := it.Sections[0].Items
	if it.Status != nil || len(secs) != 2 || secs[0].Status == nil || secs[0].Status.State != "in_library" ||
		secs[1].Status == nil || secs[1].Status.State != "queued" || secs[1].Status.DownloadID != q.ID {
		t.Fatalf("%+v", secs)
	}
	al := decode[source.Item](t, x.do(http.MethodGet, "/api/items/fake/album/album-3", nil))
	if al.Status != nil || len(al.Tracks) != 3 || al.Tracks[0].Status != nil || al.Tracks[1].Status == nil || al.Tracks[1].Status.State != "in_library" {
		t.Fatalf("%+v", al.Tracks)
	}
	// Search results carry it too.
	page := decode[source.Page](t, x.do(http.MethodGet, "/api/search?source=fake&type=album&q=northern", nil))
	if page.Items[0].Status == nil || page.Items[0].Status.State != "in_library" || page.Items[1].Status.State != "queued" {
		t.Fatalf("%+v", page.Items)
	}
	// A pasted link resolves to the same answer.
	rec := x.do(http.MethodPost, "/api/resolve", map[string]string{"url": "https://fake.test/video/v-1"})
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), source.RefusedLinkText) {
		t.Fatalf("refused link: %d %s", rec.Code, rec.Body)
	}
	rec = x.do(http.MethodPost, "/api/resolve", map[string]string{"url": "https://fake.test/album/album-2"})
	if r := decode[source.Item](t, rec); r.Ref.ID != "album-2" || r.Status == nil || r.Status.State != "queued" {
		t.Fatalf("%+v", r)
	}
	for _, tc := range []struct {
		method, path string
		body         any
		want         int
	}{
		{http.MethodPost, "/api/resolve", map[string]string{"url": "https://example.com/x"}, http.StatusUnprocessableEntity},
		{http.MethodPost, "/api/resolve", map[string]string{"url": "https://dark.test/album/1"}, http.StatusUnprocessableEntity},
		{http.MethodPost, "/api/resolve", map[string]string{}, http.StatusBadRequest},
		{http.MethodGet, "/api/items/fake/album/missing-1", nil, http.StatusNotFound},
		{http.MethodGet, "/api/items/fake/song/1", nil, http.StatusNotFound},
		{http.MethodGet, "/api/items/nope/album/1", nil, http.StatusNotFound},
	} {
		if rec := x.do(tc.method, tc.path, tc.body); rec.Code != tc.want {
			t.Errorf("%s %s: %d %s", tc.method, tc.path, rec.Code, rec.Body)
		}
	}
}
