package library

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/tikhonp/shelf/internal/source"
)

// folderSource is a source that writes into a folder of its own.
type folderSource struct{ id, name, folder string }

func (s folderSource) ID() string                           { return s.id }
func (s folderSource) Name() string                         { return s.name }
func (s folderSource) Health(context.Context) source.Health { return source.Health{OK: true} }
func (s folderSource) Folder() string                       { return s.folder }

var (
	qobuzInfo = sourceInfo{id: "qobuz", name: "Qobuz", folder: "qobuz"}
	appleInfo = sourceInfo{id: "applemusic", name: "Apple Music", folder: "music/apple"}
)

var at1 = time.Date(2026, 11, 3, 11, 5, 11, 0, time.UTC)

const blockText = `Music Shelf download 418
Source: Apple Music
Kind: album
Source ID: 1440000000
Source link: https://music.apple.com/ru/album/example/1440000000
Delivered: FLAC 24-bit 48 kHz
Finished: 2026-11-03T14:05:11+03:00
Tracks:
  1  01. Shoreline.flac  song 1440000001  FLAC 24/48  done
`

func TestTellSources(t *testing.T) {
	block := parseProvenance([]byte(blockText))
	cases := []struct {
		name   string
		folder string
		others []string
		recs   []Record
		blocks []provenance
		want   AlbumSource
	}{
		{name: "a download", folder: "qobuz/Blue",
			recs: []Record{{Source: "qobuz", Kind: "album", ItemID: "a1", Folder: "qobuz/Blue", DownloadID: 7, Quality: "FLAC 24/96", At: at1}},
			want: AlbumSource{ID: "qobuz", Name: "Qobuz", By: byDownload, Kind: "album", ItemID: "a1", DownloadID: 7, Quality: "FLAC 24/96", At: "2026-11-03T11:05:11Z"}},
		{name: "an import has no date", folder: "qobuz/Blue",
			recs: []Record{{Source: "qobuz", Kind: "album", ItemID: "a1", Folder: "qobuz/Blue", Imported: true, At: at1}},
			want: AlbumSource{ID: "qobuz", Name: "Qobuz", By: byImport, Kind: "album", ItemID: "a1"}},
		{name: "the album over an earlier track", folder: "qobuz/Blue",
			recs: []Record{
				{Source: "qobuz", Kind: "track", ItemID: "t1", Folder: "qobuz/Blue", DownloadID: 9, At: at1.Add(time.Hour)},
				{Source: "qobuz", Kind: "album", ItemID: "a1", Folder: "qobuz/Blue", DownloadID: 8, At: at1},
			},
			want: AlbumSource{ID: "qobuz", Name: "Qobuz", By: byDownload, Kind: "album", ItemID: "a1", DownloadID: 8, At: "2026-11-03T11:05:11Z"}},
		{name: "a moved folder", folder: "jazz/Blue",
			recs: []Record{{Source: "qobuz", Kind: "album", ItemID: "a1", Folder: "qobuz/Blue", Imported: true}},
			want: AlbumSource{ID: "qobuz", Name: "Qobuz", By: byImport, Kind: "album", ItemID: "a1", Recorded: "qobuz/Blue"}},
		{name: "a name two albums have", folder: "jazz/Blue", others: []string{"rock/Blue"},
			recs: []Record{{Source: "qobuz", Kind: "album", ItemID: "a1", Folder: "qobuz/Blue", Imported: true}}},
		{name: "a name two records have", folder: "jazz/Blue",
			recs: []Record{
				{Source: "qobuz", Kind: "album", ItemID: "a1", Folder: "qobuz/Blue", Imported: true},
				{Source: "applemusic", Kind: "album", ItemID: "a2", Folder: "music/apple/Blue", Imported: true},
			}},
		{name: "a record whose folder still holds an album", folder: "jazz/Blue", others: []string{"qobuz/Blue"},
			recs: []Record{{Source: "qobuz", Kind: "album", ItemID: "a1", Folder: "qobuz/Blue", Imported: true}}},
		{name: "shelf.txt alone", folder: "rock/Shoreline", blocks: block,
			want: AlbumSource{ID: "applemusic", Name: "Apple Music", By: byFile, Kind: "album", ItemID: "1440000000", DownloadID: 418,
				Quality: "FLAC 24-bit 48 kHz", At: "2026-11-03T11:05:11Z", Link: "https://music.apple.com/ru/album/example/1440000000"}},
		{name: "shelf.txt gives the record its link", folder: "rock/Shoreline", blocks: block,
			recs: []Record{{Source: "applemusic", Kind: "album", ItemID: "1440000000", Folder: "rock/Shoreline", DownloadID: 418, At: at1}},
			want: AlbumSource{ID: "applemusic", Name: "Apple Music", By: byDownload, Kind: "album", ItemID: "1440000000", DownloadID: 418,
				At: "2026-11-03T11:05:11Z", Link: "https://music.apple.com/ru/album/example/1440000000"}},
		{name: "shelf.txt of another item", folder: "rock/Shoreline", blocks: block,
			recs: []Record{{Source: "applemusic", Kind: "album", ItemID: "other", Folder: "rock/Shoreline", Imported: true}},
			want: AlbumSource{ID: "applemusic", Name: "Apple Music", By: byImport, Kind: "album", ItemID: "other"}},
		{name: "shelf.txt of an unknown source", folder: "rock/Shoreline",
			blocks: parseProvenance([]byte("Music Shelf download 1\nSource: Tidal\nKind: album\n"))},
		{name: "a source folder", folder: "qobuz/Old",
			want: AlbumSource{ID: "qobuz", Name: "Qobuz", By: byFolder}},
		{name: "a nested source folder", folder: "music/apple/Court",
			want: AlbumSource{ID: "applemusic", Name: "Apple Music", By: byFolder}},
		{name: "a folder that only starts like one", folder: "qobuz-old/Hejira"},
		{name: "a record of a source no longer registered", folder: "rock/Old",
			recs: []Record{{Source: "tidal", Kind: "album", ItemID: "x", Folder: "rock/Old", Imported: true}},
			want: AlbumSource{ID: "tidal", Name: "tidal", By: byImport, Kind: "album", ItemID: "x"}},
		{name: "nothing tells", folder: "rock/Plain"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := &album{Folder: c.folder, Source: AlbumSource{ID: "stale"}}
			albums := []*album{a}
			for _, f := range c.others {
				albums = append(albums, &album{Folder: f})
			}
			blocks := map[string][]provenance{}
			if c.blocks != nil {
				blocks[c.folder] = c.blocks
			}
			tellSources(albums, c.recs, blocks, []sourceInfo{qobuzInfo, appleInfo})
			if a.Source != c.want {
				t.Errorf("got %+v\nwant %+v", a.Source, c.want)
			}
		})
	}
}

// A walk tells each album's source, the list and the filter menu show it,
// and a run selects by it. A failed read of the records keeps the last.
func TestWalkTellsSources(t *testing.T) {
	r := newRig(t)
	reg := source.NewRegistry()
	for _, s := range []folderSource{{"qobuz", "Qobuz", "qobuz"}, {"applemusic", "Apple Music", "apple-music"}} {
		if err := reg.Add(s); err != nil {
			t.Fatal(err)
		}
	}
	r.m.sources, r.m.ix.sources = reg, reg
	var fail error
	r.m.ix.recs = func(context.Context) ([]Record, error) {
		return []Record{
			{Source: "qobuz", Kind: "album", ItemID: "a1", Folder: "qobuz/Blue", DownloadID: 7, Quality: "FLAC 24/96", At: at1},
			{Source: "qobuz", Kind: "album", ItemID: "a2", Folder: "qobuz/Hejira", Imported: true, At: at1},
		}, fail
	}
	album := func(folder, title string) {
		r.add(folder+"/01.flac", "track.flac", tagged(map[string][]string{"ALBUM": {title}}))
	}
	album("qobuz/Blue", "Blue")
	album("jazz/Hejira", "Hejira")
	album("apple-music/Court", "Court and Spark")
	album("rock/Shoreline", "Shoreline")
	r.write("rock/Shoreline/shelf.txt", []byte(blockText))
	album("rock/Plain", "Plain")
	r.walk()

	titles := func(q string) []string {
		var out []string
		for _, a := range r.albums(q).Albums {
			out = append(out, a.Title)
		}
		return out
	}
	cases := map[string][]string{
		"?source=qobuz":                      {"Blue", "Hejira"},
		"?source=applemusic":                 {"Court and Spark", "Shoreline"},
		"?source=none":                       {"Plain"},
		"?source=qobuz&q=hejira&folder=jazz": {"Hejira"},
	}
	for q, want := range cases {
		if got := titles(q); !slices.Equal(got, want) {
			t.Errorf("%s: %q, want %q", q, got, want)
		}
	}
	if rec := r.do(http.MethodGet, "/api/library/albums?source=tidal"); rec.Code != http.StatusBadRequest {
		t.Errorf("an unknown source: %d", rec.Code)
	}
	blue := r.albums("?q=blue").Albums[0]
	if s := blue.Source; s.ID != "qobuz" || s.Name != "Qobuz" || s.By != byDownload || s.DownloadID != 7 {
		t.Fatalf("Blue: %+v", s)
	}
	var detail detailJSON
	r.getJSON("/api/library/albums/"+itoa(blue.ID), &detail)
	if detail.Source != blue.Source {
		t.Errorf("the editor's source %+v, the list's %+v", detail.Source, blue.Source)
	}
	if s := r.albums("?q=hejira").Albums[0].Source; s.By != byImport || s.Recorded != "qobuz/Hejira" {
		t.Errorf("Hejira: %+v", s)
	}
	if s := r.albums("?q=shoreline").Albums[0].Source; s.By != byFile || s.Link == "" {
		t.Errorf("Shoreline: %+v", s)
	}

	var f filtersJSON
	r.getJSON("/api/library/filters", &f)
	want := []sourceCountJSON{{"qobuz", "Qobuz", 2}, {"applemusic", "Apple Music", 2}}
	if !slices.Equal(f.Sources, want) || f.NoSource != 1 {
		t.Errorf("filters: %+v, no source %d", f.Sources, f.NoSource)
	}

	if p := r.preview(runBody{Selection: selection{All: true, Source: "qobuz"}, Fixes: []string{FixReplayGain}}); p.Selected != 2 {
		t.Errorf("a run over qobuz selects %d", p.Selected)
	}
	if rec := r.post("/api/library/runs/preview", runBody{Selection: selection{All: true, Source: "tidal"}, Fixes: []string{FixReplayGain}}); rec.Code != http.StatusBadRequest {
		t.Errorf("a run over an unknown source: %d", rec.Code)
	}

	fail = errors.New("database is locked")
	r.walk()
	if got := titles("?source=qobuz"); !slices.Equal(got, []string{"Blue", "Hejira"}) {
		t.Errorf("after a failed read: %q", got)
	}
}
