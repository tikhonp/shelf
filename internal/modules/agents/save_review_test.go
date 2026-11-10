package agents

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/tikhonp/shelf/internal/libraryport"
)

func savedChange() libraryport.HistoryChange {
	return libraryport.HistoryChange{ID: 42, Kind: "save", State: "done", AuthorID: "user:7", Author: "Owner", CreatedAt: time.Now().Add(time.Second).UTC().Format(time.RFC3339Nano), Files: []libraryport.HistoryFile{{PreviewFile: libraryport.PreviewFile{TrackID: 1, Path: "music/album/1.flac", Keys: []libraryport.KeyChange{{Key: "GENRE", After: []string{"Art Rock"}}}}, Written: true, Tags: []libraryport.Tag{{Key: "GENRE", Fields: []string{"genre"}, Values: []string{"Art Rock"}}}}}}
}
func TestSavedOpRequiresEveryWrittenTarget(t *testing.T) {
	base := []BaseTrack{{TrackID: 1, Path: "music/album/1.flac"}, {TrackID: 2, Path: "music/album/2.flac"}}
	for _, name := range []string{"match", "wrong value", "unwritten", "missing track", "new path", "reused id", "raw key", "remove", "cover", "aliases", "unrelated"} {
		t.Run(name, func(t *testing.T) {
			c := savedChange()
			op := Op{Type: "tag", Field: "genre", Tracks: []OpTrack{{TrackID: 1, Values: []string{"Art Rock"}}}}
			want := false
			switch name {
			case "match":
				want = true
			case "wrong value":
				op.Tracks[0].Values = []string{"Other"}
			case "unwritten":
				c.Files[0].Written = false
			case "missing track":
				op.Tracks = append(op.Tracks, OpTrack{TrackID: 2, Values: []string{"Art Rock"}})
			case "new path":
				c.Files[0].Path = "other.flac"
			case "reused id":
				c.Files[0].TrackID = 3
			case "raw key":
				op.Field = ""
				op.TagKey = "genre"
				want = true
			case "remove":
				op.Tracks[0].Values = nil
				c.Files[0].Tags[0].Values = []string{}
				want = true
			case "cover":
				op.Type = "cover"
			case "aliases":
				c.Files[0].Tags = append(c.Files[0].Tags, libraryport.Tag{Key: "ALIAS", Fields: []string{"genre"}, Values: []string{"Art Rock"}})
				want = true
			case "unrelated":
				c.Files[0].Tags[0].Fields = []string{"mood"}
			}
			if got := savedOp(op, base, c); got != want {
				t.Fatalf("got %v, want %v", got, want)
			}
		})
	}
}

func TestSaveAcceptanceIsDurableIdempotentAndAuthored(t *testing.T) {
	r := rig(t, true, nil)
	id := create(t, r)
	c := savedChange()
	ctx := context.Background()
	for range 2 {
		if err := r.m.acceptSaved(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	as, err := getAcceptances(ctx, r.app.DB.R, id)
	if err != nil || len(as) != 1 {
		t.Fatalf("acceptances %v %v", as, err)
	}
	a := as[0]
	if a.ChangeID != 42 || a.AuthorID != "user:7" || a.Author != "Owner" || !a.ViaSave {
		t.Fatalf("acceptance %+v", a)
	}
	rows, err := getOps(ctx, r.app.DB.R, id)
	if err != nil || rows["genre"].Decision != "accepted" || rows["mood"].Decision != "pending" {
		t.Fatalf("decisions %v %v", rows, err)
	}
	r.lib.states[42] = libraryport.ChangeStatus{ID: 42, State: "partial", Files: 2, Written: 1}
	if err = r.m.reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	// A partial Save still wrote every file of this dependency.
	if _, err = r.app.DB.W.Exec(`UPDATE agents_revisions SET ops=? WHERE proposal_id=?`, mustJSON([]Op{{ID: "genre", Type: "tag", Field: "genre", Tracks: []OpTrack{{TrackID: 1, Values: []string{"Art Rock"}}}}, {ID: "mood", Type: "tag", Field: "mood", Tracks: []OpTrack{{TrackID: 1, Values: []string{"Happy"}}}, Requires: []string{"genre"}}}), id); err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, err = r.m.selection(ctx, id, selectionBody{Revision: 1, Ops: []string{"mood"}}, false); err != nil {
		t.Fatal(err)
	}
	r.lib.states[42] = libraryport.ChangeStatus{ID: 42, State: "done", Undone: true}
	if _, _, _, _, err = r.m.selection(ctx, id, selectionBody{Revision: 1, Ops: []string{"mood"}}, false); err == nil {
		t.Fatal("undone dependency satisfied")
	}
	want(t, r.call(t, "GET", fmt.Sprintf("/api/inbox/proposals/%d", id), nil, false), 200)
}

func TestSavesBeforeRevisionAndOtherActionsStayPending(t *testing.T) {
	for _, kind := range []string{"old", "undo", "redo", "origin", "truncated", "wrong", "elsewhere", "nothing written"} {
		t.Run(kind, func(t *testing.T) {
			r := rig(t, true, nil)
			id := create(t, r)
			c := savedChange()
			switch kind {
			case "old":
				c.CreatedAt = time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano)
			case "undo", "redo":
				c.Kind = kind
			case "origin":
				c.Origin = "proposal:1/acceptance:1"
			case "truncated":
				c.Truncated = true
			case "wrong":
				c.Files[0].Tags[0].Values = []string{"Other"}
			case "elsewhere":
				// The same track ID on another album's file, which the proposal doesn't target.
				c.Files[0].Path = "music/other/1.flac"
			case "nothing written":
				c.Files[0].Written = false
			}
			if err := r.m.acceptSaved(context.Background(), c); err != nil {
				t.Fatal(err)
			}
			as, err := getAcceptances(context.Background(), r.app.DB.R, id)
			if err != nil || len(as) != 0 {
				t.Fatalf("acceptances %v %v", as, err)
			}
		})
	}
}

func TestMissedSaveEventAndCursorReplay(t *testing.T) {
	r := rig(t, true, nil)
	id := create(t, r)
	ctx := context.Background()
	c := savedChange()
	r.lib.history = []libraryport.HistoryChange{c}
	r.lib.states[c.ID] = libraryport.ChangeStatus{ID: c.ID, State: "done", Files: 1, Written: 1}
	for range 2 {
		if err := r.m.reconcile(ctx); err != nil {
			t.Fatal(err)
		}
		as, err := getAcceptances(ctx, r.app.DB.R, id)
		if err != nil || len(as) != 1 || as[0].outcome().State != "done" {
			t.Fatalf("acceptances %v %v", as, err)
		}
		// Simulate a crash after decisions commit but before the cursor advances.
		if _, err = r.app.DB.W.Exec(`UPDATE agents_state SET value=0 WHERE key='save_cursor'`); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMissedSaveEventStillRecordsAnUndoneSave(t *testing.T) {
	r := rig(t, true, nil)
	id := create(t, r)
	ctx := context.Background()
	c := savedChange()
	c.Undone = true
	r.lib.history = []libraryport.HistoryChange{c}
	r.lib.states[c.ID] = libraryport.ChangeStatus{ID: c.ID, State: "done", Files: 1, Written: 1, Undone: true}
	if err := r.m.reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	as, err := getAcceptances(ctx, r.app.DB.R, id)
	if err != nil || len(as) != 1 || !as[0].outcome().Undone {
		t.Fatalf("acceptances %v %v", as, err)
	}
}

func TestHistoryCatchUpStopsAtCutoffAndHonorsDeadline(t *testing.T) {
	r := rig(t, true, nil)
	id := create(t, r)
	c := savedChange()
	r.lib.history = []libraryport.HistoryChange{c}
	r.lib.states[c.ID] = libraryport.ChangeStatus{ID: c.ID, State: "done", Files: 1, Written: 1}
	cursor, err := r.m.CatchUpHistory(context.Background(), c.ID-1)
	if err != nil || cursor != c.ID-1 {
		t.Fatalf("cursor %d: %v", cursor, err)
	}
	as, err := getAcceptances(context.Background(), r.app.DB.R, id)
	if err != nil || len(as) != 0 {
		t.Fatalf("accepted beyond cutoff: %v %v", as, err)
	}
	cursor, err = r.m.CatchUpHistory(context.Background(), c.ID)
	if err != nil || cursor != c.ID {
		t.Fatalf("cursor %d: %v", cursor, err)
	}
	r.m.reconcileGate <- struct{}{}
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	_, err = r.m.CatchUpHistory(ctx, c.ID)
	<-r.m.reconcileGate
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline: %v", err)
	}
}
