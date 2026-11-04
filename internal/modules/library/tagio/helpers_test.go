package tagio

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"go.senan.xyz/taglib"
)

// fixtures are the untagged files testdata/make.sh writes, one per format.
var fixtures = map[string]string{
	"flac": "track.flac",
	"mp3":  "track.mp3",
	"m4a":  "track.m4a",
	"alac": "alac.m4a",
	"ogg":  "track.ogg",
	"opus": "track.opus",
	"wav":  "track.wav",
	"aiff": "track.aiff",
	"wma":  "track.wma",
}

// fixture copies a fixture into dir under name.
func fixture(t *testing.T, dir, fixture, name string) string {
	t.Helper()
	in, err := os.Open(filepath.Join("testdata", fixture))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = in.Close() }()
	path := filepath.Join(dir, name)
	out, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(out, in); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

// seed tags a file directly through go-taglib, as Picard or a source would
// have left it.
func seed(t *testing.T, path string, tags map[string][]string, pics ...[]byte) {
	t.Helper()
	if len(tags) > 0 {
		if err := taglib.WriteTags(path, tags, 0); err != nil {
			t.Fatal(err)
		}
	}
	types := []string{"Front Cover", "Back Cover"}
	for i, p := range pics {
		if err := taglib.WriteImageOptions(path, p, i, types[i%2], "seeded", mimeOf(p)); err != nil {
			t.Fatal(err)
		}
	}
}

func mimeOf(b []byte) string {
	if bytes.HasPrefix(b, []byte("\x89PNG")) {
		return "image/png"
	}
	return "image/jpeg"
}

func pngOf(t *testing.T, c color.Color) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for x := range 4 {
		for y := range 4 {
			img.Set(x, y, c)
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func jpegOf(t *testing.T, c color.Color) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for x := range 8 {
		for y := range 8 {
			img.Set(x, y, c)
		}
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// needFFmpeg skips a test that checks audio through ffmpeg where it isn't
// installed. The image and the dev machines have it.
func needFFmpeg(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg is not installed")
	}
}

func mustRead(t *testing.T, path string) File {
	t.Helper()
	f, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func mustBytes(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// noTemps fails when a temp file is left in dir.
func noTemps(t *testing.T, dir string) {
	t.Helper()
	es, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range es {
		if IsTemp(e.Name()) {
			t.Errorf("temp file %s left behind", e.Name())
		}
	}
}

// recorder is a Journal that keeps what it was told.
type recorder struct {
	begun  []Result
	ended  []Result
	errs   []error
	failAt string
}

func (r *recorder) Begin(_ context.Context, res Result) (int64, error) {
	r.begun = append(r.begun, res)
	if r.failAt == "begin" {
		return 0, errTest
	}
	return int64(len(r.begun)), nil
}

func (r *recorder) End(_ context.Context, _ int64, res Result, err error) error {
	r.ended = append(r.ended, res)
	r.errs = append(r.errs, err)
	return nil
}

var errTest = errors.New("injected")
