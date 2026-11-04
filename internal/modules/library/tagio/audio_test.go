package tagio

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"image/color"
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path string, b []byte) string {
	t.Helper()
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// id3v2 is an empty ID3v2.4 tag with size bytes of padding.
func id3v2(size int) []byte {
	b := []byte{'I', 'D', '3', 4, 0, 0,
		byte(size >> 21 & 0x7f), byte(size >> 14 & 0x7f), byte(size >> 7 & 0x7f), byte(size & 0x7f)}
	return append(b, make([]byte, size)...)
}

func TestFLACHashSkipsTags(t *testing.T) {
	dir := t.TempDir()
	plain := mustBytes(t, filepath.Join("testdata", "track.flac"))
	base, err := flacHash(writeFile(t, filepath.Join(dir, "plain.flac"), plain))
	if err != nil {
		t.Fatal(err)
	}

	tagged := fixture(t, dir, "track.flac", "tagged.flac")
	seed(t, tagged, map[string][]string{"TITLE": {"a long title to move the frames"}}, pngOf(t, color.Black))
	id3 := append(id3v2(300), plain...)
	v1 := append(append([]byte{}, plain...), append([]byte("TAG"), make([]byte, 125)...)...)
	for name, path := range map[string]string{
		"retagged":      tagged,
		"id3v2 prefix":  writeFile(t, filepath.Join(dir, "id3.flac"), id3),
		"id3v1 trailer": writeFile(t, filepath.Join(dir, "v1.flac"), v1),
	} {
		got, err := flacHash(path)
		if err != nil {
			t.Errorf("%s: %v", name, err)
		} else if got != base {
			t.Errorf("%s: the audio hash moved", name)
		}
	}

	damaged := append([]byte{}, plain...)
	damaged[len(damaged)-200] ^= 0xff
	if got, _ := flacHash(writeFile(t, filepath.Join(dir, "damaged.flac"), damaged)); got == base {
		t.Error("a damaged frame kept the hash")
	}

	for name, b := range map[string][]byte{
		"no marker":  []byte("RIFF0000WAVEfmt "),
		"short":      plain[:20],
		"past end":   append([]byte("fLaC"), 0x80, 0xff, 0xff, 0xff),
		"bare id3v2": id3v2(4),
	} {
		if _, err := flacHash(writeFile(t, filepath.Join(dir, "bad.flac"), b)); err == nil {
			t.Errorf("%s: hashed without an error", name)
		}
	}
}

func TestRangeHashAcrossWrites(t *testing.T) {
	data := bytes.Repeat([]byte("0123456789"), 100)
	want := sha256.Sum256(data[123:877])
	for _, chunk := range []int{1, 7, 100, 1000} {
		rh := &rangeHash{h: sha256.New(), start: 123, end: 877}
		for i := 0; i < len(data); i += chunk {
			_, _ = rh.Write(data[i:min(i+chunk, len(data))])
		}
		if rh.sum() != hex.EncodeToString(want[:]) {
			t.Errorf("chunk %d: wrong hash", chunk)
		}
	}
}

func TestFFmpegHash(t *testing.T) {
	needFFmpeg(t)
	ctx := context.Background()
	dir := t.TempDir()
	for name, fx := range fixtures {
		if name == "flac" {
			continue
		}
		t.Run(name, func(t *testing.T) {
			plain := fixture(t, dir, fx, name+"-plain")
			tagged := fixture(t, dir, fx, name+"-tagged")
			seed(t, tagged, map[string][]string{"TITLE": {"a title long enough to move things"}, "ALBUM": {"b"}}, pngOf(t, color.Black))
			a, err := ffmpegHash(ctx, "ffmpeg", plain)
			if err != nil {
				t.Fatal(err)
			}
			b, err := ffmpegHash(ctx, "ffmpeg", tagged)
			if err != nil {
				t.Fatal(err)
			}
			if a != b {
				t.Error("tags moved the audio hash")
			}
		})
	}

	wav := mustBytes(t, filepath.Join("testdata", "track.wav"))
	a, _ := ffmpegHash(ctx, "ffmpeg", writeFile(t, filepath.Join(dir, "a.wav"), wav))
	wav[len(wav)-10] ^= 0xff
	b, _ := ffmpegHash(ctx, "ffmpeg", writeFile(t, filepath.Join(dir, "-b.wav"), wav))
	if a == "" || a == b {
		t.Error("changed samples kept the hash, or the hash failed")
	}
	if _, err := ffmpegHash(ctx, "ffmpeg", filepath.Join(dir, "missing.wav")); err == nil {
		t.Error("a missing file hashed")
	}
}
