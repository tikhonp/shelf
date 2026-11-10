package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/jmoiron/sqlx"

	"github.com/tikhonp/shelf/internal/libraryport"
)

func TestSetupPromptFillsAddressesAndHoldsNoToken(t *testing.T) {
	r := rig(t, true, nil)
	res := r.call(t, "GET", "/api/inbox/setup", nil, false)
	want(t, res, 200)
	var b struct {
		Prompt string `json:"prompt"`
	}
	if e := json.Unmarshal(res.Body.Bytes(), &b); e != nil {
		t.Fatal(e)
	}
	for _, s := range []string{"http://example.com/api/agent/v1", "http://example.com/inbox/<id>", "$SHELF_AGENT_TOKEN",
		"https://www.navidrome.org/docs/usage/library/tagging/", "https://www.navidrome.org/docs/usage/customtags/",
		// docs/agents.md promises the artist credit rules and the feedback loop.
		"\"artists\" lists the track's individual artists", "Keep working until the user stops you", "every 10 minutes", "reset_at"} {
		if !strings.Contains(b.Prompt, s) {
			t.Errorf("prompt lacks %q", s)
		}
	}
	for _, s := range []string{"{base}", "{review}", r.token} {
		if strings.Contains(b.Prompt, s) {
			t.Errorf("prompt holds %q", s)
		}
	}
}

// The API root is the agent's only reference for request bodies, so it
// must document every JSON field a body decodes.
func TestAPIRootDocumentsFieldsAndEveryBody(t *testing.T) {
	r := rig(t, true, nil)
	res := r.call(t, "GET", "/api/agent/v1", nil, true)
	want(t, res, 200)
	var b struct {
		Fields []libraryport.Field          `json:"fields"`
		Bodies map[string]map[string]string `json:"bodies"`
	}
	if e := json.Unmarshal(res.Body.Bytes(), &b); e != nil {
		t.Fatal(e)
	}
	if len(b.Fields) != 2 || b.Fields[1].Name != "albumartists" || !b.Fields[1].Album {
		t.Fatalf("fields %+v", b.Fields)
	}
	for name, v := range map[string]any{
		"op": Op{}, "proposal evidence": Evidence{}, "memory evidence": memEvidence{}, "memory write": memoryWrite{}, "learning write": learningWrite{},
		"POST /proposals": createBody{}, "POST /proposals/:id/revisions": reviseBody{}, "POST /proposals/:id/comments": commentBody{},
		"POST /proposals/:id/withdraw": withdrawBody{},
	} {
		doc, ok := b.Bodies[name]
		if !ok {
			t.Errorf("no body %q", name)
			continue
		}
		for _, f := range jsonFields(reflect.TypeOf(v)) {
			if _, ok := doc[f]; !ok {
				t.Errorf("body %q lacks %q", name, f)
			}
		}
	}
}

func jsonFields(t reflect.Type) []string {
	var out []string
	for i := range t.NumField() {
		f := t.Field(i)
		if f.Anonymous {
			out = append(out, jsonFields(f.Type)...)
			continue
		}
		if name, _, _ := strings.Cut(f.Tag.Get("json"), ","); name != "" && name != "-" {
			out = append(out, name)
		}
	}
	return out
}

func TestNewConnectionPromptHoldsItsToken(t *testing.T) {
	p := prompt("https://shelf.test/api/agent/v1", "https://shelf.test/inbox/", "tok123")
	if !strings.Contains(p, `Bearer tok123"`) || strings.Contains(p, "$SHELF_AGENT_TOKEN") || strings.Contains(p, "{auth}") {
		t.Fatalf("prompt:\n%s", p)
	}
	m := New()
	if _, ok := m.Prompt("tok123"); ok {
		t.Fatal("a prompt without SHELF_PUBLIC_URL")
	}
	m.public, _ = url.Parse("https://shelf.test/")
	p, ok := m.Prompt("tok123")
	if !ok || !strings.Contains(p, "https://shelf.test/api/agent/v1.") || !strings.Contains(p, "https://shelf.test/inbox/<id>") || !strings.Contains(p, "tok123") {
		t.Fatalf("prompt:\n%s", p)
	}
}

func startRun(t *testing.T, r *testRig, token string) map[string]any {
	t.Helper()
	req := httptest.NewRequest("POST", "/api/agent/v1/runs", strings.NewReader(`{"client":"test"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	r.handler.ServeHTTP(rec, req)
	want(t, rec, 201)
	var b map[string]any
	if e := json.Unmarshal(rec.Body.Bytes(), &b); e != nil {
		t.Fatal(e)
	}
	return b
}

func TestRunStartListsOwnOutstandingWork(t *testing.T) {
	r := rig(t, true, nil)
	id := create(t, r)
	want(t, r.call(t, "POST", fmt.Sprintf("/api/inbox/proposals/%d/request-changes", id), map[string]any{"revision": 1, "comment": "Use Art Pop"}, false), 201)
	want(t, r.call(t, "POST", fmt.Sprintf("/api/inbox/proposals/%d/comments", id), map[string]any{"body": "Why this genre?"}, false), 201)
	ctx := context.Background()
	if e := r.app.DB.Write(ctx, func(tx *sqlx.Tx) error {
		_, e := r.m.insertEvent(ctx, tx, event{Kind: "outcome", Proposal: id, ActorKind: "system", ActorID: "system", Actor: "Shelf", Data: map[string]any{"state": "done", "undone": true}})
		return e
	}); e != nil {
		t.Fatal(e)
	}

	b := startRun(t, r, r.token)
	out, _ := json.Marshal(b["outstanding"])
	var o struct {
		Proposals []int64 `json:"proposals"`
		Pending   int     `json:"pending"`
		Changes   []struct {
			Proposal int64  `json:"proposal"`
			URL      string `json:"url"`
			Event    int64  `json:"comment_event"`
		} `json:"changes_requested"`
		Comments []struct {
			Proposal int64 `json:"proposal"`
		} `json:"unanswered_comments"`
		Outcomes []struct {
			Proposal int64  `json:"proposal"`
			State    string `json:"state"`
			Undone   bool   `json:"undone"`
		} `json:"outcomes"`
	}
	if e := json.Unmarshal(out, &o); e != nil {
		t.Fatal(e)
	}
	if !slices.Equal(o.Proposals, []int64{id}) || o.Pending != 1 {
		t.Fatalf("proposals %v, pending %d", o.Proposals, o.Pending)
	}
	if len(o.Changes) != 1 || o.Changes[0].Proposal != id || o.Changes[0].Event == 0 || !strings.HasSuffix(o.Changes[0].URL, fmt.Sprintf("/inbox/%d", id)) {
		t.Fatalf("changes %+v", o.Changes)
	}
	if len(o.Comments) != 1 || o.Comments[0].Proposal != id {
		t.Fatalf("comments %+v", o.Comments)
	}
	if len(o.Outcomes) != 1 || o.Outcomes[0].State != "done" || !o.Outcomes[0].Undone {
		t.Fatalf("outcomes %+v", o.Outcomes)
	}
	if n, _ := b["learning"].(map[string]any)["unprocessed_events"].(float64); n < 3 {
		t.Fatalf("unprocessed events %v", n)
	}

	// An agent comment answers the user's.
	want(t, r.call(t, "POST", fmt.Sprintf("/api/agent/v1/proposals/%d/comments", id), map[string]any{"run_id": r.run, "body": "MusicBrainz lists Art Rock"}, true), 201)
	if c := startRun(t, r, r.token)["outstanding"].(map[string]any)["unanswered_comments"].([]any); len(c) != 0 {
		t.Fatalf("still unanswered: %v", c)
	}

	// Another connection's run sees none of this work.
	_, other, e := r.m.AddConnection(ctx, "Other agent", nil)
	if e != nil {
		t.Fatal(e)
	}
	o2 := startRun(t, r, other)["outstanding"].(map[string]any)
	if len(o2["proposals"].([]any)) != 0 || len(o2["changes_requested"].([]any)) != 0 || len(o2["outcomes"].([]any)) != 0 {
		t.Fatalf("other connection sees %v", o2)
	}
}
