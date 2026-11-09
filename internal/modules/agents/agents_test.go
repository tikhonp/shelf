package agents

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/tikhonp/shelf/internal/platform/db/dbtest"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/tikhonp/shelf/internal/libraryport"
	"github.com/tikhonp/shelf/internal/platform"
	"github.com/tikhonp/shelf/internal/platform/config"
	"github.com/tikhonp/shelf/internal/platform/identity"
)

type testLibrary struct {
	libraryport.Port
	mu       sync.Mutex
	album    libraryport.Album
	values   map[int64]map[string][]string
	states   map[int64]libraryport.ChangeStatus
	warnings []libraryport.Warning
	history  []libraryport.HistoryChange
}

func newTestLibrary() *testLibrary {
	return &testLibrary{album: libraryport.Album{AlbumSummary: libraryport.AlbumSummary{ID: 1, Folder: "music/album", Title: "Album", Artist: "Artist"}, Tracks: []libraryport.Track{{ID: 1, Path: "music/album/1.flac", Revision: "r1"}}}, values: map[int64]map[string][]string{1: {"genre": {"Rock"}, "mood": {"Calm"}}}, states: map[int64]libraryport.ChangeStatus{}}
}
func (l *testLibrary) Album(context.Context, int64) (libraryport.Album, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	a := l.album
	a.Tracks = slices.Clone(l.album.Tracks)
	return a, nil
}
func (l *testLibrary) Albums(context.Context, libraryport.AlbumQuery) (libraryport.AlbumPage, error) {
	return libraryport.AlbumPage{Albums: []libraryport.AlbumSummary{l.album.AlbumSummary}, Total: 1}, nil
}
func (l *testLibrary) AlbumsOf(_ context.Context, ps []string) (map[string]int64, error) {
	out := map[string]int64{}
	for _, p := range ps {
		out[p] = 1
	}
	return out, nil
}
func (l *testLibrary) Plan(_ context.Context, r libraryport.PlanRequest) (libraryport.Plan, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	p := libraryport.Plan{AlbumID: 1, Preview: emptyPreview()}
	p.Preview.Album = libraryport.PreviewAlbum{ID: 1, Title: "Album", Artist: "Artist", Folder: "music/album"}
	p.Preview.Warnings = slices.Clone(l.warnings)
	if p.Preview.Warnings == nil {
		p.Preview.Warnings = []libraryport.Warning{}
	}
	for _, f := range r.Files {
		pf := libraryport.PreviewFile{TrackID: f.TrackID, Path: fmt.Sprintf("music/album/%d.flac", f.TrackID), Keys: []libraryport.KeyChange{}}
		for _, e := range f.Edits {
			key := e.Field
			if key == "" {
				key = e.Key
			}
			before := l.values[f.TrackID][key]
			if !slices.Equal(before, e.Values) {
				pf.Keys = append(pf.Keys, libraryport.KeyChange{Key: key, Before: before, After: e.Values})
			}
		}
		p.Preview.Files = append(p.Preview.Files, pf)
	}
	raw := mustJSON(p.Preview)
	h := sha256.Sum256([]byte(raw))
	p.Preview.Digest = hex.EncodeToString(h[:])
	p.Stored = json.RawMessage(mustJSON(r))
	for _, w := range l.warnings {
		if !slices.Contains(r.Confirm, w.Kind) {
			p.Missing = append(p.Missing, w.Kind)
		}
	}
	return p, nil
}
func (l *testLibrary) Enqueue(ctx context.Context, tx *sqlx.Tx, p libraryport.Plan, origin string) (int64, func(), error) {
	a := identity.From(ctx)
	r, e := tx.ExecContext(ctx, `INSERT INTO test_changes(origin,author,payload) VALUES(?,?,?)`, origin, a.Name, string(p.Stored))
	if e != nil {
		return 0, nil, e
	}
	id, e := r.LastInsertId()
	return id, func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		l.states[id] = libraryport.ChangeStatus{ID: id, State: "queued"}
	}, e
}
func (l *testLibrary) Changes(_ context.Context, ids []int64) (map[int64]libraryport.ChangeStatus, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := map[int64]libraryport.ChangeStatus{}
	for _, id := range ids {
		if s, ok := l.states[id]; ok {
			out[id] = s
		}
	}
	return out, nil
}
func (*testLibrary) Pin(context.Context, *sqlx.Tx, string, []string) error { return nil }
func (*testLibrary) Unpin(context.Context, *sqlx.Tx, string) error         { return nil }
func (*testLibrary) History(_ context.Context, after int64, _ int) (libraryport.HistoryPage, error) {
	return libraryport.HistoryPage{Next: after, Changes: []libraryport.HistoryChange{}}, nil
}

type testRig struct {
	handler http.Handler
	m       *Module
	app     *platform.App
	lib     *testLibrary
	token   string
	run     int64
}

func rig(t *testing.T, on bool, ops []string) *testRig {
	t.Helper()
	cfg := &config.Config{DataDir: t.TempDir(), MusicDir: t.TempDir(), AuthProvider: "none", AgentEnabled: on, TZ: time.UTC}
	m := New()
	lib := newTestLibrary()
	m.SetLibrary(lib)
	app, e := platform.Open(cfg, dbtest.Discard, m)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = app.Close() })
	if e = app.Migrate(context.Background()); e != nil {
		t.Fatal(e)
	}
	if _, e = app.DB.W.Exec(`CREATE TABLE test_changes(id INTEGER PRIMARY KEY,origin TEXT UNIQUE,author TEXT,payload TEXT) STRICT`); e != nil {
		t.Fatal(e)
	}
	_, token, e := m.AddConnection(context.Background(), "Test agent", ops)
	if e != nil {
		t.Fatal(e)
	}
	r := &testRig{m: m, app: app, lib: lib, token: token, handler: app.HTTP()}
	if on {
		res := r.call(t, "POST", "/api/agent/v1/runs", map[string]any{"client": "test"}, true)
		if res.Code != 201 {
			t.Fatal(res.Body.String())
		}
		var b struct {
			Run int64 `json:"run_id"`
		}
		_ = json.Unmarshal(res.Body.Bytes(), &b)
		r.run = b.Run
	}
	return r
}
func (r *testRig) call(t *testing.T, method, path string, body any, agent bool) *httptest.ResponseRecorder {
	t.Helper()
	b := []byte{}
	if body != nil {
		var e error
		b, e = json.Marshal(body)
		if e != nil {
			t.Fatal(e)
		}
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	if agent {
		req.Header.Set("Authorization", "Bearer "+r.token)
	}
	rec := httptest.NewRecorder()
	r.handler.ServeHTTP(rec, req)
	return rec
}
func want(t *testing.T, r *httptest.ResponseRecorder, code int) {
	t.Helper()
	if r.Code != code {
		t.Fatalf("got %d, want %d: %s", r.Code, code, r.Body.String())
	}
}
func create(t *testing.T, r *testRig) int64 {
	t.Helper()
	res := r.call(t, "POST", "/api/agent/v1/proposals", map[string]any{"run_id": r.run, "album_id": 1, "title": "Specific genre", "reason": "Match the user preference", "base": []BaseTrack{{TrackID: 1, Revision: "r1"}}, "ops": []Op{{ID: "genre", Type: "tag", Field: "genre", Tracks: []OpTrack{{TrackID: 1, Values: []string{"Art Rock"}}}}, {ID: "mood", Type: "tag", Field: "mood", Tracks: []OpTrack{{TrackID: 1, Values: []string{"Happy"}}}}}}, true)
	want(t, res, 201)
	var p struct {
		ID int64 `json:"id"`
	}
	_ = json.Unmarshal(res.Body.Bytes(), &p)
	return p.ID
}
func preview(t *testing.T, r *testRig, id int64, ops []string) libraryport.Preview {
	t.Helper()
	res := r.call(t, "POST", fmt.Sprintf("/api/inbox/proposals/%d/preview", id), map[string]any{"revision": 1, "ops": ops}, false)
	want(t, res, 200)
	var b struct {
		Preview libraryport.Preview `json:"preview"`
	}
	_ = json.Unmarshal(res.Body.Bytes(), &b)
	return b.Preview
}
func count(t *testing.T, r *testRig, table string) int {
	t.Helper()
	var n int
	if e := r.app.DB.R.Get(&n, `SELECT count(*) FROM `+table); e != nil {
		t.Fatal(e)
	}
	return n
}
func TestFeatureOffAndScopedTokens(t *testing.T) {
	r := rig(t, false, nil)
	want(t, r.call(t, "GET", "/api/inbox", nil, false), 404)
	want(t, r.call(t, "POST", "/api/agent/v1/runs", nil, true), 404)
	if e := r.m.RevokeConnection(context.Background(), 1); e != nil {
		t.Fatal(e)
	}
	r.m.enabled = true
	r.handler = r.app.HTTP()
	want(t, r.call(t, "POST", "/api/agent/v1/runs", nil, true), 401)
	s := rig(t, true, nil)
	want(t, s.call(t, "GET", "/api/inbox", nil, true), 401)
	want(t, s.call(t, "POST", "/api/library/albums/1/save", nil, true), 401)
	want(t, s.call(t, "POST", "/api/agent/v1/runs", nil, false), 401)
	want(t, s.call(t, "GET", "/api/agent/v1/../../inbox", nil, false), 400)
	want(t, s.call(t, "POST", "/api/inbox/connections", map[string]any{"name": "No account"}, false), 403)
}
func TestProposalDoesNotWriteAndAcceptanceIsIdempotent(t *testing.T) {
	r := rig(t, true, nil)
	id := create(t, r)
	if count(t, r, "test_changes") != 0 {
		t.Fatal("proposal wrote library")
	}
	p := preview(t, r, id, []string{"genre"})
	b := map[string]any{"revision": 1, "ops": []string{"genre"}, "digest": p.Digest, "confirm": []string{}, "request_id": "request-1"}
	path := fmt.Sprintf("/api/inbox/proposals/%d/accept", id)
	want(t, r.call(t, "POST", path, b, false), 202)
	want(t, r.call(t, "POST", path, b, false), 200)
	if count(t, r, "test_changes") != 1 {
		t.Fatal("repeated acceptance wrote twice")
	}
	b["ops"] = []string{"mood"}
	want(t, r.call(t, "POST", path, b, false), 409)
	var pending int
	_ = r.app.DB.R.Get(&pending, `SELECT pending FROM agents_proposals WHERE id=?`, id)
	if pending != 1 {
		t.Fatal("unselected field was decided")
	}
	want(t, r.call(t, "POST", fmt.Sprintf("/api/inbox/proposals/%d/decline", id), map[string]any{"revision": 1, "remaining": true, "reason": "Keep calm mood"}, false), 201)
	if count(t, r, "test_changes") != 1 {
		t.Fatal("decline wrote library")
	}
}
func TestListNamesFieldsWithoutValues(t *testing.T) {
	r := rig(t, true, nil)
	create(t, r)
	res := r.call(t, "GET", "/api/inbox/proposals?state=pending", nil, false)
	want(t, res, 200)
	var b struct {
		Proposals []struct {
			Fields     []map[string]any `json:"fields"`
			Confidence *int             `json:"confidence"`
		} `json:"proposals"`
	}
	_ = json.Unmarshal(res.Body.Bytes(), &b)
	if len(b.Proposals) != 1 || len(b.Proposals[0].Fields) != 2 {
		t.Fatalf("fields: %s", res.Body.String())
	}
	f := b.Proposals[0].Fields[0]
	if f["id"] != "genre" || f["field"] != "genre" || f["tracks"] != nil {
		t.Fatalf("field entry %v", f)
	}
}
func TestChangedPreviewAndConfirm(t *testing.T) {
	r := rig(t, true, nil)
	id := create(t, r)
	p := preview(t, r, id, []string{"genre"})
	r.lib.values[1]["genre"] = []string{"Pop"}
	path := fmt.Sprintf("/api/inbox/proposals/%d/accept", id)
	b := map[string]any{"revision": 1, "ops": []string{"genre"}, "digest": p.Digest, "request_id": "changed"}
	want(t, r.call(t, "POST", path, b, false), 409)
	if count(t, r, "test_changes") != 0 {
		t.Fatal("changed diff was applied")
	}
	r.lib.warnings = []libraryport.Warning{{Kind: "split", Message: "Splits the album"}}
	p = preview(t, r, id, []string{"genre"})
	b["digest"] = p.Digest
	want(t, r.call(t, "POST", path, b, false), 409)
	b["confirm"] = []string{"split"}
	want(t, r.call(t, "POST", path, b, false), 202)
}
func TestMemoryCommitAtomicVersionsAndCorrections(t *testing.T) {
	r := rig(t, true, nil)
	base := map[string]any{"run_id": r.run, "writes": []map[string]any{{"key": "genres", "expected_version": 0, "layer": "preference", "title": "Specific genres", "body": "Prefer art rock"}}, "learning": map[string]any{"expected_version": 0, "events_cursor": 0, "history_cursor": 0, "checkpoint": map[string]any{}}}
	want(t, r.call(t, "POST", "/api/agent/v1/memory/commit", base, true), 200)
	base["writes"] = []map[string]any{{"key": "context", "expected_version": 0, "layer": "context", "title": "Work", "body": "One album left"}}
	want(t, r.call(t, "POST", "/api/agent/v1/memory/commit", base, true), 409)
	if count(t, r, "agents_memory") != 1 {
		t.Fatal("stale learning committed memory")
	}
	want(t, r.call(t, "PUT", "/api/inbox/memory/1", map[string]any{"expected_version": 1, "body": "Preserve existing genres"}, false), 200)
	want(t, r.call(t, "DELETE", "/api/inbox/memory/1?expected_version=1", nil, false), 409)
	want(t, r.call(t, "DELETE", "/api/inbox/memory/1?expected_version=2", nil, false), 200)
	base["learning"] = nil
	base["writes"] = []map[string]any{{"key": "genres", "expected_version": 3, "layer": "preference", "title": "Restore", "body": "Old preference"}}
	want(t, r.call(t, "POST", "/api/agent/v1/memory/commit", base, true), 409)
	want(t, r.call(t, "GET", "/api/inbox/memory/export", nil, false), 200)
}
func TestUserRulesAndOperationScope(t *testing.T) {
	r := rig(t, true, []string{OpMemoryWrite})
	want(t, r.call(t, "GET", "/api/agent/v1/albums", nil, true), 403)
	want(t, r.call(t, "POST", "/api/inbox/memory", map[string]any{"key": "dates", "layer": "rule", "title": "Dates", "body": "Keep original dates"}, false), 200)
	b := map[string]any{"run_id": r.run, "writes": []map[string]any{{"key": "dates", "expected_version": 1, "layer": "rule", "title": "Dates", "body": "Replace dates"}}}
	want(t, r.call(t, "POST", "/api/agent/v1/memory/commit", b, true), 409)
}
func TestOutcomeFeedbackSurvivesRestartAndTracksUndo(t *testing.T) {
	r := rig(t, true, nil)
	id := create(t, r)
	p := preview(t, r, id, []string{"genre"})
	want(t, r.call(t, "POST", fmt.Sprintf("/api/inbox/proposals/%d/accept", id), map[string]any{"revision": 1, "ops": []string{"genre"}, "digest": p.Digest, "request_id": "outcome"}, false), 202)
	r.lib.states[1] = libraryport.ChangeStatus{ID: 1, State: "done", Files: 1, Written: 1}
	if e := r.m.reconcile(context.Background()); e != nil {
		t.Fatal(e)
	}
	n := count(t, r, "agents_events")
	if e := r.m.reconcile(context.Background()); e != nil {
		t.Fatal(e)
	}
	if count(t, r, "agents_events") != n {
		t.Fatal("duplicate outcome events")
	}
	r.lib.states[1] = libraryport.ChangeStatus{ID: 1, State: "done", Files: 1, Written: 1, Undone: true}
	if e := r.m.reconcile(context.Background()); e != nil {
		t.Fatal(e)
	}
	if count(t, r, "agents_events") != n+1 {
		t.Fatal("undo outcome missing")
	}
	want(t, r.call(t, "GET", "/api/agent/v1/events?after=0", nil, true), 200)
}
func TestConcurrentAcceptAndTransactionRollback(t *testing.T) {
	r := rig(t, true, nil)
	id := create(t, r)
	p := preview(t, r, id, []string{"genre"})
	path := fmt.Sprintf("/api/inbox/proposals/%d/accept", id)
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for _, request := range []string{"reviewer-1", "reviewer-2"} {
		wg.Go(func() {
			res := r.call(t, "POST", path, map[string]any{"revision": 1, "ops": []string{"genre"}, "digest": p.Digest, "request_id": request}, false)
			codes <- res.Code
		})
	}
	wg.Wait()
	close(codes)
	seen := map[int]int{}
	for c := range codes {
		seen[c]++
	}
	if seen[202] != 1 || seen[409] != 1 || count(t, r, "test_changes") != 1 {
		t.Fatalf("concurrent results %v", seen)
	}
	// A library insert failure must roll back the decision and acceptance.
	id2 := createDifferent(t, r)
	p2 := preview(t, r, id2, []string{"other"})
	if _, e := r.app.DB.W.Exec(`CREATE TRIGGER fail_writes BEFORE INSERT ON test_changes BEGIN SELECT RAISE(ABORT,'disk error'); END`); e != nil {
		t.Fatal(e)
	}
	want(t, r.call(t, "POST", fmt.Sprintf("/api/inbox/proposals/%d/accept", id2), map[string]any{"revision": 1, "ops": []string{"other"}, "digest": p2.Digest, "request_id": "rollback"}, false), 500)
	var decision string
	_ = r.app.DB.R.Get(&decision, `SELECT decision FROM agents_ops WHERE proposal_id=?`, id2)
	if decision != "pending" || count(t, r, "agents_acceptances") != 1 {
		t.Fatal("failed transaction left an accepted operation")
	}
}
func createDifferent(t *testing.T, r *testRig) int64 {
	t.Helper()
	res := r.call(t, "POST", "/api/agent/v1/proposals", map[string]any{"run_id": r.run, "album_id": 1, "title": "Other", "reason": "Test", "base": []BaseTrack{{TrackID: 1, Revision: "r1"}}, "ops": []Op{{ID: "other", Type: "tag", Field: "genre", Tracks: []OpTrack{{TrackID: 1, Values: []string{"Jazz"}}}}}}, true)
	want(t, res, 201)
	var p struct {
		ID int64 `json:"id"`
	}
	_ = json.Unmarshal(res.Body.Bytes(), &p)
	return p.ID
}

type missingAccounts struct{}

func (missingAccounts) AuthenticationEnabled() bool                 { return true }
func (missingAccounts) Exists(context.Context, int64) (bool, error) { return false, nil }
func TestDeletedCreatorAndBudgets(t *testing.T) {
	r := rig(t, true, nil)
	ctx := identity.With(context.Background(), identity.Account(99, "Deleted"))
	_, token, e := r.m.AddConnection(ctx, "Deleted creator", nil)
	if e != nil {
		t.Fatal(e)
	}
	r.m.accounts = missingAccounts{}
	r.token = token
	want(t, r.call(t, "POST", "/api/agent/v1/runs", nil, true), 401)
	var reason string
	_ = r.app.DB.R.Get(&reason, `SELECT revoke_reason FROM agents_connections WHERE name='Deleted creator'`)
	if reason != "account_deleted" {
		t.Fatal("connection not revoked after account deletion")
	}
	if e = r.app.DB.Write(ctx, func(tx *sqlx.Tx) error { return r.m.spend(ctx, tx, 1, "proposal", daily["proposal"]) }); e != nil {
		t.Fatal(e)
	}
	if e = r.m.spendNow(ctx, 1, "proposal", 1); e == nil {
		t.Fatal("daily proposal budget not enforced")
	}
}
func TestRevisionRaceAndDependencies(t *testing.T) {
	r := rig(t, true, nil)
	id := create(t, r)
	path := fmt.Sprintf("/api/inbox/proposals/%d/revisions", id)
	body := map[string]any{"parent_revision": 1, "ops": []map[string]any{{"id": "genre", "tracks": []OpTrack{{TrackID: 1, Values: []string{"Jazz"}}}}}}
	want(t, r.call(t, "POST", path, body, false), 200)
	want(t, r.call(t, "POST", path, body, false), 409)
	ops := []Op{{ID: "a", Type: "tag", Field: "genre", Tracks: []OpTrack{{TrackID: 1, Values: []string{"A"}}}, Requires: []string{"b"}}, {ID: "b", Type: "tag", Field: "mood", Tracks: []OpTrack{{TrackID: 1, Values: []string{"B"}}}}}
	if _, e := selectedOps(revisionRow{Ops: mustJSON(ops)}, map[string]opRow{"a": {Decision: "pending"}, "b": {Decision: "pending"}}, []string{"a"}, nil, nil); e == nil {
		t.Fatal("missing required op accepted")
	}
	ops[1].Requires = []string{"a"}
	if e := checkRequires(ops); e == nil {
		t.Fatal("cyclic dependencies accepted")
	}
}
func TestStaleBaseDuplicateAndStableTarget(t *testing.T) {
	r := rig(t, true, nil)
	id := create(t, r)
	want(t, r.call(t, "POST", "/api/agent/v1/proposals", map[string]any{"run_id": r.run, "album_id": 1, "title": "Duplicate", "reason": "Repeat", "base": []BaseTrack{{TrackID: 1, Revision: "r1"}}, "ops": []Op{{ID: "genre", Type: "tag", Field: "genre", Tracks: []OpTrack{{TrackID: 1, Values: []string{"Art Rock"}}}}}}, true), 409)
	r.lib.album.Tracks[0].Path = "music/other/1.flac"
	want(t, r.call(t, "POST", fmt.Sprintf("/api/inbox/proposals/%d/preview", id), map[string]any{"revision": 1, "ops": []string{"genre"}}, false), 409)
}

func TestContextDeletionAndRetentionRespectOutstandingFeedback(t *testing.T) {
	r := rig(t, true, nil)
	want(t, r.call(t, "POST", "/api/agent/v1/memory/commit", map[string]any{"run_id": r.run, "writes": []map[string]any{{"op": "put", "key": "work", "expected_version": 0, "layer": "context", "title": "Working notes", "body": "Finish this album"}}}, true), 200)
	want(t, r.call(t, "DELETE", "/api/inbox/memory/1?expected_version=1", nil, false), 200)
	if count(t, r, "agents_memory") != 0 {
		t.Fatal("deleted context stayed in memory")
	}
	id := create(t, r)
	if _, e := r.app.DB.W.Exec(`UPDATE agents_proposals SET updated_at='2000-01-01T00:00:00.000Z' WHERE id=?`, id); e != nil {
		t.Fatal(e)
	}
	if e := r.m.retain(context.Background()); e != nil {
		t.Fatal(e)
	}
	if count(t, r, "agents_proposals") != 1 {
		t.Fatal("pending proposal was trimmed")
	}
	want(t, r.call(t, "POST", fmt.Sprintf("/api/inbox/proposals/%d/decline", id), map[string]any{"revision": 1, "remaining": true}, false), 201)
	if _, e := r.app.DB.W.Exec(`UPDATE agents_proposals SET updated_at='2000-01-01T00:00:00.000Z' WHERE id=?`, id); e != nil {
		t.Fatal(e)
	}
	if e := r.m.retain(context.Background()); e != nil {
		t.Fatal(e)
	}
	if count(t, r, "agents_proposals") != 1 {
		t.Fatal("unlearned feedback was trimmed")
	}
	if _, e := r.app.DB.W.Exec(`UPDATE agents_learning SET events_cursor=(SELECT max(id) FROM agents_events)`); e != nil {
		t.Fatal(e)
	}
	if e := r.m.retain(context.Background()); e != nil {
		t.Fatal(e)
	}
	if count(t, r, "agents_proposals") != 0 {
		t.Fatal("old reviewed and processed proposal was not trimmed")
	}
}

func (l *testLibrary) SaveHistory(_ context.Context, after int64, _ int) (libraryport.HistoryPage, error) {
	page := libraryport.HistoryPage{Next: after}
	for _, c := range l.history {
		if c.ID > after {
			page.Changes = append(page.Changes, c)
			page.Next = c.ID
		}
	}
	return page, nil
}
