package tagio

import (
	"errors"
	"image/color"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestReadEveryFormat(t *testing.T) {
	want := map[string]struct {
		format, codec string
		rate          uint
	}{
		"flac": {"flac", "", 44100}, "mp3": {"mp3", "", 44100}, "m4a": {"m4a", "aac", 44100},
		"alac": {"m4a", "alac", 44100}, "ogg": {"ogg", "vorbis", 44100}, "opus": {"opus", "opus", 48000},
		"wav": {"wav", "pcm", 44100}, "aiff": {"aiff", "pcm", 44100}, "wma": {"wma", "wma2", 44100},
	}
	for name, fx := range fixtures {
		t.Run(name, func(t *testing.T) {
			f := mustRead(t, filepath.Join("testdata", fx))
			w := want[name]
			if f.Format.Name != w.format || f.Audio.Codec != w.codec || f.Audio.SampleRate != w.rate || f.Audio.Channels != 2 {
				t.Errorf("read %s %+v, want %+v", f.Format.Name, f.Audio, w)
			}
			if f.Audio.Length < 150*time.Millisecond || f.Audio.Length > 600*time.Millisecond {
				t.Errorf("length %v", f.Audio.Length)
			}
			if f.Revision() != mustRead(t, f.Path).Revision() {
				t.Error("revision differs between two reads")
			}
		})
	}
}

func TestRevisionMoves(t *testing.T) {
	dir := t.TempDir()
	p := fixture(t, dir, "track.flac", "a.flac")
	r0 := mustRead(t, p).Revision()

	seed(t, p, map[string][]string{"TITLE": {"x"}})
	r1 := mustRead(t, p).Revision()
	if r1 == r0 {
		t.Error("a tag write kept the revision")
	}

	// A tool that restores the mtime and keeps the size still moves the
	// tag hash.
	st, _ := os.Stat(p)
	seed(t, p, map[string][]string{"TITLE": {"y"}})
	if err := os.Chtimes(p, st.ModTime(), st.ModTime()); err != nil {
		t.Fatal(err)
	}
	if r2 := mustRead(t, p); r2.Size == st.Size() && r2.Revision() == r1 {
		t.Error("a retag with the old mtime and size kept the revision")
	}

	r3 := mustRead(t, p).Revision()
	seed(t, p, nil, pngOf(t, color.White))
	if mustRead(t, p).Revision() == r3 {
		t.Error("adding a picture kept the revision")
	}
}

func TestReadBadFiles(t *testing.T) {
	dir := t.TempDir()
	if _, err := Read(filepath.Join(dir, "notes.txt")); !errors.Is(err, ErrNotAudio) {
		t.Errorf("a .txt reads with %v", err)
	}
	if _, err := Read(filepath.Join(dir, "missing.flac")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a missing file reads with %v", err)
	}
	if err := os.Mkdir(filepath.Join(dir, "folder.flac"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(filepath.Join(dir, "folder.flac")); err == nil {
		t.Error("a folder read as a file")
	}

	write := func(name string, b []byte) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	// Each of these returns an error and never panics.
	for _, p := range []string{
		write("empty.flac", nil),
		write("garbage.flac", []byte("this is not a flac file at all, just text")),
		write("header.m4a", []byte("\x00\x00\x00\x20ftypM4A \x00\x00\x00\x00")),
	} {
		if _, err := Read(p); err == nil {
			t.Errorf("%s read without an error", filepath.Base(p))
		}
	}

	// Half a file reads, with or without an error, and doesn't panic.
	full := mustBytes(t, filepath.Join("testdata", "track.flac"))
	_, _ = Read(write("half.flac", full[:len(full)/2]))
}

func TestGroups(t *testing.T) {
	p := fixture(t, t.TempDir(), "track.flac", "a.flac")
	seed(t, p, map[string][]string{
		"UPC": {"1"}, "ENSEMBLE": {"e"}, "ARTIST": {"a"}, "TITLE": {"t"}, "ALBUM ARTIST": {"x"}, "ALBUMARTIST": {"y"},
	})
	std, ext := mustRead(t, p).Groups()
	if want := []string{"TITLE", "ARTIST", "ALBUM ARTIST", "ALBUMARTIST"}; !slices.Equal(std, want) {
		t.Errorf("standard %v, want %v", std, want)
	}
	if want := []string{"ENSEMBLE", "UPC"}; !slices.Equal(ext, want) {
		t.Errorf("extended %v, want %v", ext, want)
	}
}
