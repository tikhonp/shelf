package library

import (
	"net/http"
	"slices"
	"testing"

	"go.senan.xyz/taglib"
)

func (r *rig) albumHistory(album int64, query string) historyJSON {
	r.t.Helper()
	var h historyJSON
	r.getJSON("/api/library/history?album="+itoa(album)+query, &h)
	return h
}

func changeIDs(rows []changeJSON) []int64 {
	var out []int64
	for _, c := range rows {
		out = append(out, c.ID)
	}
	return out
}

func TestAlbumHistory(t *testing.T) {
	r := newRig(t)
	r.album("Blue", legacy(), legacy())
	r.album("Hejira", legacy())
	r.album("Court", legacy())
	r.walk()
	r.startJobs()
	a, b, quiet := r.albumID("Blue"), r.albumID("Hejira"), r.albumID("Court")

	for _, id := range []int64{quiet, 99999} {
		if h := r.albumHistory(id, ""); h.Changes == nil || len(h.Changes) != 0 || h.Next != nil {
			t.Fatalf("album %d: %+v", id, h)
		}
	}

	s := r.saved(a, saveBody{Tracks: everyTrack(r.open(a), editJSON{Key: "MOOD", Values: []string{"calm"}})})
	if got := changeIDs(r.albumHistory(a, "").Changes); !slices.Equal(got, []int64{s.ID}) {
		t.Fatalf("A: %v", got)
	}
	if h := r.albumHistory(b, ""); len(h.Changes) != 0 {
		t.Fatalf("B: %+v", h)
	}
	if h := r.albumHistory(a, ""); h.Changes[0].Author != "Unknown" || h.Changes[0].Tracks != 2 {
		t.Errorf("author %q, tracks %d", h.Changes[0].Author, h.Changes[0].Tracks)
	}

	for _, bad := range []string{"0", "x", "-1"} {
		if rec := r.do(http.MethodGet, "/api/library/history?album="+bad); rec.Code != http.StatusBadRequest {
			t.Errorf("history album=%s: %d", bad, rec.Code)
		}
		if rec := r.do(http.MethodGet, "/api/library/changes/"+itoa(s.ID)+"?album="+bad); rec.Code != http.StatusBadRequest {
			t.Errorf("change album=%s: %d", bad, rec.Code)
		}
	}
	// A change that is not a run ignores album.
	if c := r.getChangeAlbum(s.ID, b); len(c.Files) != 2 {
		t.Errorf("a save with album: %+v", c.Files)
	}
}

func (r *rig) getChangeAlbum(id, album int64) changeJSON {
	r.t.Helper()
	var c changeJSON
	r.getJSON("/api/library/changes/"+itoa(id)+"?album="+itoa(album), &c)
	return c
}

func TestAlbumHistoryRun(t *testing.T) {
	r := newRig(t)
	r.album("Blue", legacy(), legacy())
	r.album("Hejira", legacy())
	r.walk()
	r.startJobs()
	a, b := r.albumID("Blue"), r.albumID("Hejira")

	run := r.startRun(every(string(NoReleaseDate)))
	r.drain()
	for _, id := range []int64{a, b} {
		if got := changeIDs(r.albumHistory(id, "").Changes); !slices.Equal(got, []int64{run.ID}) {
			t.Fatalf("album %d: %v", id, got)
		}
	}
	if h := r.albumHistory(a, ""); h.Changes[0].Tracks != 0 {
		t.Errorf("a run's row has %d tracks", h.Changes[0].Tracks)
	}
	if c := r.change(run.ID); len(c.Files) != 3 {
		t.Fatalf("without album: %d files", len(c.Files))
	}
	if c := r.getChangeAlbum(run.ID, a); len(c.Files) != 2 {
		t.Fatalf("A: %d files", len(c.Files))
	}
	if c := r.getChangeAlbum(run.ID, b); len(c.Files) != 1 {
		t.Fatalf("B: %d files", len(c.Files))
	}
	if c := r.getChangeAlbum(run.ID, 99999); len(c.Files) != 0 {
		t.Fatalf("unknown: %d files", len(c.Files))
	}

	// With stale album IDs on the run's album writes, their files still find it.
	if _, err := r.m.store.db.W.Exec(`UPDATE library_changes SET album_id = 424242 WHERE id = ? OR parent = ?`, run.ID, run.ID); err != nil {
		t.Fatal(err)
	}
	if got := changeIDs(r.albumHistory(b, "").Changes); !slices.Equal(got, []int64{run.ID}) {
		t.Fatalf("stale B: %v", got)
	}
	if c := r.getChangeAlbum(run.ID, b); len(c.Files) != 1 {
		t.Fatalf("stale B: %d files", len(c.Files))
	}
}

func TestAlbumHistoryPages(t *testing.T) {
	r := newRig(t)
	r.album("Blue", legacy())
	r.album("Hejira", legacy())
	r.walk()
	r.startJobs()
	a, b := r.albumID("Blue"), r.albumID("Hejira")
	var ids []int64
	for i, v := range []string{"a", "b", "c"} {
		ids = append(ids, r.saved(a, saveBody{Tracks: everyTrack(r.open(a), editJSON{Key: "MOOD", Values: []string{v}})}).ID)
		if i == 1 {
			r.saved(b, saveBody{Tracks: everyTrack(r.open(b), editJSON{Key: "MOOD", Values: []string{v}})})
		}
	}
	h := r.albumHistory(a, "&limit=2")
	if got := changeIDs(h.Changes); !slices.Equal(got, []int64{ids[2], ids[1]}) || h.Next == nil || *h.Next != ids[1] {
		t.Fatalf("page 1 %v %v", got, h.Next)
	}
	h = r.albumHistory(a, "&limit=2&before="+itoa(*h.Next))
	if got := changeIDs(h.Changes); !slices.Equal(got, []int64{ids[0]}) || h.Next != nil {
		t.Fatalf("page 2 %v %v", got, h.Next)
	}
}

func TestAlbumHistoryShowsUndoneSaveWithConflicts(t *testing.T) {
	r := newRig(t)
	p1 := r.add("folk/Blue/01.flac", "track.flac", blue("1", nil))
	r.add("folk/Blue/02.flac", "track.flac", blue("2", nil))
	r.walk()
	r.startJobs()
	id := r.albums("").Albums[0].ID
	s := r.saved(id, saveBody{Tracks: everyTrack(r.open(id), editJSON{Field: "title", Values: []string{"Same"}})})
	if err := taglib.WriteTags(p1, map[string][]string{"TITLE": {"Picard's"}}, 0); err != nil {
		t.Fatal(err)
	}
	u := r.reversed(KindUndo, s.ID)
	if len(u.Conflicts) != 1 || u.Tracks != 1 {
		t.Fatalf("undo %+v", u)
	}
	h := r.albumHistory(id, "")
	if len(h.Changes) != 1 || h.Changes[0].ID != s.ID || !h.Changes[0].Undone {
		t.Fatalf("%+v", h)
	}
}

func TestAlbumHistoryFollowsFilesToTheirAlbum(t *testing.T) {
	r := newRig(t)
	r.album("Blue", legacy())
	r.walk()
	r.startJobs()
	a := r.albumID("Blue")
	s := r.saved(a, saveBody{Tracks: everyTrack(r.open(a), editJSON{Key: "MOOD", Values: []string{"calm"}})})
	// An album row ID that a re-index changed leaves the stored one stale.
	if _, err := r.m.store.db.W.Exec(`UPDATE library_changes SET album_id = 424242 WHERE id = ?`, s.ID); err != nil {
		t.Fatal(err)
	}
	if got := changeIDs(r.albumHistory(a, "").Changes); !slices.Equal(got, []int64{s.ID}) {
		t.Fatalf("%v", got)
	}
}

func TestAlbumHistoryShowsADelete(t *testing.T) {
	r := newRig(t)
	r.album("Blue", nil, nil)
	r.album("Hejira", nil)
	r.walk()
	r.deletes(&fakeNavidrome{markAfter: 1, tracks: 2})
	r.startJobs()
	a, b := r.albumID("Blue"), r.albumID("Hejira")
	c := r.deleteAlbum(a, ConfirmDelete)
	r.drain()
	if got := changeIDs(r.albumHistory(a, "").Changes); !slices.Equal(got, []int64{c.ID}) {
		t.Fatalf("A: %v", got)
	}
	if h := r.albumHistory(a, ""); h.Changes[0].Tracks != 0 {
		t.Errorf("a delete has %d tracks", h.Changes[0].Tracks)
	}
	if h := r.albumHistory(b, ""); len(h.Changes) != 0 {
		t.Fatalf("B: %+v", h)
	}
}
