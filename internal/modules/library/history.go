package library

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"time"

	"github.com/labstack/echo/v5"
)

// changeJSON is a change as the editor and the history show it.
type changeJSON struct {
	ID     int64  `json:"id"`
	Kind   string `json:"kind"`
	Target *int64 `json:"target"`
	// AlbumID is the album the change's files belong to now. A Save that
	// changes the album's Navidrome ID gives it a new row after the next
	// walk.
	AlbumID   int64          `json:"album_id"`
	Folder    string         `json:"folder"`
	Title     string         `json:"title"`
	Artist    string         `json:"artist"`
	State     string         `json:"state"`
	Reason    string         `json:"reason"`
	Error     string         `json:"error"`
	Undone    bool           `json:"undone"`
	Tags      int            `json:"tags"`
	Conflicts []conflictJSON `json:"conflicts"`
	JobID     *int64         `json:"job_id"`
	CreatedAt string         `json:"created_at"`
	// FinishedAt is null until the write ends.
	FinishedAt *string `json:"finished_at"`
	// Files are in the detail only.
	Files []changeFileJSON `json:"files,omitempty"`
}

// conflictJSON is a key an undo or redo left alone, because the file no
// longer held the value the change wrote.
type conflictJSON struct {
	Path string `json:"path"`
	Key  string `json:"key"`
}

type changeFileJSON struct {
	// TrackID is the file's row now, or 0 when the index has lost it.
	TrackID int64  `json:"track_id"`
	Path    string `json:"path"`
	// Kind is "audio", or "cover" for a folder's cover file.
	Kind    string          `json:"kind"`
	Written bool            `json:"written"`
	Keys    []keyChangeJSON `json:"keys"`
	// CoverBefore and CoverAfter are cover IDs under /api/library/covers,
	// "" for none. They are equal when the change left the cover alone.
	CoverBefore string `json:"cover_before"`
	CoverAfter  string `json:"cover_after"`
}

// keyChangeJSON is one key a change set, added or removed.
type keyChangeJSON struct {
	Key    string   `json:"key"`
	Before []string `json:"before"`
	After  []string `json:"after"`
}

func (m *Module) changeJSON(ctx context.Context, c changeRow) changeJSON {
	j := changeJSON{ID: c.ID, Kind: c.Kind, Target: c.Target, AlbumID: c.AlbumID, Folder: c.Folder, Title: c.Title,
		Artist: c.Artist, State: c.State, Reason: c.Reason, Error: c.Error, Undone: c.Undone, Tags: c.Tags,
		JobID: c.JobID, CreatedAt: c.CreatedAt.UTC().Format(time.RFC3339), Conflicts: []conflictJSON{}}
	if !c.FinishedAt.IsZero() {
		s := c.FinishedAt.UTC().Format(time.RFC3339)
		j.FinishedAt = &s
	}
	_ = json.Unmarshal([]byte(c.Conflicts), &j.Conflicts)
	if j.Conflicts == nil {
		j.Conflicts = []conflictJSON{}
	}
	var sc storedChange
	if json.Unmarshal([]byte(c.Request), &sc) == nil && len(sc.Files) > 0 {
		if id, err := m.store.albumOf(ctx, sc.Files[0].Path); err == nil && id != 0 {
			j.AlbumID = id
		}
	}
	return j
}

func (m *Module) publishChange(ctx context.Context, c changeRow) {
	if m.hub != nil {
		m.hub.Publish(ChangeTopic, m.changeJSON(ctx, c))
	}
}

// getChange answers one change with each file's keys before and after.
func (m *Module) getChange(c *echo.Context) error {
	id, err := idParam(c, "change")
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	row, err := m.store.change(ctx, m.store.db.R, id)
	if errors.Is(err, errNotFound) {
		return echo.NewHTTPError(http.StatusNotFound, "no such change")
	}
	if err != nil {
		return err
	}
	files, err := m.store.changeFiles(ctx, id)
	if err != nil {
		return err
	}
	out := m.changeJSON(ctx, row)
	out.Files = []changeFileJSON{}
	for _, f := range files {
		fj := changeFileJSON{Path: f.Path, Kind: f.Kind, Written: f.Written, Keys: diffKeys(f.Before, f.After),
			CoverBefore: f.CoverBefore, CoverAfter: f.CoverAfter}
		if tf, err := m.store.fileByPath(ctx, f.Path); err == nil {
			fj.TrackID = tf
		}
		out.Files = append(out.Files, fj)
	}
	return c.JSON(http.StatusOK, out)
}

// diffKeys lists the keys whose values differ, in key order.
func diffKeys(before, after map[string][]string) []keyChangeJSON {
	var keys []string
	for k, v := range after {
		if !slices.Equal(before[k], v) {
			keys = append(keys, k)
		}
	}
	for k := range before {
		if _, ok := after[k]; !ok {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	out := make([]keyChangeJSON, len(keys))
	for i, k := range keys {
		out[i] = keyChangeJSON{Key: k, Before: nonNil(before[k]), After: nonNil(after[k])}
	}
	return out
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
