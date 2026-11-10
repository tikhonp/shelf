package agents

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

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
		"https://www.navidrome.org/docs/usage/library/tagging/", "https://www.navidrome.org/docs/usage/customtags/"} {
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
