package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/tikhonp/shelf/internal/libraryport"
)

func TestRequiresChecksCurrentAcceptanceOutcome(t *testing.T) {
	for _, state := range []string{"queued", "writing", "done", "failed", "partial", "undone", "trimmed-done", "trimmed-failed"} {
		t.Run(state, func(t *testing.T) {
			r := rig(t, true, nil)
			ops := []Op{{ID: "a", Type: "tag", Field: "genre", Tracks: []OpTrack{{TrackID: 1, Values: []string{"Jazz"}}}, Requires: []string{"b"}}, {ID: "b", Type: "tag", Field: "mood", Tracks: []OpTrack{{TrackID: 1, Values: []string{"Happy"}}}}}
			res := r.call(t, "POST", "/api/agent/v1/proposals", map[string]any{"run_id": r.run, "album_id": 1, "title": "Dependencies", "reason": "Test", "base": []BaseTrack{{TrackID: 1, Revision: "r1"}}, "ops": ops}, true)
			want(t, res, 201)
			var p struct{ ID int64 }
			_ = json.Unmarshal(res.Body.Bytes(), &p)
			path := fmt.Sprintf("/api/inbox/proposals/%d", p.ID)
			pv := preview(t, r, p.ID, []string{"b"})
			want(t, r.call(t, "POST", path+"/accept", map[string]any{"revision": 1, "ops": []string{"b"}, "digest": pv.Digest, "request_id": "b"}, false), 202)
			r.lib.mu.Lock()
			if outcome, ok := strings.CutPrefix(state, "trimmed-"); ok {
				// Trimmed from the history: the last recorded outcome decides.
				delete(r.lib.states, 1)
				if _, err := r.app.DB.W.Exec(`UPDATE agents_acceptances SET outcome=?,final=1`, `{"state":"`+outcome+`","expired":true}`); err != nil {
					t.Fatal(err)
				}
			} else {
				s := libraryport.ChangeStatus{ID: 1, State: state}
				if state == "undone" {
					s.State = "done"
					s.Undone = true
				}
				r.lib.states[1] = s
			}
			r.lib.mu.Unlock()
			good := state == "queued" || state == "writing" || state == "done" || state == "trimmed-done"
			code := 422
			if good {
				code = 200
			}
			body := map[string]any{"revision": 1, "ops": []string{"a"}}
			want(t, r.call(t, "POST", path+"/preview", body, false), code)
			body["digest"] = strings.Repeat("a", 64)
			body["request_id"] = "a"
			if !good {
				want(t, r.call(t, "POST", path+"/accept", body, false), 422)
			}
			if state == "failed" {
				pv = preview(t, r, p.ID, []string{"a", "b"})
				want(t, r.call(t, "POST", path+"/accept", map[string]any{"revision": 1, "ops": []string{"a", "b"}, "digest": pv.Digest, "request_id": "retry"}, false), 202)
			}
		})
	}
}

func memoryDoc(t *testing.T, r *testRig, key string) memoryRow {
	t.Helper()
	var doc memoryRow
	if err := r.app.DB.R.Get(&doc, "SELECT * FROM agents_memory WHERE key=?", key); err != nil {
		t.Fatal(err)
	}
	return doc
}
func putMemory(t *testing.T, r *testRig, w memoryWrite) {
	t.Helper()
	want(t, r.call(t, "POST", "/api/agent/v1/memory/commit", map[string]any{"run_id": r.run, "writes": []memoryWrite{w}}, true), 200)
}
func TestMemoryVersionSurvivesRemovalAndReset(t *testing.T) {
	for _, layer := range []string{"rule", "context", "preference"} {
		for _, reset := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/reset=%v", layer, reset), func(t *testing.T) {
				r := rig(t, true, nil)
				w := memoryWrite{Key: "album.notes", Layer: layer, Title: "Notes", Body: "Original"}
				putMemory(t, r, w)
				old := memoryDoc(t, r, w.Key)
				if reset {
					want(t, r.call(t, "POST", "/api/inbox/memory/reset", map[string]any{"layers": []string{layer}}, false), 200)
				} else {
					want(t, r.call(t, "DELETE", fmt.Sprintf("/api/inbox/memory/%d?expected_version=%d", old.ID, old.Version), nil, false), 200)
				}
				if layer == "preference" {
					tomb := memoryDoc(t, r, w.Key)
					if tomb.Version <= old.Version || tomb.Body != old.Body {
						t.Fatalf("tombstone %+v", tomb)
					}
					return
				}
				w.Body = "Recreated"
				putMemory(t, r, w)
				fresh := memoryDoc(t, r, w.Key)
				if fresh.Version <= old.Version {
					t.Fatalf("version reused: %d -> %d", old.Version, fresh.Version)
				}
				w.Expected = old.Version
				w.Body = "Stale"
				want(t, r.call(t, "POST", "/api/agent/v1/memory/commit", map[string]any{"run_id": r.run, "writes": []memoryWrite{w}}, true), 409)
				w.Expected = fresh.Version
				putMemory(t, r, w)
				if memoryDoc(t, r, w.Key).Version <= fresh.Version {
					t.Fatal("update reused version")
				}
			})
		}
	}
}
func TestAgentContextDeletionReleasesQuota(t *testing.T) {
	r := rig(t, true, nil)
	write := func(w memoryWrite) {
		t.Helper()
		if err := r.app.DB.Write(context.Background(), func(tx *sqlx.Tx) error {
			_, err := r.m.memoryWrite(context.Background(), tx, w, Connection{ID: 1, Name: "Test agent"}, r.run)
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 64; i++ {
		write(memoryWrite{Key: fmt.Sprintf("notes.%d", i), Layer: "context", Title: "Notes", Scope: "album", Body: strings.Repeat("x", 65536), Evidence: []memEvidence{{Note: "Research"}}})
	}
	for i := 0; i < 64; i++ {
		key := fmt.Sprintf("notes.%d", i)
		old := memoryDoc(t, r, key)
		write(memoryWrite{Op: "delete", Key: key, Expected: old.Version})
		doc := memoryDoc(t, r, key)
		if doc.Body != "" || doc.Scope != "" || doc.Evidence != "[]" {
			t.Fatalf("deleted context retained content: %+v", doc)
		}
	}
	write(memoryWrite{Key: "more", Layer: "context", Title: "More notes", Body: "New research"})
}
func TestMemoryEventRetentionPreservesCursorAndCorrection(t *testing.T) {
	r := rig(t, true, nil)
	putMemory(t, r, memoryWrite{Key: "preference", Layer: "preference", Title: "Preference", Body: "Original"})
	doc := memoryDoc(t, r, "preference")
	want(t, r.call(t, "PUT", fmt.Sprintf("/api/inbox/memory/%d", doc.ID), map[string]any{"expected_version": doc.Version, "body": "Correction"}, false), 200)
	var corrected int64
	if err := r.app.DB.R.Get(&corrected, "SELECT max(id) FROM agents_events"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.app.DB.W.Exec("UPDATE agents_events SET at='2000-01-01T00:00:00.000Z'"); err != nil {
		t.Fatal(err)
	}
	if err := r.m.retain(context.Background()); err != nil {
		t.Fatal(err)
	}
	if count(t, r, "agents_events") != 1 {
		t.Fatal("unprocessed event removed")
	}
	if _, err := r.app.DB.W.Exec("UPDATE agents_learning SET events_cursor=?", corrected); err != nil {
		t.Fatal(err)
	}
	if err := r.m.retain(context.Background()); err != nil {
		t.Fatal(err)
	}
	if count(t, r, "agents_events") != 0 {
		t.Fatal("processed old event retained")
	}
	want(t, r.call(t, "POST", "/api/agent/v1/memory/commit", map[string]any{"run_id": r.run, "learning": map[string]any{"expected_version": 0, "events_cursor": corrected, "history_cursor": 0, "checkpoint": map[string]any{}}}, true), 200)
	doc = memoryDoc(t, r, "preference")
	w := memoryWrite{Key: doc.Key, Expected: doc.Version, Layer: doc.Layer, Title: doc.Title, Body: "Override"}
	want(t, r.call(t, "POST", "/api/agent/v1/memory/commit", map[string]any{"run_id": r.run, "writes": []memoryWrite{w}}, true), 409)
	want(t, r.call(t, "POST", "/api/inbox/memory", map[string]any{"key": "new.evidence", "layer": "rule", "title": "New evidence", "body": "New feedback"}, false), 200)
	var newer int64
	if err := r.app.DB.R.Get(&newer, "SELECT max(id) FROM agents_events"); err != nil {
		t.Fatal(err)
	}
	if newer <= corrected {
		t.Fatal("event ID reused")
	}
	doc = memoryDoc(t, r, "preference")
	w.Expected = doc.Version
	w.Reconsider = &struct {
		Reason string  `json:"reason"`
		Events []int64 `json:"evidence_events"`
	}{Events: []int64{corrected}, Reason: "Old evidence"}
	want(t, r.call(t, "POST", "/api/agent/v1/memory/commit", map[string]any{"run_id": r.run, "writes": []memoryWrite{w}}, true), 409)
	w.Reconsider.Events = []int64{newer}
	putMemory(t, r, w)
}

func TestMemoryVersionMigrationSeedsExistingMaximum(t *testing.T) {
	r := rig(t, true, nil)
	putMemory(t, r, memoryWrite{Key: "existing", Layer: "context", Title: "Existing"})
	if _, err := r.app.DB.W.Exec("UPDATE agents_memory SET version=83"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.app.DB.W.Exec("DELETE FROM agents_state WHERE key='memory_version'"); err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile("migrations/00002_memory_versions.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.app.DB.W.Exec(strings.Split(string(migration), "-- +goose Down")[0]); err != nil {
		t.Fatal(err)
	}
	putMemory(t, r, memoryWrite{Key: "new", Layer: "context", Title: "New"})
	if memoryDoc(t, r, "new").Version != 84 {
		t.Fatal("migration did not seed maximum")
	}
}
