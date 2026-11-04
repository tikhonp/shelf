package library

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/labstack/echo/v5"
	"github.com/tikhonp/shelf/internal/modules/library/tagio"
	"github.com/tikhonp/shelf/internal/platform/db"
	"github.com/tikhonp/shelf/internal/platform/jobs"
	"github.com/tikhonp/shelf/internal/platform/testwait"
	"github.com/tikhonp/shelf/internal/platform/web"
	"go.senan.xyz/taglib"
)

// startJobs runs the rig's jobs until the test ends.
func (r *rig) startJobs() {
	r.t.Helper()
	ctx, cancel := context.WithCancel(bg)
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := r.sys.Start(ctx); err != nil {
			r.t.Errorf("jobs: %v", err)
		}
	}()
	r.t.Cleanup(func() { cancel(); <-done })
}

// drain waits until no job runs or waits.
func (r *rig) drain() {
	r.t.Helper()
	deadline := testwait.Deadline(30 * time.Second)
	for time.Now().Before(deadline) {
		var n int
		if err := r.m.store.db.R.Get(&n, `SELECT count(*) FROM jobs WHERE state IN ('queued', 'running', 'interrupted')`); err != nil {
			r.t.Fatal(err)
		}
		if n == 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	r.t.Fatal("jobs did not end")
}

func (r *rig) post(path string, body any) *httptest.ResponseRecorder {
	r.t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		r.t.Fatal(err)
	}
	e := echo.New()
	e.HTTPErrorHandler = web.ErrorHandler
	r.m.Routes(e.Group("/api"))
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b)))
	return rec
}

func (r *rig) open(id int64) detailJSON {
	r.t.Helper()
	var d detailJSON
	r.getJSON("/api/library/albums/"+itoa(id), &d)
	return d
}

// everyTrack edits each track of an album the same way.
func everyTrack(d detailJSON, edits ...editJSON) []trackEdits {
	var out []trackEdits
	for _, t := range d.Tracks {
		out = append(out, trackEdits{ID: t.ID, Revision: t.Revision, Edits: edits})
	}
	return out
}

func (r *rig) change(id int64) changeJSON {
	r.t.Helper()
	var c changeJSON
	r.getJSON("/api/library/changes/"+itoa(id), &c)
	return c
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("%d %s: %v", rec.Code, rec.Body, err)
	}
	return v
}

func tagsOf(t *testing.T, path string) map[string][]string {
	t.Helper()
	tags, err := taglib.ReadTags(path)
	if err != nil {
		t.Fatal(err)
	}
	return tags
}

func TestSaveWritesTheAlbum(t *testing.T) {
	r := newRig(t)
	p1 := r.add("folk/Blue/01.flac", "track.flac", blue("1", nil))
	p2 := r.add("folk/Blue/02.flac", "track.flac", blue("2", nil))
	r.walk()
	var scans atomic.Int32
	r.m.SetScanner(func(context.Context) error { scans.Add(1); return nil })
	events, stop := r.hub.Subscribe(ChangeTopic)
	defer stop()
	old := r.albums("").Albums[0]
	d := r.open(old.ID)

	rec := r.post("/api/library/albums/"+itoa(old.ID)+"/save", saveBody{Tracks: everyTrack(d,
		editJSON{Field: "album", Values: []string{"Blue (Remastered)"}},
		editJSON{Field: "albumartist", Values: []string{"Joni Mitchell"}},
		editJSON{Key: "MOOD", Values: []string{"calm"}})})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("save: %d %s", rec.Code, rec.Body)
	}
	c := decode[changeJSON](t, rec)
	if c.State != StateQueued || c.Kind != KindSave || c.Title != "Blue" || c.JobID == nil {
		t.Fatalf("queued %+v", c)
	}
	r.startJobs()
	r.drain()

	// A field and a raw key change; an edit to the value a file holds drops out.
	for _, p := range []string{p1, p2} {
		tags := tagsOf(t, p)
		if !slices.Equal(tags["ALBUM"], []string{"Blue (Remastered)"}) || !slices.Equal(tags["MOOD"], []string{"calm"}) ||
			!slices.Equal(tags["ALBUMARTIST"], []string{"Joni Mitchell"}) {
			t.Errorf("%s: %v", p, tags)
		}
	}
	c = r.change(c.ID)
	if c.State != StateDone || c.Error != "" || c.FinishedAt == nil || c.Tags != 4 || len(c.Files) != 2 {
		t.Fatalf("done %+v", c)
	}
	f := c.Files[1]
	if !f.Written || f.Path != "folk/Blue/02.flac" || f.TrackID != d.Tracks[1].ID || len(f.Keys) != 2 ||
		f.Keys[0].Key != "ALBUM" || !slices.Equal(f.Keys[0].Before, []string{"Blue"}) || f.Keys[1].Key != "MOOD" || len(f.Keys[1].Before) != 0 {
		t.Fatalf("file %+v", f)
	}
	if scans.Load() != 1 {
		t.Errorf("%d scans asked for", scans.Load())
	}
	select {
	case <-r.m.kick:
	default:
		t.Error("no walk asked for")
	}
	var states []string
	for len(events) > 0 {
		states = append(states, (<-events).Data.(changeJSON).State)
	}
	if !slices.Equal(states, []string{StateQueued, StateDone}) {
		t.Errorf("events %q", states)
	}

	// The new title is a new Navidrome album, and the change follows it.
	r.walk()
	now := r.albums("").Albums
	if len(now) != 1 || now[0].ID == old.ID || now[0].Title != "Blue (Remastered)" {
		t.Fatalf("albums %+v", now)
	}
	if c = r.change(c.ID); c.AlbumID != now[0].ID {
		t.Errorf("change on album %d, not %d", c.AlbumID, now[0].ID)
	}

	// The returned revisions are what the album shows now.
	d = r.open(now[0].ID)
	rec = r.post("/api/library/albums/"+itoa(now[0].ID)+"/save", saveBody{Tracks: everyTrack(d, editJSON{Key: "MOOD"})})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("second save: %d %s", rec.Code, rec.Body)
	}
	r.drain()
	if _, ok := tagsOf(t, p1)["MOOD"]; ok {
		t.Error("MOOD stayed")
	}
}

func TestSaveRefuses(t *testing.T) {
	r := newRig(t)
	r.add("folk/Blue/01.flac", "track.flac", blue("1", nil))
	r.add("folk/Blue/02.flac", "track.flac", blue("2", nil))
	r.add("folk/Court/01.flac", "track.flac", blue("1", map[string][]string{"ALBUM": {"Court and Spark"}}))
	r.walk()
	var blueID, courtID int64
	for _, a := range r.albums("").Albums {
		if a.Title == "Blue" {
			blueID = a.ID
		} else {
			courtID = a.ID
		}
	}
	d := r.open(blueID)
	other := r.open(courtID).Tracks[0]
	save := "/api/library/albums/" + itoa(blueID) + "/save"
	title := editJSON{Field: "title", Values: []string{"New"}}
	stale := d.Tracks[0]
	stale.Revision = "0-0-0"

	for name, tc := range map[string]struct {
		path   string
		body   any
		code   int
		reason string
		track  int64
	}{
		"no body":        {save, "x", http.StatusBadRequest, "", 0},
		"no tracks":      {save, saveBody{}, http.StatusBadRequest, "", 0},
		"no album":       {"/api/library/albums/999/save", saveBody{Tracks: everyTrack(d, title)}, http.StatusNotFound, "", 0},
		"another album":  {save, saveBody{Tracks: []trackEdits{{ID: other.ID, Revision: other.Revision, Edits: []editJSON{title}}}}, http.StatusUnprocessableEntity, "", 0},
		"conflict":       {save, saveBody{Tracks: []trackEdits{{ID: stale.ID, Revision: stale.Revision, Edits: []editJSON{title}}}}, http.StatusConflict, ReasonConflict, stale.ID},
		"unknown field":  {save, saveBody{Tracks: everyTrack(d, editJSON{Field: "nope", Values: []string{"x"}})}, http.StatusUnprocessableEntity, ReasonRefused, d.Tracks[0].ID},
		"nothing to do":  {save, saveBody{Tracks: everyTrack(d, editJSON{Field: "album", Values: []string{"Blue"}})}, http.StatusUnprocessableEntity, ReasonRefused, 0},
		"too many edits": {save, saveBody{Tracks: everyTrack(d, slices.Repeat([]editJSON{title}, 300)...)}, http.StatusBadRequest, "", 0},
	} {
		rec := r.post(tc.path, tc.body)
		if rec.Code != tc.code {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body)
			continue
		}
		if tc.reason != "" {
			if f := decode[refusal](t, rec); f.Reason != tc.reason || f.Track != tc.track || f.Error == "" || strings.Contains(f.Error, r.root) {
				t.Errorf("%s: %+v", name, f)
			}
		}
	}

	// A folder a download works in is busy, and one shelf can't write is locked.
	r.m.SetBusy(func(dir string) bool { return strings.HasSuffix(dir, "Blue") })
	if f := decode[refusal](t, r.post(save, saveBody{Tracks: everyTrack(d, title)})); f.Reason != ReasonBusy {
		t.Errorf("busy: %+v", f)
	}
	r.m.SetBusy(nil)
	dir := filepath.Join(r.root, "folk", "Blue")
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	if f := decode[refusal](t, r.post(save, saveBody{Tracks: everyTrack(d, title)})); f.Reason != ReasonLocked {
		t.Errorf("locked: %+v", f)
	}
	var n int
	if err := r.m.store.db.R.Get(&n, `SELECT count(*) FROM library_changes`); err != nil || n != 0 {
		t.Errorf("%d changes stored, %v", n, err)
	}
}

func TestSaveAsksBeforeNavidromeMovesAStar(t *testing.T) {
	r := newRig(t)
	r.add("folk/Blue/01.flac", "track.flac", blue("1", nil))
	r.add("folk/Blue/02.flac", "track.flac", blue("2", nil))
	r.add("folk/Court/01.flac", "track.flac", blue("1", map[string][]string{"ALBUM": {"Court and Spark"}}))
	r.walk()
	var blueID int64
	for _, a := range r.albums("").Albums {
		if a.Title == "Blue" {
			blueID = a.ID
		}
	}
	d := r.open(blueID)
	save := "/api/library/albums/" + itoa(blueID) + "/save"
	asks := func(name string, body saveBody, want ...string) {
		t.Helper()
		rec := r.post(save, body)
		if want == nil {
			if rec.Code != http.StatusAccepted {
				t.Errorf("%s: %d %s", name, rec.Code, rec.Body)
			}
			return
		}
		f := decode[refusal](t, rec)
		var kinds []string
		for _, w := range f.Confirm {
			kinds = append(kinds, w.Kind)
			if w.Message == "" {
				t.Errorf("%s: %s has no message", name, w.Kind)
			}
		}
		if rec.Code != http.StatusConflict || f.Reason != "confirm" || !slices.Equal(kinds, want) {
			t.Errorf("%s: %d %+v", name, rec.Code, f)
		}
	}
	one := func(edits ...editJSON) []trackEdits {
		return []trackEdits{{ID: d.Tracks[0].ID, Revision: d.Tracks[0].Revision, Edits: edits}}
	}
	album := func(v string) editJSON { return editJSON{Field: "album", Values: []string{v}} }

	asks("one track renamed", saveBody{Tracks: one(album("Blue Too"))}, ConfirmSplit)
	asks("one track merged", saveBody{Tracks: one(album("Court and Spark"))}, ConfirmSplit, ConfirmMerge)
	asks("all tracks merged", saveBody{Tracks: everyTrack(d, album("Court and Spark"))}, ConfirmMerge)
	// ALBUMARTISTS wins over ALBUMARTIST, so Navidrome renames nobody until it changes.
	asks("album artist renamed", saveBody{Tracks: everyTrack(d, editJSON{Field: "albumartist", Values: []string{"Joni"}})})
	asks("album artists renamed", saveBody{Tracks: everyTrack(d, editJSON{Field: "albumartists", Values: []string{"Joni"}})}, ConfirmArtist)
	asks("a confirmation that covers only one", saveBody{Tracks: one(album("Court and Spark")), Confirm: []string{ConfirmSplit}}, ConfirmMerge)
	asks("whole album renamed", saveBody{Tracks: everyTrack(d, album("Blue (Remastered)"))})
	asks("confirmed", saveBody{Tracks: one(album("Blue Too")), Confirm: []string{ConfirmSplit}})
}

func TestSaveSeesAChangeBeforeItsJob(t *testing.T) {
	r := newRig(t)
	p := r.add("folk/Blue/01.flac", "track.flac", blue("1", nil))
	r.walk()
	id := r.albums("").Albums[0].ID
	d := r.open(id)
	rec := r.post("/api/library/albums/"+itoa(id)+"/save", saveBody{Tracks: everyTrack(d, editJSON{Field: "title", Values: []string{"Mine"}})})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	c := decode[changeJSON](t, rec)
	if err := taglib.WriteTags(p, map[string][]string{"TITLE": {"Picard's"}}, 0); err != nil {
		t.Fatal(err)
	}
	r.startJobs()
	r.drain()
	c = r.change(c.ID)
	if c.State != StateFailed || c.Reason != ReasonConflict || !strings.HasPrefix(c.Error, "folk/Blue/01.flac: ") || len(c.Files) != 0 {
		t.Fatalf("%+v", c)
	}
	if got := tagsOf(t, p)["TITLE"]; !slices.Equal(got, []string{"Picard's"}) {
		t.Errorf("title %q", got)
	}
}

// writing stores a change that a stop cut off, with one file before and one
// after.
func (r *rig) writing(files ...changeFile) int64 {
	r.t.Helper()
	var id int64
	err := r.m.store.db.Write(bg, func(tx *sqlx.Tx) error {
		c := changeRow{Kind: KindSave, Folder: "folk/Blue", Title: "Blue", Artist: "Joni Mitchell", State: StateWriting,
			CreatedAt: db.At(time.Now())}
		if err := r.m.store.insertChange(bg, tx, &c); err != nil {
			return err
		}
		id = c.ID
		for _, f := range files {
			before, _ := json.Marshal(f.Before)
			after, _ := json.Marshal(f.After)
			if _, err := tx.Exec(`INSERT INTO library_change_files (change_id, path, before, after) VALUES (?, ?, ?, ?)`,
				id, f.Path, string(before), string(after)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		r.t.Fatal(err)
	}
	return id
}

func TestInterruptedChangeIsSettled(t *testing.T) {
	r := newRig(t)
	p1 := r.add("folk/Blue/01.flac", "track.flac", blue("1", nil))
	r.add("folk/Blue/02.flac", "track.flac", blue("2", nil))
	now := func(p string) map[string][]string { return tagsOf(t, filepath.Join(r.root, p)) }
	moved := now("folk/Blue/01.flac")
	moved["TITLE"] = []string{"Old"}
	partial := r.writing(
		changeFile{Path: "folk/Blue/01.flac", Before: moved, After: now("folk/Blue/01.flac")},
		changeFile{Path: "folk/Blue/02.flac", Before: now("folk/Blue/02.flac"), After: map[string][]string{"TITLE": {"New"}}})
	none := r.writing(changeFile{Path: "folk/Blue/02.flac", Before: now("folk/Blue/02.flac"), After: map[string][]string{"TITLE": {"New"}}})
	all := r.writing(changeFile{Path: "folk/Blue/01.flac", Before: moved, After: now("folk/Blue/01.flac")})

	if err := r.m.reconcileAll(bg); err != nil {
		t.Fatal(err)
	}
	for id, want := range map[int64]string{partial: StatePartial, none: StateFailed, all: StateDone} {
		c := r.change(id)
		if c.State != want || c.Reason != ReasonInterrupted || c.FinishedAt == nil {
			t.Errorf("change %d: %+v", id, c)
		}
	}
	c := r.change(partial)
	rev, err := tagio.Read(p1)
	if err != nil {
		t.Fatal(err)
	}
	if !c.Files[0].Written || c.Files[1].Written {
		t.Errorf("files %+v", c.Files)
	}
	var stored string
	if err := r.m.store.db.R.Get(&stored, `SELECT revision FROM library_change_files WHERE change_id = ? AND path = ?`, partial, "folk/Blue/01.flac"); err != nil || stored != rev.Revision() {
		t.Errorf("revision %q, not %q: %v", stored, rev.Revision(), err)
	}

	// A job resumed after the stop settles its change too, and a second
	// settling changes nothing.
	job := r.writing(changeFile{Path: "folk/Blue/01.flac", Before: moved, After: now("folk/Blue/01.flac")})
	if err := r.m.store.db.Write(bg, func(tx *sqlx.Tx) error {
		e, err := r.sys.Enqueue(bg, tx, jobsRequest(job))
		if err != nil {
			return err
		}
		_, err = tx.Exec(`UPDATE library_changes SET job_id = ? WHERE id = ?`, e.ID, job)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	r.startJobs()
	r.drain()
	if c := r.change(job); c.State != StateDone || c.Reason != ReasonInterrupted {
		t.Errorf("resumed: %+v", c)
	}
	before := r.change(all)
	if err := r.m.reconcile(bg, changeRow{ID: all}); err != nil {
		t.Fatal(err)
	}
	if after := r.change(all); *after.FinishedAt != *before.FinishedAt {
		t.Error("settled twice")
	}
}

func TestProbeAndLockedFiles(t *testing.T) {
	r := newRig(t)
	r.add("folk/Blue/01.flac", "track.flac", blue("1", nil))
	r.add("jazz/Kind of Blue/01.flac", "track.flac", blue("1", nil))
	r.write(".hidden/x", nil)
	jazz := filepath.Join(r.root, "jazz")
	if err := os.Chmod(jazz, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(jazz, 0o755) })
	r.m.ix.probe()
	st := r.m.ix.status()
	if len(st.Folders) != 2 || st.Folders[0] != (FolderState{Name: "folk", Writable: true}) ||
		st.Folders[1].Name != "jazz" || st.Folders[1].Writable || st.Folders[1].Error == "" || strings.Contains(st.Folders[1].Error, r.root) {
		t.Fatalf("folders %+v", st.Folders)
	}
	entries, _ := os.ReadDir(filepath.Join(r.root, "folk"))
	if len(entries) != 1 {
		t.Errorf("the probe left %v", entries)
	}

	// Files another user owns are locked; the tests run as their owner.
	r.m.ix.uid++
	if st = r.walk(); st.Locked != 2 || st.Folders[0].Locked != 1 || st.Folders[1].Locked != 1 {
		t.Errorf("locked %+v", st)
	}
}

func jobsRequest(change int64) jobs.Request {
	return jobs.Request{Type: WriteJob, Payload: writePayload{Change: change}, CreatedBy: "test"}
}
