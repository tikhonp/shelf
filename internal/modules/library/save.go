package library

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"slices"
	"strconv"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/labstack/echo/v5"
	"github.com/tikhonp/shelf/internal/modules/library/tagio"
	"github.com/tikhonp/shelf/internal/platform/db"
	"github.com/tikhonp/shelf/internal/platform/jobs"
)

// WriteJob is the job of every tag write.
const WriteJob = "library.write"

// Limits of a Save request.
const (
	maxEdits = 500
)

// The Navidrome changes a Save asks the owner to confirm, because a star or
// a rating moves or stays behind.
const (
	ConfirmSplit  = "split"
	ConfirmMerge  = "merge"
	ConfirmArtist = "album_artist"
)

// saveBody is a Save as the editor sends it. Each track names the
// revision the editor loaded, so a file another tool changed since is a
// conflict.
type saveBody struct {
	Tracks  []trackEdits `json:"tracks"`
	Confirm []string     `json:"confirm"`
}

type trackEdits struct {
	ID       int64      `json:"id"`
	Revision string     `json:"revision"`
	Edits    []editJSON `json:"edits"`
}

// editJSON is one tagio.Edit: a field, which changes every key that feeds
// it, or a raw key. No values removes the tag.
type editJSON struct {
	Field  string   `json:"field,omitempty"`
	Key    string   `json:"key,omitempty"`
	Values []string `json:"values"`
}

func (e editJSON) edit() tagio.Edit {
	return tagio.Edit{Field: e.Field, Key: e.Key, Values: e.Values}
}

// warning is a Navidrome change the owner confirms before a Save runs.
type warning struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

// refusal is a Save shelf won't run, with the track it stopped at.
type refusal struct {
	Error   string    `json:"error"`
	Reason  string    `json:"reason"`
	Track   int64     `json:"track,omitempty"`
	Confirm []warning `json:"confirm,omitempty"`
}

// save checks a Save against the files as they are now, asks for the
// confirmations it needs, and queues its write.
func (m *Module) save(c *echo.Context) error {
	id, err := idParam(c, "album")
	if err != nil {
		return err
	}
	var b saveBody
	if err := json.NewDecoder(c.Request().Body).Decode(&b); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the body is not JSON with tracks")
	}
	if len(b.Tracks) == 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "a save names at least one track")
	}
	edits := 0
	for _, t := range b.Tracks {
		edits += len(t.Edits)
	}
	if edits > maxEdits {
		return echo.NewHTTPError(http.StatusBadRequest, "a save holds up to "+strconv.Itoa(maxEdits)+" edits")
	}
	ctx := c.Request().Context()
	row, err := m.store.album(ctx, id)
	if errors.Is(err, errNotFound) {
		return echo.NewHTTPError(http.StatusNotFound, "no such album")
	}
	if err != nil {
		return err
	}
	files, err := m.store.albumFiles(ctx, id)
	if err != nil {
		return err
	}
	byID := map[int64]*file{}
	for _, f := range files {
		byID[f.ID] = f
	}
	var ch tagio.Change
	stored := storedChange{}
	trackOf := map[string]int64{}
	for _, t := range b.Tracks {
		f := byID[t.ID]
		if f == nil {
			return echo.NewHTTPError(http.StatusUnprocessableEntity, fmt.Sprintf("track %d is not on this album", t.ID))
		}
		abs := m.abs(f.Path)
		fc := tagio.FileChange{Path: abs, Revision: t.Revision}
		for _, e := range t.Edits {
			fc.Edits = append(fc.Edits, e.edit())
		}
		ch.Files = append(ch.Files, fc)
		stored.Files = append(stored.Files, storedFile{Path: f.Path, Revision: t.Revision, Edits: t.Edits})
		trackOf[abs] = t.ID
	}
	planned, err := m.writer(nil).Check(ch)
	if err != nil {
		return m.refuse(c, err, trackOf)
	}
	if len(planned.Files) == 0 {
		return c.JSON(http.StatusUnprocessableEntity, refusal{Error: "nothing to change", Reason: ReasonRefused})
	}
	warns, err := m.identity(ctx, row, files, planned)
	if err != nil {
		return err
	}
	var missing []warning
	for _, w := range warns {
		if !slices.Contains(b.Confirm, w.Kind) {
			missing = append(missing, w)
		}
	}
	if missing != nil {
		return c.JSON(http.StatusConflict, refusal{Error: "confirm what Navidrome will do", Reason: "confirm", Confirm: missing})
	}
	req, err := json.Marshal(stored)
	if err != nil {
		return err
	}
	cr := changeRow{Kind: KindSave, AlbumID: row.ID, Folder: row.Folder, Title: row.Title, Artist: row.Artist,
		State: StateQueued, Request: string(req), CreatedAt: db.At(time.Now())}
	if err := m.queueChange(ctx, &cr, "web"); err != nil {
		return err
	}
	return c.JSON(http.StatusAccepted, m.changeJSON(ctx, cr))
}

// queueChange stores a change and its write job together.
func (m *Module) queueChange(ctx context.Context, cr *changeRow, by string) error {
	err := m.store.db.Write(ctx, func(tx *sqlx.Tx) error {
		if err := m.store.insertChange(ctx, tx, cr); err != nil {
			return err
		}
		e, err := m.jobs.Enqueue(ctx, tx, jobs.Request{Type: WriteJob, Payload: writePayload{Change: cr.ID}, CreatedBy: by})
		if err != nil {
			return err
		}
		cr.JobID = &e.ID
		_, err = tx.ExecContext(ctx, `UPDATE library_changes SET job_id = ? WHERE id = ?`, e.ID, cr.ID)
		return err
	})
	if err != nil {
		return err
	}
	m.jobs.Kick()
	m.publishChange(ctx, *cr)
	return nil
}

// refuse answers a write the checks stopped, naming the track.
func (m *Module) refuse(c *echo.Context, err error, trackOf map[string]int64) error {
	reason := reasonOf(err)
	status := http.StatusConflict
	switch reason {
	case ReasonRefused:
		status = http.StatusUnprocessableEntity
	case ReasonError:
		return err
	}
	out := refusal{Error: (&journal{root: m.root}).message(err), Reason: reason}
	var fe *tagio.FileError
	if errors.As(err, &fe) {
		out.Track = trackOf[fe.Path]
	}
	return c.JSON(status, out)
}

// identity finds what a Save does to the album in Navidrome. A star and a
// rating belong to an album ID and an artist name, so moving some tracks,
// merging into another album and renaming an album artist each lose or
// move one. A whole-album change keeps them under the new ID.
func (m *Module) identity(ctx context.Context, row albumRow, files []*file, planned tagio.Result) ([]warning, error) {
	rules := m.currentRules()
	after := map[string]string{}
	for _, f := range files {
		after[f.Path] = f.Facts.AlbumID
	}
	var before, now []string
	for _, pf := range planned.Files {
		rel := m.rel(pf.Path)
		ft := tagio.FormatOf(pf.Path)
		was := rules.View(tagio.File{Path: pf.Path, Format: ft, Tags: pf.Before}, rel)
		will := rules.View(tagio.File{Path: pf.Path, Format: ft, Tags: pf.After}, rel)
		after[rel] = will.AlbumID()
		before = appendNew(before, was.AlbumArtists()...)
		now = appendNew(now, will.AlbumArtists()...)
	}
	var ids []string
	for _, id := range after {
		ids = appendNew(ids, id)
	}
	var out []warning
	if len(ids) > 1 {
		out = append(out, warning{Kind: ConfirmSplit, Message: fmt.Sprintf(
			"Navidrome will show these tracks as %d albums. The album's star and rating go with the tracks that move, and an undo leaves them on neither album.", len(ids))})
	}
	for _, id := range ids {
		if id == row.NDID {
			continue
		}
		taken, err := m.store.albumIDTaken(ctx, id, row.ID)
		if err != nil {
			return nil, err
		}
		if taken {
			out = append(out, warning{Kind: ConfirmMerge, Message: "Navidrome will merge these tracks into another album. If both albums have a star or a rating, this album's is lost."})
			break
		}
	}
	slices.Sort(before)
	slices.Sort(now)
	if !slices.Equal(before, now) {
		out = append(out, warning{Kind: ConfirmArtist, Message: "This renames the album artist in Navidrome. The old name keeps its star and rating, and an undo brings them back."})
	}
	return out, nil
}

func appendNew(ss []string, vs ...string) []string {
	for _, v := range vs {
		if !slices.Contains(ss, v) {
			ss = append(ss, v)
		}
	}
	return ss
}

// writePayload is the write job's payload. The edits stay in the change's
// row, since a payload stops at 64 KiB.
type writePayload struct {
	Change int64 `json:"change"`
}

// JobTypes is the write job, on the tags queue, one write at a time.
func (m *Module) JobTypes() []jobs.Type {
	return []jobs.Type{{
		Name: WriteJob, Queue: jobs.QueueTags, MaxAttempts: 1,
		Steps: []jobs.Step{{Name: "write", Run: m.write}},
	}}
}

// Schedules: none yet.
func (*Module) Schedules() []jobs.Schedule { return nil }

// write runs a queued change. A change a stop cut off is settled from the
// files. A write that the checks stopped fails its change, not the job.
func (m *Module) write(ctx context.Context, r *jobs.Run) error {
	var p writePayload
	if err := r.Payload(&p); err != nil {
		return jobs.Permanent(err)
	}
	c, err := m.store.change(ctx, m.store.db.R, p.Change)
	if err != nil {
		return jobs.Permanent(err)
	}
	switch c.State {
	case StateQueued:
	case StateWriting:
		r.Log().Warn("The write was cut off; settling it from the files")
		err := m.reconcile(ctx, c)
		m.afterWrite(ctx, c.ID)
		return err
	default:
		return nil
	}
	var sc storedChange
	if err := json.Unmarshal([]byte(c.Request), &sc); err != nil {
		return jobs.Permanent(err)
	}
	var ch tagio.Change
	for _, f := range sc.Files {
		fc := tagio.FileChange{Path: m.abs(f.Path), Revision: f.Revision}
		for _, e := range f.Edits {
			fc.Edits = append(fc.Edits, e.edit())
		}
		ch.Files = append(ch.Files, fc)
	}
	j := &journal{s: m.store, root: m.root, id: c.ID, now: time.Now}
	res, werr := m.writer(j).Write(ctx, ch)
	switch {
	case !j.begun && werr != nil:
		if err := j.fail(context.WithoutCancel(ctx), werr); err != nil {
			return err
		}
	case !j.begun:
		// Another tool made the same change since the Save was checked.
		if err := j.End(ctx, c.ID, res, nil); err != nil {
			return err
		}
	}
	if werr != nil {
		r.Log().Warn("The write stopped: %s", j.message(werr))
	} else {
		r.Log().Info("Wrote %d files of %s", len(res.Files), c.Folder)
	}
	m.afterWrite(ctx, c.ID)
	return nil
}

// afterWrite tells the editor how a change ended, and asks Navidrome and
// the index to read the files.
func (m *Module) afterWrite(ctx context.Context, id int64) {
	ctx = context.WithoutCancel(ctx)
	c, err := m.store.change(ctx, m.store.db.R, id)
	if err != nil {
		m.log.Error("library: read a change", "change", id, "error", err)
		return
	}
	m.publishChange(ctx, c)
	if c.State == StateFailed {
		return
	}
	m.Kick()
	if scan := m.scanner(); scan != nil {
		if err := scan(ctx); err != nil {
			m.log.Warn("library: ask Navidrome to scan", "error", err)
		}
	}
}

func (m *Module) abs(rel string) string { return filepath.Join(m.root, filepath.FromSlash(rel)) }

func (m *Module) rel(abs string) string {
	rel, err := filepath.Rel(m.root, abs)
	if err != nil {
		return abs
	}
	return filepath.ToSlash(rel)
}

// writer is a tagio.Writer that keeps out of the folders downloads work in.
func (m *Module) writer(j tagio.Journal) *tagio.Writer {
	w := tagio.NewWriter()
	m.mu.Lock()
	w.Busy = m.busy
	m.mu.Unlock()
	if j != nil {
		w.Journal = j
	}
	return w
}
