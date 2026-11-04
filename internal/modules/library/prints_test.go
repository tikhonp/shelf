package library

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/tikhonp/shelf/internal/modules/library/acoustid"
)

// printRig is a rig whose AcoustID answers with the release, and whose fpcalc
// is a script.
func printRig(t *testing.T, release string) (*rig, *services, *atomic.Int32) {
	t.Helper()
	r, sv, _ := suggestRig(t)
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		if release == "" {
			_, _ = w.Write([]byte(`{"status": "ok", "results": []}`))
			return
		}
		_, _ = fmt.Fprintf(w, `{"status": "ok", "results": [{"score": 0.95, "recordings": [{"id": "rec", "releases": [{"id": %q}]}]},
			{"score": 0.2, "recordings": [{"id": "weak", "releases": [{"id": "not-this-one"}]}]}]}`, release)
	}))
	t.Cleanup(srv.Close)
	fp := filepath.Join(t.TempDir(), "fpcalc")
	if err := os.WriteFile(fp, []byte("#!/bin/sh\necho '{\"duration\": 1, \"fingerprint\": \"AQAA\"}'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	r.m.Prints = acoustid.New("key", "test")
	r.m.Prints.Base, r.m.Prints.FPCalc = srv.URL, fp
	return r, sv, &calls
}

func TestAudioFindsTheAnchorWhenTextFindsNothing(t *testing.T) {
	r, _, calls := printRig(t, blueID)
	id := r.blueAlbum(map[string][]string{"ALBUM": {"Nothing Like It"}}, map[string][]string{"ALBUM": {"Nothing Like It"}})
	events, stop := r.hub.Subscribe(SuggestionsTopic)
	defer stop()

	first := r.suggest(id, "")
	if first.Anchor != nil || !first.Fingerprinting {
		t.Fatalf("%+v", first)
	}
	r.startJobs()
	r.drain()
	if n := calls.Load(); n != 2 {
		t.Fatalf("%d lookups", n)
	}
	select {
	case e := <-events:
		if e.Data.(printPayload).Album != id {
			t.Fatalf("%+v", e)
		}
	default:
		t.Fatal("no event")
	}
	second := r.suggest(id, "")
	if second.Fingerprinting || second.Anchor == nil || second.Anchor.By != byPrint || second.Anchor.ID != blueID || !second.Anchor.Matched {
		t.Fatalf("%+v", second)
	}
	// The answer is kept, so another opening asks nobody.
	r.suggest(id, "")
	r.drain()
	if n := calls.Load(); n != 2 {
		t.Fatalf("%d lookups", n)
	}
}

func TestAudioThatFindsNothingIsKeptAsAMiss(t *testing.T) {
	r, _, calls := printRig(t, "")
	id := r.blueAlbum(map[string][]string{"ALBUM": {"Nothing Like It"}}, map[string][]string{"ALBUM": {"Nothing Like It"}})
	r.suggest(id, "")
	r.startJobs()
	r.drain()
	got := r.suggest(id, "")
	if got.Anchor != nil || got.Fingerprinting {
		t.Fatalf("%+v", got)
	}
	r.drain()
	if n := calls.Load(); n != 2 {
		t.Fatalf("%d lookups", n)
	}
}

func TestAudioWaitsForTextLookups(t *testing.T) {
	// A text search that finds the release needs no fingerprint.
	r, _, calls := printRig(t, blueID)
	id := r.blueAlbum(nil, nil)
	if got := r.suggest(id, ""); got.Anchor == nil || got.Anchor.By != bySearch || got.Fingerprinting {
		t.Fatalf("%+v", got)
	}
	if calls.Load() != 0 {
		t.Fatal("asked AcoustID")
	}
}

func TestAudioDoesNothingWhenMusicBrainzFails(t *testing.T) {
	r, sv, _ := printRig(t, blueID)
	id := r.blueAlbum(map[string][]string{"ALBUM": {"Nothing Like It"}}, map[string][]string{"ALBUM": {"Nothing Like It"}})
	sv.mu.Lock()
	sv.down = true
	sv.mu.Unlock()
	if got := r.suggest(id, ""); got.Fingerprinting {
		t.Fatalf("%+v", got)
	}
}

func TestNoKeyNoFingerprint(t *testing.T) {
	r, _, _ := suggestRig(t)
	id := r.blueAlbum(map[string][]string{"ALBUM": {"Nothing Like It"}}, map[string][]string{"ALBUM": {"Nothing Like It"}})
	if got := r.suggest(id, ""); got.Fingerprinting {
		t.Fatalf("%+v", got)
	}
}

func TestFailedFingerprintStoresNothing(t *testing.T) {
	r, _, calls := printRig(t, blueID)
	r.m.Prints.FPCalc = filepath.Join(t.TempDir(), "gone")
	id := r.blueAlbum(map[string][]string{"ALBUM": {"Nothing Like It"}}, map[string][]string{"ALBUM": {"Nothing Like It"}})
	r.suggest(id, "")
	r.startJobs()
	r.drain()
	if calls.Load() != 0 {
		t.Fatal("asked AcoustID")
	}
	// The next opening tries again.
	if got := r.suggest(id, ""); !got.Fingerprinting {
		t.Fatalf("%+v", got)
	}
}
