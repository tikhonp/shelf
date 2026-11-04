package library

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/labstack/echo/v5"
	"github.com/tikhonp/shelf/internal/modules/library/tagio"
	"github.com/tikhonp/shelf/internal/platform/db"
	"github.com/tikhonp/shelf/internal/platform/jobs"
)

// DeleteJob removes an album's release folder, then has Navidrome forget
// its tracks.
const DeleteJob = "library.delete"

// The confirmations a delete asks for. Every delete asks the first, since
// nothing brings the files back.
const (
	ConfirmDelete       = "delete"
	ConfirmSharedFolder = "shared_folder"
)

// What became of a deleted folder's tracks in Navidrome.
const (
	// NavidromeWaiting means the job waits for Navidrome's scan to mark
	// the tracks missing.
	NavidromeWaiting = "waiting"
	NavidromeRemoved = "removed"
	// NavidromePurges means Navidrome forgets missing files in its own
	// scans.
	NavidromePurges = "purges"
	// NavidromeNone means its scan marked no track missing in time, and the
	// tracks wait on Navidrome's Missing Files page.
	NavidromeNone   = "none"
	NavidromeFailed = "failed"
	NavidromeOff    = "off"
)

// DeleteWait and deleteTries bound the wait for Navidrome's scan: about
// five minutes.
const (
	DeleteWait  = 30 * time.Second
	deleteTries = 10
)

var errDeleting = errors.New("a delete of this folder hasn't finished")

// Navidrome is what a delete asks of Navidrome. main wires it to the
// navidrome module when Navidrome is set up.
type Navidrome struct {
	ScanNow func(ctx context.Context) error
	// RemoveMissing has Navidrome forget the tracks of a deleted folder that
	// its scan marked missing. purges says Navidrome does that itself.
	RemoveMissing func(ctx context.Context, folder string) (removed int, purges bool, err error)
}

// SetNavidrome wires Navidrome for deletes. Without it a delete leaves
// Navidrome alone.
func (m *Module) SetNavidrome(n Navidrome) {
	m.mu.Lock()
	m.nav = &n
	m.mu.Unlock()
}

func (m *Module) navidrome() *Navidrome {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.nav
}

// SetForget wires the downloads module's Forget, so the items of a deleted
// folder download again.
func (m *Module) SetForget(forget func(ctx context.Context, folder string) (int64, error)) {
	m.mu.Lock()
	m.forget = forget
	m.mu.Unlock()
}

// storedDelete is a delete's request: what the folder held, and how
// Navidrome and the downloads took it.
type storedDelete struct {
	Files int   `json:"files"`
	Bytes int64 `json:"bytes"`
	// Albums are the other albums the folder held, which go with it.
	Albums []deletedAlbum `json:"albums"`
	// Forgot counts the downloaded items dropped, so they download again.
	Forgot          int64  `json:"forgot"`
	Navidrome       string `json:"navidrome"`
	NavidromeTracks int    `json:"navidrome_tracks"`
}

type deletedAlbum struct {
	Title  string `json:"title"`
	Artist string `json:"artist"`
}

// deletePayload is the delete job's payload. Tries counts the looks at
// Navidrome's missing files.
type deletePayload struct {
	Change int64 `json:"change"`
	Tries  int   `json:"tries,omitempty"`
}

// deletable says why a folder can't be deleted, or "" when it can. A top
// folder such as qobuz holds the whole of a source's downloads.
func deletable(folder string) string {
	if folder == "." || folder == "" || !strings.Contains(folder, "/") {
		return "shelf deletes a release folder, never a top folder of the music folder"
	}
	return ""
}

// folderSurvey is what a release folder holds.
type folderSurvey struct {
	files int
	bytes int64
}

// survey checks that shelf may remove a folder and all it holds, touching
// nothing: every entry is shelf's, every folder writable, the folder above
// writable, and no download works inside.
func (m *Module) survey(rel string) (folderSurvey, error) {
	var sv folderSurvey
	abs := m.abs(rel)
	st, err := os.Lstat(abs)
	if err != nil {
		return sv, &tagio.FileError{Path: abs, Err: err}
	}
	if !st.IsDir() {
		return sv, &tagio.FileError{Path: abs, Err: fmt.Errorf("%w: not a folder", tagio.ErrRefused)}
	}
	if !tagio.WritableDir(filepath.Dir(abs)) {
		return sv, &tagio.FileError{Path: filepath.Dir(abs), Err: fmt.Errorf("%w: the folder isn't writable", tagio.ErrLocked)}
	}
	busy := m.writer(nil).Busy
	uid := os.Getuid()
	err = filepath.WalkDir(abs, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return &tagio.FileError{Path: p, Err: err}
		}
		fi, err := d.Info()
		if err != nil {
			return &tagio.FileError{Path: p, Err: err}
		}
		if tagio.Locked(fi, uid) {
			return &tagio.FileError{Path: p, Err: fmt.Errorf("%w: another user owns it", tagio.ErrLocked)}
		}
		if d.IsDir() {
			if busy != nil && busy(p) {
				return &tagio.FileError{Path: p, Err: tagio.ErrBusy}
			}
			if !tagio.WritableDir(p) {
				return &tagio.FileError{Path: p, Err: fmt.Errorf("%w: the folder isn't writable", tagio.ErrLocked)}
			}
			return nil
		}
		sv.files++
		sv.bytes += fi.Size()
		return nil
	})
	return sv, err
}

// deleteAlbum checks that shelf may remove an album's release folder, asks
// the owner to confirm, and queues the delete.
func (m *Module) deleteAlbum(c *echo.Context) error {
	id, err := idParam(c, "album")
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
	row, err := m.store.album(ctx, id)
	if errors.Is(err, errNotFound) {
		return echo.NewHTTPError(http.StatusNotFound, "no such album")
	}
	if err != nil {
		return err
	}
	if msg := deletable(row.Folder); msg != "" {
		return c.JSON(http.StatusUnprocessableEntity, refusal{Error: msg, Reason: ReasonRefused})
	}
	sv, err := m.survey(row.Folder)
	if err != nil {
		return m.refuse(c, err, nil)
	}
	inside, err := m.store.albumsUnder(ctx, row.Folder)
	if err != nil {
		return err
	}
	var others []deletedAlbum
	for _, a := range inside {
		if a.ID != row.ID {
			others = append(others, deletedAlbum{Title: a.Title, Artist: a.Artist})
		}
	}
	if ok, err := confirmed(c, deleteWarnings(sv, others), b.Confirm); !ok {
		return err
	}
	sd := storedDelete{Files: sv.files, Bytes: sv.bytes, Albums: others, Navidrome: NavidromeOff}
	if m.navidrome() != nil {
		sd.Navidrome = NavidromeWaiting
	}
	req, err := json.Marshal(sd)
	if err != nil {
		return err
	}
	cr := changeRow{Kind: KindDelete, AlbumID: row.ID, Folder: row.Folder, Title: row.Title, Artist: row.Artist,
		State: StateQueued, Request: string(req), CreatedAt: db.At(time.Now())}
	err = m.queueJob(ctx, &cr, "web", DeleteJob)
	if errors.Is(err, errDeleting) {
		return echo.NewHTTPError(http.StatusConflict, err.Error())
	}
	if err != nil {
		return err
	}
	return c.JSON(http.StatusAccepted, m.changeJSON(ctx, cr))
}

// deleteWarnings are what a delete asks the owner to confirm.
func deleteWarnings(sv folderSurvey, others []deletedAlbum) []warning {
	out := []warning{{Kind: ConfirmDelete, Message: fmt.Sprintf(
		"This deletes %s, %s, from the disk, and nothing brings them back. Navidrome forgets the album, with its star, rating, plays and place in playlists.",
		plural(sv.files, "file"), sizeText(sv.bytes))}}
	if len(others) == 0 {
		return out
	}
	names := make([]string, 0, 3)
	for _, a := range others[:min(3, len(others))] {
		names = append(names, a.Title+" by "+a.Artist)
	}
	list := strings.Join(names, "; ")
	if len(others) > 3 {
		list += fmt.Sprintf("; and %d more", len(others)-3)
	}
	msg := "Another album is in this folder and goes too: " + list + "."
	if len(others) > 1 {
		msg = fmt.Sprintf("%d other albums are in this folder and go too: %s.", len(others), list)
	}
	return append(out, warning{Kind: ConfirmSharedFolder, Message: msg})
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// sizeText is a size in decimal units, as the web app shows sizes.
func sizeText(b int64) string {
	switch {
	case b >= 1e9:
		return fmt.Sprintf("%.1f GB", float64(b)/1e9)
	case b >= 1e6:
		return fmt.Sprintf("%.0f MB", float64(b)/1e6)
	case b >= 1e3:
		return fmt.Sprintf("%.0f KB", float64(b)/1e3)
	}
	return fmt.Sprintf("%d bytes", b)
}

// albumsUnder are the albums in folder and in the folders under it.
func (s store) albumsUnder(ctx context.Context, folder string) ([]albumRow, error) {
	var rows []albumRow
	err := s.db.R.SelectContext(ctx, &rows, `SELECT `+albumCols+` FROM library_albums
		WHERE folder = ? OR substr(folder, 1, ?) = ? ORDER BY id`, folder, len(folder)+1, folder+"/")
	return rows, err
}

// deleteStep removes the folder. The checks run again, since the folder may
// have changed while the delete waited. A delete a stop cut off goes on
// with what is left.
func (m *Module) deleteStep(ctx context.Context, r *jobs.Run) error {
	var p deletePayload
	if err := r.Payload(&p); err != nil {
		return jobs.Permanent(err)
	}
	c, err := m.store.change(ctx, m.store.db.R, p.Change)
	if err != nil {
		return jobs.Permanent(err)
	}
	var sd storedDelete
	if err := json.Unmarshal([]byte(c.Request), &sd); err != nil {
		return jobs.Permanent(err)
	}
	bg := context.WithoutCancel(ctx)
	switch c.State {
	case StateQueued:
		sv, err := m.survey(c.Folder)
		if err != nil {
			r.Log().Warn("Not deleted: %s", m.message(err))
			return m.endDelete(bg, c, StateFailed, err)
		}
		sd.Files, sd.Bytes = sv.files, sv.bytes
		if err := m.store.startDelete(bg, c.ID, sd); err != nil {
			return err
		}
	case StateWriting:
		r.Log().Warn("The delete was cut off; deleting what is left")
	default:
		return nil
	}
	abs := m.abs(c.Folder)
	// RemoveAll goes on past a file it can't remove, so a failure leaves
	// only what it couldn't.
	rerr := os.RemoveAll(abs)
	if rerr == nil {
		rerr = syncFolder(filepath.Dir(abs))
	}
	state := StateDone
	if rerr != nil {
		state = StatePartial
		if countFiles(abs) >= sd.Files {
			state = StateFailed
		}
		r.Log().Warn("The delete stopped: %s", m.message(rerr))
	} else {
		r.Log().Info("Deleted %s, %s, in %s", plural(sd.Files, "file"), sizeText(sd.Bytes), c.Folder)
	}
	return m.endDelete(bg, c, state, rerr)
}

// endDelete stores how the delete went, has the index walk and the
// downloads forget the folder, and tells the editor.
func (m *Module) endDelete(ctx context.Context, c changeRow, state string, werr error) error {
	reason, msg := "", ""
	if werr != nil {
		reason, msg = reasonOf(werr), m.message(werr)
	}
	err := m.store.db.Write(ctx, func(tx *sqlx.Tx) error {
		var out changeRow
		return m.store.finish(ctx, tx, c.ID, state, reason, msg, time.Now(), &out)
	})
	if err != nil {
		return err
	}
	if state != StateFailed {
		m.Kick()
		m.mu.Lock()
		forget := m.forget
		m.mu.Unlock()
		if forget != nil {
			n, err := forget(ctx, c.Folder)
			if err != nil {
				m.log.Error("library: forget a deleted folder's downloads", "folder", c.Folder, "error", err)
			} else if err := m.store.updateDelete(ctx, c.ID, func(sd *storedDelete) { sd.Forgot = n }); err != nil {
				return err
			}
		}
	} else if err := m.store.updateDelete(ctx, c.ID, func(sd *storedDelete) { sd.Navidrome = "" }); err != nil {
		return err
	}
	m.publishDelete(ctx, c.ID)
	return nil
}

// navidromeStep has Navidrome forget the deleted tracks. Navidrome forgets
// only tracks its scan marked missing, so the step asks for a scan, then
// looks every DeleteWait until they show, up to deleteTries times.
func (m *Module) navidromeStep(ctx context.Context, r *jobs.Run) error {
	var p deletePayload
	if err := r.Payload(&p); err != nil {
		return jobs.Permanent(err)
	}
	c, err := m.store.change(ctx, m.store.db.R, p.Change)
	if err != nil {
		return jobs.Permanent(err)
	}
	nav := m.navidrome()
	if c.State == StateFailed || nav == nil {
		return nil
	}
	if p.Tries == 0 {
		if err := nav.ScanNow(ctx); err != nil {
			r.Log().Warn("Navidrome didn't take the scan: %v", err)
		}
		p.Tries = 1
		if err := r.SavePayload(ctx, p); err != nil {
			return err
		}
		return jobs.Defer(m.DeleteWait, "waiting for Navidrome's scan")
	}
	removed, purges, err := nav.RemoveMissing(ctx, c.Folder)
	set := func(f func(sd *storedDelete)) error {
		if err := m.store.updateDelete(context.WithoutCancel(ctx), c.ID, f); err != nil {
			return err
		}
		m.publishDelete(ctx, c.ID)
		return nil
	}
	switch {
	case err == nil && purges:
		r.Log().Info("Navidrome forgets missing files by itself")
		return set(func(sd *storedDelete) { sd.Navidrome = NavidromePurges })
	case err == nil && removed > 0:
		r.Log().Info("Navidrome forgot %s", plural(removed, "track"))
		return set(func(sd *storedDelete) { sd.Navidrome, sd.NavidromeTracks = NavidromeRemoved, removed })
	case ctx.Err() != nil:
		return ctx.Err()
	case p.Tries >= deleteTries:
		if err != nil {
			r.Log().Warn("Navidrome didn't forget the tracks: %v", err)
			return set(func(sd *storedDelete) { sd.Navidrome = NavidromeFailed })
		}
		r.Log().Warn("Navidrome's scan marked no track of %s missing", c.Folder)
		return set(func(sd *storedDelete) { sd.Navidrome = NavidromeNone })
	}
	if err != nil {
		r.Log().Warn("Navidrome didn't answer: %v", err)
	}
	p.Tries++
	if err := r.SavePayload(ctx, p); err != nil {
		return err
	}
	return jobs.Defer(m.DeleteWait, "waiting for Navidrome to mark the tracks missing")
}

// startDelete moves a queued delete to writing, with what the folder holds
// now.
func (s store) startDelete(ctx context.Context, id int64, sd storedDelete) error {
	req, err := json.Marshal(sd)
	if err != nil {
		return err
	}
	return s.db.Write(ctx, func(tx *sqlx.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE library_changes SET state = ?, request = ? WHERE id = ? AND state = ?`,
			StateWriting, string(req), id, StateQueued)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return errors.New("the delete isn't queued")
		}
		return nil
	})
}

// updateDelete changes a delete's request.
func (s store) updateDelete(ctx context.Context, id int64, f func(sd *storedDelete)) error {
	return s.db.Write(ctx, func(tx *sqlx.Tx) error {
		c, err := s.change(ctx, tx, id)
		if err != nil {
			return err
		}
		var sd storedDelete
		if err := json.Unmarshal([]byte(c.Request), &sd); err != nil {
			return err
		}
		f(&sd)
		req, err := json.Marshal(sd)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE library_changes SET request = ? WHERE id = ?`, string(req), id)
		return err
	})
}

func (m *Module) publishDelete(ctx context.Context, id int64) {
	c, err := m.store.change(ctx, m.store.db.R, id)
	if err != nil {
		m.log.Error("library: read a change", "change", id, "error", err)
		return
	}
	m.publishChange(ctx, c)
}

// message is an error with paths made relative to the music folder.
func (m *Module) message(err error) string { return (&journal{root: m.root}).message(err) }

// countFiles counts what is left of a folder, whoever owns it.
func countFiles(dir string) int {
	n := 0
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			n++
		}
		return nil
	})
	return n
}

func syncFolder(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}
