package tagio

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"go.senan.xyz/taglib"
)

func change(files ...FileChange) Change { return Change{Files: files} }

// edit loads a file's revision and builds its change, as the editor would.
func edit(t *testing.T, path string, edits ...Edit) FileChange {
	t.Helper()
	return FileChange{Path: path, Revision: mustRead(t, path).Revision(), Edits: edits}
}

func set(field string, vs ...string) Edit { return Edit{Field: field, Values: vs} }

func writer(t *testing.T) (*Writer, *recorder) {
	t.Helper()
	w := NewWriter()
	j := &recorder{}
	w.Journal = j
	return w, j
}

// A write keeps every tag and picture it wasn't asked to change, keeps the
// order of multi-value tags, and leaves the audio alone, in every format.
func TestWriteEveryFormat(t *testing.T) {
	for name, fx := range fixtures {
		t.Run(name, func(t *testing.T) {
			if name != "flac" {
				needFFmpeg(t)
			}
			dir := t.TempDir()
			p := fixture(t, dir, fx, "01 Track"+filepath.Ext(fx))
			front, back := pngOf(t, color.White), jpegOf(t, color.Black)
			tags := map[string][]string{
				"TITLE": {"Old"}, "ARTIST": {"A"}, "ALBUM": {"Al"}, "ALBUMARTIST": {"AA"}, "TRACKNUMBER": {"1"},
			}
			wma := name == "wma"
			if !wma {
				tags["ARTISTS"] = []string{"Zed", "Abe"}
				tags["ENSEMBLE"] = []string{"kept"}
			}
			pics := [][]byte{front, back}
			if wma {
				// TagLib reorders WMA pictures on save; TestWriteWMALimits
				// shows the read-back refusing that.
				pics = pics[:1]
			}
			seed(t, p, tags, pics...)
			if err := os.Chmod(p, 0o640); err != nil {
				t.Fatal(err)
			}
			before := mustRead(t, p)
			audio := audioHash(t, p)

			edits := []Edit{set("title", "New"), set("musicbrainz_albumid", "f1d5d5a2-0000-4000-8000-000000000001")}
			if !wma {
				edits = append(edits, set("genre", "Rock", "Jazz"), Edit{Key: "MYFIELD", Values: []string{"m"}})
			}
			w, j := writer(t)
			res, err := w.Write(context.Background(), change(FileChange{Path: p, Revision: before.Revision(), Edits: edits}))
			if err != nil {
				t.Fatal(err)
			}

			after := mustRead(t, p)
			want := apply(before.Tags, map[string][]string{"TITLE": {"New"}, "MUSICBRAINZ_ALBUMID": {"f1d5d5a2-0000-4000-8000-000000000001"}})
			if !wma {
				want["GENRE"] = []string{"Rock", "Jazz"}
				want["MYFIELD"] = []string{"m"}
			}
			if k, ok := diffTags(want, after.Tags); !ok {
				t.Errorf("%s = %q, want %q", k, after.Tags[k], want[k])
			}
			if !slices.Equal(after.Pictures, before.Pictures) {
				t.Errorf("pictures %+v, want %+v", after.Pictures, before.Pictures)
			}
			for i, b := range pics {
				got, err := readImage(p, i)
				if err != nil || !bytes.Equal(got, b) {
					t.Errorf("picture %d changed: %v", i, err)
				}
			}
			if audioHash(t, p) != audio {
				t.Error("the audio changed")
			}
			st, _ := os.Stat(p)
			if st.Mode().Perm() != 0o640 {
				t.Errorf("mode %v, want 0640", st.Mode().Perm())
			}
			if len(res.Files) != 1 || !res.Files[0].Written || res.Files[0].Revision != after.Revision() {
				t.Errorf("result %+v, revision now %s", res.Files, after.Revision())
			}
			if len(j.begun) != 1 || len(j.ended) != 1 || j.errs[0] != nil || j.begun[0].Files[0].Written {
				t.Errorf("journal begun %d ended %d errs %v", len(j.begun), len(j.ended), j.errs)
			}
			noTemps(t, dir)
		})
	}
}

func audioHash(t *testing.T, path string) string {
	t.Helper()
	var h string
	var err error
	if FormatOf(path).flac {
		h, err = flacHash(path)
	} else {
		h, err = ffmpegHash(context.Background(), "ffmpeg", path)
	}
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func flacFile(t *testing.T, dir, name string, tags map[string][]string) string {
	t.Helper()
	p := fixture(t, dir, "track.flac", name)
	seed(t, p, tags)
	return p
}

func TestWriteUpdatesEveryAlias(t *testing.T) {
	dir := t.TempDir()
	p := flacFile(t, dir, "a.flac", map[string][]string{
		"ALBUMARTIST": {"Old"}, "ALBUM ARTIST": {"Stale"}, "DISC": {"1"}, "DISCNUMBER": {"1/2"}, "TITLE": {"t"},
	})
	w, _ := writer(t)
	if _, err := w.Write(context.Background(), change(edit(t, p, set("albumartist", "New"), set("disc", "2")))); err != nil {
		t.Fatal(err)
	}
	got := mustRead(t, p).Tags
	for k, v := range map[string]string{"ALBUMARTIST": "New", "ALBUM ARTIST": "New", "DISC": "2", "DISCNUMBER": "2", "TITLE": "t"} {
		if !slices.Equal(got[k], []string{v}) {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
}

func TestWriteAddsAndRemoves(t *testing.T) {
	dir := t.TempDir()
	p := flacFile(t, dir, "a.flac", map[string][]string{"COMMENT": {"c"}, "DESCRIPTION": {"d"}, "TITLE": {"t"}, "OLD": {"o"}})
	w, _ := writer(t)
	_, err := w.Write(context.Background(), change(edit(t, p,
		set("comment"), // both aliases go
		set("musicbrainz_trackid", "a-release-track"), // the trap: Navidrome's track ID
		set("releasedate", "2006-01-02"),
		Edit{Key: "old"}, // a raw key in another case
	)))
	if err != nil {
		t.Fatal(err)
	}
	got := mustRead(t, p).Tags
	want := map[string][]string{"TITLE": {"t"}, "MUSICBRAINZ_RELEASETRACKID": {"a-release-track"}, "RELEASEDATE": {"2006-01-02"}}
	if k, ok := diffTags(want, got); !ok {
		t.Errorf("%s = %q, want %q", k, got[k], want[k])
	}
}

func TestWriteNothingToChange(t *testing.T) {
	dir := t.TempDir()
	p := flacFile(t, dir, "a.flac", map[string][]string{"TITLE": {"t"}})
	before := mustBytes(t, p)
	st0, _ := os.Stat(p)
	w, j := writer(t)
	res, err := w.Write(context.Background(), change(edit(t, p, set("title", "t"), set("comment"), Edit{Key: "NOPE"})))
	if err != nil || len(res.Files) != 0 || len(j.begun) != 0 {
		t.Fatalf("a change to nothing: %v, %+v, journal %d", err, res, len(j.begun))
	}
	st1, _ := os.Stat(p)
	if !bytes.Equal(before, mustBytes(t, p)) || !st1.ModTime().Equal(st0.ModTime()) {
		t.Error("a change to nothing rewrote the file")
	}

	fc := edit(t, p, set("title", "x"))
	if _, err := w.Write(context.Background(), change(fc, fc)); !errors.Is(err, ErrRefused) {
		t.Errorf("a file listed twice: %v", err)
	}
}

func TestWriteRefuses(t *testing.T) {
	dir := t.TempDir()
	p := flacFile(t, dir, "a.flac", map[string][]string{"TITLE": {"t"}})
	ctx := context.Background()
	cases := map[string]struct {
		fc   FileChange
		want error
	}{
		"stale revision":  {FileChange{Path: p, Revision: "1-2-3", Edits: []Edit{set("title", "x")}}, ErrConflict},
		"no revision":     {FileChange{Path: p, Edits: []Edit{set("title", "x")}}, ErrConflict},
		"relative path":   {FileChange{Path: "a.flac", Edits: []Edit{set("title", "x")}}, ErrRefused},
		"not audio":       {FileChange{Path: filepath.Join(dir, "cover.mp4")}, ErrRefused},
		"unknown field":   {edit(t, p, set("nofield", "x")), ErrRefused},
		"field and key":   {edit(t, p, Edit{Field: "title", Key: "TITLE", Values: []string{"x"}}), ErrRefused},
		"no field or key": {edit(t, p, Edit{Values: []string{"x"}}), ErrRefused},
		"empty value":     {edit(t, p, set("title", "")), ErrRefused},
		"separator":       {edit(t, p, set("title", "a\vb")), ErrRefused},
		"bad key":         {edit(t, p, Edit{Key: "A=B", Values: []string{"x"}}), ErrRefused},
		"twice":           {edit(t, p, set("title", "x"), Edit{Key: "TITLE", Values: []string{"y"}}), ErrRefused},
		"cover not image": {FileChange{Path: p, Revision: mustRead(t, p).Revision(), Cover: &Cover{Data: []byte("GIF89a....")}}, ErrRefused},
		"missing":         {FileChange{Path: filepath.Join(dir, "gone.flac"), Revision: "x"}, os.ErrNotExist},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			w, j := writer(t)
			_, err := w.Write(ctx, change(c.fc))
			var fe *FileError
			if !errors.Is(err, c.want) || !errors.As(err, &fe) {
				t.Errorf("got %v, want %v in a FileError", err, c.want)
			}
			if len(j.begun) != 0 {
				t.Error("a refused write reached the journal")
			}
		})
	}
	if got := mustRead(t, p).Tags["TITLE"]; !slices.Equal(got, []string{"t"}) {
		t.Errorf("a refused write changed the file: %q", got)
	}
	noTemps(t, dir)
}

func TestWriteLockedAndBusy(t *testing.T) {
	dir := t.TempDir()
	p := flacFile(t, dir, "a.flac", map[string][]string{"TITLE": {"t"}})
	ctx := context.Background()

	w, _ := writer(t)
	w.UID = os.Getuid() + 1
	if _, err := w.Write(ctx, change(edit(t, p, set("title", "x")))); !errors.Is(err, ErrLocked) {
		t.Errorf("another owner's file: %v", err)
	}

	w, _ = writer(t)
	var asked []string
	w.Busy = func(d string) bool { asked = append(asked, d); return true }
	if _, err := w.Write(ctx, change(edit(t, p, set("title", "x")))); !errors.Is(err, ErrBusy) || !slices.Equal(asked, []string{dir}) {
		t.Errorf("a busy folder: %v, asked %v", err, asked)
	}

	link := filepath.Join(dir, "b.flac")
	if err := os.Link(p, link); err != nil {
		t.Fatal(err)
	}
	w, _ = writer(t)
	if _, err := w.Write(ctx, change(edit(t, p, set("title", "x")))); !errors.Is(err, ErrRefused) {
		t.Errorf("a hard link: %v", err)
	}
	_ = os.Remove(link)

	sym := filepath.Join(dir, "c.flac")
	if err := os.Symlink(p, sym); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(ctx, change(FileChange{Path: sym, Revision: mustRead(t, p).Revision(), Edits: []Edit{set("title", "x")}})); !errors.Is(err, ErrRefused) {
		t.Errorf("a symlink: %v", err)
	}

	if os.Getuid() != 0 {
		ro := t.TempDir()
		q := flacFile(t, ro, "a.flac", nil)
		fc := edit(t, q, set("title", "x"))
		if err := os.Chmod(ro, 0o555); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(ro, 0o755) })
		if _, err := w.Write(ctx, change(fc)); !errors.Is(err, ErrLocked) {
			t.Errorf("a read-only folder: %v", err)
		}
	}
	if got := mustRead(t, p).Tags["TITLE"]; !slices.Equal(got, []string{"t"}) {
		t.Errorf("the file changed: %q", got)
	}
}

func TestWriteWMALimits(t *testing.T) {
	dir := t.TempDir()
	p := fixture(t, dir, "track.wma", "a.wma")
	ctx := context.Background()
	w, _ := writer(t)
	if _, err := w.Write(ctx, change(edit(t, p, set("artists", "A", "B")))); !errors.Is(err, ErrRefused) {
		t.Errorf("multi-value: %v", err)
	}
	if _, err := w.Write(ctx, change(edit(t, p, Edit{Key: "MYFIELD", Values: []string{"x"}}))); !errors.Is(err, ErrRefused) {
		t.Errorf("custom field: %v", err)
	}
	// TagLib drops COMPILATION from WMA without an error. The read-back
	// sees it.
	if _, err := w.Write(ctx, change(edit(t, p, set("compilation", "1")))); !errors.Is(err, ErrVerify) {
		t.Errorf("a dropped key: %v", err)
	}

	two := fixture(t, dir, "track.wma", "b.wma")
	seed(t, two, nil, pngOf(t, color.White), jpegOf(t, color.Black))
	orig := mustBytes(t, two)
	if _, err := w.Write(ctx, change(edit(t, two, set("title", "x")))); !errors.Is(err, ErrVerify) {
		t.Errorf("two pictures, reordered on save: %v", err)
	}
	if !bytes.Equal(orig, mustBytes(t, two)) {
		t.Error("the file changed")
	}
	noTemps(t, dir)
}

func TestResolveM4AReadOnly(t *testing.T) {
	m4a := FormatOf("x.m4a")
	f := File{Format: m4a, Tags: map[string][]string{
		"----:com.apple.iTunes:originaldate": {"1958"}, "DATE": {"1984"},
	}}
	if _, err := resolve(f, []Edit{set("originaldate", "1958-01-01")}); !errors.Is(err, ErrRefused) {
		t.Errorf("Picard's lowercase atom: %v", err)
	}
	if _, err := resolve(f, []Edit{{Key: "----:com.apple.iTunes:originaldate"}}); !errors.Is(err, ErrRefused) {
		t.Errorf("removing it: %v", err)
	}
	// M4A's DATE feeds both dates, so two date edits collide on it.
	if _, err := resolve(f, []Edit{set("releasedate", "2001"), set("recordingdate", "2002")}); !errors.Is(err, ErrRefused) {
		t.Errorf("two edits of DATE: %v", err)
	}
	ops, err := resolve(f, []Edit{set("releasedate", "2001")})
	if err != nil || !slices.Equal(ops["DATE"], []string{"2001"}) || len(ops) != 1 {
		t.Errorf("release date on M4A: %v %v", ops, err)
	}
}

func TestWriteCovers(t *testing.T) {
	ctx := context.Background()
	red, blue := pngOf(t, color.RGBA{R: 255, A: 255}), jpegOf(t, color.RGBA{B: 255, A: 255})
	back := jpegOf(t, color.Black)
	for _, fx := range []string{"track.flac", "track.m4a"} {
		t.Run(fx, func(t *testing.T) {
			if fx != "track.flac" {
				needFFmpeg(t)
			}
			dir := t.TempDir()
			w, _ := writer(t)
			p := fixture(t, dir, fx, "a"+filepath.Ext(fx))

			// Add to a file with none, then replace.
			for _, img := range [][]byte{red, blue} {
				fc := edit(t, p)
				fc.Cover = &Cover{Data: img}
				if _, err := w.Write(ctx, change(fc)); err != nil {
					t.Fatal(err)
				}
				f := mustRead(t, p)
				got, _ := readImage(p, 0)
				if len(f.Pictures) != 1 || !bytes.Equal(got, img) || f.Pictures[0].MIME != mimeOf(img) {
					t.Fatalf("pictures %+v", f.Pictures)
				}
			}

			// Removing it leaves none; removing again changes nothing.
			for range 2 {
				fc := edit(t, p)
				fc.Cover = &Cover{}
				if _, err := w.Write(ctx, change(fc)); err != nil {
					t.Fatal(err)
				}
				if n := len(mustRead(t, p).Pictures); n != 0 {
					t.Fatalf("%d pictures after removal", n)
				}
			}
		})
	}

	// A FLAC with a back cover first keeps it when the front changes.
	dir := t.TempDir()
	w, _ := writer(t)
	p := fixture(t, dir, "track.flac", "b.flac")
	seedType(t, p, 0, back, "Back Cover")
	seedType(t, p, 1, red, "Front Cover")
	fc := edit(t, p)
	fc.Cover = &Cover{Data: blue}
	if _, err := w.Write(ctx, change(fc)); err != nil {
		t.Fatal(err)
	}
	f := mustRead(t, p)
	b0, _ := readImage(p, 0)
	b1, _ := readImage(p, 1)
	if len(f.Pictures) != 2 || f.Pictures[0].Type != "Back Cover" || !bytes.Equal(b0, back) || !bytes.Equal(b1, blue) {
		t.Errorf("pictures %+v", f.Pictures)
	}
}

func seedType(t *testing.T, path string, i int, img []byte, typ string) {
	t.Helper()
	if err := taglib.WriteImageOptions(path, img, i, typ, "", mimeOf(img)); err != nil {
		t.Fatal(err)
	}
}

// album makes three tagged FLAC files in one folder and their change.
func album(t *testing.T) (string, []string, Change) {
	t.Helper()
	dir := t.TempDir()
	var paths []string
	var ch Change
	for i := range 3 {
		p := flacFile(t, dir, fmt.Sprintf("%02d.flac", i+1), map[string][]string{"TITLE": {fmt.Sprint("t", i)}, "ALBUM": {"Old"}})
		paths = append(paths, p)
		ch.Files = append(ch.Files, edit(t, p, set("album", "New")))
	}
	return dir, paths, ch
}

func snapshot(t *testing.T, paths []string) [][]byte {
	var out [][]byte
	for _, p := range paths {
		out = append(out, mustBytes(t, p))
	}
	return out
}

// A failure at any stage before the renames leaves every original as it
// was and no temp file behind.
func TestWriteFailsAtEachStage(t *testing.T) {
	for _, stage := range []string{stageCopy, stageTags, stageReadBack, stageAudio, stageSync, stageRestat} {
		t.Run(stage, func(t *testing.T) {
			dir, paths, ch := album(t)
			orig := snapshot(t, paths)
			w, j := writer(t)
			n := 0
			w.fault = func(s, _ string) error {
				if s != stage {
					return nil
				}
				if n++; n == 2 {
					return errTest
				}
				return nil
			}
			res, err := w.Write(context.Background(), ch)
			if !errors.Is(err, errTest) {
				t.Fatalf("got %v", err)
			}
			for i, b := range snapshot(t, paths) {
				if !bytes.Equal(b, orig[i]) {
					t.Errorf("file %d changed", i+1)
				}
				if res.Files[i].Written {
					t.Errorf("file %d marked written", i+1)
				}
			}
			if len(j.ended) != 1 || !errors.Is(j.errs[0], errTest) {
				t.Errorf("journal ended %d with %v", len(j.ended), j.errs)
			}
			noTemps(t, dir)
		})
	}
}

func TestWriteFailsAtCoverStage(t *testing.T) {
	dir := t.TempDir()
	p := flacFile(t, dir, "a.flac", nil)
	orig := mustBytes(t, p)
	w, _ := writer(t)
	w.fault = func(s, _ string) error {
		if s == stageCover {
			return errTest
		}
		return nil
	}
	fc := edit(t, p)
	fc.Cover = &Cover{Data: pngOf(t, color.White)}
	if _, err := w.Write(context.Background(), change(fc)); !errors.Is(err, errTest) {
		t.Fatalf("got %v", err)
	}
	if !bytes.Equal(orig, mustBytes(t, p)) {
		t.Error("the file changed")
	}
	noTemps(t, dir)
}

// The checks can fail: a copy damaged behind TagLib's back is caught.
func TestWriteCatchesDamage(t *testing.T) {
	damage := map[string]func(t *testing.T, tmp string){
		"an extra tag": func(t *testing.T, tmp string) {
			seed(t, tmp, map[string][]string{"SNEAKY": {"x"}})
		},
		"a lost tag": func(t *testing.T, tmp string) {
			seed(t, tmp, map[string][]string{"ALBUM": nil})
		},
		"a lost picture": func(t *testing.T, tmp string) {
			if err := writeImage(tmp, nil, 0, ""); err != nil {
				t.Fatal(err)
			}
		},
		"other audio": func(t *testing.T, tmp string) {
			b := mustBytes(t, tmp)
			b[len(b)-100] ^= 0xff
			writeFile(t, tmp, b)
		},
	}
	for name, fn := range damage {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			p := flacFile(t, dir, "a.flac", map[string][]string{"ALBUM": {"a"}})
			seed(t, p, nil, pngOf(t, color.White))
			orig := mustBytes(t, p)
			w, _ := writer(t)
			w.fault = func(s, tmp string) error {
				if s == stageReadBack {
					fn(t, tmp)
				}
				return nil
			}
			if _, err := w.Write(context.Background(), change(edit(t, p, set("title", "x")))); !errors.Is(err, ErrVerify) {
				t.Fatalf("got %v", err)
			}
			if !bytes.Equal(orig, mustBytes(t, p)) {
				t.Error("the file changed")
			}
			noTemps(t, dir)
		})
	}

	t.Run("mp3 audio", func(t *testing.T) {
		needFFmpeg(t)
		dir := t.TempDir()
		p := fixture(t, dir, "track.mp3", "a.mp3")
		w, _ := writer(t)
		w.fault = func(s, tmp string) error {
			if s == stageAudio {
				// Clear of the ID3v1 tag TagLib appends to MP3.
				b := mustBytes(t, tmp)
				b[len(b)-600] ^= 0xff
				writeFile(t, tmp, b)
			}
			return nil
		}
		if _, err := w.Write(context.Background(), change(edit(t, p, set("title", "x")))); !errors.Is(err, ErrVerify) {
			t.Fatalf("got %v", err)
		}
	})
}

// A file another tool changes between the checks and the renames stops the
// write.
func TestWriteRestatSeesAChange(t *testing.T) {
	dir, paths, ch := album(t)
	w, _ := writer(t)
	w.fault = func(s, path string) error {
		if s == stageRestat && path == paths[2] {
			later := time.Now().Add(time.Minute)
			return os.Chtimes(path, later, later)
		}
		return nil
	}
	orig := snapshot(t, paths)
	if _, err := w.Write(context.Background(), ch); !errors.Is(err, ErrConflict) {
		t.Fatalf("got %v", err)
	}
	for i, b := range snapshot(t, paths) {
		if !bytes.Equal(b, orig[i]) {
			t.Errorf("file %d changed", i+1)
		}
	}
	noTemps(t, dir)

	// So does a download that started in the folder.
	_, paths, ch = album(t)
	w, _ = writer(t)
	calls := 0
	w.Busy = func(string) bool { calls++; return calls > len(paths) }
	if _, err := w.Write(context.Background(), ch); !errors.Is(err, ErrBusy) {
		t.Fatalf("a download that started: %v", err)
	}
}

func TestWriteRenameFails(t *testing.T) {
	t.Run("first", func(t *testing.T) {
		dir, paths, ch := album(t)
		orig := snapshot(t, paths)
		w, _ := writer(t)
		w.fault = func(s, path string) error {
			if s == stageRename {
				return errTest
			}
			return nil
		}
		_, err := w.Write(context.Background(), ch)
		if !errors.Is(err, errTest) || errors.Is(err, ErrPartial) {
			t.Fatalf("got %v", err)
		}
		for i, b := range snapshot(t, paths) {
			if !bytes.Equal(b, orig[i]) {
				t.Errorf("file %d changed", i+1)
			}
		}
		noTemps(t, dir)
	})

	// A rename that fails halfway leaves each file whole, old or new, and
	// says which.
	t.Run("second", func(t *testing.T) {
		dir, paths, ch := album(t)
		orig := snapshot(t, paths)
		w, j := writer(t)
		w.fault = func(s, path string) error {
			if s == stageRename && path == paths[1] {
				return errTest
			}
			return nil
		}
		res, err := w.Write(context.Background(), ch)
		if !errors.Is(err, ErrPartial) || !errors.Is(err, errTest) {
			t.Fatalf("got %v", err)
		}
		written := []bool{res.Files[0].Written, res.Files[1].Written, res.Files[2].Written}
		if !slices.Equal(written, []bool{true, false, false}) {
			t.Errorf("written %v", written)
		}
		if got := mustRead(t, paths[0]).Tags["ALBUM"]; !slices.Equal(got, []string{"New"}) {
			t.Errorf("file 1 album %q", got)
		}
		now := snapshot(t, paths)
		for i := 1; i < 3; i++ {
			if !bytes.Equal(now[i], orig[i]) {
				t.Errorf("file %d changed", i+1)
			}
		}
		if len(j.ended) != 1 || !j.ended[0].Files[0].Written {
			t.Error("the journal didn't hear which file was written")
		}
		noTemps(t, dir)
	})
}

func TestWriteDirSyncFails(t *testing.T) {
	dir, paths, ch := album(t)
	w, _ := writer(t)
	w.fault = func(s, _ string) error {
		if s == stageDirSync {
			return errTest
		}
		return nil
	}
	res, err := w.Write(context.Background(), ch)
	if !errors.Is(err, errTest) {
		t.Fatalf("got %v", err)
	}
	for i, p := range paths {
		if !res.Files[i].Written || !slices.Equal(mustRead(t, p).Tags["ALBUM"], []string{"New"}) {
			t.Errorf("file %d not written", i+1)
		}
	}
	noTemps(t, dir)
}

func TestWriteCanceled(t *testing.T) {
	dir, paths, ch := album(t)
	orig := snapshot(t, paths)
	ctx, cancel := context.WithCancel(context.Background())
	w, _ := writer(t)
	w.fault = func(s, _ string) error {
		if s == stageSync {
			cancel()
		}
		return nil
	}
	if _, err := w.Write(ctx, ch); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
	for i, b := range snapshot(t, paths) {
		if !bytes.Equal(b, orig[i]) {
			t.Errorf("file %d changed", i+1)
		}
	}
	noTemps(t, dir)
}

func TestWriteJournalBeginFails(t *testing.T) {
	dir, paths, ch := album(t)
	orig := snapshot(t, paths)
	w, j := writer(t)
	j.failAt = "begin"
	if _, err := w.Write(context.Background(), ch); !errors.Is(err, errTest) {
		t.Fatalf("got %v", err)
	}
	for i, b := range snapshot(t, paths) {
		if !bytes.Equal(b, orig[i]) {
			t.Errorf("file %d changed", i+1)
		}
	}
	noTemps(t, dir)
}

func TestWriteResultFeedsTheNextWrite(t *testing.T) {
	dir := t.TempDir()
	p := flacFile(t, dir, "a.flac", map[string][]string{"TITLE": {"t"}})
	w, j := writer(t)
	res, err := w.Write(context.Background(), change(edit(t, p, set("title", "u"))))
	if err != nil {
		t.Fatal(err)
	}
	b := j.begun[0].Files[0]
	if !slices.Equal(b.Before["TITLE"], []string{"t"}) || !slices.Equal(b.After["TITLE"], []string{"u"}) {
		t.Errorf("journal begin %+v", b)
	}
	// The returned revision is the file's, so an undo can run on it.
	next := FileChange{Path: p, Revision: res.Files[0].Revision, Edits: []Edit{set("title", "t")}}
	if _, err := w.Write(context.Background(), change(next)); err != nil {
		t.Fatal(err)
	}
	if got := mustRead(t, p).Tags["TITLE"]; !slices.Equal(got, []string{"t"}) {
		t.Errorf("title %q", got)
	}
}
