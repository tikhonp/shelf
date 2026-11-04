package qobuz

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	qobuzdl "github.com/tikhonp/qobuz-dl-go"
	"github.com/tikhonp/qobuz-dl-go/qobuzdltest"
	"github.com/tikhonp/shelf/internal/modules/downloads"
	"github.com/tikhonp/shelf/internal/platform/db/dbtest"
	"github.com/tikhonp/shelf/internal/platform/jobs/jobstest"
	"github.com/tikhonp/shelf/internal/platform/module"
	"github.com/tikhonp/shelf/internal/source"
)

// jobRig is the module behind the real downloads module and its jobs. The
// pools run, and the module logs in when the test calls up.
type jobRig struct {
	*rig
	h   *jobstest.Harness
	svc *downloads.Service
}

func newJobRig(t *testing.T) *jobRig {
	t.Helper()
	h := jobstest.New(t, downloads.New())
	dl := downloads.New()
	dl.KeepAlive = 20 * time.Millisecond
	r := newRig(t, func(r *rig) {
		r.m = New(dl.Service())
		r.m.Options = func(o *qobuzdl.Options) { r.srv.Configure(o) }
	})
	reg := source.NewRegistry()
	if err := reg.Add(r.s); err != nil {
		t.Fatal(err)
	}
	closing := make(chan struct{})
	t.Cleanup(func() { close(closing) })
	err := dl.Init(module.Deps{Cfg: r.cfg, Log: dbtest.Discard, DB: h.DB, Jobs: h.Sys, Hub: r.hub, Sources: reg, Closing: closing})
	if err != nil {
		t.Fatal(err)
	}
	dl.Service().Now = h.Clock.Now
	if err := h.Sys.Register(downloads.Name, dl.JobTypes()...); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.Start(h.Sys))
	return &jobRig{rig: r, h: h, svc: dl.Service()}
}

func (r *jobRig) queue(link string) downloads.Download {
	r.t.Helper()
	d, err := r.svc.Queue(bg, downloads.QueueRequest{URL: link, Origin: downloads.OriginWeb})
	if err != nil {
		r.t.Fatal(err)
	}
	return d
}

// wait returns the download once it has ended.
func (r *jobRig) wait(id int64) downloads.Detail {
	r.t.Helper()
	var d downloads.Detail
	r.h.WaitFor("the download to end", func() bool {
		var err error
		if d, err = r.svc.Get(bg, id); err != nil {
			r.t.Fatal(err)
		}
		return d.Status.Final()
	})
	return d
}

func (r *jobRig) provenance(d downloads.Detail) string {
	r.t.Helper()
	b, err := os.ReadFile(filepath.Join(r.cfg.MusicDir, d.Folder, downloads.ProvenanceFile))
	if err != nil {
		r.t.Fatal(err)
	}
	return string(b)
}

func TestAlbumThroughTheJob(t *testing.T) {
	r := newJobRig(t)
	r.up()
	d := r.wait(r.queue("https://www.qobuz.com/us-en/album/begin-again/" + qobuzdltest.AlbumHiRes96).ID)
	if d.Status != downloads.Done || d.Title != "Begin Again" || d.Artist != "Norah Jones" || d.TracksDone != 7 || d.Bytes == 0 {
		t.Fatalf("%+v", d.Download)
	}
	folder := "qobuz/Norah Jones - Begin Again (2019) [24B-96kHz]"
	if d.Folder != folder {
		t.Fatalf("folder %q", d.Folder)
	}
	first := d.Tracks[0]
	if first.Status != "done" || first.Path != folder+"/01 - My Heart Is Full.flac" || first.Artist != "Norah Jones" ||
		first.Codec != "flac" || first.BitDepth != 24 || first.SampleRate != 96000 || first.Size == 0 {
		t.Fatalf("%+v", first)
	}
	txt := r.provenance(d)
	for _, want := range []string{
		"Source: Qobuz\n", "Kind: album\n", "Source ID: " + qobuzdltest.AlbumHiRes96 + "\n",
		"Source link: https://www.qobuz.com/fr-fr/album/begin-again-norah-jones/k43q1q3kx88fa\n",
		"Title: Begin Again\n", "Artist: Norah Jones\n", "Label: Blue Note Records\n", "Release date: 2019-04-12\n",
		"UPC: 0602577440397\n", "Requested format: Qobuz 27, FLAC up to 24-bit 192 kHz\n", "Delivered: FLAC 24-bit 96 kHz\n",
		", qobuz-dl-go v0.1.0 porting qobuz-dl 2.5.9\n",
		"  1  01 - My Heart Is Full.flac  track 58780541  ISRC USUM71807619  FLAC 24/96  done\n",
	} {
		if !strings.Contains(txt, want) {
			t.Errorf("no %q in\n%s", want, txt)
		}
	}
	var row struct {
		DownloadID int64  `db:"download_id"`
		Quality    string `db:"quality"`
		Path       string `db:"path"`
		Imported   bool   `db:"imported"`
	}
	if err := r.h.DB.R.Get(&row, `SELECT download_id, quality, path, imported FROM downloaded_items WHERE item_id = ?`, qobuzdltest.AlbumHiRes96); err != nil {
		t.Fatal(err)
	}
	if row.DownloadID != d.ID || row.Quality != "FLAC 24/96" || row.Path != folder || row.Imported {
		t.Fatalf("%+v", row)
	}
}

// Both copies pass Queue's check, because neither has finished. The second
// finds the first's row and files when its turn comes. The pools also start
// before the module has logged in, as after a restart.
func TestTwoCopiesOfAnAlbum(t *testing.T) {
	r := newJobRig(t)
	link := "https://play.qobuz.com/album/" + qobuzdltest.AlbumHiRes96
	a, b := r.queue(link), r.queue(link)
	r.up()
	first, second := r.wait(a.ID), r.wait(b.ID)
	if first.Status != downloads.Done || second.Status != downloads.Done || second.Error != "" {
		t.Fatalf("%+v\n%+v", first.Download, second.Download)
	}
	if second.Folder != first.Folder {
		t.Fatalf("%q and %q", first.Folder, second.Folder)
	}
	for _, tr := range second.Tracks {
		if tr.Status != "skipped" || tr.Path == "" || tr.BitDepth != 24 {
			t.Fatalf("%+v", tr)
		}
	}
	if n := strings.Count(r.provenance(second), "Music Shelf download "); n != 2 {
		t.Fatalf("%d blocks", n)
	}
}

func TestPlaylistThroughTheJob(t *testing.T) {
	r := newJobRig(t)
	r.up()
	d := r.wait(r.queue("https://play.qobuz.com/playlist/" + qobuzdltest.Playlist).ID)
	if d.Status != downloads.Done || d.Folder != "qobuz/Top 10 Operas" || d.TracksDone != 10 || d.Artist != "gramophonemagazine" {
		t.Fatalf("%+v", d.Download)
	}
	if !r.exists(d.Folder + "/Top 10 Operas.m3u8") {
		t.Fatal("no m3u8")
	}
	if txt := r.provenance(d); !strings.Contains(txt, "Kind: playlist\n") || !strings.Contains(txt, "Source link: https://play.qobuz.com/playlist/2082798\n") {
		t.Fatal(txt)
	}
	// Its tracks are on record, so one of them alone is skipped.
	track := r.queue("https://play.qobuz.com/track/" + d.Tracks[0].ItemID)
	if track.Status != downloads.Skipped || track.Folder != d.Folder {
		t.Fatalf("%+v", track)
	}
}

func TestArtistThroughTheJob(t *testing.T) {
	r := newJobRig(t)
	r.up()
	d := r.wait(r.queue("https://www.qobuz.com/us-en/interpreter/norah-jones/" + qobuzdltest.ArtistAlbums).ID)
	if len(d.Children) != 3 {
		t.Fatalf("%+v", d)
	}
	for _, c := range d.Children {
		if c := r.wait(c.ID); c.Status != downloads.Done || c.Ref.Source != ID || c.Ref.Kind != source.KindAlbum {
			t.Fatalf("%+v", c.Download)
		}
	}
	r.h.WaitFor("the artist to end", func() bool {
		d, _ = r.svc.Get(bg, d.ID)
		return d.Status == downloads.Done
	})

	d = r.wait(r.queue("https://play.qobuz.com/artist/" + qobuzdltest.ArtistEmpty).ID)
	if d.Status != downloads.Done || d.Error != "nothing to download" {
		t.Fatalf("%+v", d.Download)
	}
}

func TestPartialAlbumThroughTheJob(t *testing.T) {
	r := newJobRig(t)
	r.up()
	a := r.item(source.KindAlbum, qobuzdltest.AlbumHiRes96)
	r.srv.Fail404(a.Tracks[1].ID)
	d := r.wait(r.queue("https://play.qobuz.com/album/" + qobuzdltest.AlbumHiRes96).ID)
	if d.Status != downloads.Partial || d.TracksDone != 6 || d.TracksFailed != 1 || !strings.HasPrefix(d.Folder, "qobuz/[INCOMPLETE] ") {
		t.Fatalf("%+v", d.Download)
	}
	if d.Tracks[1].Status != "failed" || d.Tracks[1].Error == "" {
		t.Fatalf("%+v", d.Tracks[1])
	}
	if txt := r.provenance(d); !strings.Contains(txt, "failed") {
		t.Fatal(txt)
	}
}

// Force passes the check in Queue, and qobuz-dl-go still skips complete files.
func TestForceFetchesWhatIsMissing(t *testing.T) {
	r := newJobRig(t)
	r.up()
	link := "https://play.qobuz.com/album/" + qobuzdltest.AlbumHiRes96
	d := r.wait(r.queue(link).ID)
	if err := os.Remove(filepath.Join(r.cfg.MusicDir, d.Tracks[1].Path)); err != nil {
		t.Fatal(err)
	}
	q, err := r.svc.Queue(bg, downloads.QueueRequest{URL: link, Force: true, Origin: downloads.OriginWeb})
	if err != nil {
		t.Fatal(err)
	}
	again := r.wait(q.ID)
	if again.Status != downloads.Done || again.TracksDone != 7 {
		t.Fatalf("%+v", again.Download)
	}
	for i, tr := range again.Tracks {
		want := "skipped"
		if i == 1 {
			want = "done"
		}
		if tr.Status != want {
			t.Errorf("track %d: %s, want %s", tr.Position, tr.Status, want)
		}
	}
	if _, err := os.Stat(filepath.Join(r.cfg.MusicDir, d.Tracks[1].Path)); err != nil {
		t.Fatal(err)
	}
}

func TestExpiredTokenThroughTheJob(t *testing.T) {
	r := newJobRig(t)
	r.up()
	r.srv.ExpireToken()
	d := r.wait(r.queue("https://play.qobuz.com/album/" + qobuzdltest.AlbumCD).ID)
	if d.Status != downloads.Failed || d.Error != "Lookup failed: token expired" {
		t.Fatalf("%+v", d.Download)
	}
}
