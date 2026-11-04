package library

import (
	"net/http"
	"slices"
	"testing"

	"go.senan.xyz/taglib"
)

// saved runs one Save of the rig's only album and returns the change.
func (r *rig) saved(id int64, body saveBody) changeJSON {
	r.t.Helper()
	rec := r.post("/api/library/albums/"+itoa(id)+"/save", body)
	if rec.Code != http.StatusAccepted {
		r.t.Fatalf("save: %d %s", rec.Code, rec.Body)
	}
	c := decode[changeJSON](r.t, rec)
	r.drain()
	return r.change(c.ID)
}

// reversed runs an undo or redo and returns the change it queued.
func (r *rig) reversed(kind string, id int64, confirm ...string) changeJSON {
	r.t.Helper()
	rec := r.post("/api/library/changes/"+itoa(id)+"/"+kind, map[string]any{"confirm": confirm})
	if rec.Code != http.StatusAccepted {
		r.t.Fatalf("%s: %d %s", kind, rec.Code, rec.Body)
	}
	c := decode[changeJSON](r.t, rec)
	r.drain()
	return r.change(c.ID)
}

func TestUndoAndRedo(t *testing.T) {
	r := newRig(t)
	p1 := r.add("folk/Blue/01.flac", "track.flac", blue("1", map[string][]string{"MOOD": {"calm"}}))
	p2 := r.add("folk/Blue/02.flac", "track.flac", blue("2", nil))
	r.walk()
	r.startJobs()
	id := r.albums("").Albums[0].ID
	before := tagsOf(t, p1)
	s := r.saved(id, saveBody{Tracks: everyTrack(r.open(id),
		editJSON{Field: "title", Values: []string{"Same"}}, editJSON{Key: "MOOD"}, editJSON{Key: "GROUPING", Values: []string{"x"}})})
	if s.State != StateDone || s.Undone {
		t.Fatalf("save %+v", s)
	}
	after := tagsOf(t, p1)

	u := r.reversed(KindUndo, s.ID)
	if u.Kind != KindUndo || u.Target == nil || *u.Target != s.ID || u.State != StateDone || len(u.Conflicts) != 0 || u.Title != "Blue" {
		t.Fatalf("undo %+v", u)
	}
	if got := tagsOf(t, p1); !sameTags(got, before) {
		t.Errorf("after undo %v, not %v", got, before)
	}
	if got := tagsOf(t, p2); !slices.Equal(got["TITLE"], []string{"Track 2"}) || got["GROUPING"] != nil {
		t.Errorf("track 2 after undo %v", got)
	}
	if s = r.change(s.ID); !s.Undone {
		t.Error("the save isn't marked undone")
	}
	if rec := r.post("/api/library/changes/"+itoa(s.ID)+"/undo", nil); rec.Code != http.StatusConflict {
		t.Errorf("undo of an undone save: %d", rec.Code)
	}

	if rd := r.reversed(KindRedo, s.ID); rd.State != StateDone || rd.Kind != KindRedo {
		t.Fatalf("redo %+v", rd)
	}
	if got := tagsOf(t, p1); !sameTags(got, after) {
		t.Errorf("after redo %v, not %v", got, after)
	}
	if s = r.change(s.ID); s.Undone {
		t.Error("the save is still marked undone")
	}
	if rec := r.post("/api/library/changes/"+itoa(s.ID)+"/redo", nil); rec.Code != http.StatusConflict {
		t.Errorf("redo of a save that isn't undone: %d", rec.Code)
	}
	// Undo again, in any order.
	r.reversed(KindUndo, s.ID)
	if got := tagsOf(t, p1); !sameTags(got, before) {
		t.Errorf("after a second undo %v", got)
	}

	for path, code := range map[string]int{
		"/api/library/changes/999/undo":                http.StatusNotFound,
		"/api/library/changes/x/undo":                  http.StatusNotFound,
		"/api/library/changes/" + itoa(u.ID) + "/undo": http.StatusNotFound,
	} {
		if rec := r.post(path, nil); rec.Code != code {
			t.Errorf("%s: %d", path, rec.Code)
		}
	}
}

func TestUndoLeavesWhatChangedSince(t *testing.T) {
	r := newRig(t)
	p1 := r.add("folk/Blue/01.flac", "track.flac", blue("1", nil))
	p2 := r.add("folk/Blue/02.flac", "track.flac", blue("2", nil))
	r.walk()
	r.startJobs()
	id := r.albums("").Albums[0].ID
	s := r.saved(id, saveBody{Tracks: everyTrack(r.open(id),
		editJSON{Field: "title", Values: []string{"Same"}}, editJSON{Key: "MOOD", Values: []string{"calm"}})})
	if err := taglib.WriteTags(p1, map[string][]string{"TITLE": {"Picard's"}}, 0); err != nil {
		t.Fatal(err)
	}
	u := r.reversed(KindUndo, s.ID)
	if u.State != StateDone || !slices.Equal(u.Conflicts, []conflictJSON{{Path: "folk/Blue/01.flac", Key: "TITLE"}}) {
		t.Fatalf("undo %+v", u)
	}
	t1, t2 := tagsOf(t, p1), tagsOf(t, p2)
	if !slices.Equal(t1["TITLE"], []string{"Picard's"}) || t1["MOOD"] != nil || !slices.Equal(t2["TITLE"], []string{"Track 2"}) || t2["MOOD"] != nil {
		t.Errorf("after undo %v %v", t1, t2)
	}

	// A redo that finds nothing it can redo answers with the conflicts.
	if err := taglib.WriteTags(p2, map[string][]string{"TITLE": {"Mine"}, "MOOD": {"loud"}}, 0); err != nil {
		t.Fatal(err)
	}
	if err := taglib.WriteTags(p1, map[string][]string{"MOOD": {"loud"}}, 0); err != nil {
		t.Fatal(err)
	}
	rec := r.post("/api/library/changes/"+itoa(s.ID)+"/redo", nil)
	f := decode[refusal](t, rec)
	if rec.Code != http.StatusConflict || f.Reason != ReasonConflict || len(f.Conflicts) != 4 {
		t.Fatalf("redo: %d %+v", rec.Code, f)
	}
}

func TestUndoKeepsReleaseIDsTogether(t *testing.T) {
	r := newRig(t)
	p1 := r.add("folk/Blue/01.flac", "track.flac", blue("1", nil))
	p2 := r.add("folk/Blue/02.flac", "track.flac", blue("2", nil))
	r.walk()
	r.startJobs()
	id := r.albums("").Albums[0].ID
	const album, group = "6a3a4bc4-1d68-4c8a-a2e0-a6d0b1b3c6f1", "0b3e1f5c-5c3f-4b5e-9f0a-1a2b3c4d5e6f"
	s := r.saved(id, saveBody{Tracks: everyTrack(r.open(id),
		editJSON{Field: "musicbrainz_albumid", Values: []string{album}},
		editJSON{Field: "musicbrainz_releasegroupid", Values: []string{group}},
		editJSON{Field: "genre", Values: []string{"Folk"}})})
	if s.State != StateDone {
		t.Fatalf("save %+v", s)
	}
	r.walk()
	if err := taglib.WriteTags(p2, map[string][]string{"MUSICBRAINZ_ALBUMID": {"11111111-1111-1111-1111-111111111111"}}, 0); err != nil {
		t.Fatal(err)
	}
	r.walk()
	// Track 1 alone would go back to no release, beside track 2 on another one.
	rec := r.post("/api/library/changes/"+itoa(s.ID)+"/undo", map[string]any{"confirm": []string{ConfirmSplit, ConfirmMerge}})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("undo: %d %s", rec.Code, rec.Body)
	}
	r.drain()
	u := r.change(decode[changeJSON](t, rec).ID)
	if u.State != StateDone || len(u.Conflicts) != 4 {
		t.Fatalf("undo %+v", u)
	}
	t1 := tagsOf(t, p1)
	if !slices.Equal(t1["MUSICBRAINZ_ALBUMID"], []string{album}) || !slices.Equal(t1["MUSICBRAINZ_RELEASEGROUPID"], []string{group}) || t1["GENRE"] != nil {
		t.Errorf("track 1 after undo %v", t1)
	}
}

func TestUndoAsksBeforeNavidromeMovesAStar(t *testing.T) {
	r := newRig(t)
	r.add("folk/Blue/01.flac", "track.flac", blue("1", nil))
	r.add("folk/Blue/02.flac", "track.flac", blue("2", nil))
	r.walk()
	r.startJobs()
	id := r.albums("").Albums[0].ID
	d := r.open(id)
	s := r.saved(id, saveBody{Tracks: []trackEdits{{ID: d.Tracks[0].ID, Revision: d.Tracks[0].Revision,
		Edits: []editJSON{{Field: "album", Values: []string{"Blue Too"}}}}}, Confirm: []string{ConfirmSplit}})
	r.walk()
	rec := r.post("/api/library/changes/"+itoa(s.ID)+"/undo", nil)
	if f := decode[refusal](t, rec); rec.Code != http.StatusConflict || len(f.Confirm) != 1 || f.Confirm[0].Kind != ConfirmMerge {
		t.Fatalf("undo: %d %+v", rec.Code, f)
	}
	if u := r.reversed(KindUndo, s.ID, ConfirmMerge); u.State != StateDone {
		t.Fatalf("undo %+v", u)
	}
	r.walk()
	if l := r.albums(""); len(l.Albums) != 1 || l.Albums[0].Tracks != 2 {
		t.Errorf("albums after undo %+v", l.Albums)
	}
}

func TestHistory(t *testing.T) {
	r := newRig(t)
	r.add("folk/Blue/01.flac", "track.flac", blue("1", nil))
	r.walk()
	id := r.albums("").Albums[0].ID
	queue := func(v string) changeJSON {
		t.Helper()
		rec := r.post("/api/library/albums/"+itoa(id)+"/save", saveBody{Tracks: everyTrack(r.open(id), editJSON{Key: "MOOD", Values: []string{v}})})
		if rec.Code != http.StatusAccepted {
			t.Fatalf("save: %d %s", rec.Code, rec.Body)
		}
		r.drain()
		return decode[changeJSON](t, rec)
	}
	r.startJobs()
	var saves []changeJSON
	for _, v := range []string{"a", "b", "c"} {
		saves = append(saves, queue(v))
	}
	r.reversed(KindUndo, saves[2].ID)

	var h historyJSON
	r.getJSON("/api/library/history?limit=2", &h)
	if len(h.Changes) != 2 || h.Changes[0].ID != saves[2].ID || !h.Changes[0].Undone || h.Next == nil || *h.Next != saves[1].ID {
		t.Fatalf("page 1 %+v", h)
	}
	r.getJSON("/api/library/history?limit=2&before="+itoa(*h.Next), &h)
	if len(h.Changes) != 1 || h.Changes[0].ID != saves[0].ID || h.Next != nil || h.Changes[0].Tags != 1 {
		t.Fatalf("page 2 %+v", h)
	}
	if rec := r.do(http.MethodGet, "/api/library/history?limit=0"); rec.Code != http.StatusBadRequest {
		t.Errorf("limit 0: %d", rec.Code)
	}

	// Retention keeps the newest saves, and each takes its undos with it.
	if n, err := r.m.store.trim(bg, 2); err != nil || n != 1 {
		t.Fatalf("trim: %d %v", n, err)
	}
	r.getJSON("/api/library/history", &h)
	if len(h.Changes) != 2 {
		t.Fatalf("after trim %+v", h)
	}

	// Clear history keeps a save whose undo is still to run.
	r.stopJobs()
	if rec := r.post("/api/library/changes/"+itoa(saves[1].ID)+"/undo", nil); rec.Code != http.StatusAccepted {
		t.Fatalf("undo: %d %s", rec.Code, rec.Body)
	}
	if rec := r.post("/api/library/changes/"+itoa(saves[1].ID)+"/undo", nil); rec.Code != http.StatusConflict {
		t.Errorf("a second undo while the first waits: %d", rec.Code)
	}
	rec := r.do(http.MethodDelete, "/api/library/history")
	if got := decode[map[string]int64](t, rec); rec.Code != http.StatusOK || got["deleted"] != 1 {
		t.Fatalf("clear: %d %v", rec.Code, got)
	}
	r.getJSON("/api/library/history", &h)
	if len(h.Changes) != 1 || h.Changes[0].ID != saves[1].ID {
		t.Fatalf("after clear %+v", h)
	}
}
