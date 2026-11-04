package library

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/tikhonp/shelf/internal/modules/library/tagio"
	"github.com/tikhonp/shelf/internal/platform/db"
)

// The kinds and states of a change.
const (
	KindSave = "save"
	KindUndo = "undo"
	KindRedo = "redo"

	StateQueued  = "queued"
	StateWriting = "writing"
	StateDone    = "done"
	StatePartial = "partial"
	StateFailed  = "failed"
)

// Why a change failed, for the editor's states.
const (
	ReasonConflict    = "conflict"
	ReasonLocked      = "locked"
	ReasonBusy        = "busy"
	ReasonRefused     = "refused"
	ReasonVerify      = "verify"
	ReasonInterrupted = "interrupted"
	ReasonError       = "error"
)

// ChangeTopic is the hub topic of change events.
const ChangeTopic = "change"

// changeRow is one tag write as stored.
type changeRow struct {
	ID         int64   `db:"id"`
	Kind       string  `db:"kind"`
	Target     *int64  `db:"target"`
	AlbumID    int64   `db:"album_id"`
	Folder     string  `db:"folder"`
	Title      string  `db:"title"`
	Artist     string  `db:"artist"`
	State      string  `db:"state"`
	Reason     string  `db:"reason"`
	Error      string  `db:"error"`
	Undone     bool    `db:"undone"`
	Request    string  `db:"request"`
	Conflicts  string  `db:"conflicts"`
	Tags       int     `db:"tags"`
	JobID      *int64  `db:"job_id"`
	CreatedAt  db.Time `db:"created_at"`
	FinishedAt db.Time `db:"finished_at"`
}

const changeCols = `id, kind, target, album_id, folder, title, artist, state, reason, error, undone, request,
	conflicts, tags, job_id, created_at, finished_at`

// The kinds of a change's file.
const (
	fileAudio = "audio"
	fileCover = "cover"
)

// changeFile is one file of a change: an audio file with its tags and front
// cover before and after, or a folder's cover file.
type changeFile struct {
	Path     string
	Kind     string
	Before   map[string][]string
	After    map[string][]string
	Revision string
	Written  bool
	// CoverBefore and CoverAfter are cover store IDs, "" for none. An audio
	// file whose cover the change left alone has the same in both.
	CoverBefore string
	CoverAfter  string
}

type changeFileRow struct {
	Path        string `db:"path"`
	Kind        string `db:"kind"`
	Before      string `db:"before"`
	After       string `db:"after"`
	Revision    string `db:"revision"`
	Written     bool   `db:"written"`
	CoverBefore string `db:"cover_before"`
	CoverAfter  string `db:"cover_after"`
}

// storedChange is what a change's job writes: the audio files with their
// edits, then the folder's cover files.
type storedChange struct {
	Files  []storedFile `json:"files"`
	Folder []folderOp   `json:"folder,omitempty"`
}

type storedFile struct {
	Path     string     `json:"path"` // inside the music folder
	Revision string     `json:"revision"`
	Edits    []editJSON `json:"edits"`
	// Cover, when set, replaces the front cover with a stored image, or
	// removes it when its ID is empty.
	Cover *storedCover `json:"cover,omitempty"`
}

type storedCover struct {
	ID string `json:"id"`
}

// folderOp writes a stored image to a cover file of a folder, or removes
// the file when ID is empty. Was is the hash the file had when the change
// was asked for, "" for none, so a file another tool changed is a conflict.
type folderOp struct {
	Path string `json:"path"`
	ID   string `json:"id"`
	Was  string `json:"was"`
}

func (s store) change(ctx context.Context, q sqlx.QueryerContext, id int64) (changeRow, error) {
	var r changeRow
	err := sqlx.GetContext(ctx, q, &r, `SELECT `+changeCols+` FROM library_changes WHERE id = ?`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return r, errNotFound
	}
	return r, err
}

func (s store) changeFiles(ctx context.Context, id int64) ([]changeFile, error) {
	var rows []changeFileRow
	if err := s.db.R.SelectContext(ctx, &rows, `SELECT path, kind, before, after, revision, written, cover_before, cover_after
		FROM library_change_files WHERE change_id = ? ORDER BY kind, path`, id); err != nil {
		return nil, err
	}
	out := make([]changeFile, len(rows))
	for i, r := range rows {
		out[i] = changeFile{Path: r.Path, Kind: r.Kind, Revision: r.Revision, Written: r.Written,
			CoverBefore: r.CoverBefore, CoverAfter: r.CoverAfter}
		if err := json.Unmarshal([]byte(r.Before), &out[i].Before); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(r.After), &out[i].After); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// insertChange stores a new change in tx and gives it its ID.
func (s store) insertChange(ctx context.Context, tx *sqlx.Tx, c *changeRow) error {
	if c.Conflicts == "" {
		c.Conflicts = "[]"
	}
	return tx.GetContext(ctx, &c.ID, `INSERT INTO library_changes
		(kind, target, album_id, folder, title, artist, state, request, conflicts, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
		c.Kind, c.Target, c.AlbumID, c.Folder, c.Title, c.Artist, c.State, c.Request, c.Conflicts, c.CreatedAt)
}

// writingChanges are the changes a stop cut off between their first and
// last write.
func (s store) writingChanges(ctx context.Context) ([]changeRow, error) {
	var rows []changeRow
	err := s.db.R.SelectContext(ctx, &rows, `SELECT `+changeCols+` FROM library_changes WHERE state = ?`, StateWriting)
	return rows, err
}

// keysChanged counts the keys whose values differ between two tag maps.
func keysChanged(before, after map[string][]string) int {
	n := 0
	for k, v := range after {
		if !slices.Equal(before[k], v) {
			n++
		}
	}
	for k := range before {
		if _, ok := after[k]; !ok {
			n++
		}
	}
	return n
}

// journal records one change's write in its row, as the writer's Journal.
// Begin runs before any file is touched, and End when the audio files are
// done, however they end. A change with folder cover files ends in close,
// after those.
type journal struct {
	s    store
	root string
	id   int64
	now  func() time.Time
	// covers maps each audio file whose front cover changes to its cover
	// store IDs before and after.
	covers map[string][2]string
	folder []folderOp
	begun  bool
	closed bool
}

func (j *journal) rel(abs string) string {
	rel, err := filepath.Rel(j.root, abs)
	if err != nil {
		return abs
	}
	return filepath.ToSlash(rel)
}

func (j *journal) Begin(ctx context.Context, r tagio.Result) (int64, error) {
	err := j.s.db.Write(ctx, func(tx *sqlx.Tx) error {
		tags := 0
		for _, f := range r.Files {
			before, err := json.Marshal(f.Before)
			if err != nil {
				return err
			}
			after, err := json.Marshal(f.After)
			if err != nil {
				return err
			}
			rel := j.rel(f.Path)
			cv := j.covers[rel]
			if _, err := tx.ExecContext(ctx, `INSERT INTO library_change_files (change_id, path, kind, before, after, cover_before, cover_after)
				VALUES (?, ?, ?, ?, ?, ?, ?)`, j.id, rel, fileAudio, string(before), string(after), cv[0], cv[1]); err != nil {
				return err
			}
			tags += keysChanged(f.Before, f.After)
			if cv[0] != cv[1] {
				tags++
			}
		}
		for _, op := range j.folder {
			if _, err := tx.ExecContext(ctx, `INSERT INTO library_change_files (change_id, path, kind, before, after, cover_before, cover_after)
				VALUES (?, ?, ?, '{}', '{}', ?, ?)`, j.id, op.Path, fileCover, op.Was, op.ID); err != nil {
				return err
			}
			tags++
		}
		res, err := tx.ExecContext(ctx, `UPDATE library_changes SET state = ?, tags = ? WHERE id = ? AND state = ?`,
			StateWriting, tags, j.id, StateQueued)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return errors.New("the change isn't queued")
		}
		return nil
	})
	if err == nil {
		j.begun = true
	}
	return j.id, err
}

func (j *journal) End(ctx context.Context, _ int64, r tagio.Result, werr error) error {
	err := j.s.db.Write(ctx, func(tx *sqlx.Tx) error {
		for _, f := range r.Files {
			after, err := json.Marshal(f.After)
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE library_change_files SET after = ?, revision = ?, written = ?
				WHERE change_id = ? AND path = ?`, string(after), f.Revision, f.Written, j.id, j.rel(f.Path)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil || len(j.folder) > 0 {
		return err
	}
	return j.close(ctx, werr)
}

// close ends the change with the state its files' rows add up to.
func (j *journal) close(ctx context.Context, werr error) error {
	j.closed = true
	return j.s.db.Write(ctx, func(tx *sqlx.Tx) error {
		var n struct {
			Files   int `db:"files"`
			Written int `db:"written"`
		}
		if err := tx.GetContext(ctx, &n, `SELECT count(*) AS files, coalesce(sum(written), 0) AS written
			FROM library_change_files WHERE change_id = ?`, j.id); err != nil {
			return err
		}
		reason, msg := "", ""
		if werr != nil {
			reason, msg = reasonOf(werr), j.message(werr)
		}
		var out changeRow
		return j.s.finish(ctx, tx, j.id, stateOf(n.Written, n.Files), reason, msg, j.now(), &out)
	})
}

// fail ends a change whose write stopped before its journal began.
func (j *journal) fail(ctx context.Context, werr error) error {
	j.closed = true
	return j.s.db.Write(ctx, func(tx *sqlx.Tx) error {
		var out changeRow
		return j.s.finish(ctx, tx, j.id, StateFailed, reasonOf(werr), j.message(werr), j.now(), &out)
	})
}

// wrote marks one file of the change written.
func (j *journal) wrote(ctx context.Context, path string) error {
	return j.s.db.Write(ctx, func(tx *sqlx.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE library_change_files SET written = 1 WHERE change_id = ? AND path = ?`, j.id, path)
		return err
	})
}

// message is an error with paths made relative to the music folder.
func (j *journal) message(err error) string {
	return strings.ReplaceAll(err.Error(), j.root+string(filepath.Separator), "")
}

// stateOf is a change's state from how many of its files were replaced.
func stateOf(written, files int) string {
	switch {
	case written == files:
		return StateDone
	case written > 0:
		return StatePartial
	}
	return StateFailed
}

// finish stores how a change ended. An undo or redo that changed a file
// moves its save's undone flag.
func (s store) finish(ctx context.Context, tx *sqlx.Tx, id int64, state, reason, msg string, at time.Time, out *changeRow) error {
	if _, err := tx.ExecContext(ctx, `UPDATE library_changes SET state = ?, reason = ?, error = ?, finished_at = ?
		WHERE id = ?`, state, reason, cut(msg, 4000), db.At(at), id); err != nil {
		return err
	}
	c, err := s.change(ctx, tx, id)
	if err != nil {
		return err
	}
	if c.Target != nil && state != StateFailed {
		if _, err := tx.ExecContext(ctx, `UPDATE library_changes SET undone = ? WHERE id = ?`, c.Kind == KindUndo, *c.Target); err != nil {
			return err
		}
	}
	*out = c
	return nil
}

// reasonOf names why a write stopped.
func reasonOf(err error) string {
	switch {
	case errors.Is(err, tagio.ErrConflict), errors.Is(err, fs.ErrNotExist):
		return ReasonConflict
	case errors.Is(err, tagio.ErrLocked), errors.Is(err, fs.ErrPermission):
		return ReasonLocked
	case errors.Is(err, tagio.ErrBusy):
		return ReasonBusy
	case errors.Is(err, tagio.ErrRefused):
		return ReasonRefused
	case errors.Is(err, tagio.ErrVerify):
		return ReasonVerify
	}
	return ReasonError
}

// reconcile settles a change a stop cut off while it wrote. Each file holds
// its tags from before or after the write, or a third tool's, and the
// change ends with what the files hold.
func (m *Module) reconcile(ctx context.Context, c changeRow) error {
	files, err := m.store.changeFiles(ctx, c.ID)
	if err != nil {
		return err
	}
	landed := map[string]string{}
	for _, f := range files {
		abs := m.abs(f.Path)
		if f.Kind == fileCover {
			if h, _, err := fileHash(abs); err == nil && h == f.CoverAfter {
				landed[f.Path] = ""
			}
			continue
		}
		tf, err := tagio.Read(abs)
		if err != nil || !sameTags(tf.Tags, f.After) {
			continue
		}
		if f.CoverBefore != f.CoverAfter {
			if b, err := tagio.FrontCover(abs); err != nil || coverHash(b) != f.CoverAfter {
				continue
			}
		}
		landed[f.Path] = tf.Revision()
	}
	return m.store.db.Write(ctx, func(tx *sqlx.Tx) error {
		// The job and the start may both settle one change.
		if now, err := m.store.change(ctx, tx, c.ID); err != nil || now.State != StateWriting {
			return err
		}
		for p, rev := range landed {
			if _, err := tx.ExecContext(ctx, `UPDATE library_change_files SET revision = ?, written = 1
				WHERE change_id = ? AND path = ?`, rev, c.ID, p); err != nil {
				return err
			}
		}
		written := len(landed)
		var out changeRow
		if err := m.store.finish(ctx, tx, c.ID, stateOf(written, len(files)), ReasonInterrupted,
			"shelf stopped while it wrote these files", time.Now(), &out); err != nil {
			return err
		}
		m.log.Info("library: settled an interrupted change", "change", c.ID, "state", out.State, "written", written, "files", len(files))
		return nil
	})
}

// reconcileAll settles every change a stop cut off.
func (m *Module) reconcileAll(ctx context.Context) error {
	rows, err := m.store.writingChanges(ctx)
	if err != nil {
		return err
	}
	var errs []error
	for _, c := range rows {
		errs = append(errs, m.reconcile(ctx, c))
	}
	return errors.Join(errs...)
}

func sameTags(a, b map[string][]string) bool {
	return len(a) == len(b) && keysChanged(a, b) == 0
}
