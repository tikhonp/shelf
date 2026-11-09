package library

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/tikhonp/shelf/internal/platform/jobs"
)

// album adds a one-track-per-tags album in its own folder.
func (r *rig) album(name string, tracks ...map[string][]string) []string {
	r.t.Helper()
	var out []string
	for i, tags := range tracks {
		all := map[string][]string{"ALBUM": {name}, "ALBUMARTIST": {"Joni Mitchell"}, "ARTIST": {"Joni Mitchell"},
			"TITLE": {"Track"}, "TRACKNUMBER": {itoa(int64(i + 1))}}
		for k, v := range tags {
			all[k] = v
		}
		out = append(out, r.add("folk/"+name+"/"+itoa(int64(i+1))+".flac", "track.flac", all))
	}
	return out
}

func (r *rig) albumID(name string) int64 {
	r.t.Helper()
	for _, a := range r.albums("").Albums {
		if a.Title == name {
			return a.ID
		}
	}
	r.t.Fatalf("no album %s", name)
	return 0
}

// legacy are tags under Navidrome's legacy date rule.
func legacy() map[string][]string {
	return map[string][]string{"DATE": {"2010-05-01"}, "ORIGINALDATE": {"1971-06-22"}}
}

func every(names ...string) runBody {
	return runBody{Selection: selection{All: true}, Fixes: names}
}

func (r *rig) startRun(b runBody) runProgressJSON {
	r.t.Helper()
	rec := r.post("/api/library/runs", b)
	if rec.Code != http.StatusAccepted {
		r.t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	return decode[runProgressJSON](r.t, rec)
}

func (r *rig) run(id int64) runProgressJSON {
	r.t.Helper()
	var out runProgressJSON
	r.getJSON("/api/library/runs/"+itoa(id), &out)
	return out
}

func (r *rig) preview(b runBody) previewJSON {
	r.t.Helper()
	rec := r.post("/api/library/runs/preview", b)
	if rec.Code != http.StatusOK {
		r.t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	return decode[previewJSON](r.t, rec)
}

func (r *rig) historyRows() []changeJSON {
	r.t.Helper()
	var h historyJSON
	r.getJSON("/api/library/history", &h)
	return h.Changes
}

func TestRunFixesAlbumsAsOneUndoStep(t *testing.T) {
	r := newRig(t)
	a := r.album("Blue", legacy(), legacy())
	b := r.album("Hejira", legacy())
	r.walk()
	var scans atomic.Int32
	r.m.SetScanner(func(context.Context) error { scans.Add(1); return nil })
	r.startJobs()

	run := r.startRun(every(string(NoReleaseDate)))
	r.drain()
	got := r.run(run.ID)
	if got.State != StateDone || got.Total != 2 || got.Done != 2 || got.Files != 3 {
		t.Fatalf("%+v", got)
	}
	if len(got.Log) != 2 || got.Log[0].Fixes != 1 || got.Log[0].Files != 2 {
		t.Fatalf("%+v", got.Log)
	}
	for _, p := range append(slices.Clone(a), b...) {
		tags := tagsOf(t, p)
		if !slices.Equal(tags["RELEASEDATE"], []string{"2010-05-01"}) || !slices.Equal(tags["DATE"], []string{"1971-06-22"}) {
			t.Fatalf("%s: %v", p, tags)
		}
	}
	// One walk and one scan for the whole run, not one per album.
	if n := scans.Load(); n != 1 {
		t.Fatalf("%d scans", n)
	}

	rows := r.historyRows()
	if len(rows) != 1 || rows[0].Run == nil || rows[0].Tags != 6 || rows[0].Run.Albums != 2 || rows[0].Run.Files != 3 {
		t.Fatalf("%+v", rows)
	}
	if d := r.change(rows[0].ID); len(d.Files) != 3 {
		t.Fatalf("%+v", d)
	}
	var child int64
	if err := r.m.store.db.R.Get(&child, `SELECT id FROM library_changes WHERE parent = ? LIMIT 1`, rows[0].ID); err != nil {
		t.Fatal(err)
	}
	if rec := r.post("/api/library/changes/"+itoa(child)+"/undo", nil); rec.Code != http.StatusConflict {
		t.Fatalf("an album of a run undid alone: %d", rec.Code)
	}

	if rec := r.post("/api/library/changes/"+itoa(rows[0].ID)+"/undo", nil); rec.Code != http.StatusAccepted {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	r.drain()
	for _, p := range append(slices.Clone(a), b...) {
		tags := tagsOf(t, p)
		if tags["RELEASEDATE"] != nil || !slices.Equal(tags["DATE"], []string{"2010-05-01"}) {
			t.Fatalf("%s: %v", p, tags)
		}
	}
	rows = r.historyRows()
	if len(rows) != 1 || !rows[0].Undone {
		t.Fatalf("%+v", rows)
	}
	if rec := r.post("/api/library/changes/"+itoa(rows[0].ID)+"/redo", nil); rec.Code != http.StatusAccepted {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	r.drain()
	if tags := tagsOf(t, a[0]); !slices.Equal(tags["RELEASEDATE"], []string{"2010-05-01"}) {
		t.Fatalf("%v", tags)
	}
	if rows = r.historyRows(); rows[0].Undone {
		t.Fatalf("%+v", rows[0])
	}
}

func TestRunUndoLeavesWhatChangedSince(t *testing.T) {
	r := newRig(t)
	a := r.album("Blue", legacy())
	b := r.album("Hejira", legacy())
	r.walk()
	r.startJobs()
	r.startRun(every(string(NoReleaseDate)))
	r.drain()
	row := r.historyRows()[0]

	// Another tool rewrites one album's date.
	writeTags(t, a[0], map[string][]string{"RELEASEDATE": {"1999"}})
	if rec := r.post("/api/library/changes/"+itoa(row.ID)+"/undo", nil); rec.Code != http.StatusAccepted {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	r.drain()
	if tags := tagsOf(t, a[0]); !slices.Equal(tags["RELEASEDATE"], []string{"1999"}) {
		t.Fatalf("%v", tags)
	}
	if tags := tagsOf(t, b[0]); tags["RELEASEDATE"] != nil {
		t.Fatalf("%v", tags)
	}
	undo := r.change(row.ID + 0)
	if !undo.Undone {
		t.Fatalf("%+v", undo)
	}
	var conflicts string
	if err := r.m.store.db.R.Get(&conflicts, `SELECT conflicts FROM library_changes WHERE kind = 'undo' AND parent IS NULL`); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(conflicts, "RELEASEDATE") {
		t.Fatal(conflicts)
	}
}

func TestRunPreview(t *testing.T) {
	r := newRig(t)
	r.album("Blue", legacy(), legacy())
	r.album("Hejira", map[string][]string{"RELEASEDATE": {"1976"}, "DATE": {"1976"}})
	r.album("Mingus", map[string][]string{"RELEASEDATE": {"1979"}, "DATE": {"1979"}, "ALBUMARTIST": {"Various Artists"}})
	r.walk()

	p := r.preview(every(string(NoReleaseDate)))
	if p.Selected != 3 || p.Albums != 1 || p.Files != 2 || p.Bytes == 0 {
		t.Fatalf("%+v", p)
	}
	count := func(fix string) int {
		i := slices.IndexFunc(p.Fixes, func(c fixCountJSON) bool { return c.Fix == fix })
		return p.Fixes[i].Albums
	}
	if count(string(NoReleaseDate)) != 1 || count(string(VariousArtists)) != 1 || count(FixReplayGain) != 3 {
		t.Fatalf("%+v", p.Fixes)
	}
	want := []sampleJSON{{Album: "Blue", Key: "DATE", Before: "2010-05-01", After: "1971-06-22"}, {Album: "Blue", Key: "RELEASEDATE", After: "2010-05-01"}}
	if !slices.Equal(p.Samples, want) {
		t.Fatalf("%+v", p.Samples)
	}

	// ReplayGain reaches every album, and its values aren't known yet.
	p = r.preview(every(FixReplayGain))
	if p.Albums != 3 || p.Files != 4 || len(p.Samples) != 1 || p.Samples[0].After != "Measured in the run" {
		t.Fatalf("%+v", p)
	}

	// A selection is a list, or a query with exceptions.
	id := r.albumID("Blue")
	if p = r.preview(runBody{Selection: selection{Albums: []int64{id}}, Fixes: []string{FixReplayGain}}); p.Selected != 1 || p.Files != 2 {
		t.Fatalf("%+v", p)
	}
	if p = r.preview(runBody{Selection: selection{All: true, Except: []int64{id}}, Fixes: []string{FixReplayGain}}); p.Selected != 2 {
		t.Fatalf("%+v", p)
	}
	if p = r.preview(runBody{Selection: selection{All: true, Filter: string(NoReleaseDate)}}); p.Selected != 1 || p.Albums != 0 {
		t.Fatalf("%+v", p)
	}
	if p = r.preview(runBody{Selection: selection{All: true, Q: "heji"}}); p.Selected != 1 {
		t.Fatalf("%+v", p)
	}
	if rec := r.post("/api/library/runs/preview", runBody{Fixes: []string{"split_album"}}); rec.Code != http.StatusBadRequest {
		t.Fatalf("%d", rec.Code)
	}
	if rec := r.post("/api/library/runs/preview", runBody{Selection: selection{All: true, Filter: "nope"}}); rec.Code != http.StatusBadRequest {
		t.Fatalf("%d", rec.Code)
	}
}

func TestStartRunRefuses(t *testing.T) {
	r := newRig(t)
	r.album("Blue", map[string][]string{"RELEASEDATE": {"1971"}, "DATE": {"1971"}})
	r.album("Hejira", legacy())
	r.walk()
	if rec := r.post("/api/library/runs", every()); rec.Code != http.StatusBadRequest {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	id := r.albumID("Blue")
	rec := r.post("/api/library/runs", runBody{Selection: selection{Albums: []int64{id}}, Fixes: []string{string(NoReleaseDate)}})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	// Jobs aren't started, so the first run stays queued.
	first := r.startRun(every(string(NoReleaseDate)))
	if rec := r.post("/api/library/runs", every(string(NoReleaseDate))); rec.Code != http.StatusConflict {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	var cur struct {
		Run *runProgressJSON `json:"run"`
	}
	r.getJSON("/api/library/runs/current", &cur)
	if cur.Run == nil || cur.Run.ID != first.ID {
		t.Fatalf("%+v", cur)
	}
}

func TestRunSkipsWhatItCantDecide(t *testing.T) {
	r := newRig(t)
	good := r.album("Blue", legacy())
	busy := r.album("Hejira", legacy())
	// Half the tracks have a date, so the fix would split the album in two,
	// and a Save would ask.
	split := r.album("Mingus", legacy(), map[string][]string{"ORIGINALDATE": {"1971-06-22"}})
	r.walk()
	r.m.SetBusy(func(dir string) bool { return strings.HasSuffix(dir, "Hejira") })
	r.startJobs()

	run := r.startRun(every(string(NoReleaseDate)))
	r.drain()
	got := r.run(run.ID)
	if got.State != StatePartial || got.Done != 1 || got.Skipped != 2 {
		t.Fatalf("%+v", got)
	}
	if tags := tagsOf(t, good[0]); tags["RELEASEDATE"] == nil {
		t.Fatalf("%v", tags)
	}
	for _, p := range []string{busy[0], split[0]} {
		if tags := tagsOf(t, p); tags["RELEASEDATE"] != nil {
			t.Fatalf("%s %v", p, tags)
		}
	}
	lines := map[string]runLine{}
	for _, l := range got.Log {
		lines[l.Album] = l
	}
	if !strings.Contains(lines["Hejira"].Message, "download") || !strings.Contains(lines["Mingus"].Message, "albums") {
		t.Fatalf("%+v", lines)
	}
}

func TestRunWithNothingToChangeEndsWell(t *testing.T) {
	r := newRig(t)
	// The index still says no RELEASEDATE, and the files have one by now.
	p := r.album("Blue", legacy())
	r.walk()
	writeTags(t, p[0], map[string][]string{"RELEASEDATE": {"1971"}})
	r.startJobs()
	run := r.startRun(every(string(NoReleaseDate)))
	r.drain()
	got := r.run(run.ID)
	if got.State != StateDone || got.Done != 1 || got.Files != 0 || got.Log[0].Message != "nothing to change" {
		t.Fatalf("%+v", got)
	}
}

func TestRunSkipsAnAlbumThatLeftTheIndex(t *testing.T) {
	r := newRig(t)
	p := r.album("Blue", legacy())
	r.album("Hejira", legacy())
	r.walk()
	run := r.startRun(every(string(NoReleaseDate)))
	if err := os.RemoveAll(filepath.Dir(p[0])); err != nil {
		t.Fatal(err)
	}
	r.walk()
	r.startJobs()
	r.drain()
	if got := r.run(run.ID); got.State != StatePartial || got.Done != 1 || got.Skipped != 1 {
		t.Fatalf("%+v", got)
	}
}

func TestSaveGoesBetweenSlices(t *testing.T) {
	r := newRig(t)
	r.album("Blue", legacy())
	r.album("Hejira", legacy())
	other := r.album("Mingus", map[string][]string{"RELEASEDATE": {"1979"}, "DATE": {"1979"}})
	r.walk()
	id := r.albumID("Mingus")
	d := r.open(id)
	var done atomic.Int32
	var save changeJSON
	stateAtSecond := ""
	r.m.afterSlice = func() {
		switch done.Add(1) {
		case 1:
			rec := r.post("/api/library/albums/"+itoa(id)+"/save", saveBody{Tracks: everyTrack(d, editJSON{Field: "title", Values: []string{"New"}})})
			save = decode[changeJSON](t, rec)
		case 2:
			if err := r.m.store.db.R.Get(&stateAtSecond, `SELECT state FROM library_changes WHERE id = ?`, save.ID); err != nil {
				t.Error(err)
			}
		}
	}
	r.startJobs()
	r.startRun(every(string(NoReleaseDate)))
	r.drain()
	if stateAtSecond != StateDone {
		t.Fatalf("the Save waited behind the run: %q", stateAtSecond)
	}
	if tags := tagsOf(t, other[0]); !slices.Equal(tags["TITLE"], []string{"New"}) {
		t.Fatalf("%v", tags)
	}
}

func TestRunSurvivesARestart(t *testing.T) {
	r := newRig(t)
	var paths []string
	for _, n := range []string{"A1", "A2", "A3", "A4"} {
		paths = append(paths, r.album(n, legacy())...)
	}
	r.walk()
	reached, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	r.m.afterSlice = func() {
		once.Do(func() {
			close(reached)
			<-release
		})
	}
	r.startJobs()
	run := r.startRun(every(string(NoReleaseDate)))
	<-reached
	stopped := make(chan struct{})
	go func() {
		r.stopJobs()
		close(stopped)
	}()
	time.Sleep(20 * time.Millisecond)
	close(release)
	<-stopped
	if got := r.run(run.ID); got.State != StateWriting || got.Done != 1 {
		t.Fatalf("%+v", got)
	}

	r.m = r.module(nil)
	r.startJobs()
	r.drain()
	got := r.run(run.ID)
	if got.State != StateDone || got.Done != 4 || got.Files != 4 {
		t.Fatalf("%+v", got)
	}
	for _, p := range paths {
		if tags := tagsOf(t, p); !slices.Equal(tags["RELEASEDATE"], []string{"2010-05-01"}) {
			t.Fatalf("%s %v", p, tags)
		}
	}
	row := r.historyRows()[0]
	if rec := r.post("/api/library/changes/"+itoa(row.ID)+"/undo", nil); rec.Code != http.StatusAccepted {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	r.drain()
	for _, p := range paths {
		if tags := tagsOf(t, p); tags["RELEASEDATE"] != nil {
			t.Fatalf("%s %v", p, tags)
		}
	}
}

func TestRunSettlesAWriteAStopCutOff(t *testing.T) {
	r := newRig(t)
	p := r.album("Blue", legacy())
	r.walk()
	run := r.startRun(every(string(NoReleaseDate)))
	a, _, err := r.m.store.nextAlbum(bg, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	// The write landed on the file, and the stop came before its row closed.
	before := tagsOf(t, p[0])
	writeTags(t, p[0], map[string][]string{"RELEASEDATE": {"2010-05-01"}, "DATE": {"1971-06-22"}})
	after := tagsOf(t, p[0])
	child := r.writing(changeFile{Path: "folk/Blue/1.flac", Kind: fileAudio, Before: before, After: after})
	if err := r.m.store.setAlbum(bg, run.ID, a.Position, albumWriting, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := r.m.store.db.W.Exec(`UPDATE library_run_albums SET change_id = ? WHERE run = ?`, child, run.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := r.m.store.db.W.Exec(`UPDATE library_changes SET parent = ? WHERE id = ?`, run.ID, child); err != nil {
		t.Fatal(err)
	}
	r.startJobs()
	r.drain()
	got := r.run(run.ID)
	if got.State != StateDone || got.Done != 1 || got.Files != 1 {
		t.Fatalf("%+v", got)
	}
}

func TestCancelRun(t *testing.T) {
	r := newRig(t)
	first := r.album("A1", legacy())
	last := r.album("A2", legacy())
	r.album("A3", legacy())
	r.walk()
	var once sync.Once
	var run runProgressJSON
	started := make(chan struct{})
	r.m.afterSlice = func() {
		once.Do(func() {
			<-started
			if rec := r.post("/api/library/runs/"+itoa(run.ID)+"/cancel", nil); rec.Code != http.StatusAccepted {
				t.Errorf("%d %s", rec.Code, rec.Body)
			}
		})
	}
	r.startJobs()
	run = r.startRun(every(string(NoReleaseDate)))
	close(started)
	r.drain()
	got := r.run(run.ID)
	if got.State != StatePartial || got.Reason != ReasonCancelled || got.Done != 1 || got.Skipped != 2 {
		t.Fatalf("%+v", got)
	}
	if tags := tagsOf(t, first[0]); tags["RELEASEDATE"] == nil {
		t.Fatalf("%v", tags)
	}
	if tags := tagsOf(t, last[0]); tags["RELEASEDATE"] != nil {
		t.Fatalf("%v", tags)
	}
	if rec := r.post("/api/library/runs/"+itoa(run.ID)+"/cancel", nil); rec.Code != http.StatusConflict {
		t.Fatalf("%d", rec.Code)
	}
	// What a cancelled run wrote still undoes.
	if rec := r.post("/api/library/changes/"+itoa(run.ID)+"/undo", nil); rec.Code != http.StatusAccepted {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	r.drain()
	if tags := tagsOf(t, first[0]); tags["RELEASEDATE"] != nil {
		t.Fatalf("%v", tags)
	}
}

func TestCancelledUndoFinishesOnTheNextPress(t *testing.T) {
	r := newRig(t)
	a := r.album("A1", legacy())
	b := r.album("A2", legacy())
	r.walk()
	r.startJobs()
	run := r.startRun(every(string(NoReleaseDate)))
	r.drain()

	var once sync.Once
	var undo changeJSON
	started := make(chan struct{})
	r.m.afterSlice = func() {
		once.Do(func() {
			<-started
			if rec := r.post("/api/library/runs/"+itoa(undo.ID)+"/cancel", nil); rec.Code != http.StatusAccepted {
				t.Errorf("%d %s", rec.Code, rec.Body)
			}
		})
	}
	rec := r.post("/api/library/changes/"+itoa(run.ID)+"/undo", nil)
	undo = decode[changeJSON](t, rec)
	close(started)
	r.drain()
	if r.change(run.ID).Undone {
		t.Fatal("half an undo counts as undone")
	}
	r.m.afterSlice = nil
	if rec := r.post("/api/library/changes/"+itoa(run.ID)+"/undo", nil); rec.Code != http.StatusAccepted {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	r.drain()
	if !r.change(run.ID).Undone {
		t.Fatal("not undone")
	}
	for _, p := range []string{a[0], b[0]} {
		if tags := tagsOf(t, p); tags["RELEASEDATE"] != nil {
			t.Fatalf("%v", tags)
		}
	}
}

func TestRunWritesReplayGain(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg is not installed")
	}
	r := newRig(t)
	flac := r.album("Blue", nil, nil)
	m4a := r.add("folk/Hejira/1.m4a", "track.m4a", map[string][]string{"ALBUM": {"Hejira"}, "ALBUMARTIST": {"Joni Mitchell"},
		"ARTIST": {"Joni Mitchell"}, "TITLE": {"Coyote"}, "TRACKNUMBER": {"1"}})
	r.walk()
	r.startJobs()
	run := r.startRun(every(FixReplayGain))
	r.drain()
	got := r.run(run.ID)
	if got.State != StateDone || got.Done != 2 || got.Files != 3 {
		t.Fatalf("%+v", got)
	}
	for _, p := range []string{flac[0], flac[1], m4a} {
		tags := tagsOf(t, p)
		for _, k := range []string{"REPLAYGAIN_TRACK_GAIN", "REPLAYGAIN_ALBUM_GAIN"} {
			if len(tags[k]) != 1 || !strings.HasSuffix(tags[k][0], " dB") {
				t.Fatalf("%s %s: %v", p, k, tags)
			}
		}
		for _, k := range []string{"REPLAYGAIN_TRACK_PEAK", "REPLAYGAIN_ALBUM_PEAK"} {
			if len(tags[k]) != 1 {
				t.Fatalf("%s %s: %v", p, k, tags)
			}
		}
	}
	// Both tracks of Blue are the same audio, so the album's gain is theirs.
	tags := tagsOf(t, flac[0])
	if !slices.Equal(tags["REPLAYGAIN_ALBUM_GAIN"], tags["REPLAYGAIN_TRACK_GAIN"]) {
		t.Fatalf("%v", tags)
	}

	// A second run finds the same values, decodes nothing, and writes nothing.
	r.walk()
	r.m.FFmpeg = filepath.Join(t.TempDir(), "no-ffmpeg")
	id := r.albumID("Blue")
	again := r.startRun(runBody{Selection: selection{Albums: []int64{id}}, Fixes: []string{FixReplayGain}})
	r.drain()
	if got := r.run(again.ID); got.State != StateDone || got.Files != 0 {
		t.Fatalf("%+v", got)
	}
}

func TestRunMeasureFailureSkipsTheAlbum(t *testing.T) {
	r := newRig(t)
	p := r.album("Blue", nil)
	r.add("folk/Hejira/1.m4a", "track.m4a", map[string][]string{"ALBUM": {"Hejira"}, "ALBUMARTIST": {"Joni Mitchell"},
		"ARTIST": {"Joni Mitchell"}, "TITLE": {"Coyote"}, "TRACKNUMBER": {"1"}})
	r.walk()
	r.m.FFmpeg = filepath.Join(t.TempDir(), "no-ffmpeg")
	r.startJobs()
	run := r.startRun(every(FixReplayGain))
	r.drain()
	got := r.run(run.ID)
	if got.State != StateFailed || got.Skipped != 2 {
		t.Fatalf("%+v", got)
	}
	if tags := tagsOf(t, p[0]); tags["REPLAYGAIN_ALBUM_GAIN"] != nil {
		t.Fatalf("%v", tags)
	}
}

func TestCancelStopsTheAnalysis(t *testing.T) {
	r := newRig(t)
	r.album("Blue", nil)
	r.walk()
	// Jobs aren't started, so the run is still waiting for its analysis.
	run := r.startRun(every(FixReplayGain))
	if rec := r.post("/api/library/runs/"+itoa(run.ID)+"/cancel", nil); rec.Code != http.StatusAccepted {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	r.startJobs()
	r.drain()
	got := r.run(run.ID)
	if got.State != StateFailed || got.Reason != ReasonCancelled {
		t.Fatalf("%+v", got)
	}
}

func TestFailedAnalysisClosesTheRun(t *testing.T) {
	r := newRig(t)
	r.album("Blue", nil)
	r.walk()
	run := r.startRun(every(FixReplayGain))
	err := r.m.store.db.Write(bg, func(tx *sqlx.Tx) error {
		return r.m.runFailed(bg, tx, jobs.Info{ResourceKey: analyzeKey(run.ID)}, errors.New("the job broke"))
	})
	if err != nil {
		t.Fatal(err)
	}
	got := r.run(run.ID)
	if got.State != StateFailed || got.Reason != ReasonError || got.Error != "the job broke" {
		t.Fatalf("%+v", got)
	}
	// A new run may start, because this one no longer holds the queue.
	if rec := r.post("/api/library/runs", every(FixReplayGain)); rec.Code != http.StatusAccepted {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
}

func TestHistoryRetentionKeepsARunWhole(t *testing.T) {
	r := newRig(t)
	r.album("Blue", legacy())
	r.album("Hejira", legacy())
	r.walk()
	r.startJobs()
	run := r.startRun(every(string(NoReleaseDate)))
	r.drain()
	count := func(q string) int {
		var n int
		if err := r.m.store.db.R.Get(&n, q); err != nil {
			t.Fatal(err)
		}
		return n
	}
	// A run counts as one save, so keeping one keeps it and its albums.
	if _, err := r.m.store.trim(bg, 1, time.Now()); err != nil {
		t.Fatal(err)
	}
	if n := count(`SELECT count(*) FROM library_changes`); n != 3 {
		t.Fatalf("%d changes", n)
	}
	if rec := r.do(http.MethodDelete, "/api/library/history"); rec.Code != http.StatusOK {
		t.Fatalf("%d", rec.Code)
	}
	if n := count(`SELECT count(*) FROM library_changes`); n != 0 {
		t.Fatalf("%d changes", n)
	}
	if n := count(`SELECT count(*) FROM library_run_albums`); n != 0 {
		t.Fatalf("%d albums", n)
	}
	if rec := r.do(http.MethodGet, "/api/library/runs/"+itoa(run.ID)); rec.Code != http.StatusNotFound {
		t.Fatalf("%d", rec.Code)
	}
}

func TestStartLeavesARunningRunAlone(t *testing.T) {
	r := newRig(t)
	r.album("Blue", legacy())
	r.walk()
	run := r.startRun(every(string(NoReleaseDate)))
	if err := r.m.store.startRun(bg, run.ID); err != nil {
		t.Fatal(err)
	}
	if err := r.m.reconcileAll(bg); err != nil {
		t.Fatal(err)
	}
	if got := r.run(run.ID); got.State != StateWriting {
		t.Fatalf("%+v", got)
	}
}
