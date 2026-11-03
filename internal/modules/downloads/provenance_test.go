package downloads

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tikhonp/shelf/internal/platform/obs"
	"github.com/tikhonp/shelf/internal/source"
)

// fixture is the Qobuz example of docs/provenance.md, rendered from a
// download, its payload and its tracks.
func fixture() (Download, payload, []Track) {
	d := Download{
		ID: 418, Ref: source.Ref{Source: "fake", Kind: source.KindAlbum, ID: "123456789"}, Format: "fake:flac",
		Title: "Low Tide Hymns", Artist: "Northern Static", Origin: OriginTelegram,
		OriginRef: OriginRef{UserID: 123456, Username: "name"},
		StartedAt: time.Date(2026, 11, 3, 11, 1, 52, 0, time.UTC), FinishedAt: time.Date(2026, 11, 3, 11, 5, 11, 0, time.UTC),
	}
	p := payload{DownloadID: 418,
		Item: &itemFacts{URL: "https://www.qobuz.com/album/example/123456789", Label: "Example Label",
			ReleaseDate: "2019-03-01", UPC: "0000000000000", ISRC: "XX0000000009"},
		Result: &resultInfo{Folders: []string{"qobuz/Northern Static - Low Tide Hymns"}, Tool: "qobuz-dl-go v0.1.0 porting qobuz-dl 2.5.9"},
	}
	tracks := []Track{
		{Position: 1, ItemID: "11111111", ISRC: "XX0000000001", Status: "done", Path: "qobuz/Northern Static - Low Tide Hymns/01 - Shoreline.flac", Codec: "flac", BitDepth: 24, SampleRate: 96000},
		{Position: 2, ItemID: "11111112", ISRC: "XX0000000002", Status: "skipped", Path: "qobuz/Northern Static - Low Tide Hymns/02 - Undertow.flac", Codec: "flac", BitDepth: 24, SampleRate: 96000},
		{Position: 3, ItemID: "11111113", Title: "Breakwater", Status: "failed", Error: "refused"},
	}
	return d, p, tracks
}

func TestProvenanceFormat(t *testing.T) {
	r := newRig(t)
	r.cfg.PublicURL = mustURL(t, "https://shelf.music.tikhonnnnn.com/")
	d, p, tracks := fixture()
	got := r.svc.provenance(d, p, tracks)
	want := `Music Shelf download 418
Shelf link: https://shelf.music.tikhonnnnn.com/downloads/418
Source: Fake
Kind: album
Source ID: 123456789
Source link: https://www.qobuz.com/album/example/123456789
Title: Low Tide Hymns
Artist: Northern Static
Label: Example Label
Release date: 2019-03-01
UPC: 0000000000000
Requested format: Fake FLAC, 24-bit 96 kHz
Delivered: FLAC 24-bit 96 kHz
Requested by: Telegram @name, user 123456
Started: 2026-11-03T14:01:52+03:00
Finished: 2026-11-03T14:05:11+03:00
Tools: shelf ` + obs.AppVersion + `, qobuz-dl-go v0.1.0 porting qobuz-dl 2.5.9
Tracks:
  1  01 - Shoreline.flac  track 11111111  ISRC XX0000000001  FLAC 24/96  done
  2  02 - Undertow.flac  track 11111112  ISRC XX0000000002  FLAC 24/96  skipped
  3  Breakwater  track 11111113  failed
`
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}

	// A single track names its ISRC, not a UPC; a source may call tracks
	// songs; the web is "Web".
	d.Ref.Kind, d.Origin = source.KindTrack, OriginWeb
	p.Result.TrackNoun = "song"
	got = r.svc.provenance(d, p, tracks[:1])
	for _, line := range []string{"ISRC: XX0000000009\n", "Requested by: Web\n", "  1  01 - Shoreline.flac  song 11111111  "} {
		if !strings.Contains(got, line) {
			t.Errorf("missing %q in:\n%s", line, got)
		}
	}
	if strings.Contains(got, "UPC:") {
		t.Errorf("a track has a UPC line:\n%s", got)
	}
}

func TestProvenanceWithoutPublicURL(t *testing.T) {
	r := newRig(t)
	d, p, tracks := fixture()
	got := r.svc.provenance(d, p, tracks)
	if strings.Contains(got, "Shelf link") {
		t.Fatalf("a Shelf link without SHELF_PUBLIC_URL:\n%s", got)
	}
	if !strings.HasPrefix(got, "Music Shelf download 418\nSource: Fake\n") {
		t.Fatalf("%s", got)
	}
}

func TestProvenanceAppends(t *testing.T) {
	r := newRig(t)
	r.start()
	// Two single tracks land in one folder, Singles.
	a := r.queue(QueueRequest{Ref: source.Ref{Source: "fake", Kind: source.KindTrack, ID: "track-a"}})
	b := r.queue(QueueRequest{Ref: source.Ref{Source: "fake", Kind: source.KindTrack, ID: "track-b"}})
	r.h.Drain()
	da, db := r.get(a.ID), r.get(b.ID)
	if da.ProvenancePath != "fake/Singles/shelf.txt" || db.ProvenancePath != da.ProvenancePath {
		t.Fatalf("%q %q", da.ProvenancePath, db.ProvenancePath)
	}
	txt, err := os.ReadFile(filepath.Join(r.cfg.MusicDir, da.ProvenancePath))
	if err != nil {
		t.Fatal(err)
	}
	blocks := strings.Split(string(txt), "\n\n")
	if len(blocks) != 2 || !strings.HasPrefix(blocks[0], "Music Shelf download 1\n") || !strings.HasPrefix(blocks[1], "Music Shelf download 2\n") ||
		!strings.HasSuffix(string(txt), " done\n") {
		t.Fatalf("shelf.txt:\n%s", txt)
	}
}

func TestProvenanceSkipsSameDownload(t *testing.T) {
	dir := t.TempDir()
	block := "Music Shelf download 7\nSource: Fake\nTracks:\n"
	for range 2 {
		if err := AppendProvenance(dir, 7, block); err != nil {
			t.Fatal(err)
		}
	}
	// A block that only mentions the header further down is another one.
	other := "Music Shelf download 8\nTitle: Music Shelf download 7\nTracks:\n"
	if err := AppendProvenance(dir, 8, other); err != nil {
		t.Fatal(err)
	}
	if err := AppendProvenance(dir, 8, other); err != nil {
		t.Fatal(err)
	}
	txt, _ := os.ReadFile(filepath.Join(dir, ProvenanceFile))
	if string(txt) != block+"\n"+other {
		t.Fatalf("shelf.txt:\n%q", txt)
	}
	// A file someone else wrote without a final newline still gets an
	// empty line between blocks, and keeps its mode.
	path := filepath.Join(dir, ProvenanceFile)
	_ = os.Remove(path)
	if err := os.WriteFile(path, []byte("Notes by hand"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := AppendProvenance(dir, 9, "Music Shelf download 9\n"); err != nil {
		t.Fatal(err)
	}
	txt, _ = os.ReadFile(path)
	if string(txt) != "Notes by hand\n\nMusic Shelf download 9\n" {
		t.Fatalf("%q", txt)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode changed to %v", fi.Mode().Perm())
	}
}
