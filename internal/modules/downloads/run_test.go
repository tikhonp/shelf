package downloads

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tikhonp/shelf/internal/platform/jobs"
	"github.com/tikhonp/shelf/internal/source"
)

type nopLog struct{}

func (nopLog) Debug(string, ...any) {}
func (nopLog) Info(string, ...any)  {}
func (nopLog) Warn(string, ...any)  {}
func (nopLog) Error(string, ...any) {}

func TestProgressThrottled(t *testing.T) {
	r := newRig(t)
	d := r.queue(QueueRequest{Ref: album("album-1")})
	// Fewer events than the hub buffers, so the test needn't read them as
	// they come.
	events, stopEvents := r.hub.Subscribe(Topic)

	p := newProgress(r.svc, d, nil, nopLog{})
	var writes atomic.Int32
	store := p.write
	p.write = func(ctx context.Context, s snapshot) error { writes.Add(1); return store(ctx, s) }
	tick := make(chan time.Time)
	stop := p.run(bg, tick)

	p.Plan([]source.Track{{ID: "a", Position: 1, Title: "One"}, {ID: "b", Position: 2, Title: "Two"}})
	for i := range 20 {
		p.Start(1)
		p.Progress(1, int64(i*100))
	}
	p.Done(1, source.TrackResult{Path: "fake/x/01.flac", Size: 5000, Quality: source.Quality{Codec: "flac", BitDepth: 24, SampleRate: 96000}})
	if n := writes.Load(); n != 0 {
		t.Fatalf("%d writes before the first tick", n)
	}
	tick <- time.Time{} // a second passes
	tick <- time.Time{} // and another, with nothing new
	h := r.get(d.ID)
	if n := writes.Load(); n != 1 {
		t.Fatalf("%d writes, want one per second with changes", n)
	}
	if h.TracksDone != 1 || h.TracksTotal != 2 || h.Bytes != 5000 || len(h.Tracks) != 2 || h.Tracks[0].Path != "fake/x/01.flac" ||
		h.Tracks[0].BitDepth != 24 || h.Tracks[1].Status != "pending" {
		t.Fatalf("stored %+v", h)
	}
	p.Start(2)
	if err := stop(); err != nil {
		t.Fatal(err)
	}
	if n := writes.Load(); n != 2 {
		t.Fatalf("%d writes: stop must write the last state", n)
	}
	if st := r.get(d.ID).Tracks[1].Status; st != "running" {
		t.Fatalf("track 2 is %s", st)
	}
	// Every change reached the hub: the plan, 20 starts, 20 byte counts,
	// the end of track 1 and the start of track 2.
	stopEvents()
	var got []Event
	for e := range events {
		got = append(got, e.Data.(Event))
	}
	if len(got) != 43 {
		t.Fatalf("%d events", len(got))
	}
	if last := got[42]; last.Current != "Two" || last.TracksDone != 1 || last.ID != d.ID {
		t.Fatalf("last event %+v", last)
	}
}

func TestFinishDone(t *testing.T) {
	r := newRig(t)
	r.start()
	d := r.queue(QueueRequest{Ref: album("album-1")})
	r.h.Drain()
	det := r.get(d.ID)
	if det.Status != Done || det.TracksDone != 3 || det.TracksTotal != 3 || det.TracksFailed != 0 || det.Bytes != 3*1024 ||
		det.Title != "Low Tide Hymns" || det.Artist != "Northern Static" || det.CoverURL == "" || det.StartedAt.IsZero() ||
		det.FinishedAt.IsZero() || det.Folder != "fake/Northern Static - Low Tide Hymns" ||
		det.ProvenancePath != "fake/Northern Static - Low Tide Hymns/shelf.txt" {
		t.Fatalf("%+v", det.Download)
	}
	for _, tr := range det.Tracks {
		if tr.Status != "done" || tr.Path == "" || tr.Codec != "flac" || tr.SampleRate != 96000 || tr.ISRC == "" {
			t.Fatalf("%+v", tr)
		}
	}
	var item struct {
		DownloadID int64  `db:"download_id"`
		Quality    string `db:"quality"`
		Path       string `db:"path"`
		Imported   bool   `db:"imported"`
	}
	if err := r.h.DB.R.Get(&item, `SELECT download_id, quality, path, imported FROM downloaded_items WHERE source = 'fake' AND kind = 'album' AND item_id = 'album-1'`); err != nil {
		t.Fatal(err)
	}
	if item.DownloadID != d.ID || item.Quality != "FLAC 24/96" || item.Path != det.Folder || item.Imported {
		t.Fatalf("%+v", item)
	}
	txt, err := os.ReadFile(filepath.Join(r.cfg.MusicDir, det.ProvenancePath))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(txt), "Music Shelf download 1\n") || !strings.Contains(string(txt), "Delivered: FLAC 24-bit 96 kHz\n") {
		t.Fatalf("shelf.txt:\n%s", txt)
	}
	if fi, _ := os.Stat(filepath.Join(r.cfg.MusicDir, det.ProvenancePath)); fi.Mode().Perm() != 0o644 {
		t.Fatalf("mode %v", fi.Mode().Perm())
	}
	if st := r.job(d.JobID).State; st != jobs.Succeeded {
		t.Fatalf("job %s", st)
	}
}

func TestFinishPartial(t *testing.T) {
	r := newRig(t)
	r.start()
	d := r.queue(QueueRequest{Ref: album("album-6-fail")})
	r.h.Drain()
	det := r.get(d.ID)
	if det.Status != Partial || det.TracksDone != 2 || det.TracksFailed != 1 || det.Tracks[1].Status != "failed" ||
		det.Tracks[1].Error != "fake failure" {
		t.Fatalf("%+v %+v", det.Download, det.Tracks)
	}
	txt, err := os.ReadFile(filepath.Join(r.cfg.MusicDir, det.ProvenancePath))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(txt), "  2  Undertow  track album-6-fail-t2  ISRC XX0000000002  failed\n") {
		t.Fatalf("shelf.txt:\n%s", txt)
	}
	if n := r.count(`SELECT count(*) FROM downloaded_items`); n != 0 {
		t.Fatal("a partial download was recorded as downloaded")
	}
}

func TestFinishFailedWithoutFiles(t *testing.T) {
	r := newRig(t)
	r.src.Hook = func(context.Context, source.Ref, int) error { return errors.New("the CDN refused") }
	r.start()
	d := r.queue(QueueRequest{Ref: album("album-1")})
	r.h.Drain()
	det := r.get(d.ID)
	if det.Status != Failed || det.Error != "Download failed: the CDN refused" || det.TracksFailed != 3 || det.ProvenancePath != "" {
		t.Fatalf("%+v", det.Download)
	}
	if _, err := os.Stat(filepath.Join(r.cfg.MusicDir, "fake", "Northern Static - Low Tide Hymns", ProvenanceFile)); !os.IsNotExist(err) {
		t.Fatalf("shelf.txt written: %v", err)
	}
	if n := r.count(`SELECT count(*) FROM downloaded_items`); n != 0 {
		t.Fatal("a failed download was recorded as downloaded")
	}
	// A lookup that fails ends the same way.
	d = r.queue(QueueRequest{Ref: album("missing-7")})
	r.h.Drain()
	if det := r.get(d.ID); det.Status != Failed || det.Error != "Fake has no such item" {
		t.Fatalf("%+v", det.Download)
	}
}

func TestCancelQueued(t *testing.T) {
	r := newRig(t)
	var ran atomic.Int32
	r.src.Hook = func(context.Context, source.Ref, int) error { ran.Add(1); return nil }
	d := r.queue(QueueRequest{Ref: album("album-1")})
	if rec := r.do("POST", "/api/downloads/1/cancel", nil); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"status":"canceled"`) {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if st := r.job(d.JobID).State; st != jobs.Cancelled {
		t.Fatalf("job %s", st)
	}
	r.start()
	r.h.Drain()
	if ran.Load() != 0 || r.get(d.ID).Status != Canceled {
		t.Fatalf("ran %d, status %s", ran.Load(), r.get(d.ID).Status)
	}
	if err := r.svc.Cancel(bg, d.ID, "web"); !errors.Is(err, ErrNotActive) {
		t.Fatalf("second cancel: %v", err)
	}
	if rec := r.do("POST", "/api/downloads/1/cancel", nil); rec.Code != 409 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
}

func TestCancelRunning(t *testing.T) {
	r := newRig(t)
	reached := make(chan struct{})
	var cause atomic.Value
	r.src.Hook = func(ctx context.Context, _ source.Ref, pos int) error {
		if pos != 2 {
			return nil
		}
		close(reached)
		<-ctx.Done()
		cause.Store(ctx.Err())
		return ctx.Err()
	}
	r.start()
	d := r.queue(QueueRequest{Ref: album("album-1")})
	<-reached
	if err := r.svc.Cancel(bg, d.ID, "web"); err != nil {
		t.Fatal(err)
	}
	r.waitStatus(d.ID, Canceled)
	r.h.Drain()
	if err, _ := cause.Load().(error); !errors.Is(err, context.Canceled) {
		t.Fatalf("the source's context ended with %v", err)
	}
	det := r.get(d.ID)
	if det.Tracks[0].Status != "done" || det.FinishedAt.IsZero() {
		t.Fatalf("%+v", det)
	}
	if j := r.job(d.JobID); j.State != jobs.Cancelled || j.CancelRequestedBy != "web" {
		t.Fatalf("%+v", j)
	}
}

func TestRetryFailed(t *testing.T) {
	r := newRig(t)
	var fail atomic.Bool
	fail.Store(true)
	r.src.Hook = func(context.Context, source.Ref, int) error {
		if fail.Load() {
			return errors.New("network down")
		}
		return nil
	}
	r.start()
	d := r.queue(QueueRequest{Ref: album("album-1")})
	r.h.Drain()
	if st := r.get(d.ID).Status; st != Failed {
		t.Fatalf("status %s", st)
	}
	oldJob := d.JobID
	fail.Store(false)
	rec := r.do("POST", "/api/downloads/1/retry", nil)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"status":"queued"`) || !strings.Contains(rec.Body.String(), `"error":""`) {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	r.h.Drain()
	det := r.get(d.ID)
	if det.Status != Done || det.JobID == oldJob || det.TracksDone != 3 {
		t.Fatalf("%+v", det.Download)
	}
	old, err := r.h.Sys.LogAfter(bg, oldJob, 0, 100)
	if err != nil || len(old) == 0 {
		t.Fatalf("the old job's log is gone: %v", err)
	}
	found := false
	for _, l := range old {
		found = found || strings.Contains(l.Text, "network down")
	}
	if !found {
		t.Fatal("the old log lost the failure")
	}
	if j := r.job(det.JobID); j.CreatedBy != "web" || j.ResourceKey != "item:fake:album:album-1" {
		t.Fatalf("%+v", j)
	}
}

func TestRetryDoneRefused(t *testing.T) {
	r := newRig(t)
	r.start()
	d := r.queue(QueueRequest{Ref: album("album-1")})
	r.h.Drain()
	if _, err := r.svc.Retry(bg, d.ID, "web"); !errors.Is(err, ErrNotRetryable) {
		t.Fatalf("err = %v", err)
	}
	if rec := r.do("POST", "/api/downloads/1/retry", nil); rec.Code != 409 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if rec := r.do("POST", "/api/downloads/99/retry", nil); rec.Code != 404 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
}

func TestResumeAfterRestart(t *testing.T) {
	r := newRig(t)
	var hold atomic.Bool
	hold.Store(true)
	reached := make(chan struct{}, 1)
	r.src.Hook = func(ctx context.Context, _ source.Ref, pos int) error {
		if pos == 2 && hold.Load() {
			reached <- struct{}{}
			<-ctx.Done() // the shutdown cuts the step
			return ctx.Err()
		}
		return nil
	}
	r.start()
	d := r.queue(QueueRequest{Ref: album("album-1")})
	<-reached
	hold.Store(false)
	r.restart()
	r.h.Drain()
	det := r.get(d.ID)
	if det.Status != Done || det.TracksDone != 3 {
		t.Fatalf("%+v", det.Download)
	}
	if st := []string{det.Tracks[0].Status, det.Tracks[1].Status, det.Tracks[2].Status}; st[0] != "skipped" || st[1] != "done" || st[2] != "done" {
		t.Fatalf("track states %v", st)
	}
	if n := r.src.ItemCalls(); n != 1 {
		t.Fatalf("resolve ran %d times, want once", n)
	}
	lines, _ := r.h.Sys.LogAfter(bg, d.JobID, 0, 500)
	var texts []string
	for _, l := range lines {
		texts = append(texts, l.Text)
	}
	if !strings.Contains(strings.Join(texts, "\n"), "Interrupted by restart") {
		t.Fatalf("log: %q", texts)
	}
	if j := r.job(d.JobID); j.Attempt != 1 {
		t.Fatalf("attempt %d", j.Attempt)
	}
}

func artist(id string) source.Ref { return source.Ref{Source: "fake", Kind: source.KindArtist, ID: id} }

func TestArtistExpands(t *testing.T) {
	r := newRig(t)
	r.start()
	d := r.queue(QueueRequest{Ref: artist("artist-1"), Format: "fake:mp3"})
	r.h.Drain()
	det := r.get(d.ID)
	if det.Status != Done || len(det.Children) != 2 || len(det.Tracks) != 0 || det.Title != "Northern Static" {
		t.Fatalf("%+v", det)
	}
	for i, c := range det.Children {
		if c.ParentID != d.ID || c.Status != Done || c.Ref != album([]string{"album-1", "album-2"}[i]) || c.Format != "fake:mp3" ||
			c.Origin != OriginWeb {
			t.Fatalf("%+v", c)
		}
		if j := r.job(c.JobID); j.CreatedBy != "job:"+itoa(d.JobID) {
			t.Fatalf("child created by %q", j.CreatedBy)
		}
	}
	if det.TracksTotal != 6 || det.TracksDone != 6 || det.Bytes != 6*1024 {
		t.Fatalf("counts %+v", det.Download)
	}
	// The list shows the artist, not its albums.
	list, err := r.svc.List(bg, Filter{})
	if err != nil || len(list) != 1 || list[0].ID != d.ID {
		t.Fatalf("%+v %v", list, err)
	}
	// An artist download writes no shelf.txt and no downloaded_items row
	// itself; each album does.
	if n := r.count(`SELECT count(*) FROM downloaded_items`); n != 2 {
		t.Fatalf("%d rows", n)
	}
	if det.ProvenancePath != "" {
		t.Fatal(det.ProvenancePath)
	}
}

func TestParentFollowsChildren(t *testing.T) {
	r := newRig(t)
	g := newGate()
	r.src.Hook = func(ctx context.Context, ref source.Ref, pos int) error {
		if ref.ID == "pa-a1" && pos == 1 {
			return g.wait(ctx)
		}
		if ref.ID == "pa-a2" {
			return errors.New("refused")
		}
		return nil
	}
	r.start()
	d := r.queue(QueueRequest{Ref: artist("pa")})
	g.waitReached(t)
	det := r.get(d.ID)
	if det.Status != Running || len(det.Children) != 2 || det.TracksTotal != 3 {
		t.Fatalf("while the first album runs: %+v", det.Download)
	}
	close(g.release)
	r.h.Drain()
	det = r.get(d.ID)
	if det.Status != Partial || det.TracksDone != 3 || det.TracksFailed != 3 || det.TracksTotal != 6 || det.FinishedAt.IsZero() {
		t.Fatalf("%+v", det.Download)
	}
	if det.Children[0].Status != Done || det.Children[1].Status != Failed {
		t.Fatalf("%+v", det.Children)
	}
	// Retry of the artist retries the album that failed.
	r.src.Hook = nil
	rd, err := r.svc.Retry(bg, d.ID, "web")
	if err != nil || rd.Status != Running {
		t.Fatalf("%+v %v", rd, err)
	}
	r.h.Drain()
	if det := r.get(d.ID); det.Status != Done || det.TracksDone != 6 {
		t.Fatalf("after retry %+v", det.Download)
	}
	if _, err := r.svc.Retry(bg, det.Children[0].ID, "web"); !errors.Is(err, ErrNotRetryable) {
		t.Fatalf("retry of a child: %v", err)
	}
}

func TestArtistWithoutAlbums(t *testing.T) {
	r := newRig(t)
	r.start()
	d := r.queue(QueueRequest{Ref: artist("artist-3-empty")})
	r.h.Drain()
	det := r.get(d.ID)
	if det.Status != Done || det.Error != "nothing to download" || len(det.Children) != 0 || det.FinishedAt.IsZero() {
		t.Fatalf("%+v", det)
	}
}

type countingNotifier struct {
	mu    sync.Mutex
	calls []Download
}

func (n *countingNotifier) Notify(_ context.Context, d Download) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.calls = append(n.calls, d)
	return nil
}

func (n *countingNotifier) count() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return len(n.calls)
}

func TestNotifyOnce(t *testing.T) {
	r := newRig(t)
	n := &countingNotifier{}
	r.svc.SetNotifier(n)
	r.start()
	tg := QueueRequest{Ref: album("album-1"), Origin: OriginTelegram, OriginRef: OriginRef{ChatID: 1, UserID: 2}}
	d := r.queue(tg)
	web := r.queue(QueueRequest{Ref: album("album-2")})
	r.h.Drain()
	if n.count() != 1 || n.calls[0].ID != d.ID || n.calls[0].Status != Done || !r.get(d.ID).Notified || r.get(web.ID).Notified {
		t.Fatalf("calls %+v", n.calls)
	}

	// After a restart the step may run again; the flag stops a second call.
	r.restart()
	r.svc.SetNotifier(n)
	if _, err := r.h.Sys.EnqueueNow(bg, jobs.Request{Type: NotifyJobType, Payload: payload{DownloadID: d.ID}, CreatedBy: "cli"}); err != nil {
		t.Fatal(err)
	}
	r.h.Drain()
	if n.count() != 1 {
		t.Fatalf("%d calls", n.count())
	}

	// A canceled download never reaches its notify step; a notify job
	// gives it the final text.
	r.src.Hook = func(ctx context.Context, _ source.Ref, _ int) error { <-ctx.Done(); return ctx.Err() }
	c := r.queue(QueueRequest{Ref: album("album-3"), Origin: OriginTelegram, OriginRef: OriginRef{ChatID: 1, UserID: 2}})
	r.h.WaitFor("running", func() bool { return r.get(c.ID).Status == Running })
	if err := r.svc.Cancel(bg, c.ID, "telegram:2"); err != nil {
		t.Fatal(err)
	}
	r.h.Drain()
	if n.count() != 2 || n.calls[1].ID != c.ID || n.calls[1].Status != Canceled {
		t.Fatalf("calls %+v", n.calls)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
