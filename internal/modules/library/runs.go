package library

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/labstack/echo/v5"
	"github.com/tikhonp/shelf/internal/modules/library/loudness"
	"github.com/tikhonp/shelf/internal/modules/library/tagio"
	"github.com/tikhonp/shelf/internal/platform/db"
	"github.com/tikhonp/shelf/internal/platform/jobs"
)

// The jobs of a bulk run. RunJob writes one album per slice on the tags
// queue, so a Save waits for one album at most. AnalyzeJob measures ahead of
// it on the analysis queue.
const (
	RunJob     = "library.run"
	AnalyzeJob = "library.analyze"
)

// RunTopic is the hub topic of a run's progress.
const RunTopic = "run"

// The states of an album in a run.
const (
	albumWaiting   = "waiting"
	albumAnalyzing = "analyzing"
	albumReady     = "ready"
	albumWriting   = "writing"
	albumDone      = "done"
	albumSkipped   = "skipped"
	albumFailed    = "failed"
	albumCancelled = "cancelled"
)

const (
	// sampleAlbums and maxSamples bound what a preview reads and shows.
	sampleAlbums = 5
	maxSamples   = 6
	// logLines is how much of a run's log the API answers.
	logLines = 100
	// detailFiles is how many files a run's detail lists.
	detailFiles = 40
	// maxConflicts is how many an undo of a run keeps. The history names a
	// conflict's keys, which repeat album after album.
	maxConflicts = 200
)

var errRunActive = errors.New("a bulk run is working; wait for it or cancel it")

// runRow is what a run keeps beside its head change.
type runRow struct {
	ChangeID    int64  `db:"change_id"`
	Fixes       string `db:"fixes"`
	Cancelled   bool   `db:"cancelled"`
	AnalysisJob *int64 `db:"analysis_job"`
}

func (r runRow) fixes() []string {
	out := []string{}
	_ = json.Unmarshal([]byte(r.Fixes), &out)
	return out
}

// runAlbum is one album of a run.
type runAlbum struct {
	Run        int64   `db:"run"`
	Position   int     `db:"position"`
	AlbumID    int64   `db:"album_id"`
	Target     *int64  `db:"target"`
	ChangeID   *int64  `db:"change_id"`
	Title      string  `db:"title"`
	Artist     string  `db:"artist"`
	State      string  `db:"state"`
	Gains      string  `db:"gains"`
	Fixes      int     `db:"fixes"`
	Files      int     `db:"files"`
	Message    string  `db:"message"`
	FinishedAt db.Time `db:"finished_at"`
}

const runAlbumCols = `run, position, album_id, target, change_id, title, artist, state, gains, fixes, files, message, finished_at`

// selection names a run's albums: a list, or everything a list query finds,
// because the list holds only the pages the owner has scrolled through.
type selection struct {
	Albums []int64 `json:"albums"`
	All    bool    `json:"all"`
	Q      string  `json:"q"`
	Filter string  `json:"filter"`
	Folder string  `json:"folder"`
	Source string  `json:"source"`
	Except []int64 `json:"except"`
}

type runBody struct {
	Selection selection `json:"selection"`
	Fixes     []string  `json:"fixes"`
}

func validFilter(f string) bool {
	return f == "" || f == "all" || f == "issues" || f == "guide" || f == "no_mbid" || slices.Contains(Kinds, Kind(f))
}

func (m *Module) selected(ctx context.Context, sel selection) ([]albumRow, error) {
	if !sel.All {
		ids, err := json.Marshal(sel.Albums)
		if err != nil {
			return nil, err
		}
		var rows []albumRow
		err = m.store.db.R.SelectContext(ctx, &rows, `SELECT `+albumCols+` FROM library_albums
			WHERE id IN (SELECT value FROM json_each(?)) ORDER BY title COLLATE NOCASE, artist COLLATE NOCASE`, string(ids))
		return rows, err
	}
	if !validFilter(sel.Filter) {
		return nil, echo.NewHTTPError(http.StatusBadRequest, "unknown filter "+strconv.Quote(sel.Filter))
	}
	if !m.validSource(sel.Source) {
		return nil, echo.NewHTTPError(http.StatusBadRequest, "unknown source "+strconv.Quote(sel.Source))
	}
	q := listQuery{Search: strings.TrimSpace(sel.Q), Folder: strings.Trim(sel.Folder, "/"), Source: sel.Source, Sort: "album", Limit: 1 << 30}
	if sel.Filter != "all" {
		q.Filter = sel.Filter
	}
	rows, _, err := m.store.albums(ctx, q)
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(rows, func(r albumRow) bool { return slices.Contains(sel.Except, r.ID) }), nil
}

// wants reports whether a fix has work on an album, by what the index knows.
func wants(r albumRow, fix string) bool {
	return fix == FixReplayGain || slices.ContainsFunc(toJSON(r).Issues, func(i Issue) bool { return string(i.Kind) == fix })
}

func readRunBody(c *echo.Context) (runBody, error) {
	var b runBody
	if err := json.NewDecoder(c.Request().Body).Decode(&b); err != nil {
		return b, echo.NewHTTPError(http.StatusBadRequest, "the body is not JSON with a selection and fixes")
	}
	for _, f := range b.Fixes {
		if !slices.Contains(runFixes, f) {
			return b, echo.NewHTTPError(http.StatusBadRequest, "a run can't apply "+strconv.Quote(f))
		}
	}
	return b, nil
}

type fixCountJSON struct {
	Fix    string `json:"fix"`
	Albums int    `json:"albums"`
}

type sampleJSON struct {
	Album  string `json:"album"`
	Key    string `json:"key"`
	Before string `json:"before"`
	After  string `json:"after"`
}

// previewJSON is what a run would touch. Files and Bytes count whole albums,
// as a retag rewrites each file and the backups copy it again, and they are a
// ceiling: an album may turn out to have nothing to change.
type previewJSON struct {
	Selected int            `json:"selected"`
	Fixes    []fixCountJSON `json:"fixes"`
	Albums   int            `json:"albums"`
	Files    int            `json:"files"`
	Bytes    int64          `json:"bytes"`
	Samples  []sampleJSON   `json:"samples"`
}

func (m *Module) previewRun(c *echo.Context) error {
	b, err := readRunBody(c)
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	rows, err := m.selected(ctx, b.Selection)
	if err != nil {
		return err
	}
	out := previewJSON{Selected: len(rows), Fixes: []fixCountJSON{}, Samples: []sampleJSON{}}
	for _, fix := range runFixes {
		n := 0
		for _, r := range rows {
			if wants(r, fix) {
				n++
			}
		}
		out.Fixes = append(out.Fixes, fixCountJSON{Fix: fix, Albums: n})
	}
	chosen := chosenAlbums(rows, b.Fixes)
	for _, r := range chosen {
		out.Albums++
		out.Files += r.Tracks
		out.Bytes += r.Size
	}
	out.Samples = m.samples(ctx, chosen, b.Fixes)
	return c.JSON(http.StatusOK, out)
}

func chosenAlbums(rows []albumRow, fixes []string) []albumRow {
	return slices.DeleteFunc(slices.Clone(rows), func(r albumRow) bool {
		return !slices.ContainsFunc(fixes, func(f string) bool { return wants(r, f) })
	})
}

// samples plans the first albums for real, so the preview shows what a run
// would write. ReplayGain isn't known until the run decodes the audio.
func (m *Module) samples(ctx context.Context, rows []albumRow, fixes []string) []sampleJSON {
	out := []sampleJSON{}
	for i, r := range rows {
		if i == sampleAlbums || len(out) >= maxSamples {
			break
		}
		p, err := m.planAlbum(ctx, r.ID, fixes, nil)
		if err != nil || len(p.stored.Files) == 0 {
			continue
		}
		f := p.stored.Files[0]
		for _, e := range f.Edits {
			if len(out) < maxSamples {
				out = append(out, sampleJSON{Album: r.Title, Key: e.Key, Before: strings.Join(p.before[f.Path][e.Key], ", "), After: strings.Join(e.Values, ", ")})
			}
		}
	}
	if slices.Contains(fixes, FixReplayGain) && len(rows) > 0 {
		out = append(out, sampleJSON{Album: rows[0].Title, Key: "REPLAYGAIN_ALBUM_GAIN", After: "Measured in the run"})
	}
	return out
}

// startRun stores the selection and queues the work. A run needs no more
// answers from here on, so what a Save would ask about, it skips.
func (m *Module) startRun(c *echo.Context) error {
	b, err := readRunBody(c)
	if err != nil {
		return err
	}
	if len(b.Fixes) == 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "a run applies at least one fix")
	}
	ctx := c.Request().Context()
	rows, err := m.selected(ctx, b.Selection)
	if err != nil {
		return err
	}
	rows = chosenAlbums(rows, b.Fixes)
	if len(rows) == 0 {
		return c.JSON(http.StatusUnprocessableEntity, refusal{Error: "none of these albums has anything to fix", Reason: ReasonRefused})
	}
	state := albumReady
	if slices.Contains(b.Fixes, FixReplayGain) {
		state = albumWaiting
	}
	albums := make([]runAlbum, len(rows))
	for i, r := range rows {
		albums[i] = runAlbum{Position: i, AlbumID: r.ID, Title: r.Title, Artist: r.Artist, State: state}
	}
	head := changeRow{Kind: KindSave, State: StateQueued, Request: "{}", CreatedAt: db.At(time.Now())}
	if err := m.queueRun(ctx, &head, b.Fixes, albums); err != nil {
		if errors.Is(err, errRunActive) {
			return echo.NewHTTPError(http.StatusConflict, err.Error())
		}
		return err
	}
	return m.answerRun(c, head.ID)
}

func (m *Module) answerRun(c *echo.Context, id int64) error {
	ctx := c.Request().Context()
	head, err := m.store.change(ctx, m.store.db.R, id)
	if err != nil {
		return err
	}
	out, err := m.runJSON(ctx, head)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusAccepted, out)
}

// queueRun stores a run with its albums and its first job. One run works at a
// time, because two would take turns on the tags queue and each would double
// the wait of a Save.
func (m *Module) queueRun(ctx context.Context, head *changeRow, fixes []string, albums []runAlbum) error {
	fj, err := json.Marshal(fixes)
	if err != nil {
		return err
	}
	analyze := slices.ContainsFunc(albums, func(a runAlbum) bool { return a.State == albumWaiting })
	err = m.store.db.Write(ctx, func(tx *sqlx.Tx) error {
		var n int
		if err := tx.GetContext(ctx, &n, `SELECT count(*) FROM library_runs r JOIN library_changes c ON c.id = r.change_id
			WHERE c.state IN (?, ?)`, StateQueued, StateWriting); err != nil {
			return err
		}
		if n > 0 {
			return errRunActive
		}
		if err := m.store.insertChange(ctx, tx, head); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO library_runs (change_id, fixes) VALUES (?, ?)`, head.ID, string(fj)); err != nil {
			return err
		}
		for _, a := range albums {
			if _, err := tx.ExecContext(ctx, `INSERT INTO library_run_albums (run, position, album_id, target, title, artist, state)
				VALUES (?, ?, ?, ?, ?, ?, ?)`, head.ID, a.Position, a.AlbumID, a.Target, a.Title, a.Artist, a.State); err != nil {
				return err
			}
		}
		if analyze {
			e, err := m.jobs.Enqueue(ctx, tx, jobs.Request{Type: AnalyzeJob, Payload: runPayload{Run: head.ID},
				ResourceKey: analyzeKey(head.ID), CreatedBy: "web"})
			if err != nil {
				return err
			}
			_, err = tx.ExecContext(ctx, `UPDATE library_runs SET analysis_job = ? WHERE change_id = ?`, e.ID, head.ID)
			return err
		}
		_, err := m.jobs.Enqueue(ctx, tx, runRequest(head.ID, "web"))
		return err
	})
	if err != nil {
		return err
	}
	m.jobs.Kick()
	m.publishRun(ctx, head.ID)
	return nil
}

type runPayload struct {
	Run int64 `json:"run"`
}

func runKey(id int64) string     { return "library-run:" + strconv.FormatInt(id, 10) }
func analyzeKey(id int64) string { return "library-analyze:" + strconv.FormatInt(id, 10) }

// runRequest asks for the job that writes the run's ready albums. Whoever has
// news of an album asks, and one queued job takes all of it.
func runRequest(id int64, by string) jobs.Request {
	return jobs.Request{Type: RunJob, Payload: runPayload{Run: id}, ResourceKey: runKey(id), CoalescingKey: runKey(id), CreatedBy: by}
}

// runSlice writes one album and queues itself behind whatever waits, so a
// Save goes next. With nothing ready it ends: the analysis asks for it again.
func (m *Module) runSlice(ctx context.Context, r *jobs.Run) error {
	var p runPayload
	if err := r.Payload(&p); err != nil {
		return jobs.Permanent(err)
	}
	head, err := m.store.change(ctx, m.store.db.R, p.Run)
	if err != nil {
		return jobs.Permanent(err)
	}
	if head.State != StateQueued && head.State != StateWriting {
		return nil
	}
	if err := m.store.startRun(ctx, head.ID); err != nil {
		return err
	}
	info, err := m.store.run(ctx, head.ID)
	if err != nil {
		return err
	}
	a, ok, err := m.store.nextAlbum(ctx, head.ID)
	if err != nil {
		return err
	}
	// A write a stop cut off still gets settled, cancelled or not.
	cutOff := ok && a.State == albumWriting && a.ChangeID != nil
	if info.Cancelled && !cutOff {
		return m.endRun(ctx, head, true)
	}
	if !ok {
		left, err := m.store.albumsWaiting(ctx, head.ID)
		if err != nil {
			return err
		}
		if left > 0 {
			return nil
		}
		return m.endRun(ctx, head, false)
	}
	if err := m.runOne(ctx, head, info, a, r.Log()); err != nil {
		return err
	}
	m.publishRun(ctx, head.ID)
	if m.afterSlice != nil {
		m.afterSlice()
	}
	return jobs.Defer(0, "the next album")
}

// runOne takes one album through its write, starting again where a stop left
// it.
func (m *Module) runOne(ctx context.Context, head changeRow, info runRow, a runAlbum, log *jobs.Logger) error {
	if a.ChangeID == nil {
		if err := m.store.setAlbum(ctx, a.Run, a.Position, albumWriting, ""); err != nil {
			return err
		}
		m.publishRun(ctx, head.ID)
		var c *changeRow
		var end *albumEnd
		var err error
		if head.Kind == KindSave {
			c, end, err = m.planWrite(ctx, head, info, a)
		} else {
			c, end, err = m.planReversal(ctx, head, a)
		}
		switch {
		case ctx.Err() != nil:
			return ctx.Err()
		case err != nil:
			end = &albumEnd{state: albumFailed, message: (&journal{root: m.root}).message(err)}
		}
		if c == nil {
			return m.store.endAlbum(ctx, a, *end)
		}
		if end != nil {
			a.Fixes = end.fixes
		}
		if err := m.store.attach(ctx, a, c); err != nil {
			return err
		}
		a.ChangeID = &c.ID
	}
	c, err := m.store.change(ctx, m.store.db.R, *a.ChangeID)
	if err != nil {
		return err
	}
	switch c.State {
	case StateQueued:
		if err := m.writeChange(ctx, c, log); err != nil {
			return err
		}
	case StateWriting:
		log.Warn("The write of %s was cut off; settling it from the files", c.Folder)
		if err := m.reconcile(ctx, c); err != nil {
			return err
		}
	}
	bg := context.WithoutCancel(ctx)
	if c, err = m.store.change(bg, m.store.db.R, c.ID); err != nil {
		return err
	}
	if c.State == StateFailed && ctx.Err() != nil {
		// A stop, not the album's fault, and no file was replaced. The album
		// starts over after the restart.
		if err := m.store.detach(bg, a); err != nil {
			return err
		}
		return ctx.Err()
	}
	written, err := m.store.written(bg, c.ID)
	if err != nil {
		return err
	}
	end := albumEnd{state: albumDone, fixes: a.Fixes, files: written}
	switch {
	case c.State == StatePartial:
		end.state, end.message = albumFailed, c.Error
	case c.State == StateFailed && slices.Contains([]string{ReasonConflict, ReasonLocked, ReasonBusy, ReasonRefused}, c.Reason):
		end.state, end.message = albumSkipped, c.Error
	case c.State == StateFailed:
		end.state, end.message = albumFailed, c.Error
	}
	return m.store.endAlbum(bg, a, end)
}

// albumEnd is how an album of a run ended.
type albumEnd struct {
	state   string
	message string
	fixes   int
	files   int
}

// skipOf ends an album the checks stopped. Only a real fault is a failure.
func (m *Module) skipOf(err error) (*albumEnd, error) {
	if reasonOf(err) == ReasonError {
		return nil, err
	}
	return &albumEnd{state: albumSkipped, message: (&journal{root: m.root}).message(err)}, nil
}

// planWrite works out an album's change. The album is skipped when it left
// the index, when nothing changes, or when a Save would have to ask.
func (m *Module) planWrite(ctx context.Context, head changeRow, info runRow, a runAlbum) (*changeRow, *albumEnd, error) {
	var gains *albumGains
	if a.Gains != "" {
		gains = &albumGains{}
		if err := json.Unmarshal([]byte(a.Gains), gains); err != nil {
			return nil, nil, err
		}
	}
	p, err := m.planAlbum(ctx, a.AlbumID, info.fixes(), gains)
	if errors.Is(err, errNotFound) {
		return nil, &albumEnd{state: albumSkipped, message: "the album changed since the run started"}, nil
	}
	if err != nil {
		end, err := m.skipOf(err)
		return nil, end, err
	}
	if len(p.stored.Files) == 0 {
		return nil, &albumEnd{state: albumDone, message: "nothing to change"}, nil
	}
	ch := tagio.Change{}
	for _, f := range p.stored.Files {
		fc := tagio.FileChange{Path: m.abs(f.Path), Revision: f.Revision}
		for _, e := range f.Edits {
			fc.Edits = append(fc.Edits, e.edit())
		}
		ch.Files = append(ch.Files, fc)
	}
	planned, err := m.writer(nil).Check(ch)
	if err != nil {
		end, err := m.skipOf(err)
		return nil, end, err
	}
	warns, err := m.identity(ctx, []albumFiles{{p.row, p.indexed}}, planned)
	if err != nil {
		return nil, nil, err
	}
	if len(warns) > 0 {
		return nil, &albumEnd{state: albumSkipped, message: warns[0].Message}, nil
	}
	req, err := json.Marshal(p.stored)
	if err != nil {
		return nil, nil, err
	}
	return &changeRow{Kind: KindSave, Parent: &head.ID, AlbumID: p.row.ID, Folder: p.row.Folder, Title: p.row.Title,
		Artist: p.row.Artist, State: StateQueued, Request: string(req), CreatedAt: db.At(time.Now())}, &albumEnd{fixes: p.applied}, nil
}

// planReversal works out the undo or redo of one album's save. The owner
// pressed Undo on the whole run, so it asks about nothing.
func (m *Module) planReversal(ctx context.Context, head changeRow, a runAlbum) (*changeRow, *albumEnd, error) {
	if a.Target == nil {
		return nil, &albumEnd{state: albumSkipped, message: "the history lost this album's save"}, nil
	}
	save, err := m.store.change(ctx, m.store.db.R, *a.Target)
	if err != nil {
		return nil, nil, err
	}
	files, err := m.store.changeFiles(ctx, save.ID)
	if err != nil {
		return nil, nil, err
	}
	rv := m.reverse(ctx, head.Kind, files)
	conflicts, err := json.Marshal(rv.conflicts)
	if err != nil {
		return nil, nil, err
	}
	if len(rv.change.Files) == 0 && len(rv.stored.Folder) == 0 {
		return nil, &albumEnd{state: albumSkipped, message: "every tag this run changed has changed since"}, nil
	}
	if _, err := m.writer(nil).Check(rv.change); err != nil {
		end, err := m.skipOf(err)
		return nil, end, err
	}
	req, err := json.Marshal(rv.stored)
	if err != nil {
		return nil, nil, err
	}
	return &changeRow{Kind: head.Kind, Target: &save.ID, Parent: &head.ID, AlbumID: save.AlbumID, Folder: save.Folder, Title: save.Title,
		Artist: save.Artist, State: StateQueued, Request: string(req), Conflicts: string(conflicts), CreatedAt: db.At(time.Now())}, nil, nil
}

// endRun closes a run. Its writes asked for no walk or scan, because one per
// album would keep Navidrome scanning for hours, so one goes out here.
func (m *Module) endRun(ctx context.Context, head changeRow, cancelled bool) error {
	ctx = context.WithoutCancel(ctx)
	written := 0
	err := m.store.db.Write(ctx, func(tx *sqlx.Tx) error {
		if cancelled {
			if _, err := tx.ExecContext(ctx, `UPDATE library_run_albums SET state = ?, finished_at = ? WHERE run = ? AND state IN (?, ?, ?, ?)`,
				albumCancelled, db.Now(), head.ID, albumWaiting, albumAnalyzing, albumReady, albumWriting); err != nil {
				return err
			}
		}
		var n struct {
			Written int `db:"written"`
			Bad     int `db:"bad"`
			Tags    int `db:"tags"`
		}
		if err := tx.GetContext(ctx, &n, `SELECT
			(SELECT count(*) FROM library_run_albums WHERE run = ? AND state = 'done' AND files > 0) AS written,
			(SELECT count(*) FROM library_run_albums WHERE run = ? AND state IN ('skipped', 'failed')) AS bad,
			(SELECT coalesce(sum(tags), 0) FROM library_changes WHERE parent = ?) AS tags`, head.ID, head.ID, head.ID); err != nil {
			return err
		}
		written = n.Written
		state, reason, msg := runState(n.Written, n.Bad, cancelled)
		conflicts, err := runConflicts(ctx, tx, head.ID)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE library_changes SET state = ?, reason = ?, error = ?, tags = ?, conflicts = ?, finished_at = ?
			WHERE id = ?`, state, reason, msg, n.Tags, conflicts, db.Now(), head.ID); err != nil {
			return err
		}
		// A cancelled undo isn't an undo yet, and pressing Undo again finishes
		// it, as each album's save knows whether it was reversed.
		if head.Target != nil && state != StateFailed && !cancelled {
			_, err = tx.ExecContext(ctx, `UPDATE library_changes SET undone = ? WHERE id = ?`, head.Kind == KindUndo, *head.Target)
		}
		return err
	})
	if err != nil {
		return err
	}
	m.afterRun(ctx, head.ID, written > 0)
	return nil
}

func (m *Module) afterRun(ctx context.Context, id int64, wrote bool) {
	m.publishRun(ctx, id)
	if c, err := m.store.change(ctx, m.store.db.R, id); err == nil {
		m.publishChange(ctx, c)
	}
	if !wrote {
		return
	}
	m.Kick()
	if scan := m.scanner(); scan != nil {
		if err := scan(ctx); err != nil {
			m.log.Warn("library: ask Navidrome to scan", "error", err)
		}
	}
}

// runState is how a run ended. Albums with nothing to change count for
// neither side.
func runState(written, bad int, cancelled bool) (state, reason, msg string) {
	switch {
	case cancelled && written > 0:
		return StatePartial, ReasonCancelled, "The run was cancelled."
	case cancelled:
		return StateFailed, ReasonCancelled, "The run was cancelled."
	case bad > 0 && written > 0:
		return StatePartial, "", ""
	case bad > 0:
		return StateFailed, ReasonRefused, "No album could change."
	}
	return StateDone, "", ""
}

func runConflicts(ctx context.Context, tx *sqlx.Tx, id int64) (string, error) {
	var rows []string
	if err := tx.SelectContext(ctx, &rows, `SELECT conflicts FROM library_changes WHERE parent = ? AND conflicts != '[]'`, id); err != nil {
		return "", err
	}
	all := []conflictJSON{}
	for _, r := range rows {
		var cs []conflictJSON
		if err := json.Unmarshal([]byte(r), &cs); err != nil {
			return "", err
		}
		all = append(all, cs...)
	}
	b, err := json.Marshal(all[:min(len(all), maxConflicts)])
	return string(b), err
}

// runFailed and runCancelled close a run whose job ended early, the analysis
// included, so the history doesn't show it working forever.
func (m *Module) runFailed(ctx context.Context, tx *sqlx.Tx, j jobs.Info, err error) error {
	return m.closeRun(ctx, tx, j, ReasonError, err.Error())
}

func (m *Module) runCancelled(ctx context.Context, tx *sqlx.Tx, j jobs.Info, _ string) error {
	return m.closeRun(ctx, tx, j, ReasonCancelled, "The run was cancelled.")
}

func (m *Module) closeRun(ctx context.Context, tx *sqlx.Tx, j jobs.Info, reason, msg string) error {
	key := strings.TrimPrefix(strings.TrimPrefix(j.ResourceKey, "library-run:"), "library-analyze:")
	id, err := strconv.ParseInt(key, 10, 64)
	if err != nil {
		return nil
	}
	if _, err := tx.ExecContext(ctx, `UPDATE library_run_albums SET state = ?, finished_at = ? WHERE run = ? AND state IN (?, ?, ?, ?)`,
		albumCancelled, db.Now(), id, albumWaiting, albumAnalyzing, albumReady, albumWriting); err != nil {
		return err
	}
	var written int
	if err := tx.GetContext(ctx, &written, `SELECT count(*) FROM library_run_albums WHERE run = ? AND state = 'done' AND files > 0`, id); err != nil {
		return err
	}
	state := StateFailed
	if written > 0 {
		state = StatePartial
	}
	_, err = tx.ExecContext(ctx, `UPDATE library_changes SET state = ?, reason = ?, error = ?, finished_at = ?
		WHERE id = ? AND state IN (?, ?)`, state, reason, cut(msg, 4000), db.Now(), id, StateQueued, StateWriting)
	return err
}

// analyzeSlice measures one album. A file it can't measure skips the album,
// because an album gain needs every track.
func (m *Module) analyzeSlice(ctx context.Context, r *jobs.Run) error {
	var p runPayload
	if err := r.Payload(&p); err != nil {
		return jobs.Permanent(err)
	}
	info, err := m.store.run(ctx, p.Run)
	if errors.Is(err, errNotFound) || err == nil && info.Cancelled {
		return nil
	}
	if err != nil {
		return err
	}
	a, ok, err := m.store.nextToMeasure(ctx, p.Run)
	if err != nil || !ok {
		return err
	}
	if err := m.store.setAlbum(ctx, a.Run, a.Position, albumAnalyzing, ""); err != nil {
		return err
	}
	m.publishRun(ctx, p.Run)
	gains, err := m.measure(ctx, a.AlbumID)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		msg := (&journal{root: m.root}).message(err)
		r.Log().Warn("%s: %s", a.Title, msg)
		if err := m.store.endAlbum(ctx, a, albumEnd{state: albumSkipped, message: msg}); err != nil {
			return err
		}
	} else {
		b, err := json.Marshal(gains)
		if err != nil {
			return err
		}
		if err := m.store.setGains(ctx, a, string(b)); err != nil {
			return err
		}
	}
	if _, err := m.jobs.EnqueueNow(ctx, runRequest(p.Run, r.Info().Actor())); err != nil {
		return err
	}
	m.publishRun(ctx, p.Run)
	return jobs.Defer(0, "the next album")
}

// cancelRun stops a run after the album it is writing. A decode stops at
// once, and the run's job closes the run.
func (m *Module) cancelRun(c *echo.Context) error {
	id, err := idParam(c, "run")
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	info, err := m.store.run(ctx, id)
	if errors.Is(err, errNotFound) {
		return echo.NewHTTPError(http.StatusNotFound, "no such run")
	}
	if err != nil {
		return err
	}
	head, err := m.store.change(ctx, m.store.db.R, id)
	if err != nil {
		return err
	}
	if head.State != StateQueued && head.State != StateWriting {
		return echo.NewHTTPError(http.StatusConflict, "this run has ended")
	}
	if err := m.store.db.Write(ctx, func(tx *sqlx.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE library_runs SET cancelled = 1 WHERE change_id = ?`, id)
		return err
	}); err != nil {
		return err
	}
	if info.AnalysisJob != nil {
		// The analysis may have ended long ago, and its row with it.
		err := m.jobs.Cancel(ctx, *info.AnalysisJob, "web")
		if err != nil && !errors.Is(err, jobs.ErrNotActive) && !errors.Is(err, jobs.ErrNotFound) {
			return err
		}
	}
	if _, err := m.jobs.EnqueueNow(ctx, runRequest(id, "web")); err != nil {
		return err
	}
	return m.answerRun(c, id)
}

// reverseRun queues the undo or redo of every album's save that needs it.
func (m *Module) reverseRun(c *echo.Context, save changeRow, kind string) error {
	ctx := c.Request().Context()
	info, err := m.store.run(ctx, save.ID)
	if err != nil {
		return err
	}
	done, err := m.store.reversible(ctx, save.ID, kind == KindRedo)
	if err != nil {
		return err
	}
	if len(done) == 0 {
		return c.JSON(http.StatusConflict, refusal{Error: "no album of this run has anything to " + kind, Reason: ReasonConflict})
	}
	for i := range done {
		done[i].Position = i
		done[i].State = albumReady
	}
	head := changeRow{Kind: kind, Target: &save.ID, State: StateQueued, Request: "{}", CreatedAt: db.At(time.Now())}
	if err := m.queueRun(ctx, &head, info.fixes(), done); err != nil {
		if errors.Is(err, errRunActive) {
			return echo.NewHTTPError(http.StatusConflict, err.Error())
		}
		return err
	}
	return c.JSON(http.StatusAccepted, m.changeJSON(ctx, head))
}

// runLine is a line of a run's log.
type runLine struct {
	At      string `json:"at"`
	Album   string `json:"album"`
	State   string `json:"state"`
	Fixes   int    `json:"fixes"`
	Files   int    `json:"files"`
	Message string `json:"message"`
}

// runProgressJSON is a run as its panel shows it. Done counts the albums
// that ended well, and each album counts once, whatever became of it.
type runProgressJSON struct {
	ID         int64     `json:"id"`
	Kind       string    `json:"kind"`
	Fixes      []string  `json:"fixes"`
	State      string    `json:"state"`
	Reason     string    `json:"reason"`
	Error      string    `json:"error"`
	Cancelling bool      `json:"cancelling"`
	Total      int       `json:"total"`
	Done       int       `json:"done"`
	Skipped    int       `json:"skipped"`
	Failed     int       `json:"failed"`
	Files      int       `json:"files"`
	Now        string    `json:"now"`
	NowState   string    `json:"now_state"`
	Log        []runLine `json:"log"`
	CreatedAt  string    `json:"created_at"`
	FinishedAt *string   `json:"finished_at"`
}

func (m *Module) runJSON(ctx context.Context, head changeRow) (runProgressJSON, error) {
	info, err := m.store.run(ctx, head.ID)
	if err != nil {
		return runProgressJSON{}, err
	}
	var rows []runAlbum
	if err := m.store.db.R.SelectContext(ctx, &rows, `SELECT `+runAlbumCols+` FROM library_run_albums WHERE run = ? ORDER BY position`, head.ID); err != nil {
		return runProgressJSON{}, err
	}
	out := runProgressJSON{ID: head.ID, Kind: head.Kind, Fixes: info.fixes(), State: head.State, Reason: head.Reason, Error: head.Error,
		Cancelling: info.Cancelled, Total: len(rows), Log: []runLine{}, CreatedAt: head.CreatedAt.UTC().Format(time.RFC3339)}
	if !head.FinishedAt.IsZero() {
		s := head.FinishedAt.UTC().Format(time.RFC3339)
		out.FinishedAt = &s
	}
	var ended []runAlbum
	for _, a := range rows {
		out.Files += a.Files
		switch a.State {
		case albumDone:
			out.Done++
		case albumSkipped, albumCancelled:
			out.Skipped++
		case albumFailed:
			out.Failed++
		case albumWriting:
			out.Now, out.NowState = a.Title, a.State
		case albumAnalyzing:
			if out.NowState != albumWriting {
				out.Now, out.NowState = a.Title, a.State
			}
		}
		// Cancelled albums never started, and a long run would have thousands.
		if !a.FinishedAt.IsZero() && a.State != albumCancelled {
			ended = append(ended, a)
		}
	}
	slices.SortStableFunc(ended, func(a, b runAlbum) int { return a.FinishedAt.Compare(b.FinishedAt.Time) })
	for _, a := range ended[max(0, len(ended)-logLines):] {
		out.Log = append(out.Log, runLine{At: a.FinishedAt.UTC().Format(time.RFC3339), Album: a.Title, State: a.State,
			Fixes: a.Fixes, Files: a.Files, Message: a.Message})
	}
	return out, nil
}

func (m *Module) publishRun(ctx context.Context, id int64) {
	if m.hub == nil {
		return
	}
	ctx = context.WithoutCancel(ctx)
	head, err := m.store.change(ctx, m.store.db.R, id)
	if err != nil {
		return
	}
	out, err := m.runJSON(ctx, head)
	if err != nil {
		m.log.Error("library: read a run", "run", id, "error", err)
		return
	}
	m.hub.Publish(RunTopic, out)
}

// currentRun answers the run that works now, or null, for a page that opens
// or reconnects while one runs.
func (m *Module) currentRun(c *echo.Context) error {
	ctx := c.Request().Context()
	var id int64
	err := m.store.db.R.GetContext(ctx, &id, `SELECT r.change_id FROM library_runs r JOIN library_changes c ON c.id = r.change_id
		WHERE c.state IN (?, ?) ORDER BY r.change_id DESC LIMIT 1`, StateQueued, StateWriting)
	if isNoRows(err) {
		return c.JSON(http.StatusOK, map[string]any{"run": nil})
	}
	if err != nil {
		return err
	}
	head, err := m.store.change(ctx, m.store.db.R, id)
	if err != nil {
		return err
	}
	out, err := m.runJSON(ctx, head)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"run": out})
}

func (m *Module) getRun(c *echo.Context) error {
	id, err := idParam(c, "run")
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	head, err := m.store.change(ctx, m.store.db.R, id)
	if errors.Is(err, errNotFound) {
		return echo.NewHTTPError(http.StatusNotFound, "no such run")
	}
	if err != nil {
		return err
	}
	out, err := m.runJSON(ctx, head)
	if errors.Is(err, errNotFound) {
		return echo.NewHTTPError(http.StatusNotFound, "no such run")
	}
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, out)
}

// runSummaryJSON is what the history says of a run.
type runSummaryJSON struct {
	Fixes   []string `json:"fixes"`
	Albums  int      `json:"albums"`
	Files   int      `json:"files"`
	Skipped int      `json:"skipped"`
	Failed  int      `json:"failed"`
}

func (s store) runSummary(ctx context.Context, id int64) (*runSummaryJSON, error) {
	info, err := s.run(ctx, id)
	if errors.Is(err, errNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var n struct {
		Albums  int `db:"albums"`
		Files   int `db:"files"`
		Skipped int `db:"skipped"`
		Failed  int `db:"failed"`
	}
	err = s.db.R.GetContext(ctx, &n, `SELECT coalesce(sum(state = 'done' AND files > 0), 0) AS albums, coalesce(sum(files), 0) AS files,
		coalesce(sum(state IN ('skipped', 'cancelled')), 0) AS skipped, coalesce(sum(state = 'failed'), 0) AS failed
		FROM library_run_albums WHERE run = ?`, id)
	return &runSummaryJSON{Fixes: info.fixes(), Albums: n.Albums, Files: n.Files, Skipped: n.Skipped, Failed: n.Failed}, err
}

// runFiles lists the first files a run wrote, for its row in the history. A
// whole run would be thousands of lines nobody reads.
func (m *Module) runFiles(ctx context.Context, id int64) ([]changeFile, error) {
	var ids []int64
	if err := m.store.db.R.SelectContext(ctx, &ids, `SELECT id FROM library_changes WHERE parent = ? ORDER BY id`, id); err != nil {
		return nil, err
	}
	var out []changeFile
	for _, c := range ids {
		files, err := m.store.changeFiles(ctx, c)
		if err != nil {
			return nil, err
		}
		out = append(out, files...)
		if len(out) >= detailFiles {
			return out[:detailFiles], nil
		}
	}
	return out, nil
}

func isNoRows(err error) bool { return errors.Is(err, sql.ErrNoRows) }

func (s store) isRun(ctx context.Context, id int64) (bool, error) {
	_, err := s.run(ctx, id)
	if errors.Is(err, errNotFound) {
		return false, nil
	}
	return err == nil, err
}

func (s store) run(ctx context.Context, id int64) (runRow, error) {
	var r runRow
	err := s.db.R.GetContext(ctx, &r, `SELECT change_id, fixes, cancelled, analysis_job FROM library_runs WHERE change_id = ?`, id)
	if isNoRows(err) {
		return r, errNotFound
	}
	return r, err
}

// startRun marks a queued run as working.
func (s store) startRun(ctx context.Context, id int64) error {
	return s.db.Write(ctx, func(tx *sqlx.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE library_changes SET state = ? WHERE id = ? AND state = ?`, StateWriting, id, StateQueued)
		return err
	})
}

// nextAlbum is the album a stop cut off, else the first that is ready.
func (s store) nextAlbum(ctx context.Context, run int64) (runAlbum, bool, error) {
	return s.firstAlbum(ctx, `run = ? AND state IN ('writing', 'ready') ORDER BY state = 'writing' DESC, position`, run)
}

func (s store) nextToMeasure(ctx context.Context, run int64) (runAlbum, bool, error) {
	return s.firstAlbum(ctx, `run = ? AND state IN ('waiting', 'analyzing') ORDER BY position`, run)
}

func (s store) firstAlbum(ctx context.Context, cond string, run int64) (runAlbum, bool, error) {
	var a runAlbum
	err := s.db.R.GetContext(ctx, &a, `SELECT `+runAlbumCols+` FROM library_run_albums WHERE `+cond+` LIMIT 1`, run)
	if isNoRows(err) {
		return a, false, nil
	}
	return a, err == nil, err
}

func (s store) albumsWaiting(ctx context.Context, run int64) (int, error) {
	var n int
	err := s.db.R.GetContext(ctx, &n, `SELECT count(*) FROM library_run_albums WHERE run = ? AND state IN ('waiting', 'analyzing')`, run)
	return n, err
}

func (s store) setAlbum(ctx context.Context, run int64, position int, state, message string) error {
	return s.db.Write(ctx, func(tx *sqlx.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE library_run_albums SET state = ?, message = ? WHERE run = ? AND position = ?`, state, message, run, position)
		return err
	})
}

func (s store) setGains(ctx context.Context, a runAlbum, gains string) error {
	return s.db.Write(ctx, func(tx *sqlx.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE library_run_albums SET state = ?, gains = ? WHERE run = ? AND position = ?`, albumReady, gains, a.Run, a.Position)
		return err
	})
}

func (s store) endAlbum(ctx context.Context, a runAlbum, e albumEnd) error {
	return s.db.Write(ctx, func(tx *sqlx.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE library_run_albums SET state = ?, message = ?, fixes = ?, files = ?, finished_at = ?
			WHERE run = ? AND position = ?`, e.state, cut(e.message, 1000), e.fixes, e.files, db.Now(), a.Run, a.Position)
		return err
	})
}

// attach stores an album's change and names it in the album's row together,
// so a stop leaves no write the run can't find.
func (s store) attach(ctx context.Context, a runAlbum, c *changeRow) error {
	return s.db.Write(ctx, func(tx *sqlx.Tx) error {
		if err := s.insertChange(ctx, tx, c); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `UPDATE library_run_albums SET change_id = ?, fixes = ? WHERE run = ? AND position = ?`, c.ID, a.Fixes, a.Run, a.Position)
		return err
	})
}

func (s store) detach(ctx context.Context, a runAlbum) error {
	return s.db.Write(ctx, func(tx *sqlx.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE library_run_albums SET change_id = NULL, state = ? WHERE run = ? AND position = ?`, albumReady, a.Run, a.Position)
		return err
	})
}

func (s store) written(ctx context.Context, change int64) (int, error) {
	var n int
	err := s.db.R.GetContext(ctx, &n, `SELECT count(*) FROM library_change_files WHERE change_id = ? AND written = 1 AND kind = ?`, change, fileAudio)
	return n, err
}

// reversible lists a run's albums whose save an undo or redo can work on,
// with the save as the target.
func (s store) reversible(ctx context.Context, run int64, undone bool) ([]runAlbum, error) {
	var out []runAlbum
	err := s.db.R.SelectContext(ctx, &out, `SELECT a.run, a.position, c.album_id, c.id AS target, NULL AS change_id, a.title, a.artist,
		a.state, '' AS gains, a.fixes, 0 AS files, '' AS message, NULL AS finished_at
		FROM library_run_albums a JOIN library_changes c ON c.id = a.change_id
		WHERE a.run = ? AND c.state IN (?, ?) AND c.undone = ? ORDER BY a.position`, run, StateDone, StatePartial, undone)
	return out, err
}

func (s store) saveGain(ctx context.Context, audio string, t loudness.Track) error {
	b, err := json.Marshal(t.Blocks)
	if err != nil {
		return err
	}
	return s.db.Write(ctx, func(tx *sqlx.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO library_gains (audio, peak, blocks, measured_at) VALUES (?, ?, ?, ?)
			ON CONFLICT (audio) DO NOTHING`, audio, t.Peak, string(b), db.Now())
		return err
	})
}
