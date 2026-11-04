package qobuz

import (
	"testing"

	qobuzdl "github.com/tikhonp/qobuz-dl-go"
	"github.com/tikhonp/shelf/internal/modules/downloads"
	"github.com/tikhonp/shelf/internal/source"
)

func str(s string) *string { return &s }

func TestDownloadsDBAdapter(t *testing.T) {
	r := newJobRig(t)
	root := r.s.root
	db := downloadsDB{history: r.svc, root: root}

	has := func(kind, id string, quality int) bool {
		t.Helper()
		ok, err := db.Has(bg, kind, id, quality)
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}
	if has("album", "a1", 27) {
		t.Fatal("an empty table has the album")
	}
	rec := qobuzdl.DownloadRecord{ID: "a1", MediaType: "album", Quality: 27, FileFormat: "FLAC", BitDepth: str("24"),
		SamplingRate: str("96"), SavedPath: root + "/qobuz/Album"}
	if err := db.Add(bg, rec); err != nil {
		t.Fatal(err)
	}
	if !has("album", "a1", 27) {
		t.Fatal("the album isn't there")
	}
	// downloaded_items keeps one row per item, so any quality answers.
	if !has("album", "a1", 6) {
		t.Fatal("another quality isn't there")
	}
	// The table is keyed on the kind, where Python matches the ID alone.
	if has("track", "a1", 27) {
		t.Fatal("a track has the album's ID")
	}
	type row struct {
		Quality  string `db:"quality"`
		Path     string `db:"path"`
		Imported bool   `db:"imported"`
	}
	read := func(id string) row {
		t.Helper()
		var got row
		if err := r.h.DB.R.Get(&got, `SELECT quality, path, imported FROM downloaded_items WHERE source = 'qobuz' AND item_id = ?`, id); err != nil {
			t.Fatal(err)
		}
		return got
	}
	if got := read("a1"); got != (row{"FLAC 24/96", "qobuz/Album", false}) {
		t.Fatalf("%+v", got)
	}
	// A duplicate is no error and changes nothing.
	rec.SavedPath = root + "/qobuz/Other"
	if err := db.Add(bg, rec); err != nil {
		t.Fatal(err)
	}
	if got := read("a1"); got.Path != "qobuz/Album" {
		t.Fatalf("%+v", got)
	}
	// A path outside the music root stays as it is.
	err := db.Add(bg, qobuzdl.DownloadRecord{ID: "t1", MediaType: "track", FileFormat: "MP3", SavedPath: "/elsewhere/Track"})
	if err != nil {
		t.Fatal(err)
	}
	if got := read("t1"); got != (row{"MP3", "/elsewhere/Track", false}) {
		t.Fatalf("%+v", got)
	}
	// What the adapter added is what Queue sees.
	d, err := r.svc.Queue(bg, downloads.QueueRequest{Ref: ref(source.KindAlbum, "a1"), Origin: downloads.OriginWeb})
	if err != nil || d.Status != downloads.Skipped || d.Folder != "qobuz/Album" {
		t.Fatalf("%+v, %v", d, err)
	}
}

func TestQualityText(t *testing.T) {
	for _, tc := range []struct {
		format    string
		bits, khz *string
		want      string
	}{
		{"FLAC", str("24"), str("96"), "FLAC 24/96"},
		{"FLAC", str("24"), str("96.0"), "FLAC 24/96"},
		{"FLAC", str("16"), str("44.1"), "FLAC 16/44.1"},
		{"FLAC", nil, nil, "FLAC"},
		{"FLAC", str("None"), str("None"), "FLAC"},
		{"MP3", str("16"), str("44.1"), "MP3"},
		{"Unknown", nil, nil, ""},
		{"", nil, nil, ""},
	} {
		if got := qualityText(tc.format, tc.bits, tc.khz); got != tc.want {
			t.Errorf("%q: %q, want %q", tc.format, got, tc.want)
		}
	}
}
