package downloads

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/tikhonp/shelf/internal/source"
)

// ProgressEvery is how often a running download's progress reaches SQLite.
// The hub hears every change; the table only has to be right after a reload.
const ProgressEvery = time.Second

// progress is the Reporter a download's fetch step hands the source. Every
// change goes to the hub at once; the table gets the latest state once a
// second and when fetch ends.
type progress struct {
	s *Service
	d Download
	// ctx is the fetch step's context. A track that fails after it ended was
	// cut off.
	ctx context.Context
	log source.Logger
	// write stores a snapshot; tests count the calls.
	write func(ctx context.Context, snap snapshot) error

	mu        sync.Mutex
	tracks    map[int]*Track
	replanned bool
	dirty     map[int]bool
	changed   bool
	current   string
}

// snapshot is what one write stores.
type snapshot struct {
	d         Download
	tracks    []Track // every track when replanned, else the changed ones
	replanned bool
}

func newProgress(ctx context.Context, s *Service, d Download, tracks []Track, log source.Logger) *progress {
	p := &progress{s: s, d: d, ctx: ctx, log: log, tracks: map[int]*Track{}, dirty: map[int]bool{}}
	for i := range tracks {
		t := tracks[i]
		p.tracks[t.Position] = &t
	}
	p.write = p.store
	return p
}

// run writes a changed state on every tick until the returned stop is
// called, which writes the last state. tick nil means once a second.
func (p *progress) run(ctx context.Context, tick <-chan time.Time) (stop func() error) {
	// The writes outlive a canceled step. The files the source wrote must be
	// on record.
	bg := context.WithoutCancel(ctx)
	var ticker *time.Ticker
	if tick == nil {
		ticker = time.NewTicker(ProgressEvery)
		tick = ticker.C
	}
	quit, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-quit:
				return
			case <-tick:
				if err := p.flush(bg); err != nil {
					p.s.log.Error("downloads: progress", "download", p.d.ID, "error", err)
				}
			}
		}
	}()
	return func() error {
		close(quit)
		<-done
		if ticker != nil {
			ticker.Stop()
		}
		return p.flush(bg)
	}
}

func (p *progress) flush(ctx context.Context) error {
	p.mu.Lock()
	if !p.changed {
		p.mu.Unlock()
		return nil
	}
	snap := snapshot{d: p.d, replanned: p.replanned}
	for pos, t := range p.tracks {
		if p.replanned || p.dirty[pos] {
			snap.tracks = append(snap.tracks, *t)
		}
	}
	p.changed, p.replanned, p.dirty = false, false, map[int]bool{}
	p.mu.Unlock()
	slices.SortFunc(snap.tracks, func(a, b Track) int { return a.Position - b.Position })
	if err := p.write(ctx, snap); err != nil {
		// Try again on the next tick.
		p.mu.Lock()
		p.changed = true
		p.replanned = p.replanned || snap.replanned
		for _, t := range snap.tracks {
			p.dirty[t.Position] = true
		}
		p.mu.Unlock()
		return err
	}
	return nil
}

func (p *progress) store(ctx context.Context, snap snapshot) error {
	var parent Download
	err := p.s.db.Write(ctx, func(tx *sqlx.Tx) error {
		if snap.replanned {
			if _, err := tx.ExecContext(ctx, `DELETE FROM download_tracks WHERE download_id = ?`, snap.d.ID); err != nil {
				return err
			}
		}
		for _, t := range snap.tracks {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO download_tracks (download_id, position, disc, item_id, title, artist, isrc, status, path,
					codec, bit_depth, sample_rate, size, error)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
				ON CONFLICT (download_id, position) DO UPDATE SET status = excluded.status, path = excluded.path,
					codec = excluded.codec, bit_depth = excluded.bit_depth, sample_rate = excluded.sample_rate,
					size = excluded.size, error = excluded.error`,
				snap.d.ID, t.Position, max(t.Disc, 1), t.ItemID, t.Title, t.Artist, t.ISRC, t.Status, t.Path, t.Codec,
				nullable(t.BitDepth), nullable(t.SampleRate), nullable(t.Size), cut(t.Error, 4000)); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE downloads SET tracks_total = ?, tracks_done = ?, tracks_failed = ?, bytes = ? WHERE id = ?`,
			snap.d.TracksTotal, snap.d.TracksDone, snap.d.TracksFailed, snap.d.Bytes, snap.d.ID); err != nil {
			return err
		}
		if snap.d.ParentID != 0 {
			var err error
			parent, err = p.s.refreshParent(ctx, tx, snap.d.ParentID, false)
			return err
		}
		return nil
	})
	if err == nil && parent.ID != 0 {
		p.s.publish(parent, "")
	}
	return err
}

// changedLocked recounts the download and sends the event. p.mu is held.
func (p *progress) changedLocked() {
	p.changed = true
	done, failed, bytes := 0, 0, int64(0)
	for _, t := range p.tracks {
		switch t.Status {
		case "done", "skipped":
			done++
		case "failed":
			failed++
		}
		bytes += t.Size
	}
	p.d.TracksTotal, p.d.TracksDone, p.d.TracksFailed, p.d.Bytes = len(p.tracks), done, failed, bytes
	p.s.publish(p.d, p.current)
}

func (p *progress) track(pos int) *Track {
	t, ok := p.tracks[pos]
	if !ok {
		// A track the plan didn't list: keep it rather than lose its file.
		t = &Track{Position: pos, Disc: 1, Status: "pending"}
		p.tracks[pos] = t
	}
	p.dirty[pos] = true
	return t
}

// Plan replaces the tracks with what the source is about to fetch.
func (p *progress) Plan(tracks []source.Track) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.tracks = map[int]*Track{}
	for i, t := range tracks {
		pos := t.Position
		if pos <= 0 {
			pos = i + 1
		}
		p.tracks[pos] = &Track{Position: pos, Disc: max(t.Disc, 1), ItemID: t.ID, Title: t.Title, Artist: t.Artist,
			ISRC: t.ISRC, Status: "pending"}
	}
	p.replanned = true
	p.changedLocked()
}

// Start marks a track running.
func (p *progress) Start(pos int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	t := p.track(pos)
	t.Status, t.Error = "running", ""
	p.current = t.Title
	p.changedLocked()
}

// Progress records the bytes written of a track.
func (p *progress) Progress(pos int, written int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.track(pos).Size = written
	p.changedLocked()
}

// Done records how a track ended.
func (p *progress) Done(pos int, r source.TrackResult) {
	p.mu.Lock()
	defer p.mu.Unlock()
	t := p.track(pos)
	switch {
	case r.Err != nil && p.ctx.Err() != nil && t.Status != "failed":
		// Sources end what a cancel or a shutdown cut off as failed, and
		// nothing went wrong with those tracks.
		t.Status, t.Error = "pending", ""
	case r.Err != nil:
		t.Status, t.Error = "failed", r.Err.Error()
		p.log.Warn("Track %d, %s: %v", pos, t.Title, r.Err)
	case r.Skipped:
		t.Status, t.Error = "skipped", ""
	default:
		t.Status, t.Error = "done", ""
	}
	if r.Path != "" {
		t.Path = r.Path
	}
	if !r.Quality.IsZero() {
		t.Codec, t.BitDepth, t.SampleRate = r.Quality.Codec, r.Quality.BitDepth, r.Quality.SampleRate
	}
	if r.Size > 0 {
		t.Size = r.Size
	}
	if p.current == t.Title {
		p.current = ""
	}
	p.changedLocked()
}

// cutOff puts back to pending the tracks a canceled or stopped step left
// running. A source whose tool was killed never reports them to Done.
func (p *progress) cutOff() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for pos, t := range p.tracks {
		if t.Status == "running" {
			t.Status, t.Error = "pending", ""
			p.dirty[pos] = true
		}
	}
	p.current = ""
	p.changedLocked()
}

// Log is the job's log.
func (p *progress) Log() source.Logger { return p.log }
