package downloads

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/tikhonp/shelf/internal/platform/db"
	"github.com/tikhonp/shelf/internal/platform/jobs"
	"github.com/tikhonp/shelf/internal/source"
)

// Job types.
const (
	JobType       = "downloads.download"
	NotifyJobType = "downloads.notify"
)

// payload is the download job's payload. resolve and fetch add what the
// later steps need, so a resumed job has it without asking the source again.
type payload struct {
	DownloadID int64       `json:"download_id"`
	Item       *itemFacts  `json:"item,omitempty"`
	Result     *resultInfo `json:"result,omitempty"`
}

// itemFacts are the catalog facts shelf.txt records.
type itemFacts struct {
	URL         string `json:"url,omitempty"`
	Label       string `json:"label,omitempty"`
	ReleaseDate string `json:"release_date,omitempty"`
	UPC         string `json:"upc,omitempty"`
	ISRC        string `json:"isrc,omitempty"`
}

// resultInfo is what fetch learned from the source. The tracks are in
// download_tracks, so a large playlist doesn't outgrow the payload.
type resultInfo struct {
	Folders   []string `json:"folders,omitempty"`
	Tool      string   `json:"tool,omitempty"`
	TrackNoun string   `json:"track_noun,omitempty"`
}

func (s *Service) jobTypes() []jobs.Type {
	return []jobs.Type{
		{
			// Queue is the default; Queue picks the source's queue per job.
			Name: JobType, Queue: jobs.QueueSystem, MaxAttempts: 1,
			Steps: []jobs.Step{
				{Name: "resolve", Run: s.resolve},
				{Name: "fetch", Run: s.fetch},
				{Name: "finish", Run: s.finish},
				{Name: "notify", Run: s.notifyStep},
			},
			OnFailed: func(ctx context.Context, tx *sqlx.Tx, j jobs.Info, err error) error {
				return s.endEarly(ctx, tx, j, Failed, err.Error())
			},
			OnCancelled: func(ctx context.Context, tx *sqlx.Tx, j jobs.Info, _ string) error {
				return s.endEarly(ctx, tx, j, Canceled, "")
			},
		},
		{
			// A download that ends without reaching its notify step, because
			// it was canceled or its job failed, notifies through this job.
			Name: NotifyJobType, Queue: jobs.QueueSystem, MaxAttempts: 1,
			Steps: []jobs.Step{{Name: "notify", Run: s.notifyStep}},
		},
	}
}

func (s *Service) runDownload(r *jobs.Run) (payload, Download, error) {
	var p payload
	if err := r.Payload(&p); err != nil {
		return p, Download{}, jobs.Permanent(err)
	}
	d, err := s.load(context.Background(), p.DownloadID)
	return p, d, err
}

// fail records why the download can't go on. It stays running, and the
// later steps see the error and only finish it. Errors in shelf's own
// database fail the job instead.
func (s *Service) fail(ctx context.Context, r *jobs.Run, d *Download, msg string) error {
	r.Log().Error("%s", msg)
	d.Error = cut(msg, 4000)
	err := s.db.Write(ctx, func(tx *sqlx.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE downloads SET error = ? WHERE id = ?`, d.Error, d.ID)
		return err
	})
	if err == nil {
		s.publish(*d, "")
	}
	return err
}

// resolve fetches the item and stores its title, artist, cover and tracks.
// An artist or label creates one child download per album and ends there.
func (s *Service) resolve(ctx context.Context, r *jobs.Run) error {
	p, d, err := s.runDownload(r)
	if err != nil {
		return err
	}
	if d.Status.Final() {
		return nil
	}
	err = s.db.Write(ctx, func(tx *sqlx.Tx) error {
		_, err := tx.ExecContext(ctx, `
			UPDATE downloads SET status = 'running', started_at = COALESCE(started_at, ?) WHERE id = ?`, s.now(), d.ID)
		return err
	})
	if err != nil {
		return err
	}
	d.Status = Running
	s.publish(d, "")

	src, ok := s.sources.Enabled(d.Ref.Source)
	if !ok {
		return s.fail(ctx, r, &d, fmt.Sprintf("The source %q is not set up in Shelf", d.Ref.Source))
	}
	res, ok := src.(source.Resolver)
	if !ok {
		if d.Ref.Kind.Collection() {
			return s.fail(ctx, r, &d, src.Name()+" can't list an artist's or label's albums")
		}
		return nil
	}
	r.Log().Info("Looking up %s in %s", d.Ref, src.Name())
	item, err := res.Item(ctx, d.Ref)
	if err != nil {
		if ctx.Err() != nil {
			return err
		}
		if errors.Is(err, source.ErrNotFound) {
			return s.fail(ctx, r, &d, src.Name()+" has no such item")
		}
		return s.fail(ctx, r, &d, "Lookup failed: "+err.Error())
	}
	p.Item = &itemFacts{URL: item.URL, Label: item.Label, ReleaseDate: item.ReleaseDate, UPC: item.UPC, ISRC: item.ISRC}
	if err := r.SavePayload(ctx, p); err != nil {
		return err
	}
	artist := item.Artist
	if d.Ref.Kind.Collection() {
		return s.expand(ctx, r, d, item)
	}

	tracks := item.Tracks
	if d.Ref.Kind == source.KindTrack {
		tracks = []source.Track{{ID: item.Ref.ID, Position: 1, Disc: 1, Title: item.Title, Artist: item.Artist, ISRC: item.ISRC}}
	}
	r.Log().Info("%s by %s, %d tracks", item.Title, artist, len(tracks))
	var parent Download
	err = s.db.Write(ctx, func(tx *sqlx.Tx) error {
		if _, err := tx.ExecContext(ctx, `
			UPDATE downloads SET title = ?, artist = ?, cover_url = ?, tracks_total = ? WHERE id = ?`,
			item.Title, artist, item.CoverURL, len(tracks), d.ID); err != nil {
			return err
		}
		if err := replaceTracks(ctx, tx, d.ID, tracks); err != nil {
			return err
		}
		if d.ParentID != 0 {
			parent, err = s.refreshParent(ctx, tx, d.ParentID, false)
		}
		return err
	})
	if err != nil {
		return err
	}
	d.Title, d.Artist, d.CoverURL, d.TracksTotal = item.Title, artist, item.CoverURL, len(tracks)
	s.publish(d, "")
	if parent.ID != 0 {
		s.publish(parent, "")
	}
	return nil
}

// replaceTracks stores the planned tracks, all pending.
func replaceTracks(ctx context.Context, tx *sqlx.Tx, id int64, tracks []source.Track) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM download_tracks WHERE download_id = ?`, id); err != nil {
		return err
	}
	for i, t := range tracks {
		pos := t.Position
		if pos <= 0 {
			pos = i + 1
		}
		disc := t.Disc
		if disc <= 0 {
			disc = 1
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO download_tracks (download_id, position, disc, item_id, title, artist, isrc, status)
			VALUES (?, ?, ?, ?, ?, ?, ?, 'pending')
			ON CONFLICT (download_id, position) DO NOTHING`,
			id, pos, disc, t.ID, t.Title, t.Artist, t.ISRC); err != nil {
			return err
		}
	}
	return nil
}

// expand creates one child download per album of an artist or label, once:
// a resumed resolve finds the children there and adds none.
func (s *Service) expand(ctx context.Context, r *jobs.Run, d Download, item source.Item) error {
	seen := map[string]bool{}
	var albums []source.Item
	for _, sec := range item.Sections {
		for _, it := range sec.Items {
			if it.Ref.Kind.Collection() || seen[it.Ref.ID] {
				continue
			}
			seen[it.Ref.ID] = true
			if it.Ref.Source == "" {
				it.Ref.Source = d.Ref.Source
			}
			albums = append(albums, it)
		}
	}
	r.Log().Info("%s: %d albums", item.Title, len(albums))
	var created []int64
	err := s.db.Write(ctx, func(tx *sqlx.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE downloads SET title = ?, artist = ?, cover_url = ? WHERE id = ?`,
			item.Title, item.Artist, item.CoverURL, d.ID); err != nil {
			return err
		}
		var n int
		if err := tx.GetContext(ctx, &n, `SELECT count(*) FROM downloads WHERE parent_id = ?`, d.ID); err != nil || n > 0 {
			return err
		}
		for _, a := range albums {
			id, err := s.create(ctx, tx, newDownload{
				ref: a.Ref, format: d.Format, force: d.Force, origin: d.Origin, parentID: d.ID,
				createdBy: r.Info().Actor(), title: a.Title, artist: a.Artist, cover: a.CoverURL,
			})
			if err != nil {
				return err
			}
			created = append(created, id)
		}
		_, err := s.refreshParent(ctx, tx, d.ID, true)
		return err
	})
	if err != nil {
		return err
	}
	s.jobs.Kick()
	for _, id := range append(created, d.ID) {
		if c, err := s.load(ctx, id); err == nil {
			s.publish(c, "")
		}
	}
	return nil
}

// fetch calls the source's Download. A source error fails the download, not
// the job, so finish still runs and records the files the source wrote.
// A context that ended, by cancel or shutdown, ends the step, so cancel
// marks the download canceled and a restart resumes here.
func (s *Service) fetch(ctx context.Context, r *jobs.Run) error {
	p, d, err := s.runDownload(r)
	if err != nil {
		return err
	}
	if d.Status.Final() || d.Error != "" || d.Ref.Kind.Collection() {
		return nil
	}
	src, ok := s.sources.Enabled(d.Ref.Source)
	if !ok {
		return s.fail(ctx, r, &d, fmt.Sprintf("The source %q is not set up in Shelf", d.Ref.Source))
	}
	dl, ok := src.(source.Downloader)
	if !ok {
		return s.fail(ctx, r, &d, src.Name()+" can't download")
	}
	tracks, err := s.tracks(ctx, s.db.R, d.ID)
	if err != nil {
		return err
	}
	pr := newProgress(ctx, s, d, tracks, r.Log())
	stop := pr.run(ctx, nil)
	res, derr := dl.Download(ctx, source.DownloadRequest{
		DownloadID: d.ID, Ref: d.Ref, Format: d.Format, Force: d.Force, MusicDir: s.cfg.MusicDir,
	}, pr)
	s.workDone(d.ID)
	if ctx.Err() != nil {
		pr.cutOff()
	}
	if err := stop(); err != nil {
		return err
	}
	if derr != nil && ctx.Err() != nil {
		return derr
	}
	p.Result = &resultInfo{Folders: res.Folders, Tool: res.Tool, TrackNoun: res.TrackNoun}
	if err := r.SavePayload(ctx, p); err != nil {
		return err
	}
	folder := ""
	if len(res.Folders) > 0 {
		folder = filepath.ToSlash(res.Folders[0])
	}
	err = s.db.Write(ctx, func(tx *sqlx.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE downloads SET folder = ? WHERE id = ?`, folder, d.ID)
		return err
	})
	if err != nil {
		return err
	}
	if derr != nil {
		return s.fail(ctx, r, &d, "Download failed: "+derr.Error())
	}
	return nil
}

// finish sets the result, writes shelf.txt into each release folder,
// records downloaded_items, updates the parent and asks for a scan.
func (s *Service) finish(ctx context.Context, r *jobs.Run) error {
	p, d, err := s.runDownload(r)
	if err != nil {
		return err
	}
	if d.Status.Final() {
		return nil
	}
	if d.Ref.Kind.Collection() {
		var parent Download
		err := s.db.Write(ctx, func(tx *sqlx.Tx) (err error) {
			parent, err = s.refreshParent(ctx, tx, d.ID, true)
			return err
		})
		if err == nil {
			s.publish(parent, "")
		}
		return err
	}

	tracks, err := s.tracks(ctx, s.db.R, d.ID)
	if err != nil {
		return err
	}
	done, failed := 0, 0
	for i := range tracks {
		switch tracks[i].Status {
		case "done", "skipped":
			done++
		case "pending", "running":
			tracks[i].Status, tracks[i].Error = "failed", "not downloaded"
			failed++
		default:
			failed++
		}
	}
	status := Done
	if d.Error != "" || failed > 0 {
		status = Failed
		if done > 0 {
			status = Partial
		}
	}
	finished := s.now()
	d.Status, d.TracksDone, d.TracksFailed, d.FinishedAt = status, done, failed, finished.Time

	var folders []string
	if p.Result != nil {
		folders = p.Result.Folders
	}
	if (status == Done || status == Partial) && len(folders) > 0 {
		block := s.provenance(d, p, tracks)
		for _, f := range folders {
			if err := AppendProvenance(filepath.Join(s.cfg.MusicDir, f), d.ID, block); err != nil {
				r.Log().Error("Could not write %s in %s: %v", ProvenanceFile, f, err)
				continue
			}
			r.Log().Info("Wrote %s in %s", ProvenanceFile, f)
		}
	}

	var parent Download
	err = s.db.Write(ctx, func(tx *sqlx.Tx) error {
		if _, err := tx.ExecContext(ctx, `
			UPDATE download_tracks SET status = 'failed', error = 'not downloaded'
			WHERE download_id = ? AND status IN ('pending', 'running')`, d.ID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE downloads SET status = ?, tracks_done = ?, tracks_failed = ?, finished_at = ? WHERE id = ?`,
			status, done, failed, finished, d.ID); err != nil {
			return err
		}
		if status == Done && d.Ref.Kind != source.KindArtist && d.Ref.Kind != source.KindLabel {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO downloaded_items (source, kind, item_id, download_id, quality, path, imported, at)
				VALUES (?, ?, ?, ?, ?, ?, 0, ?)
				ON CONFLICT (source, kind, item_id) DO UPDATE SET download_id = excluded.download_id,
					quality = excluded.quality, path = excluded.path, imported = 0, at = excluded.at`,
				d.Ref.Source, d.Ref.Kind, d.Ref.ID, d.ID, delivered(tracks).Short(), d.Folder, finished); err != nil {
				return err
			}
		}
		if d.ParentID != 0 {
			var err error
			parent, err = s.refreshParent(ctx, tx, d.ParentID, false)
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}
	r.Log().Info("Finished: %s, %d of %d tracks", status, done, len(tracks))
	s.publish(d, "")
	if parent.ID != 0 {
		s.publish(parent, "")
	}
	if status == Done || status == Partial {
		if _, sc := s.ports(); sc != nil {
			if err := sc.RequestScan(ctx); err != nil {
				r.Log().Warn("Could not ask for a library scan: %v", err)
			}
		}
	}
	return nil
}

// delivered is the highest quality among the files written.
func delivered(tracks []Track) source.Quality {
	var best source.Quality
	for _, t := range tracks {
		if (t.Status == "done" || t.Status == "skipped") && t.quality().Better(best) {
			best = t.quality()
		}
	}
	return best
}

// notifyStep gives a Telegram download its final message. The notified flag
// keeps a restart from sending it twice. The bot's send queue retries on its
// own, so a failure here is logged and not retried.
func (s *Service) notifyStep(ctx context.Context, r *jobs.Run) error {
	_, d, err := s.runDownload(r)
	if err != nil {
		return err
	}
	n, _ := s.ports()
	if n == nil || d.ParentID != 0 || d.Origin != OriginTelegram || d.Notified || !d.Status.Final() {
		return nil
	}
	if err := n.Notify(ctx, d); err != nil {
		if ctx.Err() != nil {
			return err
		}
		r.Log().Warn("Could not update the Telegram message: %v", err)
		return nil
	}
	return s.db.Write(ctx, func(tx *sqlx.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE downloads SET notified = 1 WHERE id = ?`, d.ID)
		return err
	})
}

// endEarly runs in the jobs system's transaction when a download's job is
// canceled or fails before finish. It must use tx: the write connection is
// already taken.
func (s *Service) endEarly(ctx context.Context, tx *sqlx.Tx, j jobs.Info, status Status, msg string) error {
	var id int64
	err := tx.GetContext(ctx, &id, `SELECT id FROM downloads WHERE job_id = ?`, j.ID)
	if err != nil {
		// An older job of a retried download: the download has moved on.
		return nil
	}
	d, err := loadWith(ctx, tx, id)
	if err != nil || d.Status.Final() {
		return err
	}
	if msg == "" {
		msg = d.Error
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE downloads SET status = ?, error = ?, finished_at = ? WHERE id = ?`,
		status, cut(msg, 4000), s.now(), id); err != nil {
		return err
	}
	d.Status, d.Error = status, cut(msg, 4000)
	if err := s.queueNotify(ctx, tx, d, j.Actor()); err != nil {
		return err
	}
	var parent Download
	if d.ParentID != 0 {
		if parent, err = s.refreshParent(ctx, tx, d.ParentID, false); err != nil {
			return err
		}
	}
	// This publishes before the commit. The event carries the new state, so
	// a reader never needs the row.
	s.publish(d, "")
	if parent.ID != 0 {
		s.publish(parent, "")
	}
	return nil
}

// queueNotify starts a notify job for a top-level Telegram download that
// ended without its own notify step.
func (s *Service) queueNotify(ctx context.Context, tx *sqlx.Tx, d Download, by string) error {
	if d.ParentID != 0 || d.Origin != OriginTelegram || d.Notified {
		return nil
	}
	_, err := s.jobs.Enqueue(ctx, tx, jobs.Request{Type: NotifyJobType, Payload: payload{DownloadID: d.ID}, CreatedBy: by})
	return err
}

// refreshParent sets an artist or label download's counts to the sums of
// its children's and its status from theirs: running while any is queued
// or running, then done when all are done or skipped, failed when all
// failed, canceled when all were canceled, and partial otherwise. A parent
// without children is done, with nothing to download. ownJob is set when
// the parent's own job calls, whose notify step follows; otherwise a parent
// that just ended gets a notify job.
func (s *Service) refreshParent(ctx context.Context, tx *sqlx.Tx, id int64, ownJob bool) (Download, error) {
	p, err := loadWith(ctx, tx, id)
	if err != nil {
		return p, err
	}
	var kids []struct {
		Status       string `db:"status"`
		TracksTotal  int    `db:"tracks_total"`
		TracksDone   int    `db:"tracks_done"`
		TracksFailed int    `db:"tracks_failed"`
		Bytes        int64  `db:"bytes"`
	}
	if err := tx.SelectContext(ctx, &kids, `
		SELECT status, tracks_total, tracks_done, tracks_failed, bytes FROM downloads WHERE parent_id = ?`, id); err != nil {
		return p, err
	}
	total, done, failed, bytes := 0, 0, 0, int64(0)
	count := map[Status]int{}
	for _, k := range kids {
		total, done, failed, bytes = total+k.TracksTotal, done+k.TracksDone, failed+k.TracksFailed, bytes+k.Bytes
		count[Status(k.Status)]++
	}
	n := len(kids)
	status, msg := Partial, p.Error
	switch {
	case n == 0:
		status, msg = Done, "nothing to download"
	case count[Queued]+count[Running] > 0:
		status = Running
	case count[Done]+count[Skipped] == n:
		status = Done
	case count[Failed] == n:
		status = Failed
	case count[Canceled] == n:
		status = Canceled
	}
	// A parent that was canceled or failed on its own stays that way.
	if p.Status.Final() && status == Running {
		status = p.Status
	}
	was := p.Status
	p.Status, p.Error, p.TracksTotal, p.TracksDone, p.TracksFailed, p.Bytes = status, msg, total, done, failed, bytes
	var finished any
	if status.Final() {
		// A parent that had ended keeps its time.
		if !was.Final() || p.FinishedAt.IsZero() {
			p.FinishedAt = s.now().Time
		}
		finished = db.At(p.FinishedAt)
	} else {
		p.FinishedAt = time.Time{}
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE downloads SET status = ?, error = ?, tracks_total = ?, tracks_done = ?, tracks_failed = ?, bytes = ?,
			finished_at = ?
		WHERE id = ?`, status, msg, total, done, failed, bytes, finished, id); err != nil {
		return p, err
	}
	if status.Final() && !was.Final() && !ownJob {
		if err := s.queueNotify(ctx, tx, p, jobs.ActorSystem); err != nil {
			return p, err
		}
	}
	return p, nil
}
