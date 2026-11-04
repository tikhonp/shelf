// Package loudness measures tracks for ReplayGain with ffmpeg's ebur128
// filter. A track keeps its loudness blocks, because an album's gain comes
// from the blocks of all its tracks pooled, not from the tracks' results.
package loudness

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os/exec"
	"strconv"
	"strings"
)

// Reference is the loudness ReplayGain aims for, in LUFS.
const Reference = -18.0

// The gates of ITU BS.1770.
const (
	absoluteGate = -70.0
	relativeGate = -10.0
	// offset is the constant between a block's mean square and its loudness.
	offset = -0.691
)

// perLU is how many histogram bins one LU holds. Whole blocks would take
// 12 KB a track, and at this width a result moves by under 0.01 LU.
const perLU = 10

// Blocks is a histogram of a track's 400 ms blocks: how many fell in each
// bin of loudness. Blocks under the absolute gate aren't kept.
type Blocks map[int]int

func (b Blocks) add(lufs float64) {
	if lufs > absoluteGate {
		b[int(math.Round(lufs*perLU))]++
	}
}

func energy(bin int) float64 { return math.Pow(10, (float64(bin)/perLU-offset)/10) }

// Pool adds tracks' blocks together.
func Pool(tracks ...Blocks) Blocks {
	out := Blocks{}
	for _, t := range tracks {
		for bin, n := range t {
			out[bin] += n
		}
	}
	return out
}

// Integrated is the gated loudness in LUFS. Blocks with nothing above the
// absolute gate, as in silence, have none.
func (b Blocks) Integrated() (float64, bool) {
	mean := func(keep func(int) bool) (float64, bool) {
		sum, n := 0.0, 0
		for bin, c := range b {
			if keep(bin) {
				sum += energy(bin) * float64(c)
				n += c
			}
		}
		if n == 0 {
			return 0, false
		}
		return sum / float64(n), true
	}
	all, ok := mean(func(int) bool { return true })
	if !ok {
		return 0, false
	}
	gate := offset + 10*math.Log10(all) + relativeGate
	gated, ok := mean(func(bin int) bool { return float64(bin)/perLU > gate })
	if !ok {
		return 0, false
	}
	return offset + 10*math.Log10(gated), true
}

// Track is what one decode measured.
type Track struct {
	Blocks Blocks
	// Peak is the highest sample, where 1 is full scale. ffmpeg reports it
	// to three places.
	Peak float64
}

// Analyze decodes a file once and measures its first audio stream.
func Analyze(ctx context.Context, ffmpeg, path string) (Track, error) {
	// "file:" keeps a name that starts with a dash or holds a colon from
	// reading as an option or a protocol.
	cmd := exec.CommandContext(ctx, ffmpeg, "-nostdin", "-nostats", "-v", "error", "-i", "file:"+path, "-map", "0:a:0",
		"-af", "ebur128=metadata=1:peak=sample,ametadata=mode=print:file=-", "-f", "null", "-")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		return Track{}, err
	}
	if err := cmd.Start(); err != nil {
		return Track{}, fmt.Errorf("ffmpeg: %w", err)
	}
	t, perr := parse(out)
	// Wait closes the pipe, so the parse must end first.
	if err := cmd.Wait(); err != nil {
		if ctx.Err() != nil {
			return Track{}, ctx.Err()
		}
		line, _, _ := strings.Cut(stderr.String(), "\n")
		return Track{}, fmt.Errorf("ffmpeg: %w: %s", err, line)
	}
	return t, perr
}

// parse reads the frames ametadata prints. Each 100 ms frame holds the
// loudness of the 400 ms block that ends with it.
func parse(r io.Reader) (Track, error) {
	t := Track{Blocks: Blocks{}}
	frames := 0
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), "=")
		if !ok {
			continue
		}
		switch k {
		case "lavfi.r128.M":
			f, err := strconv.ParseFloat(v, 64)
			if err != nil {
				// A block with no signal prints as -inf or nan.
				continue
			}
			frames++
			t.Blocks.add(f)
		case "lavfi.r128.sample_peak":
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				t.Peak = max(t.Peak, f)
			}
		}
	}
	if err := sc.Err(); err != nil {
		return Track{}, err
	}
	if frames == 0 {
		return Track{}, errors.New("ffmpeg measured no audio")
	}
	return t, nil
}

// Gain is what brings a loudness to the reference.
func Gain(lufs float64) float64 { return Reference - lufs }

// FormatGain writes a gain as ReplayGain tags hold it.
func FormatGain(db float64) string { return fmt.Sprintf("%+.2f dB", db) }

// FormatPeak writes a peak as ReplayGain tags hold it.
func FormatPeak(peak float64) string { return fmt.Sprintf("%.6f", peak) }
