package qobuz

import (
	"os"
	"path/filepath"
	"testing"

	qobuzdl "github.com/tikhonp/qobuz-dl-go"
	"github.com/tikhonp/qobuz-dl-go/downloadsdb"
	"github.com/tikhonp/shelf/internal/modules/downloads"
	"github.com/tikhonp/shelf/internal/platform/db/dbtest"
	"github.com/tikhonp/shelf/internal/source"
)

// oldDB writes rows into a file with Python's table.
func oldDB(t *testing.T, name string, rows ...qobuzdl.DownloadRecord) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), name)
	d, err := downloadsdb.Open(bg, file)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		r.Status = "downloaded"
		if err := d.Add(bg, r); err != nil {
			t.Fatal(err)
		}
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	return file
}

type imported struct {
	Source   string `db:"source"`
	Kind     string `db:"kind"`
	ID       string `db:"item_id"`
	Quality  string `db:"quality"`
	Path     string `db:"path"`
	Imported bool   `db:"imported"`
}

func (r *jobRig) items() map[string]imported {
	r.t.Helper()
	var rows []imported
	if err := r.h.DB.R.Select(&rows, `SELECT source, kind, item_id, quality, path, imported FROM downloaded_items`); err != nil {
		r.t.Fatal(err)
	}
	out := map[string]imported{}
	for _, x := range rows {
		out[x.ID] = x
	}
	return out
}

func TestImportMapsRows(t *testing.T) {
	r := newJobRig(t)
	music := r.s.root
	// A file name a URI would cut short.
	file := oldDB(t, "qobuz dl #1?.db",
		// The old container's view of music/qobuz.
		qobuzdl.DownloadRecord{ID: "a1", MediaType: "album", Quality: 27, FileFormat: "FLAC", BitDepth: str("24"), SamplingRate: str("96"), SavedPath: "/downloads/Artist - Album (2019) [24B-96kHz]"},
		// One album at two qualities: the better row wins.
		qobuzdl.DownloadRecord{ID: "a2", MediaType: "album", Quality: 6, FileFormat: "FLAC", BitDepth: str("16"), SamplingRate: str("44.1"), SavedPath: "/downloads/Two CD"},
		qobuzdl.DownloadRecord{ID: "a2", MediaType: "album", Quality: 27, FileFormat: "FLAC", BitDepth: str("24"), SamplingRate: str("192.0"), SavedPath: "/downloads/Two Max"},
		qobuzdl.DownloadRecord{ID: "t1", MediaType: "track", Quality: 5, FileFormat: "MP3", BitDepth: str("16"), SamplingRate: str("44.1"), SavedPath: music + "/qobuz/Single"},
		// Python's None and an empty path, from the older layouts.
		qobuzdl.DownloadRecord{ID: "a3", MediaType: "album", Quality: 27, FileFormat: "Unknown"},
		qobuzdl.DownloadRecord{ID: "a4", MediaType: "album", Quality: 27, FileFormat: "FLAC", SavedPath: "/mnt/old/Album"},
		qobuzdl.DownloadRecord{ID: "p1", MediaType: "playlist", Quality: 27, FileFormat: "FLAC", SavedPath: "/downloads/List"},
		qobuzdl.DownloadRecord{ID: "x1", MediaType: "label", Quality: 27, FileFormat: "FLAC", SavedPath: "/downloads/Label"},
	)
	// The deployment mounts the old data read-only.
	dir := filepath.Dir(file)
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	res, err := Import(bg, file, r.cfg, r.svc, dbtest.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if res != (ImportResult{Rows: 8, Added: 5, Kept: 1, OtherKinds: 2, Outside: 1}) {
		t.Fatalf("%+v", res)
	}
	got := r.items()
	if len(got) != 5 {
		t.Fatalf("%+v", got)
	}
	for id, want := range map[string]imported{
		"a1": {ID: "a1", Kind: "album", Quality: "FLAC 24/96", Path: "qobuz/Artist - Album (2019) [24B-96kHz]"},
		"a2": {ID: "a2", Kind: "album", Quality: "FLAC 24/192", Path: "qobuz/Two Max"},
		"t1": {ID: "t1", Kind: "track", Quality: "MP3", Path: "qobuz/Single"},
		"a3": {ID: "a3", Kind: "album"},
		"a4": {ID: "a4", Kind: "album", Quality: "FLAC", Path: "/mnt/old/Album"},
	} {
		want.Source, want.Imported = ID, true
		if got[id] != want {
			t.Errorf("%s: %+v, want %+v", id, got[id], want)
		}
	}
	// A read of the old file leaves it as it was.
	if _, err := os.Stat(file + "-wal"); err == nil {
		t.Error("the import turned the file to WAL")
	}
	// What was imported is what Queue sees.
	d, err := r.svc.Queue(bg, downloads.QueueRequest{URL: "https://play.qobuz.com/album/a1", Origin: downloads.OriginWeb})
	if err != nil || d.Status != downloads.Skipped {
		t.Fatalf("%+v, %v", d, err)
	}
}

func TestImportFollowsTheFolderSetting(t *testing.T) {
	r := newJobRig(t)
	r.cfg.Qobuz.Dir = "library/qobuz"
	file := oldDB(t, "qobuz_dl.db", qobuzdl.DownloadRecord{ID: "a1", MediaType: "album", FileFormat: "FLAC", SavedPath: "/downloads/Album"})
	if _, err := Import(bg, file, r.cfg, r.svc, dbtest.Discard); err != nil {
		t.Fatal(err)
	}
	if got := r.items()["a1"].Path; got != "library/qobuz/Album" {
		t.Fatal(got)
	}
}

func TestImportIsIdempotent(t *testing.T) {
	r := newJobRig(t)
	// A row shelf wrote itself stays as it is.
	mine := downloads.Downloaded{Ref: ref(source.KindAlbum, "a2"), Quality: "FLAC 24/192", Path: "qobuz/Mine"}
	if err := r.svc.RecordDownloaded(bg, mine); err != nil {
		t.Fatal(err)
	}
	file := oldDB(t, "qobuz_dl.db",
		qobuzdl.DownloadRecord{ID: "a1", MediaType: "album", FileFormat: "FLAC", SavedPath: "/downloads/One"},
		qobuzdl.DownloadRecord{ID: "a2", MediaType: "album", FileFormat: "FLAC", SavedPath: "/downloads/Two"},
	)
	first, err := Import(bg, file, r.cfg, r.svc, dbtest.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if first.Added != 1 || first.Kept != 1 {
		t.Fatalf("%+v", first)
	}
	before := r.items()
	if got := before["a2"]; got.Path != "qobuz/Mine" || got.Imported {
		t.Fatalf("%+v", got)
	}
	r.h.Clock.Advance(1000)
	second, err := Import(bg, file, r.cfg, r.svc, dbtest.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if second.Added != 0 || second.Kept != 2 {
		t.Fatalf("%+v", second)
	}
	after := r.items()
	if len(after) != len(before) || after["a1"] != before["a1"] || after["a2"] != before["a2"] {
		t.Fatalf("%+v", after)
	}
}

func TestImportRefusesOtherFiles(t *testing.T) {
	r := newJobRig(t)
	if _, err := Import(bg, filepath.Join(t.TempDir(), "nothing.db"), r.cfg, r.svc, dbtest.Discard); err == nil {
		t.Error("a missing file imported")
	}
	notDB := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(notDB, []byte("not a database"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Import(bg, notDB, r.cfg, r.svc, dbtest.Discard); err == nil {
		t.Error("a text file imported")
	}
}
