package library

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/labstack/echo/v5"
	"github.com/tikhonp/shelf/internal/modules/library/tagio"
	"github.com/tikhonp/shelf/internal/platform/web"
	"go.senan.xyz/taglib"
)

func testImage(w, h int) image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x), G: uint8(y), B: 128, A: 255})
		}
	}
	return img
}

func jpegOf(t testing.TB, w, h int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := jpeg.Encode(&b, testImage(w, h), nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func pngOf(t testing.TB, w, h int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, testImage(w, h)); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func size(t *testing.T, b []byte) (int, int) {
	t.Helper()
	cfg, _, err := image.DecodeConfig(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	return cfg.Width, cfg.Height
}

func (r *rig) raw(method, path string, body []byte) *httptest.ResponseRecorder {
	e := echo.New()
	e.HTTPErrorHandler = web.ErrorHandler
	r.m.Routes(e.Group("/api"))
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(method, path, bytes.NewReader(body)))
	return rec
}

func (r *rig) upload(b []byte) imageInfo {
	r.t.Helper()
	rec := r.raw(http.MethodPost, "/api/library/covers", b)
	if rec.Code != http.StatusCreated {
		r.t.Fatalf("upload: %d %s", rec.Code, rec.Body)
	}
	return decode[imageInfo](r.t, rec)
}

func front(t *testing.T, p string) []byte {
	t.Helper()
	b, err := tagio.FrontCover(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func embedCover(t *testing.T, p string, b []byte) {
	t.Helper()
	if err := taglib.WriteImageOptions(p, b, 0, "Front Cover", "", "image/jpeg"); err != nil {
		t.Fatal(err)
	}
}

func TestCoverUploads(t *testing.T) {
	r := newRig(t)
	big := jpegOf(t, 1600, 1200)
	info := r.upload(big)
	if info.MIME != "image/jpeg" || info.Width != 1600 || info.Height != 1200 || info.Size != len(big) || info.ID != hashOf(big) {
		t.Fatalf("info %+v", info)
	}
	rec := r.do(http.MethodGet, "/api/library/covers/"+info.ID)
	if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), big) || rec.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("get: %d %s", rec.Code, rec.Header())
	}
	for name, b := range map[string][]byte{"text": []byte("hello"), "half a JPEG": big[:200], "a GIF": []byte("GIF89a....")} {
		if rec := r.raw(http.MethodPost, "/api/library/covers", b); rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s: %d", name, rec.Code)
		}
	}
	for _, id := range []string{"x", "../shelf.db", hashOf([]byte("none"))} {
		if rec := r.do(http.MethodGet, "/api/library/covers/"+id); rec.Code != http.StatusNotFound {
			t.Errorf("%s: %d", id, rec.Code)
		}
	}

	// The embedded copy stops at 1000 on its longest side, in its format.
	for _, tc := range []struct {
		in         []byte
		w, h       int
		mime, kept string
	}{
		{big, 1000, 750, "image/jpeg", ""},
		{pngOf(t, 600, 1500), 400, 1000, "image/png", ""},
		{jpegOf(t, 1000, 1000), 1000, 1000, "image/jpeg", "kept"},
	} {
		out, err := embedded(tc.in)
		if err != nil {
			t.Fatal(err)
		}
		if w, h := size(t, out); w != tc.w || h != tc.h || http.DetectContentType(out) != tc.mime || (tc.kept != "") != bytes.Equal(out, tc.in) {
			t.Errorf("%dx%d %s, want %dx%d %s", w, h, http.DetectContentType(out), tc.w, tc.h, tc.mime)
		}
	}
}

func TestSaveCover(t *testing.T) {
	r := newRig(t)
	p1 := r.add("folk/Blue/01.flac", "track.flac", blue("1", nil))
	p2 := r.add("folk/Blue/02.flac", "track.flac", blue("2", nil))
	old := jpegOf(t, 50, 50)
	embedCover(t, p1, old)
	oldPNG := pngOf(t, 40, 40)
	r.write("folk/Blue/cover.png", oldPNG)
	r.walk()
	r.startJobs()
	id := r.albums("").Albums[0].ID
	up := jpegOf(t, 1600, 1200)
	info := r.upload(up)

	s := r.saved(id, saveBody{Tracks: everyTrack(r.open(id)), Cover: &coverEdit{Upload: info.ID}})
	if s.State != StateDone || s.Tags != 0 || !s.Cover {
		t.Fatalf("save %+v", s)
	}
	if w, h := size(t, front(t, p1)); w != 1000 || h != 750 {
		t.Errorf("track 1's cover is %dx%d", w, h)
	}
	if front(t, p2) != nil {
		t.Error("track 2 got a cover it didn't have")
	}
	if b, err := os.ReadFile(filepath.Join(r.root, "folk/Blue/cover.jpg")); err != nil || !bytes.Equal(b, up) {
		t.Errorf("cover.jpg: %v", err)
	}
	if _, err := os.Stat(filepath.Join(r.root, "folk/Blue/cover.png")); !os.IsNotExist(err) {
		t.Error("cover.png is still there")
	}
	if fi, err := os.Stat(filepath.Join(r.root, "folk/Blue/cover.jpg")); err != nil || fi.Mode().Perm() != 0o644 {
		t.Errorf("cover.jpg mode %v", fi)
	}
	kinds := map[string]string{}
	for _, f := range s.Files {
		kinds[f.Path] = f.Kind + " " + f.CoverBefore[:min(8, len(f.CoverBefore))] + ">" + f.CoverAfter[:min(8, len(f.CoverAfter))]
		if !f.Written {
			t.Errorf("%s not written", f.Path)
		}
	}
	if want := map[string]string{
		"folk/Blue/01.flac":   "audio " + hashOf(old)[:8] + ">" + s.Files[0].CoverAfter[:8],
		"folk/Blue/cover.jpg": "cover >" + info.ID[:8],
		"folk/Blue/cover.png": "cover " + hashOf(oldPNG)[:8] + ">",
	}; len(kinds) != 3 || kinds["folk/Blue/01.flac"] != want["folk/Blue/01.flac"] ||
		kinds["folk/Blue/cover.jpg"] != want["folk/Blue/cover.jpg"] || kinds["folk/Blue/cover.png"] != want["folk/Blue/cover.png"] {
		t.Errorf("files %v", kinds)
	}
	if rec := r.do(http.MethodGet, "/api/library/albums/"+itoa(id)+"/cover"); rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), up) {
		t.Errorf("album cover: %d", rec.Code)
	}

	// Undo puts back the old pictures, from the store.
	if u := r.reversed(KindUndo, s.ID); u.State != StateDone || len(u.Conflicts) != 0 {
		t.Fatalf("undo %+v", u)
	}
	if !bytes.Equal(front(t, p1), old) {
		t.Error("track 1's old cover isn't back")
	}
	if b, err := os.ReadFile(filepath.Join(r.root, "folk/Blue/cover.png")); err != nil || !bytes.Equal(b, oldPNG) {
		t.Errorf("cover.png: %v", err)
	}
	if _, err := os.Stat(filepath.Join(r.root, "folk/Blue/cover.jpg")); !os.IsNotExist(err) {
		t.Error("cover.jpg is still there")
	}
	r.reversed(KindRedo, s.ID)
	if w, _ := size(t, front(t, p1)); w != 1000 {
		t.Error("redo didn't write the cover")
	}

	// Every track gets the cover when asked, and Delete removes all of it.
	r.walk()
	id = r.albums("").Albums[0].ID
	small := r.upload(jpegOf(t, 300, 300))
	r.saved(id, saveBody{Tracks: everyTrack(r.open(id)), Cover: &coverEdit{Upload: small.ID, EveryTrack: true}})
	if front(t, p2) == nil || !bytes.Equal(front(t, p2), front(t, p1)) {
		t.Error("track 2 has no cover")
	}
	r.walk()
	id = r.albums("").Albums[0].ID
	if d := r.saved(id, saveBody{Tracks: everyTrack(r.open(id)), Cover: &coverEdit{Delete: true}}); d.State != StateDone {
		t.Fatalf("delete %+v", d)
	}
	if front(t, p1) != nil || front(t, p2) != nil {
		t.Error("an embedded cover stayed")
	}
	if names := r.m.folderCovers("folk/Blue"); names != nil {
		t.Errorf("cover files %q", names)
	}
	if rec := r.do(http.MethodGet, "/api/library/albums/"+itoa(id)+"/cover"); rec.Code != http.StatusNotFound {
		t.Errorf("album cover after delete: %d", rec.Code)
	}
}

// A cover another tool described, such as Kraftwerk's "Cover", keeps its
// description through a replace and its undo, and an undo of a Delete puts
// the description back with the picture.
func TestSaveCoverKeepsDescription(t *testing.T) {
	r := newRig(t)
	p := r.add("electronic/Kraftwerk/01.flac", "track.flac", blue("1", nil))
	old := pngOf(t, 50, 50)
	if err := taglib.WriteImageOptions(p, old, 0, "Front Cover", "Cover", "image/png"); err != nil {
		t.Fatal(err)
	}
	r.walk()
	r.startJobs()
	// The part of a revision after the last dash hashes the tags and pictures.
	tagHash := func() string {
		f, err := tagio.Read(p)
		if err != nil {
			t.Fatal(err)
		}
		rev := f.Revision()
		return rev[strings.LastIndex(rev, "-")+1:]
	}
	desc := func() string {
		_, pic, err := tagio.FrontPicture(p)
		if err != nil {
			t.Fatal(err)
		}
		return pic.Description
	}
	was := tagHash()

	id := r.albums("").Albums[0].ID
	s := r.saved(id, saveBody{Tracks: everyTrack(r.open(id)), Cover: &coverEdit{Upload: r.upload(jpegOf(t, 300, 300)).ID}})
	if s.State != StateDone || desc() != "Cover" {
		t.Fatalf("replace: %s, description %q", s.State, desc())
	}
	if u := r.reversed(KindUndo, s.ID); u.State != StateDone || !bytes.Equal(front(t, p), old) || tagHash() != was {
		t.Fatalf("undo of the replace: %+v, description %q", u, desc())
	}

	r.walk()
	id = r.albums("").Albums[0].ID
	d := r.saved(id, saveBody{Tracks: everyTrack(r.open(id)), Cover: &coverEdit{Delete: true}})
	if d.State != StateDone || front(t, p) != nil {
		t.Fatalf("delete %+v", d)
	}
	if u := r.reversed(KindUndo, d.ID); u.State != StateDone || !bytes.Equal(front(t, p), old) || desc() != "Cover" || tagHash() != was {
		t.Fatalf("undo of the delete: %+v, description %q", u, desc())
	}
	if rd := r.reversed(KindRedo, d.ID); rd.State != StateDone || front(t, p) != nil {
		t.Fatalf("redo of the delete %+v", rd)
	}
}

func TestSaveCoverRefuses(t *testing.T) {
	r := newRig(t)
	r.add("folk/Blue/01.flac", "track.flac", blue("1", nil))
	r.add("folk/Blue/02.flac", "track.flac", blue("2", nil))
	r.walk()
	id := r.albums("").Albums[0].ID
	d := r.open(id)
	up := r.upload(jpegOf(t, 20, 20))
	save := "/api/library/albums/" + itoa(id) + "/save"
	for name, body := range map[string]saveBody{
		"one track":         {Tracks: everyTrack(d)[:1], Cover: &coverEdit{Upload: up.ID}},
		"upload and delete": {Tracks: everyTrack(d), Cover: &coverEdit{Upload: up.ID, Delete: true}},
		"neither":           {Tracks: everyTrack(d), Cover: &coverEdit{}},
		"no such upload":    {Tracks: everyTrack(d), Cover: &coverEdit{Upload: hashOf([]byte("x"))}},
		"nothing to delete": {Tracks: everyTrack(d), Cover: &coverEdit{Delete: true}},
	} {
		if rec := r.post(save, body); rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body)
		}
	}

	// A cover file another tool writes before the job runs is a conflict.
	rec := r.post(save, saveBody{Tracks: everyTrack(d), Cover: &coverEdit{Upload: up.ID}})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("save: %d %s", rec.Code, rec.Body)
	}
	theirs := pngOf(t, 5, 5)
	r.write("folk/Blue/cover.jpg", theirs)
	r.startJobs()
	r.drain()
	c := r.change(decode[changeJSON](t, rec).ID)
	if c.State != StateFailed || c.Reason != ReasonConflict {
		t.Fatalf("%+v", c)
	}
	if b, _ := os.ReadFile(filepath.Join(r.root, "folk/Blue/cover.jpg")); !bytes.Equal(b, theirs) {
		t.Error("their cover.jpg was replaced")
	}
}

// shown is the cover shelf says Navidrome shows for an album, or nil.
func (r *rig) shown(id int64) []byte {
	r.t.Helper()
	rec := r.do(http.MethodGet, "/api/library/albums/"+itoa(id)+"/cover")
	if rec.Code != http.StatusOK {
		return nil
	}
	return rec.Body.Bytes()
}

func exists(t *testing.T, r *rig, rel string) bool {
	t.Helper()
	_, err := os.Stat(filepath.Join(r.root, filepath.FromSlash(rel)))
	return err == nil
}

// An album with no embedded art changes only its cover file, so the audio
// files keep their revisions.
func TestSaveCoverOfTheFolderOnly(t *testing.T) {
	r := newRig(t)
	r.add("folk/Blue/01.flac", "track.flac", blue("1", nil))
	r.add("folk/Blue/02.flac", "track.flac", blue("2", nil))
	old := pngOf(t, 40, 40)
	r.write("folk/Blue/cover.png", old)
	r.walk()
	r.startJobs()
	id := r.albums("").Albums[0].ID
	loaded := r.open(id)
	img := jpegOf(t, 300, 300)

	s := r.saved(id, saveBody{Tracks: everyTrack(loaded), Cover: &coverEdit{Upload: r.upload(img).ID}})
	if s.State != StateDone || s.Tags != 0 || !s.Cover || len(s.Files) != 2 {
		t.Fatalf("save %+v", s)
	}
	if !bytes.Equal(r.shown(id), img) || exists(t, r, "folk/Blue/cover.png") {
		t.Error("the folder's cover didn't change")
	}
	for i, tr := range r.open(id).Tracks {
		if tr.Revision != loaded.Tracks[i].Revision {
			t.Errorf("%s moved", tr.Path)
		}
	}
	if u := r.reversed(KindUndo, s.ID); u.State != StateDone || len(u.Conflicts) != 0 {
		t.Fatalf("undo %+v", u)
	}
	if !bytes.Equal(r.shown(id), old) || exists(t, r, "folk/Blue/cover.jpg") {
		t.Error("the undo didn't put cover.png back")
	}
	if rd := r.reversed(KindRedo, s.ID); rd.State != StateDone || !bytes.Equal(r.shown(id), img) {
		t.Fatalf("redo %+v", rd)
	}
}

// Save cover alone moves the revision of each track with embedded art, so
// edits staged before it need the revisions the change reports. Those are
// the files as shelf left them, so another tool's change still conflicts.
func TestStagedEditsAfterSaveCoverAlone(t *testing.T) {
	r := newRig(t)
	p1 := r.add("folk/Blue/01.flac", "track.flac", blue("1", nil))
	r.add("folk/Blue/02.flac", "track.flac", blue("2", nil))
	old := jpegOf(t, 50, 50)
	embedCover(t, p1, old)
	r.walk()
	r.startJobs()
	id := r.albums("").Albums[0].ID
	loaded := r.open(id)
	save := "/api/library/albums/" + itoa(id) + "/save"
	staged := everyTrack(loaded, editJSON{Key: "MOOD", Values: []string{"calm"}})
	rebased := func(c changeJSON) []trackEdits {
		out := slices.Clone(staged)
		for i := range out {
			for _, f := range c.Files {
				if f.Kind == fileAudio && f.TrackID == out[i].ID && f.Written {
					out[i].Revision = f.Revision
				}
			}
		}
		return out
	}

	c := r.saved(id, saveBody{Tracks: everyTrack(loaded), Cover: &coverEdit{Upload: r.upload(jpegOf(t, 300, 300)).ID}})
	if c.State != StateDone {
		t.Fatalf("cover %+v", c)
	}
	rec := r.post(save, saveBody{Tracks: staged})
	if f := decode[refusal](t, rec); rec.Code != http.StatusConflict || f.Reason != ReasonConflict {
		t.Fatalf("the old revisions: %d %+v", rec.Code, f)
	}
	s := r.saved(id, saveBody{Tracks: rebased(c)})
	if s.State != StateDone || s.Tags != 2 || s.Cover {
		t.Fatalf("staged edits %+v", s)
	}

	var h historyJSON
	r.getJSON("/api/library/history", &h)
	if len(h.Changes) != 2 || h.Changes[1].ID != c.ID || !h.Changes[1].Cover || h.Changes[1].Tags != 0 || h.Changes[0].Cover {
		t.Errorf("history %+v", h.Changes)
	}

	// The cover is its own undo step: the tags written after it stay.
	if u := r.reversed(KindUndo, c.ID); u.State != StateDone || len(u.Conflicts) != 0 {
		t.Fatalf("undo %+v", u)
	}
	if !bytes.Equal(front(t, p1), old) {
		t.Error("the old cover isn't back")
	}
	f, err := tagio.Read(p1)
	if err != nil || !slices.Equal(f.Tags["MOOD"], []string{"calm"}) {
		t.Errorf("the undo took the tags too: %v %v", err, f.Tags)
	}

	c = r.saved(id, saveBody{Tracks: everyTrack(r.open(id)), Cover: &coverEdit{Upload: r.upload(jpegOf(t, 200, 200)).ID}})
	if err := taglib.WriteTags(p1, map[string][]string{"TITLE": {"Picard's"}}, 0); err != nil {
		t.Fatal(err)
	}
	if rec := r.post(save, saveBody{Tracks: rebased(c)}); rec.Code != http.StatusConflict {
		t.Errorf("another tool's change: %d %s", rec.Code, rec.Body)
	}
}

// Navidrome finds an album's cover in its disc folders too, and shows a
// disc's cover as that disc's art. A replace goes to the release folder and
// leaves them, and Delete takes them, or Navidrome falls back to them.
func TestDiscFolderCovers(t *testing.T) {
	r := newRig(t)
	r.add("folk/Blue/CD1/01.flac", "track.flac", blue("1", map[string][]string{"DISCNUMBER": {"1"}}))
	r.add("folk/Blue/CD2/01.flac", "track.flac", blue("1", map[string][]string{"DISCNUMBER": {"2"}}))
	one, two := jpegOf(t, 100, 100), jpegOf(t, 110, 110)
	r.write("folk/Blue/CD1/cover.jpg", one)
	r.write("folk/Blue/CD2/cover.jpg", two)
	r.write("folk/Blue/CD2/folder.jpg", two)
	r.walk()
	r.startJobs()
	id := r.albums("").Albums[0].ID
	if !bytes.Equal(r.shown(id), one) {
		t.Error("shelf doesn't show the first disc's cover")
	}
	if _, ok := issue(r.open(id).Issues, NoCover); ok {
		t.Error("the album has covers")
	}

	img := jpegOf(t, 300, 300)
	s := r.saved(id, saveBody{Tracks: everyTrack(r.open(id)), Cover: &coverEdit{Upload: r.upload(img).ID}})
	if s.State != StateDone || !bytes.Equal(r.shown(id), img) {
		t.Fatalf("replace %+v", s)
	}
	if b, _ := os.ReadFile(filepath.Join(r.root, "folk/Blue/CD1/cover.jpg")); !bytes.Equal(b, one) {
		t.Error("the replace changed a disc's art")
	}

	// A download working in one disc folder keeps a Delete out of the album.
	save := "/api/library/albums/" + itoa(id) + "/save"
	r.m.SetBusy(func(dir string) bool { return filepath.Base(dir) == "CD2" })
	rec := r.post(save, saveBody{Tracks: everyTrack(r.open(id)), Cover: &coverEdit{Delete: true}})
	if f := decode[refusal](t, rec); rec.Code != http.StatusConflict || f.Reason != ReasonBusy {
		t.Fatalf("busy: %d %+v", rec.Code, f)
	}
	r.m.SetBusy(nil)

	d := r.saved(id, saveBody{Tracks: everyTrack(r.open(id)), Cover: &coverEdit{Delete: true}})
	if d.State != StateDone {
		t.Fatalf("delete %+v", d)
	}
	for _, rel := range []string{"folk/Blue/cover.jpg", "folk/Blue/CD1/cover.jpg", "folk/Blue/CD2/cover.jpg"} {
		if exists(t, r, rel) {
			t.Errorf("%s stayed", rel)
		}
	}
	if !exists(t, r, "folk/Blue/CD2/folder.jpg") || !bytes.Equal(r.shown(id), two) {
		t.Error("Navidrome shows folder.jpg once no cover file is left")
	}
	if u := r.reversed(KindUndo, d.ID); u.State != StateDone || len(u.Conflicts) != 0 {
		t.Fatalf("undo %+v", u)
	}
	if b, _ := os.ReadFile(filepath.Join(r.root, "folk/Blue/CD2/cover.jpg")); !bytes.Equal(b, two) {
		t.Error("the undo didn't put the disc's cover back")
	}
	if !bytes.Equal(r.shown(id), img) {
		t.Error("the undo didn't put the album's cover back")
	}
}

// Repair has to clear a damaged disc cover, so a replace takes that one
// file, and only that one.
func TestDamagedDiscCover(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("no ffmpeg")
	}
	r := newRig(t)
	r.add("folk/Blue/CD1/01.flac", "track.flac", blue("1", map[string][]string{"DISCNUMBER": {"1"}}))
	r.add("folk/Blue/CD2/01.flac", "track.flac", blue("1", map[string][]string{"DISCNUMBER": {"2"}}))
	good := jpegOf(t, 100, 100)
	broken := jpegOf(t, 400, 400)
	broken = broken[:len(broken)/2]
	r.write("folk/Blue/CD1/cover.jpg", good)
	r.write("folk/Blue/CD2/cover.jpg", broken)
	r.walk()
	r.startJobs()
	id := r.albums("").Albums[0].ID
	is, ok := issue(r.open(id).Issues, BadCover)
	if !ok || !slices.Equal(is.Fields, []string{"folk/Blue/CD2/cover.jpg"}) {
		t.Fatalf("issue %+v", is)
	}
	rec := r.raw(http.MethodPost, "/api/library/albums/"+itoa(id)+"/cover/repair", nil)
	f := decode[imageInfo](t, rec)
	if rec.Code != http.StatusCreated || f.ID != hashOf(good) {
		t.Fatalf("repair: %d %+v", rec.Code, f)
	}
	s := r.saved(id, saveBody{Tracks: everyTrack(r.open(id)), Cover: &coverEdit{Upload: f.ID}})
	if s.State != StateDone {
		t.Fatalf("save %+v", s)
	}
	if exists(t, r, "folk/Blue/CD2/cover.jpg") || !exists(t, r, "folk/Blue/CD1/cover.jpg") {
		t.Error("the replace should take only the damaged disc cover")
	}
	if _, ok := issue(r.open(id).Issues, BadCover); ok {
		t.Error("the issue stayed after the repair")
	}
	r.reversed(KindUndo, s.ID)
	if b, _ := os.ReadFile(filepath.Join(r.root, "folk/Blue/CD2/cover.jpg")); !bytes.Equal(b, broken) {
		t.Error("the undo didn't put the damaged cover back")
	}
}

// Navidrome shows a folder's cover file for every album in the folder, so
// a cover edit that touches the file asks first. Editing only the embedded
// art reaches no other album.
func TestSaveCoverAsksInASharedFolder(t *testing.T) {
	r := newRig(t)
	named := func(n string) map[string][]string { return blue("1", map[string][]string{"ALBUM": {n}}) }
	p1 := r.add("singles/a.flac", "track.flac", named("One"))
	r.add("singles/b.flac", "track.flac", named("Two"))
	r.add("singles/c.flac", "track.flac", named("Three"))
	r.add("duo/a.flac", "track.flac", named("Four"))
	r.add("duo/b.flac", "track.flac", named("Five"))
	old := jpegOf(t, 50, 50)
	r.write("singles/cover.jpg", old)
	embedCover(t, p1, jpegOf(t, 60, 60))
	r.walk()
	r.startJobs()
	ids := map[string]int64{}
	for _, a := range r.albums("").Albums {
		ids[a.Title] = a.ID
	}
	img := jpegOf(t, 300, 300)
	up := r.upload(img)
	asks := func(name string, ce coverEdit, want string) {
		t.Helper()
		id := ids[name]
		rec := r.post("/api/library/albums/"+itoa(id)+"/save", saveBody{Tracks: everyTrack(r.open(id)), Cover: &ce})
		f := decode[refusal](t, rec)
		if rec.Code != http.StatusConflict || f.Reason != "confirm" || len(f.Confirm) != 1 ||
			f.Confirm[0].Kind != ConfirmSharedCover || !strings.Contains(f.Confirm[0].Message, want) {
			t.Fatalf("%s: %d %+v", name, rec.Code, f)
		}
	}

	asks("One", coverEdit{Upload: up.ID}, "2 other albums")
	if b, _ := os.ReadFile(filepath.Join(r.root, "singles/cover.jpg")); !bytes.Equal(b, old) {
		t.Error("the Save ran before the owner confirmed")
	}
	// A folder with no cover file yet gets one that every album there shows.
	asks("Four", coverEdit{Upload: up.ID}, "Another album")

	s := r.saved(ids["One"], saveBody{Tracks: everyTrack(r.open(ids["One"])), Confirm: []string{ConfirmSharedCover},
		Cover: &coverEdit{Upload: up.ID}})
	if s.State != StateDone || !bytes.Equal(r.shown(ids["Two"]), img) {
		t.Fatalf("confirmed %+v", s)
	}
	asks("Two", coverEdit{Delete: true}, "2 other albums")

	// An undo puts back what the owner confirmed, so it asks nothing.
	if u := r.reversed(KindUndo, s.ID); u.State != StateDone || !bytes.Equal(r.shown(ids["Two"]), old) {
		t.Fatalf("undo %+v", u)
	}

	// The folder already holds this image, so only the embedded art changes.
	s = r.saved(ids["One"], saveBody{Tracks: everyTrack(r.open(ids["One"])), Cover: &coverEdit{Upload: r.upload(old).ID}})
	if s.State != StateDone || !bytes.Equal(front(t, p1), old) {
		t.Fatalf("embedded only %+v", s)
	}
}

func TestBadCoverAndRepair(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("no ffmpeg")
	}
	r := newRig(t)
	p1 := r.add("folk/Blue/01.flac", "track.flac", blue("1", nil))
	good := jpegOf(t, 100, 100)
	embedCover(t, p1, good)
	broken := jpegOf(t, 400, 400)
	broken = broken[:len(broken)/2]
	r.write("folk/Blue/cover.jpg", broken)
	r.walk()
	id := r.albums("").Albums[0].ID
	d := r.open(id)
	is, ok := issue(d.Issues, BadCover)
	if !ok || !slices.Equal(is.Fields, []string{"folk/Blue/cover.jpg"}) || !d.Lamp {
		t.Fatalf("issues %+v", d.Issues)
	}

	// The track's good cover is the repair.
	rec := r.raw(http.MethodPost, "/api/library/albums/"+itoa(id)+"/cover/repair", nil)
	if f := decode[imageInfo](t, rec); rec.Code != http.StatusCreated || f.ID != hashOf(good) {
		t.Fatalf("repair: %d %+v", rec.Code, f)
	}
	// With no good cover, ffmpeg writes what it can decode again.
	embedCover(t, p1, broken)
	rec = r.raw(http.MethodPost, "/api/library/albums/"+itoa(id)+"/cover/repair", nil)
	f := decode[imageInfo](t, rec)
	if rec.Code != http.StatusCreated || f.MIME != "image/jpeg" || f.Width != 400 {
		t.Fatalf("re-encoded: %d %+v", rec.Code, f)
	}
	b, err := r.m.covers.get(f.ID)
	if err != nil || r.m.judge(bg, b) != nil {
		t.Fatalf("the repair is damaged too: %v", err)
	}
	r.write("folk/Blue/cover.jpg", good)
	if err := taglib.WriteImageOptions(p1, nil, 0, "", "", ""); err != nil {
		t.Fatal(err)
	}
	if rec := r.raw(http.MethodPost, "/api/library/albums/"+itoa(id)+"/cover/repair", nil); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("nothing to repair: %d", rec.Code)
	}
	if _, ok := issue(r.open(id).Issues, BadCover); ok {
		t.Error("a good cover is bad")
	}
}

func TestInterruptedCoverIsSettled(t *testing.T) {
	r := newRig(t)
	img := jpegOf(t, 10, 10)
	r.write("folk/Blue/cover.jpg", img)
	id := r.writing()
	err := r.m.store.db.Write(bg, func(tx *sqlx.Tx) error {
		for _, row := range [][3]string{{"folk/Blue/cover.jpg", "", hashOf(img)}, {"folk/Blue/cover.png", "abc", ""}} {
			if _, err := tx.Exec(`INSERT INTO library_change_files (change_id, path, kind, before, after, cover_before, cover_after)
				VALUES (?, ?, 'cover', '{}', '{}', ?, ?)`, id, row[0], row[1], row[2]); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.m.reconcileAll(bg); err != nil {
		t.Fatal(err)
	}
	if c := r.change(id); c.State != StateDone || !c.Files[0].Written || !c.Files[1].Written {
		t.Fatalf("%+v", c)
	}
}

func TestCoverStoreSweep(t *testing.T) {
	r := newRig(t)
	used, young, old := r.upload(jpegOf(t, 1, 1)), r.upload(jpegOf(t, 2, 2)), r.upload(jpegOf(t, 3, 3))
	for _, id := range []string{used.ID, old.ID} {
		p := filepath.Join(r.m.covers.dir, id+".jpg")
		if err := os.Chtimes(p, time.Now(), time.Now().Add(-48*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	id := r.writing(changeFile{Path: "a.flac"})
	if _, err := r.m.store.db.W.Exec(`UPDATE library_change_files SET cover_before = ? WHERE change_id = ?`, used.ID, id); err != nil {
		t.Fatal(err)
	}
	ids, err := r.m.store.coverIDs(bg)
	if err != nil || !ids[used.ID] {
		t.Fatalf("used %v %v", ids, err)
	}
	if n, err := r.m.covers.sweep(ids, time.Now()); err != nil || n != 1 {
		t.Fatalf("swept %d: %v", n, err)
	}
	for _, c := range []struct {
		id   string
		kept bool
	}{{used.ID, true}, {young.ID, true}, {old.ID, false}} {
		if _, err := r.m.covers.get(c.id); (err == nil) != c.kept {
			t.Errorf("%s: %v", c.id[:8], err)
		}
	}
}
