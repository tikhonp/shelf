package downloads

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/tikhonp/shelf/internal/platform/config"
	"github.com/tikhonp/shelf/internal/platform/db"
	"github.com/tikhonp/shelf/internal/platform/hub"
	"github.com/tikhonp/shelf/internal/platform/jobs"
	"github.com/tikhonp/shelf/internal/platform/module"
	"github.com/tikhonp/shelf/internal/source"
)

// Notifier gives a download's Telegram status message its final text. The
// notify step calls it once, when the download came from Telegram.
type Notifier interface {
	Notify(ctx context.Context, d Download) error
}

// Scanner asks the library to scan soon after downloads end. The library
// waits for a burst of downloads to finish.
type Scanner interface {
	RequestScan(ctx context.Context) error
}

// QueueRequest asks for a download, by ref or by link.
type QueueRequest struct {
	Ref       source.Ref // or URL
	URL       string
	Format    string // empty: the source's default
	Force     bool
	Origin    string // "web", "telegram", "cli"
	OriginRef OriginRef
}

// Service holds downloads.
type Service struct {
	// Now is the clock; tests replace it.
	Now func() time.Time

	cfg     *config.Config
	db      *db.DB
	jobs    *jobs.System
	hub     *hub.Hub
	sources *source.Registry
	log     *slog.Logger

	mu       sync.Mutex
	notifier Notifier
	scanner  Scanner
}

func newService(d module.Deps) *Service {
	return &Service{Now: time.Now, cfg: d.Cfg, db: d.DB, jobs: d.Jobs, hub: d.Hub, sources: d.Sources, log: d.Log}
}

func (s *Service) now() db.Time { return db.At(s.Now()) }

// SetNotifier wires the bot. Until then the notify step does nothing.
func (s *Service) SetNotifier(n Notifier) {
	s.mu.Lock()
	s.notifier = n
	s.mu.Unlock()
}

// SetScanner wires the library. Until then finished downloads start no scan.
func (s *Service) SetScanner(sc Scanner) {
	s.mu.Lock()
	s.scanner = sc
	s.mu.Unlock()
}

func (s *Service) ports() (Notifier, Scanner) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.notifier, s.scanner
}

// target finds the item and the source a request names, and checks that the
// source is switched on and can download.
func (s *Service) target(r QueueRequest) (source.Ref, source.Downloader, error) {
	ref := r.Ref
	var src source.Source
	if r.URL != "" {
		var ok bool
		if ref, src, ok = s.sources.ParseLink(r.URL); !ok {
			return ref, nil, ErrUnknownLink
		}
	} else {
		if !ref.Valid() {
			return ref, nil, fmt.Errorf("%w: a download needs a link or a source, kind and ID", ErrBadRequest)
		}
		var ok bool
		if src, ok = s.sources.Get(ref.Source); !ok {
			return ref, nil, fmt.Errorf("%w: no source %q", ErrUnknownSource, ref.Source)
		}
	}
	if !source.IsEnabled(src) {
		return ref, nil, fmt.Errorf("%w: %s is not set up in Shelf", ErrUnknownSource, src.Name())
	}
	dl, ok := src.(source.Downloader)
	if !ok {
		return ref, nil, fmt.Errorf("%w: %s can't download", ErrUnknownSource, src.Name())
	}
	return ref, dl, nil
}

// format picks the requested format, or the source's default.
func format(dl source.Downloader, ref source.Ref, id string) (string, error) {
	formats := dl.Formats()
	if id == "" {
		f, ok := source.DefaultFormat(formats)
		if !ok {
			return "", fmt.Errorf("%w: %s lists none", ErrUnknownFormat, ref.Source)
		}
		return f.ID, nil
	}
	if !strings.Contains(id, ":") {
		id = ref.Source + ":" + id
	}
	if _, ok := source.FindFormat(formats, id); !ok {
		return "", fmt.Errorf("%w: %q", ErrUnknownFormat, id)
	}
	return id, nil
}

// Queue creates a download and its job. An album, playlist or track that is
// already in downloaded_items gives a skipped download unless r.Force.
func (s *Service) Queue(ctx context.Context, r QueueRequest) (Download, error) {
	switch r.Origin {
	case OriginWeb, OriginTelegram, OriginCLI:
	default:
		return Download{}, fmt.Errorf("%w: unknown origin %q", ErrBadRequest, r.Origin)
	}
	ref, dl, err := s.target(r)
	if err != nil {
		return Download{}, err
	}
	f, err := format(dl, ref, r.Format)
	if err != nil {
		return Download{}, err
	}
	nd := newDownload{ref: ref, url: r.URL, format: f, force: r.Force, origin: r.Origin, originRef: r.OriginRef,
		createdBy: createdBy(r.Origin, r.OriginRef)}
	var id int64
	err = s.db.Write(ctx, func(tx *sqlx.Tx) (err error) {
		id, err = s.create(ctx, tx, nd)
		return err
	})
	if err != nil {
		return Download{}, err
	}
	s.jobs.Kick()
	d, err := s.load(ctx, id)
	if err == nil {
		s.publish(d, "")
	}
	return d, err
}

type newDownload struct {
	ref       source.Ref
	url       string
	format    string
	force     bool
	origin    string
	originRef OriginRef
	parentID  int64
	createdBy string
	// title, artist and cover, when the caller knows them already
	title, artist, cover string
}

// create inserts a download, and its job unless the item is already in the
// library. It runs inside the caller's write transaction.
func (s *Service) create(ctx context.Context, tx *sqlx.Tx, nd newDownload) (int64, error) {
	oref, err := json.Marshal(nd.originRef)
	if err != nil {
		return 0, err
	}
	now := s.now()
	var parent any
	if nd.parentID != 0 {
		parent = nd.parentID
	}

	if !nd.force && !nd.ref.Kind.Collection() {
		var have struct {
			Path string `db:"path"`
		}
		err := tx.GetContext(ctx, &have, `SELECT path FROM downloaded_items WHERE source = ? AND kind = ? AND item_id = ?`,
			nd.ref.Source, nd.ref.Kind, nd.ref.ID)
		if err == nil {
			// The last download of the item knows its title and cover.
			var last struct {
				Title    string `db:"title"`
				Artist   string `db:"artist"`
				CoverURL string `db:"cover_url"`
			}
			_ = tx.GetContext(ctx, &last, `
				SELECT title, artist, cover_url FROM downloads
				WHERE source = ? AND kind = ? AND item_id = ? AND title != '' ORDER BY id DESC LIMIT 1`,
				nd.ref.Source, nd.ref.Kind, nd.ref.ID)
			last.Title, last.Artist, last.CoverURL = first(nd.title, last.Title), first(nd.artist, last.Artist), first(nd.cover, last.CoverURL)
			res, err := tx.ExecContext(ctx, `
				INSERT INTO downloads (parent_id, source, kind, item_id, url, format, force, title, artist, cover_url,
					status, folder, origin, origin_ref, created_at, finished_at)
				VALUES (?, ?, ?, ?, ?, ?, 0, ?, ?, ?, 'skipped', ?, ?, ?, ?, ?)`,
				parent, nd.ref.Source, nd.ref.Kind, nd.ref.ID, nd.url, nd.format, last.Title, last.Artist, last.CoverURL,
				have.Path, nd.origin, string(oref), now, now)
			if err != nil {
				return 0, err
			}
			return res.LastInsertId()
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return 0, err
		}
	}

	res, err := tx.ExecContext(ctx, `
		INSERT INTO downloads (parent_id, source, kind, item_id, url, format, force, title, artist, cover_url,
			status, origin, origin_ref, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'queued', ?, ?, ?)`,
		parent, nd.ref.Source, nd.ref.Kind, nd.ref.ID, nd.url, nd.format, nd.force, nd.title, nd.artist, nd.cover,
		nd.origin, string(oref), now)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	return id, s.enqueue(ctx, tx, id, nd.ref, nd.createdBy)
}

func first(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// enqueue starts a job for download id on its source's queue.
func (s *Service) enqueue(ctx context.Context, tx *sqlx.Tx, id int64, ref source.Ref, by string) error {
	e, err := s.jobs.Enqueue(ctx, tx, jobs.Request{
		Type: JobType, Queue: queueFor(ref.Source), Payload: payload{DownloadID: id},
		ResourceKey: "item:" + ref.String(), CreatedBy: by,
	})
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE downloads SET job_id = ? WHERE id = ?`, e.ID, id)
	return err
}

// queueFor is the queue named after the source, so each source runs one
// download at a time and two sources run side by side. A source without
// its own queue, such as the fake one, uses system.
func queueFor(sourceID string) jobs.Queue {
	if q := jobs.Queue(sourceID); jobs.Concurrency[q] > 0 && q != jobs.QueueLibrary {
		return q
	}
	return jobs.QueueSystem
}

func (s *Service) load(ctx context.Context, id int64) (Download, error) {
	return loadWith(ctx, s.db.R, id)
}

func loadWith(ctx context.Context, q sqlx.QueryerContext, id int64) (Download, error) {
	var r row
	err := sqlx.GetContext(ctx, q, &r, `SELECT `+columns+` FROM downloads WHERE id = ?`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return Download{}, ErrNotFound
	}
	if err != nil {
		return Download{}, err
	}
	return r.download(), nil
}

func (s *Service) tracks(ctx context.Context, q sqlx.QueryerContext, id int64) ([]Track, error) {
	var rows []trackRow
	if err := sqlx.SelectContext(ctx, q, &rows, `SELECT `+trackColumns+` FROM download_tracks WHERE download_id = ? ORDER BY position`, id); err != nil {
		return nil, err
	}
	out := make([]Track, len(rows))
	for i, r := range rows {
		out[i] = Track(r)
	}
	return out, nil
}

func (s *Service) children(ctx context.Context, id int64) ([]Download, error) {
	var rows []row
	if err := s.db.R.SelectContext(ctx, &rows, `SELECT `+columns+` FROM downloads WHERE parent_id = ? ORDER BY id`, id); err != nil {
		return nil, err
	}
	out := make([]Download, len(rows))
	for i, r := range rows {
		out[i] = r.download()
	}
	return out, nil
}

// Get reads one download with its tracks and children.
func (s *Service) Get(ctx context.Context, id int64) (Detail, error) {
	d, err := s.load(ctx, id)
	if err != nil {
		return Detail{}, err
	}
	det := Detail{Download: d}
	if det.Tracks, err = s.tracks(ctx, s.db.R, id); err != nil {
		return Detail{}, err
	}
	if det.Children, err = s.children(ctx, id); err != nil {
		return Detail{}, err
	}
	if (d.Status == Done || d.Status == Partial) && d.Folder != "" && !d.Ref.Kind.Collection() {
		det.ProvenancePath = path.Join(d.Folder, ProvenanceFile)
	}
	return det, nil
}

// List returns top-level downloads, newest first. An artist's or label's
// albums are in its Detail.
func (s *Service) List(ctx context.Context, f Filter) ([]Download, error) {
	where := []string{"parent_id IS NULL"}
	var args []any
	if len(f.Statuses) > 0 {
		in := make([]string, len(f.Statuses))
		for i, st := range f.Statuses {
			in[i] = "?"
			args = append(args, string(st))
		}
		where = append(where, "status IN ("+strings.Join(in, ",")+")")
	}
	if f.Source != "" {
		where = append(where, "source = ?")
		args = append(args, f.Source)
	}
	if f.Before > 0 {
		where = append(where, "id < ?")
		args = append(args, f.Before)
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	var rows []row
	q := `SELECT ` + columns + ` FROM downloads WHERE ` + strings.Join(where, " AND ") + ` ORDER BY id DESC LIMIT ?`
	if err := s.db.R.SelectContext(ctx, &rows, q, append(args, limit)...); err != nil {
		return nil, err
	}
	out := make([]Download, len(rows))
	for i, r := range rows {
		out[i] = r.download()
	}
	return out, nil
}

// Cancel stops a queued or running download. A queued one ends at once; a
// running one ends when its step sees its context end. An artist or label
// cancels its albums.
func (s *Service) Cancel(ctx context.Context, id int64, by string) error {
	d, err := s.load(ctx, id)
	if err != nil {
		return err
	}
	if d.Status.Final() {
		return ErrNotActive
	}
	targets := []Download{d}
	if d.Ref.Kind.Collection() {
		kids, err := s.children(ctx, id)
		if err != nil {
			return err
		}
		targets = append(targets, kids...)
	}
	for _, t := range targets {
		if t.Status.Final() || t.JobID == 0 {
			continue
		}
		// The parent's own job has often ended already; its albums carry on.
		if err := s.jobs.Cancel(ctx, t.JobID, by); err != nil && !errors.Is(err, jobs.ErrNotActive) {
			return err
		}
	}
	return nil
}

// Retry queues a failed, partial or canceled download again, with a new job.
// The old job and its log stay. An artist or label retries its albums that
// did not end done.
func (s *Service) Retry(ctx context.Context, id int64, by string) (Download, error) {
	var touched []int64
	err := s.db.Write(ctx, func(tx *sqlx.Tx) error {
		d, err := loadWith(ctx, tx, id)
		if err != nil {
			return err
		}
		if !d.Status.Retryable() {
			return ErrNotRetryable
		}
		if d.ParentID != 0 {
			return fmt.Errorf("%w: retry the artist or label download %d instead", ErrNotRetryable, d.ParentID)
		}
		var kids []row
		if d.Ref.Kind.Collection() {
			if err := tx.SelectContext(ctx, &kids, `SELECT `+columns+` FROM downloads WHERE parent_id = ? ORDER BY id`, id); err != nil {
				return err
			}
		}
		if len(kids) == 0 {
			touched = append(touched, id)
			return s.requeue(ctx, tx, d, by)
		}
		for _, k := range kids {
			kd := k.download()
			if !kd.Status.Retryable() {
				continue
			}
			touched = append(touched, kd.ID)
			if err := s.requeue(ctx, tx, kd, by); err != nil {
				return err
			}
		}
		touched = append(touched, id)
		_, err = tx.ExecContext(ctx, `
			UPDATE downloads SET status = 'running', error = '', finished_at = NULL, notified = 0 WHERE id = ?`, id)
		if err != nil {
			return err
		}
		_, err = s.refreshParent(ctx, tx, id, false)
		return err
	})
	if err != nil {
		return Download{}, err
	}
	s.jobs.Kick()
	for _, t := range touched {
		if d, err := s.load(ctx, t); err == nil {
			s.publish(d, "")
		}
	}
	return s.load(ctx, id)
}

// requeue resets d to queued with a fresh job. The tracks stay until resolve
// plans them again.
func (s *Service) requeue(ctx context.Context, tx *sqlx.Tx, d Download, by string) error {
	_, err := tx.ExecContext(ctx, `
		UPDATE downloads SET status = 'queued', error = '', tracks_done = 0, tracks_failed = 0, bytes = 0,
			started_at = NULL, finished_at = NULL, notified = 0
		WHERE id = ?`, d.ID)
	if err != nil {
		return err
	}
	return s.enqueue(ctx, tx, d.ID, d.Ref, by)
}

func (s *Service) publish(d Download, current string) {
	s.hub.Publish(Topic, d.event(current))
}
