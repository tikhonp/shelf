package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/tikhonp/shelf/internal/libraryport"
	"github.com/tikhonp/shelf/internal/modules/library"
	"github.com/tikhonp/shelf/internal/platform"
	"github.com/tikhonp/shelf/internal/platform/config"
	"github.com/tikhonp/shelf/internal/platform/db/dbtest"
	"github.com/tikhonp/shelf/internal/platform/testwait"
	"go.senan.xyz/taglib"
)

func until(t *testing.T, ready func() bool) {
	t.Helper()
	deadline := testwait.Deadline(5 * time.Second)
	for time.Now().Before(deadline) {
		if ready() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("timed out waiting for background work")
}
func TestRealLibraryProposalAcceptAndArtwork(t *testing.T) {
	music := t.TempDir()
	dir := filepath.Join(music, "source", "Album")
	if e := os.MkdirAll(dir, 0755); e != nil {
		t.Fatal(e)
	}
	audio := filepath.Join(dir, "1.flac")
	fixture, e := os.ReadFile(filepath.Join("..", "library", "tagio", "testdata", "track.flac"))
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(audio, fixture, 0644); e != nil {
		t.Fatal(e)
	}
	if e = taglib.WriteTags(audio, map[string][]string{"TITLE": {"Track"}, "ALBUM": {"Album"}, "ARTIST": {"Artist"}, "ALBUMARTIST": {"Artist"}, "GENRE": {"Rock"}}, 0); e != nil {
		t.Fatal(e)
	}
	cfg := &config.Config{DataDir: t.TempDir(), MusicDir: music, AuthProvider: "none", AgentEnabled: true, TZ: time.UTC}
	lib := library.New()
	m := New()
	m.SetLibrary(lib.Port())
	app, e := platform.Open(cfg, dbtest.Discard, lib, m)
	if e != nil {
		t.Fatal(e)
	}
	if e = app.Migrate(context.Background()); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	libDone := make(chan error, 1)
	go func() { libDone <- lib.Run(ctx) }()
	app.Jobs.Poll = 5 * time.Millisecond
	jobDone := make(chan error, 1)
	go func() { jobDone <- app.Jobs.Start(ctx) }()
	t.Cleanup(func() {
		cancel()
		if e := <-libDone; e != nil {
			t.Error(e)
		}
		if e := <-jobDone; e != nil {
			t.Error(e)
		}
		_ = app.Close()
	})
	var album libraryport.Album
	until(t, func() bool {
		page, e := lib.Port().Albums(ctx, libraryport.AlbumQuery{})
		if e != nil || len(page.Albums) != 1 {
			return false
		}
		album, e = lib.Port().Album(ctx, page.Albums[0].ID)
		return e == nil && len(album.Tracks) == 1
	})
	_, token, e := m.AddConnection(ctx, "Real agent", nil)
	if e != nil {
		t.Fatal(e)
	}
	r := &testRig{m: m, app: app, token: token, handler: app.HTTP()}
	res := r.call(t, "POST", "/api/agent/v1/runs", nil, true)
	want(t, res, 201)
	var boot struct {
		Run int64 `json:"run_id"`
	}
	_ = json.Unmarshal(res.Body.Bytes(), &boot)
	r.run = boot.Run
	before, e := os.ReadFile(audio)
	if e != nil {
		t.Fatal(e)
	}
	body := map[string]any{"run_id": r.run, "album_id": album.ID, "title": "Specific genre", "reason": "More precise genre", "base": []BaseTrack{{TrackID: album.Tracks[0].ID, Revision: album.Tracks[0].Revision}}, "ops": []Op{{ID: "genre", Type: "tag", Field: "genre", Tracks: []OpTrack{{TrackID: album.Tracks[0].ID, Values: []string{"Art Rock"}}}}, {ID: "mood", Type: "tag", TagKey: "MOOD", Tracks: []OpTrack{{TrackID: album.Tracks[0].ID, Values: []string{"Calm"}}}}}}
	res = r.call(t, "POST", "/api/agent/v1/proposals", body, true)
	want(t, res, 201)
	var proposal struct {
		ID int64 `json:"id"`
	}
	_ = json.Unmarshal(res.Body.Bytes(), &proposal)
	after, _ := os.ReadFile(audio)
	if !bytes.Equal(before, after) {
		t.Fatal("proposal changed real file")
	}
	pv := preview(t, r, proposal.ID, []string{"genre"})
	res = r.call(t, "POST", fmt.Sprintf("/api/inbox/proposals/%d/accept", proposal.ID), map[string]any{"revision": 1, "ops": []string{"genre"}, "digest": pv.Digest, "request_id": "real-write"}, false)
	want(t, res, 202)
	var accepted struct {
		Acceptance struct {
			Change int64 `json:"change_id"`
		} `json:"acceptance"`
	}
	_ = json.Unmarshal(res.Body.Bytes(), &accepted)
	until(t, func() bool {
		st, e := lib.Port().Changes(ctx, []int64{accepted.Acceptance.Change})
		return e == nil && st[accepted.Acceptance.Change].State == "done"
	})
	tags, e := taglib.ReadTags(audio)
	if e != nil {
		t.Fatal(e)
	}
	if !slices.Equal(tags["GENRE"], []string{"Art Rock"}) || len(tags["MOOD"]) != 0 {
		t.Fatalf("selected field write %v", tags)
	}
	if e = m.reconcile(ctx); e != nil {
		t.Fatal(e)
	}
	want(t, r.call(t, "GET", fmt.Sprintf("/api/inbox/proposals/%d", proposal.ID), nil, false), 200)
	// A fresh run can learn the manual and accepted change through retained history.
	want(t, r.call(t, "GET", "/api/agent/v1/history?after=0", nil, true), 200)
	// Use a validated image byte upload, then propose a cover by its hash.
	pic := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	pic.Set(0, 0, color.NRGBA{R: 255, A: 255})
	var imageBytes bytes.Buffer
	if e = png.Encode(&imageBytes, pic); e != nil {
		t.Fatal(e)
	}
	img, e := lib.Port().PutCover(ctx, imageBytes.Bytes())
	if e != nil {
		t.Fatal(e)
	}
	album, e = lib.Port().Album(ctx, album.ID)
	if e != nil {
		t.Fatal(e)
	}
	body["title"] = "Artwork"
	body["base"] = []BaseTrack{{TrackID: album.Tracks[0].ID, Revision: album.Tracks[0].Revision}}
	body["ops"] = []Op{{ID: "cover", Type: "cover", Action: "replace", Image: img.ID}}
	res = r.call(t, "POST", "/api/agent/v1/proposals", body, true)
	want(t, res, 201)
	_ = json.Unmarshal(res.Body.Bytes(), &proposal)
	pv = preview(t, r, proposal.ID, []string{"cover"})
	res = r.call(t, "POST", fmt.Sprintf("/api/inbox/proposals/%d/accept", proposal.ID), map[string]any{"revision": 1, "ops": []string{"cover"}, "digest": pv.Digest, "request_id": "real-cover"}, false)
	want(t, res, 202)
	_ = json.Unmarshal(res.Body.Bytes(), &accepted)
	until(t, func() bool {
		st, e := lib.Port().Changes(ctx, []int64{accepted.Acceptance.Change})
		return e == nil && st[accepted.Acceptance.Change].State == "done"
	})
	stored, e := os.ReadFile(filepath.Join(dir, "cover.png"))
	if e != nil || !bytes.Equal(stored, imageBytes.Bytes()) {
		t.Fatalf("accepted artwork missing: %v", e)
	}
	// The machine credential cannot call the human approval path, even here.
	want(t, r.call(t, http.MethodPost, fmt.Sprintf("/api/inbox/proposals/%d/accept", proposal.ID), map[string]any{}, true), 401)
}
