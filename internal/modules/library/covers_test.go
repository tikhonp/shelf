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
	if s.State != StateDone || s.Tags != 3 {
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
