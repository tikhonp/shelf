package library

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/labstack/echo/v5"
	"github.com/tikhonp/shelf/internal/modules/library/musicbrainz"
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
	// Cover changes the album's cover. It needs every track of the album.
	Cover *coverEdit `json:"cover"`
}

// coverEdit replaces the album's cover with an upload, or deletes it. Each
// goes to the folder's cover file and to the front cover of every track
// that has one, or of every track with EveryTrack.
type coverEdit struct {
	Upload     string `json:"upload"`
	Delete     bool   `json:"delete"`
	EveryTrack bool   `json:"every_track"`
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
	// Anchor is the MusicBrainz release an accepted suggestion of a release
	// field came from. A typed value has none.
	Anchor string `json:"anchor,omitempty"`
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
	// Conflicts are the keys an undo or redo can't restore.
	Conflicts []conflictJSON `json:"conflicts,omitempty"`
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
	anchor, msg := saveAnchor(b)
	if msg != "" {
		return c.JSON(http.StatusUnprocessableEntity, refusal{Error: msg, Reason: ReasonRefused})
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
	if b.Cover != nil {
		if err := m.planCover(row, files, b, &ch, &stored); err != nil {
			return m.refuse(c, err, trackOf)
		}
	}
	planned, err := m.writer(nil).Check(ch)
	if err != nil {
		return m.refuse(c, err, trackOf)
	}
	if len(planned.Files) == 0 && len(stored.Folder) == 0 {
		return c.JSON(http.StatusUnprocessableEntity, refusal{Error: "nothing to change", Reason: ReasonRefused})
	}
	if anchor != "" {
		if t := m.otherRelease(files, planned, anchor); t != 0 {
			return c.JSON(http.StatusUnprocessableEntity, refusal{Track: t, Reason: ReasonRefused,
				Error: "These suggestions come from another MusicBrainz release than the album ID the tracks would keep. Accept that release's album ID too."})
		}
	}
	warns, err := m.identity(ctx, []albumFiles{{row, files}}, planned)
	if err != nil {
		return err
	}
	if ok, err := confirmed(c, warns, b.Confirm); !ok {
		return err
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

// saveAnchor is the one release a Save's suggestions came from. Two
// releases in one Save would leave the album with IDs from both. The
// message says why a Save can't have one.
func saveAnchor(b saveBody) (anchor, msg string) {
	for _, t := range b.Tracks {
		for _, e := range t.Edits {
			switch {
			case e.Anchor == "" || e.Anchor == anchor:
			case !musicbrainz.IsID(e.Anchor):
				return "", "anchor " + strconv.Quote(e.Anchor) + " is not a MusicBrainz ID"
			case anchor != "":
				return "", "These suggestions come from two MusicBrainz releases. Save them one release at a time."
			default:
				anchor = e.Anchor
			}
		}
	}
	return anchor, ""
}

// otherRelease finds a track that would end with a MusicBrainz album ID
// other than the anchor's, so the anchor's release fields would sit beside
// another release's IDs. Tracks without the ID are fine.
func (m *Module) otherRelease(files []*file, planned tagio.Result, anchor string) int64 {
	rules := m.currentRules()
	after := map[string]string{}
	for _, pf := range planned.Files {
		v := rules.View(tagio.File{Path: pf.Path, Format: tagio.FormatOf(pf.Path), Tags: pf.After}, m.rel(pf.Path))
		after[m.rel(pf.Path)] = v.First("musicbrainz_albumid")
	}
	for _, f := range files {
		id, ok := after[f.Path]
		if !ok {
			id = f.Facts.MBAlbumID
		}
		if id != "" && id != anchor {
			return f.ID
		}
	}
	return 0
}

// planCover adds a cover edit to a Save: a front cover for the tracks and
// the ops on the folder's cover files. The folder keeps one cover file,
// named after the image's format, because Navidrome shows the first it
// finds.
func (m *Module) planCover(row albumRow, files []*file, b saveBody, ch *tagio.Change, stored *storedChange) error {
	ce := b.Cover
	refuse := func(msg string) error { return fmt.Errorf("%w: %s", tagio.ErrRefused, msg) }
	if (ce.Upload == "") == !ce.Delete {
		return refuse("a cover edit has an upload or deletes the cover")
	}
	if len(b.Tracks) != len(files) {
		return refuse("a cover edit names every track of the album")
	}
	var full, emb []byte
	embID := ""
	if ce.Upload != "" {
		var err error
		if full, err = m.covers.get(ce.Upload); err != nil {
			return refuse("no such upload")
		}
		if emb, err = embedded(full); err != nil {
			return err
		}
		if embID, err = m.covers.put(emb); err != nil {
			return err
		}
	}
	for i := range ch.Files {
		tf, err := tagio.Read(ch.Files[i].Path)
		if err != nil {
			return &tagio.FileError{Path: ch.Files[i].Path, Err: err}
		}
		if !tf.HasFrontCover() && (ce.Delete || !ce.EveryTrack) {
			continue
		}
		ch.Files[i].Cover = &tagio.Cover{Data: emb}
		stored.Files[i].Cover = &storedCover{ID: embID}
	}
	target := ""
	if full != nil {
		target = path.Join(row.Folder, "cover"+storeExt(full))
	}
	dir := m.abs(row.Folder)
	if busy := m.writer(nil).Busy; busy != nil && busy(dir) {
		return &tagio.FileError{Path: dir, Err: tagio.ErrBusy}
	}
	names := m.folderCovers(row.Folder)
	if target != "" && !slices.Contains(names, path.Base(target)) {
		names = append(names, path.Base(target))
	}
	for _, name := range names {
		rel := path.Join(row.Folder, name)
		abs := m.abs(rel)
		was, _, err := fileHash(abs)
		if err != nil {
			return &tagio.FileError{Path: abs, Err: err}
		}
		op := folderOp{Path: rel, Was: was}
		if rel == target {
			op.ID = ce.Upload
		}
		if op.ID == op.Was {
			continue
		}
		if err := tagio.CheckFile(abs, os.Getuid()); err != nil {
			return &tagio.FileError{Path: abs, Err: err}
		}
		stored.Folder = append(stored.Folder, op)
	}
	return nil
}

// queueChange stores a change and its write job together.
func (m *Module) queueChange(ctx context.Context, cr *changeRow, by string) error {
	err := m.store.db.Write(ctx, func(tx *sqlx.Tx) error {
		if cr.Target != nil {
			// Two undos of one save would each check the files as they were.
			var n int
			if err := tx.GetContext(ctx, &n, `SELECT count(*) FROM library_changes WHERE target = ? AND state IN (?, ?)`,
				*cr.Target, StateQueued, StateWriting); err != nil {
				return err
			}
			if n > 0 {
				return errReversing
			}
		}
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

// albumFiles is an album of the index with its files, as a write starts.
type albumFiles struct {
	row   albumRow
	files []*file
}

// identity finds what a write does to its albums in Navidrome. A star and
// a rating belong to an album ID and an artist name, so moving some tracks,
// merging into another album and renaming an album artist each lose or
// move one. A whole-album change keeps them under the new ID.
func (m *Module) identity(ctx context.Context, albums []albumFiles, planned tagio.Result) ([]warning, error) {
	rules := m.currentRules()
	ends := map[string]string{}
	var before, now []string
	for _, pf := range planned.Files {
		rel := m.rel(pf.Path)
		ft := tagio.FormatOf(pf.Path)
		was := rules.View(tagio.File{Path: pf.Path, Format: ft, Tags: pf.Before}, rel)
		will := rules.View(tagio.File{Path: pf.Path, Format: ft, Tags: pf.After}, rel)
		ends[rel] = will.AlbumID()
		before = appendNew(before, was.AlbumArtists()...)
		now = appendNew(now, will.AlbumArtists()...)
	}
	var out []warning
	add := func(w warning) {
		if !slices.ContainsFunc(out, func(o warning) bool { return o.Kind == w.Kind }) {
			out = append(out, w)
		}
	}
	for _, a := range albums {
		var ids []string
		for _, f := range a.files {
			id, ok := ends[f.Path]
			if !ok {
				id = f.Facts.AlbumID
			}
			ids = appendNew(ids, id)
		}
		if len(ids) > 1 {
			add(warning{Kind: ConfirmSplit, Message: fmt.Sprintf(
				"Navidrome will show these tracks as %d albums. The album's star and rating go with the tracks that move, and an undo leaves them on neither album.", len(ids))})
		}
		for _, id := range ids {
			if id == a.row.NDID {
				continue
			}
			taken, err := m.store.albumIDTaken(ctx, id, a.row.ID)
			if err != nil {
				return nil, err
			}
			if taken {
				add(warning{Kind: ConfirmMerge, Message: "Navidrome will merge these tracks into another album. If both albums have a star or a rating, this album's is lost."})
			}
		}
	}
	slices.Sort(before)
	slices.Sort(now)
	if !slices.Equal(before, now) {
		add(warning{Kind: ConfirmArtist, Message: "This renames the album artist in Navidrome. The old name keeps its star and rating, and an undo brings them back."})
	}
	slices.SortStableFunc(out, func(a, b warning) int {
		order := []string{ConfirmSplit, ConfirmMerge, ConfirmArtist}
		return slices.Index(order, a.Kind) - slices.Index(order, b.Kind)
	})
	return out, nil
}

// confirmed answers 409 with the warnings a request didn't confirm, or
// reports that it confirmed them all.
func confirmed(c *echo.Context, warns []warning, confirm []string) (bool, error) {
	var missing []warning
	for _, w := range warns {
		if !slices.Contains(confirm, w.Kind) {
			missing = append(missing, w)
		}
	}
	if missing == nil {
		return true, nil
	}
	return false, c.JSON(http.StatusConflict, refusal{Error: "confirm what Navidrome will do", Reason: "confirm", Confirm: missing})
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
	}, {
		Name: RetentionJob, Queue: jobs.QueueSystem, MaxAttempts: 1, Quiet: true,
		Steps: []jobs.Step{{Name: "trim", Run: m.retain}},
	}}
}

// Schedules trims the history at 05:15, after the platform's own retention.
func (*Module) Schedules() []jobs.Schedule {
	return []jobs.Schedule{{Name: RetentionJob, At: "05:15", Request: func(context.Context) (jobs.Request, error) {
		return jobs.Request{Type: RetentionJob}, nil
	}}}
}

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
	ch, covers, perr := m.prepare(ctx, sc)
	j := &journal{s: m.store, root: m.root, id: c.ID, now: time.Now, covers: covers, folder: sc.Folder}
	var res tagio.Result
	werr := perr
	if perr == nil {
		res, werr = m.writer(j).Write(ctx, ch)
	}
	bg := context.WithoutCancel(ctx)
	if !j.begun && werr == nil {
		// Nothing in the audio files is left to change: another tool made
		// the same change, or only the folder's cover changes.
		if _, err := j.Begin(bg, tagio.Result{}); err != nil {
			werr = err
		}
	}
	if !j.begun {
		if err := j.fail(bg, werr); err != nil {
			return err
		}
	} else if !j.closed {
		if werr == nil {
			werr = m.writeFolder(ctx, j)
		}
		if err := j.close(bg, werr); err != nil {
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

// prepare builds the writer's change from a stored one. It reads each cover
// to write from the store and keeps every cover it replaces there, so an
// undo can put it back, and checks the folder's cover files.
func (m *Module) prepare(ctx context.Context, sc storedChange) (tagio.Change, map[string]coverRecord, error) {
	var ch tagio.Change
	covers := map[string]coverRecord{}
	for _, f := range sc.Files {
		abs := m.abs(f.Path)
		fc := tagio.FileChange{Path: abs, Revision: f.Revision}
		for _, e := range f.Edits {
			fc.Edits = append(fc.Edits, e.edit())
		}
		if f.Cover != nil {
			var data []byte
			if f.Cover.ID != "" {
				b, err := m.covers.get(f.Cover.ID)
				if err != nil {
					return ch, nil, fmt.Errorf("the cover to write: %w", err)
				}
				data = b
			}
			fc.Cover = &tagio.Cover{Data: data, Description: f.Cover.Description}
			old, pic, err := tagio.FrontPicture(abs)
			if err != nil {
				return ch, nil, &tagio.FileError{Path: abs, Err: err}
			}
			cv := coverRecord{after: f.Cover.ID}
			if old != nil {
				if cv.before, err = m.covers.put(old); err != nil {
					return ch, nil, err
				}
				cv.beforeDesc = pic.Description
			}
			// The writer keeps a replaced cover's description.
			switch {
			case data == nil:
			case old != nil:
				cv.afterDesc = cv.beforeDesc
			default:
				cv.afterDesc = f.Cover.Description
			}
			covers[f.Path] = cv
		}
		ch.Files = append(ch.Files, fc)
	}
	for _, op := range sc.Folder {
		abs := m.abs(op.Path)
		h, b, err := fileHash(abs)
		if err != nil {
			return ch, nil, &tagio.FileError{Path: abs, Err: err}
		}
		if h != op.Was {
			return ch, nil, &tagio.FileError{Path: abs, Err: tagio.ErrConflict}
		}
		if err := tagio.CheckFile(abs, os.Getuid()); err != nil {
			return ch, nil, &tagio.FileError{Path: abs, Err: err}
		}
		if b != nil {
			if _, err := m.covers.put(b); err != nil {
				return ch, nil, err
			}
		}
		if op.ID != "" {
			if _, err := m.covers.get(op.ID); err != nil {
				return ch, nil, fmt.Errorf("the cover to write: %w", err)
			}
		}
	}
	return ch, covers, nil
}

// writeFolder writes a change's folder cover files once its audio files
// are replaced, the new cover first, so a failure never leaves the folder
// without one.
func (m *Module) writeFolder(ctx context.Context, j *journal) error {
	ops := slices.Clone(j.folder)
	slices.SortStableFunc(ops, func(a, b folderOp) int {
		return cmp.Compare(boolInt(a.ID == ""), boolInt(b.ID == ""))
	})
	busy := m.writer(nil).Busy
	for _, op := range ops {
		abs := m.abs(op.Path)
		fail := func(err error) error { return &tagio.FileError{Path: abs, Err: err} }
		if busy != nil && busy(filepath.Dir(abs)) {
			return fail(tagio.ErrBusy)
		}
		if h, _, err := fileHash(abs); err != nil || h != op.Was {
			return fail(cmp.Or(err, tagio.ErrConflict))
		}
		if op.ID == "" {
			if err := tagio.RemoveFile(abs, os.Getuid()); err != nil {
				return fail(err)
			}
		} else {
			b, err := m.covers.get(op.ID)
			if err != nil {
				return fail(err)
			}
			if err := tagio.ReplaceFile(abs, b, os.Getuid()); err != nil {
				return fail(err)
			}
		}
		if err := j.wrote(context.WithoutCancel(ctx), op.Path); err != nil {
			return err
		}
	}
	return nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
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
