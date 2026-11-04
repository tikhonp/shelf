package library

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/labstack/echo/v5"
	"github.com/tikhonp/shelf/internal/modules/library/tagio"
	"github.com/tikhonp/shelf/internal/platform/db"
	"github.com/tikhonp/shelf/internal/platform/jobs"
)

// HistoryLimit is how many saves the history keeps. The retention job
// trims older ones.
const HistoryLimit = 1000

// RetentionJob trims the history every night.
const RetentionJob = "library.retention"

var errReversing = errors.New("an undo or redo of this save hasn't finished")

// releaseGroup are the fields that name one MusicBrainz release. An undo
// or redo restores all of them or none, so a file never ends with IDs from
// two releases.
var releaseGroup = []string{
	"musicbrainz_albumid", "musicbrainz_releasegroupid", "musicbrainz_albumartistid",
	"musicbrainz_trackid", "musicbrainz_discid",
}

func inReleaseGroup(ft *tagio.Format, key string) bool {
	return slices.ContainsFunc(tagio.FieldsOf(ft, key), func(f tagio.Field) bool { return slices.Contains(releaseGroup, f.Name) })
}

// reversal is an undo or redo worked out from the files as they are now.
type reversal struct {
	change    tagio.Change
	stored    storedChange
	trackOf   map[string]int64
	conflicts []conflictJSON
}

// reverse works out the undo or redo of a save's files. A key goes back
// only while it holds what the save wrote, or, for a redo, what it found.
// Any other value is a conflict and stays.
func (m *Module) reverse(ctx context.Context, kind string, files []changeFile) reversal {
	type op struct {
		path, key string
		values    []string
		group     bool
		// cover, when set, is the front cover to put back.
		cover *string
	}
	var ops []op
	var conflicts []conflictJSON
	out := reversal{trackOf: map[string]int64{}}
	groupConflict := false
	revs := map[string]string{}
	for _, f := range files {
		if !f.Written {
			continue
		}
		from, to := f.After, f.Before
		fromC, toC := f.CoverAfter, f.CoverBefore
		if kind == KindRedo {
			from, to = f.Before, f.After
			fromC, toC = toC, fromC
		}
		abs := m.abs(f.Path)
		if f.Kind == fileCover {
			switch h, _, err := fileHash(abs); {
			case err == nil && h == toC:
			case err == nil && h == fromC:
				out.stored.Folder = append(out.stored.Folder, folderOp{Path: f.Path, ID: toC, Was: fromC})
			default:
				conflicts = append(conflicts, conflictJSON{Path: f.Path, Key: "cover"})
			}
			continue
		}
		if fromC != toC {
			b, err := tagio.FrontCover(abs)
			switch h := coverHash(b); {
			case err == nil && h == toC:
			case err == nil && h == fromC:
				ops = append(ops, op{path: f.Path, cover: &toC})
			default:
				conflicts = append(conflicts, conflictJSON{Path: f.Path, Key: "cover"})
			}
		}
		ft := tagio.FormatOf(abs)
		tf, err := tagio.Read(abs)
		for _, k := range diffKeys(f.Before, f.After) {
			group := inReleaseGroup(ft, k.Key)
			var cur []string
			if err == nil {
				cur = tf.Tags[k.Key]
			}
			switch {
			case err == nil && slices.Equal(cur, to[k.Key]):
			case err == nil && slices.Equal(cur, from[k.Key]):
				ops = append(ops, op{path: f.Path, key: k.Key, values: to[k.Key], group: group})
			default:
				conflicts = append(conflicts, conflictJSON{Path: f.Path, Key: k.Key})
				groupConflict = groupConflict || group
			}
		}
		if err == nil {
			revs[f.Path] = tf.Revision()
		}
	}
	byPath := map[string]int{}
	for _, o := range ops {
		if o.group && groupConflict {
			conflicts = append(conflicts, conflictJSON{Path: o.path, Key: o.key})
			continue
		}
		i, ok := byPath[o.path]
		if !ok {
			i = len(out.stored.Files)
			byPath[o.path] = i
			out.stored.Files = append(out.stored.Files, storedFile{Path: o.path, Revision: revs[o.path]})
			out.change.Files = append(out.change.Files, tagio.FileChange{Path: m.abs(o.path), Revision: revs[o.path]})
			if id, err := m.store.fileByPath(ctx, o.path); err == nil {
				out.trackOf[m.abs(o.path)] = id
			}
		}
		if o.cover != nil {
			var data []byte
			if *o.cover != "" {
				b, err := m.covers.get(*o.cover)
				if err != nil {
					conflicts = append(conflicts, conflictJSON{Path: o.path, Key: "cover"})
					continue
				}
				data = b
			}
			out.stored.Files[i].Cover = &storedCover{ID: *o.cover}
			out.change.Files[i].Cover = &tagio.Cover{Data: data}
			continue
		}
		out.stored.Files[i].Edits = append(out.stored.Files[i].Edits, editJSON{Key: o.key, Values: o.values})
		out.change.Files[i].Edits = append(out.change.Files[i].Edits, tagio.Edit{Key: o.key, Values: o.values})
	}
	slices.SortFunc(conflicts, func(a, b conflictJSON) int {
		return cmp.Or(cmp.Compare(a.Path, b.Path), cmp.Compare(a.Key, b.Key))
	})
	out.conflicts = conflicts
	return out
}

// undo and redo queue the reversal of a save. The body may confirm what
// Navidrome will do, as a Save's does.
func (m *Module) undo(c *echo.Context) error { return m.reverseSave(c, KindUndo) }
func (m *Module) redo(c *echo.Context) error { return m.reverseSave(c, KindRedo) }

func (m *Module) reverseSave(c *echo.Context, kind string) error {
	id, err := idParam(c, "change")
	if err != nil {
		return err
	}
	var b struct {
		Confirm []string `json:"confirm"`
	}
	if err := json.NewDecoder(c.Request().Body).Decode(&b); err != nil && !errors.Is(err, io.EOF) {
		return echo.NewHTTPError(http.StatusBadRequest, "the body is not JSON")
	}
	ctx := c.Request().Context()
	save, err := m.store.change(ctx, m.store.db.R, id)
	if errors.Is(err, errNotFound) || err == nil && save.Kind != KindSave {
		return echo.NewHTTPError(http.StatusNotFound, "no such save")
	}
	if err != nil {
		return err
	}
	switch {
	case save.State != StateDone && save.State != StatePartial:
		return echo.NewHTTPError(http.StatusConflict, "this save wrote nothing")
	case kind == KindUndo && save.Undone:
		return echo.NewHTTPError(http.StatusConflict, "this save is undone already")
	case kind == KindRedo && !save.Undone:
		return echo.NewHTTPError(http.StatusConflict, "this save isn't undone")
	}
	files, err := m.store.changeFiles(ctx, id)
	if err != nil {
		return err
	}
	rv := m.reverse(ctx, kind, files)
	if len(rv.change.Files) == 0 && len(rv.stored.Folder) == 0 {
		return c.JSON(http.StatusConflict, refusal{Error: "every tag this save changed has changed since", Reason: ReasonConflict, Conflicts: rv.conflicts})
	}
	planned, err := m.writer(nil).Check(rv.change)
	if err != nil {
		return m.refuse(c, err, rv.trackOf)
	}
	albums, err := m.albumsOf(ctx, rv.stored)
	if err != nil {
		return err
	}
	warns, err := m.identity(ctx, albums, planned)
	if err != nil {
		return err
	}
	if ok, err := confirmed(c, warns, b.Confirm); !ok {
		return err
	}
	req, err := json.Marshal(rv.stored)
	if err != nil {
		return err
	}
	conflicts, err := json.Marshal(rv.conflicts)
	if err != nil {
		return err
	}
	cr := changeRow{Kind: kind, Target: &save.ID, AlbumID: save.AlbumID, Folder: save.Folder, Title: save.Title,
		Artist: save.Artist, State: StateQueued, Request: string(req), Conflicts: string(conflicts), CreatedAt: db.At(time.Now())}
	if len(albums) > 0 {
		cr.AlbumID = albums[0].row.ID
	}
	err = m.queueChange(ctx, &cr, "web")
	if errors.Is(err, errReversing) {
		return echo.NewHTTPError(http.StatusConflict, err.Error())
	}
	if err != nil {
		return err
	}
	return c.JSON(http.StatusAccepted, m.changeJSON(ctx, cr))
}

// albumsOf finds the albums a write's files belong to now, with their files.
func (m *Module) albumsOf(ctx context.Context, sc storedChange) ([]albumFiles, error) {
	var out []albumFiles
	seen := map[int64]bool{}
	for _, f := range sc.Files {
		id, err := m.store.albumOf(ctx, f.Path)
		if err != nil {
			return nil, err
		}
		if id == 0 || seen[id] {
			continue
		}
		seen[id] = true
		row, err := m.store.album(ctx, id)
		if err != nil {
			return nil, err
		}
		files, err := m.store.albumFiles(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, albumFiles{row, files})
	}
	return out, nil
}

type historyJSON struct {
	Changes []changeJSON `json:"changes"`
	// Next is the before of the next page, or null.
	Next *int64 `json:"next"`
}

// history lists the saves, newest first.
func (m *Module) history(c *echo.Context) error {
	limit, err := intParam(c, "limit", 50, 1, 200)
	if err != nil {
		return err
	}
	before, err := intParam(c, "before", 0, 0, 1<<62)
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	rows, err := m.store.saves(ctx, int64(before), limit+1)
	if err != nil {
		return err
	}
	out := historyJSON{Changes: []changeJSON{}}
	if len(rows) > limit {
		rows = rows[:limit]
		next := rows[limit-1].ID
		out.Next = &next
	}
	for _, r := range rows {
		out.Changes = append(out.Changes, m.changeJSON(ctx, r))
	}
	return c.JSON(http.StatusOK, out)
}

// clearHistory deletes every change that has ended, except a save whose
// undo or redo is still to run.
func (m *Module) clearHistory(c *echo.Context) error {
	n, err := m.store.trim(c.Request().Context(), 0)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]int64{"deleted": n})
}

func (s store) saves(ctx context.Context, before int64, limit int) ([]changeRow, error) {
	q := `SELECT ` + changeCols + ` FROM library_changes WHERE kind = 'save'`
	args := []any{}
	if before > 0 {
		q += ` AND id < ?`
		args = append(args, before)
	}
	var rows []changeRow
	err := s.db.R.SelectContext(ctx, &rows, q+` ORDER BY id DESC LIMIT ?`, append(args, limit)...)
	return rows, err
}

// trim deletes the ended changes beyond the newest keep saves, or all of
// them when keep is 0. A save keeps its row while an undo or redo of it is
// still to run, and each save takes its undos and redos with it.
func (s store) trim(ctx context.Context, keep int) (int64, error) {
	var n int64
	err := s.db.Write(ctx, func(tx *sqlx.Tx) error {
		cond := `state NOT IN ('queued', 'writing') AND NOT EXISTS (SELECT 1 FROM library_changes x
			WHERE x.target = library_changes.id AND x.state IN ('queued', 'writing'))`
		var res interface{ RowsAffected() (int64, error) }
		var err error
		if keep == 0 {
			res, err = tx.ExecContext(ctx, `DELETE FROM library_changes WHERE `+cond)
		} else {
			res, err = tx.ExecContext(ctx, `DELETE FROM library_changes WHERE kind = 'save' AND `+cond+` AND id < (
				SELECT id FROM library_changes WHERE kind = 'save' ORDER BY id DESC LIMIT 1 OFFSET ?)`, keep-1)
		}
		if err != nil {
			return err
		}
		n, err = res.RowsAffected()
		return err
	})
	return n, err
}

// retain is the retention job's step.
func (m *Module) retain(ctx context.Context, r *jobs.Run) error {
	n, err := m.store.trim(ctx, HistoryLimit)
	if err != nil {
		return err
	}
	if n > 0 {
		r.Log().Info("Removed %s old changes from the history", strconv.FormatInt(n, 10))
	}
	used, err := m.store.coverIDs(ctx)
	if err != nil {
		return err
	}
	removed, err := m.covers.sweep(used, time.Now())
	if removed > 0 {
		r.Log().Info("Removed %d covers no change needs", removed)
	}
	return err
}

// coverIDs are the covers the history refers to.
func (s store) coverIDs(ctx context.Context) (map[string]bool, error) {
	var ids []string
	if err := s.db.R.SelectContext(ctx, &ids, `SELECT cover_before FROM library_change_files WHERE cover_before != ''
		UNION SELECT cover_after FROM library_change_files WHERE cover_after != ''`); err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	// A queued change's covers are in its request.
	var reqs []string
	if err := s.db.R.SelectContext(ctx, &reqs, `SELECT request FROM library_changes WHERE state IN ('queued', 'writing')`); err != nil {
		return nil, err
	}
	for _, r := range reqs {
		var sc storedChange
		if json.Unmarshal([]byte(r), &sc) != nil {
			continue
		}
		for _, f := range sc.Files {
			if f.Cover != nil && f.Cover.ID != "" {
				out[f.Cover.ID] = true
			}
		}
		for _, op := range sc.Folder {
			out[op.ID], out[op.Was] = true, true
		}
	}
	delete(out, "")
	return out, nil
}
