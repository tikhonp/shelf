package downloads

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tikhonp/shelf/internal/modules/fakesource"
	"github.com/tikhonp/shelf/internal/platform/jobs"
	"github.com/tikhonp/shelf/internal/source"
)

func TestQueueByRef(t *testing.T) {
	r := newRig(t)
	d := r.queue(QueueRequest{Ref: album("album-1")})
	if d.Status != Queued || d.Format != "fake:flac" || d.Origin != OriginWeb || d.JobID == 0 || d.Ref != album("album-1") {
		t.Fatalf("%+v", d)
	}
	j := r.job(d.JobID)
	if j.Type != JobType || j.Queue != jobs.QueueSystem || j.ResourceKey != "item:fake:album:album-1" || j.CreatedBy != "web" ||
		string(j.Payload) != `{"download_id":1}` {
		t.Fatalf("%+v", j)
	}
	// A format without the source's prefix is the source's.
	d = r.queue(QueueRequest{Ref: album("album-2"), Format: "mp3"})
	if d.Format != "fake:mp3" {
		t.Fatalf("format %q", d.Format)
	}
	if _, err := r.svc.Queue(bg, QueueRequest{Ref: album("album-2"), Format: "fake:ogg", Origin: OriginWeb}); !errors.Is(err, ErrUnknownFormat) {
		t.Fatalf("unknown format: %v", err)
	}
	// Over HTTP.
	rec := r.do(http.MethodPost, "/api/downloads", map[string]any{"ref": map[string]string{"source": "fake", "kind": "album", "id": "album-3"}})
	var body downloadJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || rec.Code != http.StatusCreated || body.Status != Queued ||
		body.ItemID != "album-3" || body.Origin != "web" || body.ParentID != nil || body.StartedAt != nil {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if rec := r.do(http.MethodPost, "/api/downloads", map[string]any{}); rec.Code != http.StatusBadRequest {
		t.Fatalf("empty body: %d %s", rec.Code, rec.Body)
	}
}

func TestQueueByLink(t *testing.T) {
	r := newRig(t)
	link := "https://fake.test/playlist/pl-7"
	d := r.queue(QueueRequest{URL: link, Origin: OriginTelegram, OriginRef: OriginRef{ChatID: 5, UserID: 42, Username: "name"}})
	if d.Ref != (source.Ref{Source: "fake", Kind: source.KindPlaylist, ID: "pl-7"}) || d.URL != link || d.OriginRef.UserID != 42 {
		t.Fatalf("%+v", d)
	}
	if j := r.job(d.JobID); j.CreatedBy != "telegram:42" {
		t.Fatalf("created by %q", j.CreatedBy)
	}
	rec := r.do(http.MethodPost, "/api/downloads", map[string]any{"url": "https://fake.test/album/album-9"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
}

func TestQueueRefusedLink(t *testing.T) {
	r := newRig(t)
	if _, err := r.svc.Queue(bg, QueueRequest{URL: "https://fake.test/video/v-1", Origin: OriginWeb}); !errors.Is(err, ErrRefusedLink) {
		t.Fatalf("err = %v", err)
	}
	rec := r.do(http.MethodPost, "/api/downloads", map[string]any{"url": "https://fake.test/station/s-1"})
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), source.RefusedLinkText) {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
}

func TestQueueUnknownLink(t *testing.T) {
	r := newRig(t)
	if _, err := r.svc.Queue(bg, QueueRequest{URL: "https://example.com/album/1", Origin: OriginWeb}); !errors.Is(err, ErrUnknownLink) {
		t.Fatalf("err = %v", err)
	}
	rec := r.do(http.MethodPost, "/api/downloads", map[string]any{"url": "https://example.com/album/1"})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if n := r.count(`SELECT count(*) FROM downloads`); n != 0 {
		t.Fatalf("%d downloads", n)
	}
}

func TestQueueUnknownSource(t *testing.T) {
	r := newRig(t)
	off := fakesource.NewSource("dark", "Dark Music")
	off.Off = true
	if err := r.reg.Add(off); err != nil {
		t.Fatal(err)
	}
	for name, q := range map[string]QueueRequest{
		"unknown ref":   {Ref: source.Ref{Source: "nope", Kind: source.KindAlbum, ID: "1"}},
		"disabled ref":  {Ref: source.Ref{Source: "dark", Kind: source.KindAlbum, ID: "1"}},
		"disabled link": {URL: off.Link(source.KindAlbum, "1")},
	} {
		q.Origin = OriginWeb
		_, err := r.svc.Queue(bg, q)
		if !errors.Is(err, ErrUnknownSource) {
			t.Errorf("%s: %v", name, err)
		}
	}
	rec := r.do(http.MethodPost, "/api/downloads", map[string]any{"url": off.Link(source.KindAlbum, "1")})
	if rec.Code != http.StatusUnprocessableEntity || !json.Valid(rec.Body.Bytes()) {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	var e struct{ Error string }
	_ = json.Unmarshal(rec.Body.Bytes(), &e)
	if e.Error != "unknown or disabled source: Dark Music is not set up in Shelf" {
		t.Fatalf("message %q", e.Error)
	}
}

func TestQueueSkipsDownloaded(t *testing.T) {
	r := newRig(t)
	r.start()
	first := r.queue(QueueRequest{Ref: album("album-1")})
	r.h.Drain()
	if st := r.get(first.ID).Status; st != Done {
		t.Fatalf("first: %s", st)
	}
	d := r.queue(QueueRequest{Ref: album("album-1")})
	if d.Status != Skipped || d.JobID != 0 || d.Folder != "fake/Northern Static - Low Tide Hymns" || d.Title != "Low Tide Hymns" ||
		d.FinishedAt.IsZero() {
		t.Fatalf("%+v", d)
	}
	// An imported row with no download behind it skips too.
	if _, err := r.h.DB.W.Exec(`INSERT INTO downloaded_items (source, kind, item_id, path, imported, at) VALUES ('fake', 'track', 't-9', 'old/path', 1, ?)`,
		"2026-01-01T00:00:00.000Z"); err != nil {
		t.Fatal(err)
	}
	d = r.queue(QueueRequest{Ref: source.Ref{Source: "fake", Kind: source.KindTrack, ID: "t-9"}})
	if d.Status != Skipped || d.Folder != "old/path" {
		t.Fatalf("%+v", d)
	}
	if n := r.count(`SELECT count(*) FROM jobs`); n != 1 {
		t.Fatalf("%d jobs: a skipped download got one", n)
	}
}

// An imported row has what the older tool knew, and the source gives the rest.
func TestSkippedAsksTheSourceForWhatItLacks(t *testing.T) {
	r := newRig(t)
	ref := album("album-1")
	if _, err := r.svc.ImportDownloaded(bg, []Downloaded{{Ref: ref, Path: "old/path", Title: "Old Title"}}); err != nil {
		t.Fatal(err)
	}
	calls := r.src.ItemCalls()
	d := r.queue(QueueRequest{Ref: ref})
	if d.Status != Skipped || d.Title != "Old Title" || d.Artist != "Northern Static" || d.CoverURL == "" {
		t.Fatalf("%+v", d)
	}
	if got := r.get(d.ID); got.Artist != d.Artist || got.CoverURL != d.CoverURL {
		t.Fatalf("stored %+v", got.Download)
	}
	// The next skip reads the last download, so it asks nothing.
	if d = r.queue(QueueRequest{Ref: ref}); d.CoverURL == "" || r.src.ItemCalls() != calls+1 {
		t.Fatalf("%d calls, %+v", r.src.ItemCalls()-calls, d)
	}
}

// slowResolver never answers.
type slowResolver struct{ source.Downloader }

func (slowResolver) Item(ctx context.Context, _ source.Ref) (source.Item, error) {
	<-ctx.Done()
	return source.Item{}, ctx.Err()
}

func TestSkippedWaitsForTheSourceOnlySoLong(t *testing.T) {
	r := newRig(t)
	r.svc.lookupWait = time.Millisecond
	d := Download{ID: 1, Ref: album("album-1"), Status: Skipped, Title: "Old Title"}
	if got := r.svc.fillSkipped(bg, d, slowResolver{}); got.Title != "Old Title" || got.CoverURL != "" {
		t.Fatalf("%+v", got)
	}
}

func TestSkippedWhenTheSourceFails(t *testing.T) {
	r := newRig(t)
	ref := album("album-1")
	if _, err := r.svc.ImportDownloaded(bg, []Downloaded{{Ref: ref, Path: "old/path", Title: "Old Title", Artist: "Old Artist"}}); err != nil {
		t.Fatal(err)
	}
	r.src.Fail = errors.New("no answer")
	d := r.queue(QueueRequest{Ref: ref})
	if d.Status != Skipped || d.Title != "Old Title" || d.Artist != "Old Artist" || d.CoverURL != "" {
		t.Fatalf("%+v", d)
	}
	// The cover is still missing, so the next skip asks again.
	r.src.Fail = nil
	if d = r.queue(QueueRequest{Ref: ref}); d.CoverURL == "" {
		t.Fatalf("%+v", d)
	}
}

func TestQueueForceFetchesWhatIsMissing(t *testing.T) {
	r := newRig(t)
	r.start()
	first := r.queue(QueueRequest{Ref: album("album-1")})
	r.h.Drain()
	again := r.queue(QueueRequest{Ref: album("album-1"), Force: true})
	if again.Status != Queued || !again.Force || again.JobID == 0 {
		t.Fatalf("%+v", again)
	}
	r.h.Drain()
	det := r.get(again.ID)
	if det.Status != Done {
		t.Fatalf("status %s", det.Status)
	}
	// The files were there, so the source skipped them.
	for _, tr := range det.Tracks {
		if tr.Status != "skipped" {
			t.Fatalf("%+v", tr)
		}
	}
	var by int64
	if err := r.h.DB.R.Get(&by, `SELECT download_id FROM downloaded_items WHERE item_id = 'album-1'`); err != nil || by != again.ID {
		t.Fatalf("downloaded_items points at %d, want %d (first %d): %v", by, again.ID, first.ID, err)
	}

	// A file that went away comes back, and only that one.
	if err := os.Remove(filepath.Join(r.cfg.MusicDir, det.Tracks[1].Path)); err != nil {
		t.Fatal(err)
	}
	third := r.queue(QueueRequest{Ref: album("album-1"), Force: true})
	r.h.Drain()
	det = r.get(third.ID)
	if det.Status != Done || det.Tracks[0].Status != "skipped" || det.Tracks[1].Status != "done" || det.Tracks[2].Status != "skipped" {
		t.Fatalf("%+v", det)
	}
}

func TestSameItemRunsOneAtATime(t *testing.T) {
	r := newRig(t)
	// Two jobs could run at once on the queue; only the key keeps them apart.
	jobs.Concurrency[jobs.QueueSystem] = 2
	t.Cleanup(func() { jobs.Concurrency[jobs.QueueSystem] = 1 })
	ga, gb := newGate(), newGate()
	r.src.Hook = func(ctx context.Context, ref source.Ref, pos int) error {
		switch {
		case pos != 1:
		case ref.ID == "album-a":
			return ga.wait(ctx)
		case ref.ID == "album-b":
			return gb.wait(ctx)
		}
		return nil
	}
	a1 := r.queue(QueueRequest{Ref: album("album-a")})
	a2 := r.queue(QueueRequest{Ref: album("album-a")})
	b := r.queue(QueueRequest{Ref: album("album-b")})
	r.start()
	ga.waitReached(t)
	gb.waitReached(t) // b runs beside the first a
	if st := r.job(a2.JobID).State; st != jobs.Queued {
		t.Fatalf("the second download of the item is %s while the first runs", st)
	}
	close(ga.release)
	close(gb.release)
	r.h.Drain()
	for _, id := range []int64{a1.ID, a2.ID, b.ID} {
		if st := r.get(id).Status; st != Done {
			t.Fatalf("download %d: %s", id, st)
		}
	}
	if len(ga.reached) != 1 { // the second a went through the gate after it opened
		t.Fatalf("%d left at the gate", len(ga.reached))
	}
}

func TestSourcesRunInParallel(t *testing.T) {
	r := newRig(t)
	other := fakesource.NewSource("qobuz", "Fake Qobuz")
	if err := r.reg.Add(other); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	running := map[string]bool{}
	both := make(chan struct{})
	release := make(chan struct{})
	hook := func(ctx context.Context, ref source.Ref, pos int) error {
		if pos != 1 {
			return nil
		}
		mu.Lock()
		running[ref.Source] = true
		if len(running) == 2 {
			close(both)
		}
		mu.Unlock()
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	r.src.Hook, other.Hook = hook, hook
	f := r.queue(QueueRequest{Ref: album("album-1")})
	q := r.queue(QueueRequest{Ref: source.Ref{Source: "qobuz", Kind: source.KindAlbum, ID: "album-1"}})
	if r.job(q.JobID).Queue != jobs.QueueQobuz {
		t.Fatalf("queue %s", r.job(q.JobID).Queue)
	}
	r.start()
	r.h.WaitFor("both sources running", func() bool {
		select {
		case <-both:
			return true
		default:
			return false
		}
	})
	close(release)
	r.h.Drain()
	if r.get(f.ID).Status != Done || r.get(q.ID).Status != Done {
		t.Fatal("not done")
	}
}

func TestAhead(t *testing.T) {
	r := newRig(t)
	other := fakesource.NewSource("other", "Other")
	if err := r.reg.Add(other); err != nil {
		t.Fatal(err)
	}
	first := r.queue(QueueRequest{Ref: album("album-1")})
	r.queue(QueueRequest{Ref: album("album-2")})
	// An artist waits on its albums, and another source has its own queue.
	r.queue(QueueRequest{Ref: source.Ref{Source: "fake", Kind: source.KindArtist, ID: "artist-1"}})
	r.queue(QueueRequest{Ref: source.Ref{Source: "other", Kind: source.KindAlbum, ID: "album-1"}})
	third := r.queue(QueueRequest{Ref: album("album-3"), Origin: OriginTelegram,
		OriginRef: OriginRef{ChatID: 1, StatusMessageID: 5, StatusIsText: true}})
	for d, want := range map[int64]int{first.ID: 0, third.ID: 2} {
		if n, err := r.svc.Ahead(bg, d); err != nil || n != want {
			t.Fatalf("download %d: %d ahead, %v; want %d", d, n, err, want)
		}
	}
	if _, err := r.svc.Ahead(bg, 999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown download: %v", err)
	}
	if ref := r.get(third.ID).OriginRef; !ref.StatusIsText || ref.StatusMessageID != 5 {
		t.Fatalf("origin %+v", ref)
	}
}

func TestDownloadedAt(t *testing.T) {
	r := newRig(t)
	r.start()
	if _, err := r.svc.DownloadedAt(bg, album("album-1")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("before: %v", err)
	}
	r.queue(QueueRequest{Ref: album("album-1")})
	r.h.Drain()
	at, err := r.svc.DownloadedAt(bg, album("album-1"))
	if err != nil || !at.Equal(r.h.Clock.Now()) {
		t.Fatalf("at %v, %v", at, err)
	}
}

// A deleted folder's items download again, and the folders beside it keep
// theirs, even one whose name starts the same.
func TestForgetADeletedFolder(t *testing.T) {
	r := newRig(t)
	rows := []Downloaded{
		{Ref: album("album-1"), Path: "qobuz/Album"},
		{Ref: source.Ref{Source: "fake", Kind: source.KindTrack, ID: "track-1"}, Path: "qobuz/Album/CD 2"},
		{Ref: album("album-2"), Path: "qobuz/Album Two"},
		{Ref: album("album-3"), Path: "qobuz/Other"},
	}
	if _, err := r.svc.ImportDownloaded(bg, rows); err != nil {
		t.Fatal(err)
	}
	n, err := r.svc.Forget(bg, "qobuz/Album")
	if err != nil || n != 2 {
		t.Fatalf("forgot %d: %v", n, err)
	}
	for i, want := range []bool{false, false, true, true} {
		if got, _ := r.svc.IsDownloaded(bg, rows[i].Ref); got != want {
			t.Errorf("%s: downloaded %v, want %v", rows[i].Path, got, want)
		}
	}
	if d := r.queue(QueueRequest{Ref: album("album-1")}); d.Status == Skipped {
		t.Errorf("the deleted album was skipped: %+v", d)
	}
	if n, _ := r.svc.Forget(bg, "."); n != 0 {
		t.Errorf("the music folder itself forgot %d", n)
	}
}
