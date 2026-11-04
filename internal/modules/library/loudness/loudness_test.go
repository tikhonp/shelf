package loudness

import (
	"context"
	"math"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// needFFmpeg skips a test that decodes audio where ffmpeg isn't installed,
// such as the CI runner.
func needFFmpeg(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg is not installed")
	}
}

func tone(t *testing.T, name, filter string) string {
	t.Helper()
	needFFmpeg(t)
	p := filepath.Join(t.TempDir(), name)
	if out, err := exec.Command("ffmpeg", "-v", "error", "-y", "-f", "lavfi", "-i", filter, "-ac", "2", p).CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	return p
}

func TestIntegratedGates(t *testing.T) {
	b := Blocks{}
	for range 100 {
		b.add(-20)
	}
	// A quiet passage falls under the relative gate and changes nothing.
	for range 100 {
		b.add(-45)
	}
	// Silence falls under the absolute gate.
	for range 100 {
		b.add(-120.7)
	}
	got, ok := b.Integrated()
	if !ok || math.Abs(got-(-20)) > 0.01 {
		t.Fatalf("%v %v", got, ok)
	}
	if _, ok := (Blocks{}).Integrated(); ok {
		t.Fatal("silence has a loudness")
	}
}

func TestPoolMatchesOneDecode(t *testing.T) {
	ctx := context.Background()
	loud := tone(t, "a.flac", "sine=frequency=1000:duration=6:sample_rate=44100")
	quiet := tone(t, "b.flac", "sine=frequency=1000:duration=6:sample_rate=44100,volume=-6dB")
	a, err := Analyze(ctx, "ffmpeg", loud)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Analyze(ctx, "ffmpeg", quiet)
	if err != nil {
		t.Fatal(err)
	}
	both := filepath.Join(t.TempDir(), "both.flac")
	if out, err := exec.Command("ffmpeg", "-v", "error", "-y", "-i", loud, "-i", quiet, "-filter_complex", "concat=n=2:v=0:a=1", both).CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	whole, err := Analyze(ctx, "ffmpeg", both)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := whole.Blocks.Integrated()
	got, ok := Pool(a.Blocks, b.Blocks).Integrated()
	if !ok || math.Abs(got-want) > 0.05 {
		t.Fatalf("pooled %v, one decode %v", got, want)
	}
	la, _ := a.Blocks.Integrated()
	lb, _ := b.Blocks.Integrated()
	if math.Abs(la-lb-6) > 0.1 {
		t.Fatalf("%v and %v are not 6 LU apart", la, lb)
	}
	if a.Peak < 0.05 || a.Peak > 1 {
		t.Fatalf("peak %v", a.Peak)
	}
}

func TestAnalyzeReportsAFileItCantRead(t *testing.T) {
	needFFmpeg(t)
	if _, err := Analyze(context.Background(), "ffmpeg", filepath.Join(t.TempDir(), "gone.flac")); err == nil {
		t.Fatal("no error")
	}
}

func TestAnalyzeStopsWithItsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Analyze(ctx, "ffmpeg", tone(t, "a.flac", "sine=frequency=1000:duration=1")); err == nil {
		t.Fatal("no error")
	}
}

func TestParseKeepsThePeakAndSkipsNoSignal(t *testing.T) {
	got, err := parse(strings.NewReader(`frame:0
lavfi.r128.M=-inf
lavfi.r128.sample_peak=0.100
frame:1
lavfi.r128.M=-23.0
lavfi.r128.sample_peak=0.900
frame:2
lavfi.r128.M=-23.0
lavfi.r128.sample_peak=0.500
`))
	if err != nil || got.Peak != 0.9 || got.Blocks[-230] != 2 {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err := parse(strings.NewReader("")); err == nil {
		t.Fatal("no frames, no error")
	}
}

func TestFormats(t *testing.T) {
	if g := FormatGain(Gain(-21.214)); g != "+3.21 dB" {
		t.Fatal(g)
	}
	if g := FormatGain(Gain(-9.5)); g != "-8.50 dB" {
		t.Fatal(g)
	}
	if p := FormatPeak(0.9); p != "0.900000" {
		t.Fatal(p)
	}
}
