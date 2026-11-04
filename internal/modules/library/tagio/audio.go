package tagio

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"os/exec"
	"strings"
)

// A tag write must leave the audio as it was. shelf checks FLAC by hashing
// the bytes after the metadata blocks, and other formats by ffmpeg's hash of
// the audio packets, which no tag touches.

// flacAudio finds where a FLAC file's audio frames start and end. TagLib
// may keep or drop an ID3v2 tag before "fLaC" and an ID3v1 tag at the end,
// so neither counts as audio.
func flacAudio(r io.ReaderAt, size int64) (start, end int64, err error) {
	var off int64
	var head [10]byte
	for {
		if _, err := r.ReadAt(head[:], off); err != nil {
			return 0, 0, fmt.Errorf("flac header: %w", err)
		}
		if string(head[:3]) != "ID3" {
			break
		}
		n := int64(head[6]&0x7f)<<21 | int64(head[7]&0x7f)<<14 | int64(head[8]&0x7f)<<7 | int64(head[9]&0x7f)
		off += 10 + n
		if head[5]&0x10 != 0 {
			off += 10 // footer
		}
	}
	if string(head[:4]) != "fLaC" {
		return 0, 0, errors.New("flac header: no fLaC marker")
	}
	off += 4
	for {
		var b [4]byte
		if _, err := r.ReadAt(b[:], off); err != nil {
			return 0, 0, fmt.Errorf("flac metadata: %w", err)
		}
		off += 4 + (int64(b[1])<<16 | int64(b[2])<<8 | int64(b[3]))
		if b[0]&0x80 != 0 {
			break
		}
		if off > size {
			return 0, 0, errors.New("flac metadata runs past the end")
		}
	}
	end = size
	if size-off >= 128 {
		var tag [3]byte
		if _, err := r.ReadAt(tag[:], size-128); err == nil && string(tag[:]) == "TAG" {
			end = size - 128
		}
	}
	if off > end {
		return 0, 0, errors.New("flac metadata runs past the end")
	}
	return off, end, nil
}

// flacHash hashes a FLAC file's audio frames.
func flacHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	st, err := f.Stat()
	if err != nil {
		return "", err
	}
	start, end, err := flacAudio(f, st.Size())
	if err != nil {
		return "", fmt.Errorf("%s: %w", path, err)
	}
	h := sha256.New()
	if _, err := io.Copy(h, io.NewSectionReader(f, start, end-start)); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// rangeHash hashes the bytes written to it between start and end, so a copy
// can hash the audio as it goes.
type rangeHash struct {
	h          hash.Hash
	start, end int64
	off        int64
}

func (r *rangeHash) Write(p []byte) (int, error) {
	lo, hi := r.off, r.off+int64(len(p))
	if a, b := max(lo, r.start), min(hi, r.end); a < b {
		r.h.Write(p[a-lo : b-lo])
	}
	r.off = hi
	return len(p), nil
}

func (r *rangeHash) sum() string { return hex.EncodeToString(r.h.Sum(nil)) }

// ffmpegHash hashes the first audio stream's packets without decoding them.
func ffmpegHash(ctx context.Context, ffmpeg, path string) (string, error) {
	var out, stderr bytes.Buffer
	// "file:" keeps a name that starts with a dash or holds a colon from
	// reading as an option or a protocol.
	cmd := exec.CommandContext(ctx, ffmpeg, "-nostdin", "-v", "error", "-i", "file:"+path,
		"-map", "0:a:0", "-c", "copy", "-f", "hash", "-hash", "sha256", "-")
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("ffmpeg hash %s: %w: %s", path, err, firstLine(stderr.String()))
	}
	sum, ok := strings.CutPrefix(strings.TrimSpace(out.String()), "SHA256=")
	if !ok || len(sum) != 64 {
		return "", fmt.Errorf("ffmpeg hash %s: unexpected output %q", path, firstLine(out.String()))
	}
	return sum, nil
}

func firstLine(s string) string {
	sc := bufio.NewScanner(strings.NewReader(s))
	if sc.Scan() {
		return sc.Text()
	}
	return ""
}
