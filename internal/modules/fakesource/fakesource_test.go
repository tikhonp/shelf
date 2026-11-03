package fakesource_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/tikhonp/shelf/internal/modules/fakesource"
	"github.com/tikhonp/shelf/internal/source"
)

var bg = context.Background()

func TestParseLink(t *testing.T) {
	s := fakesource.NewSource("fake", "Fake")
	ref, ok := s.ParseLink("https://fake.test/album/album-1")
	if !ok || ref != (source.Ref{Source: "fake", Kind: source.KindAlbum, ID: "album-1"}) {
		t.Fatalf("%+v %v", ref, ok)
	}
	for _, bad := range []string{"https://other.test/album/1", "https://fake.test/song/1", "https://fake.test/album/", "https://fake.test/album/a/b"} {
		if _, ok := s.ParseLink(bad); ok {
			t.Errorf("%s claimed", bad)
		}
	}
	// Videos and stations are refused, not claimed.
	for _, link := range []string{"https://fake.test/video/v-1", "https://fake.test/station/s-1"} {
		if _, ok := s.ParseLink(link); ok || !s.RefuseLink(link) {
			t.Errorf("%s: claimed %v, refused %v", link, ok, s.RefuseLink(link))
		}
	}
	for _, link := range []string{"https://fake.test/album/album-1", "https://other.test/video/v-1", "https://fake.test/video/"} {
		if s.RefuseLink(link) {
			t.Errorf("%s refused", link)
		}
	}
}

func TestSearchFiltersCannedItems(t *testing.T) {
	s := fakesource.NewSource("fake", "Fake")
	p, err := s.Search(bg, source.Query{Kind: source.KindAlbum, Text: "northern", Limit: 1})
	if err != nil || p.Total != 2 || len(p.Items) != 1 || p.Items[0].Title != "Low Tide Hymns" {
		t.Fatalf("%+v %v", p, err)
	}
	p, _ = s.Search(bg, source.Query{Kind: source.KindAlbum, Text: "northern", Limit: 1, Offset: 1})
	if len(p.Items) != 1 || p.Items[0].Title != "Glass Weather" {
		t.Fatalf("second page %+v", p)
	}
	if p, _ := s.Search(bg, source.Query{Kind: source.KindArtist, Text: "zzz"}); p.Total != 0 || p.Items == nil {
		t.Fatalf("no hits %+v", p)
	}
}

func TestResolveKinds(t *testing.T) {
	s := fakesource.NewSource("fake", "Fake")
	if it, _ := s.Item(bg, source.Ref{Source: "fake", Kind: source.KindAlbum, ID: "x-big"}); len(it.Tracks) != 40 {
		t.Fatalf("-big has %d tracks", len(it.Tracks))
	}
	if it, _ := s.Item(bg, source.Ref{Source: "fake", Kind: source.KindArtist, ID: "artist-1"}); len(it.Sections[0].Items) != 2 {
		t.Fatalf("artist-1: %+v", it.Sections)
	}
	if it, _ := s.Item(bg, source.Ref{Source: "fake", Kind: source.KindLabel, ID: "l-empty"}); len(it.Sections[0].Items) != 0 {
		t.Fatalf("-empty: %+v", it.Sections)
	}
	if _, err := s.Item(bg, source.Ref{Source: "fake", Kind: source.KindAlbum, ID: "missing-1"}); !errors.Is(err, source.ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
}

type rep struct {
	mu   sync.Mutex
	done map[int]source.TrackResult
}

func (r *rep) Plan([]source.Track) {}
func (r *rep) Start(int)           {}
func (r *rep) Progress(int, int64) {}
func (r *rep) Log() source.Logger  { return nopLog{} }
func (r *rep) Done(p int, res source.TrackResult) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.done[p] = res
}

type nopLog struct{}

func (nopLog) Debug(string, ...any) {}
func (nopLog) Info(string, ...any)  {}
func (nopLog) Warn(string, ...any)  {}
func (nopLog) Error(string, ...any) {}

func TestDownloadWritesAndSkips(t *testing.T) {
	s := fakesource.NewSource("fake", "Fake")
	music := t.TempDir()
	req := source.DownloadRequest{Ref: source.Ref{Source: "fake", Kind: source.KindAlbum, ID: "album-6-fail"}, Format: "fake:flac", MusicDir: music}
	r := &rep{done: map[int]source.TrackResult{}}
	res, err := s.Download(bg, req, r)
	if err != nil || len(res.Folders) != 1 || res.Folders[0] != filepath.Join("fake", "Field Recordings - Broken Signal") || len(res.Files) != 2 {
		t.Fatalf("%+v %v", res, err)
	}
	if r.done[2].Err == nil || r.done[1].Skipped || r.done[1].Path == "" {
		t.Fatalf("%+v", r.done)
	}
	fi, err := os.Stat(filepath.Join(music, r.done[1].Path))
	if err != nil || fi.Size() != fakesource.TrackSize || fi.Mode().Perm() != 0o644 {
		t.Fatalf("%v %v", fi, err)
	}
	r = &rep{done: map[int]source.TrackResult{}}
	if _, err := s.Download(bg, req, r); err != nil || !r.done[1].Skipped || !r.done[3].Skipped {
		t.Fatalf("second run: %+v %v", r.done, err)
	}
}
