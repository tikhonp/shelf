package source

import (
	"strconv"
	"strings"
)

// Quality is what an item offers or a file has.
type Quality struct {
	Codec      string `json:"codec,omitempty"` // "flac", "alac", "aac", "ec-3", "mp3"
	BitDepth   int    `json:"bit_depth,omitempty"`
	SampleRate int    `json:"sample_rate,omitempty"` // Hz
	HiRes      bool   `json:"hires,omitempty"`
	Atmos      bool   `json:"atmos,omitempty"`
	// Badge is the label the UI shows, such as "Hi-Res 24/96".
	Badge string `json:"badge,omitempty"`
}

// IsZero reports whether nothing is known.
func (q Quality) IsZero() bool { return q.Codec == "" && q.BitDepth == 0 && q.SampleRate == 0 }

func (q Quality) lossless() bool { return q.BitDepth > 0 && q.SampleRate > 0 }

// KHz renders a sample rate the way the badges do: 44100 is "44.1", 96000
// is "96".
func KHz(hz int) string {
	return strconv.FormatFloat(float64(hz)/1000, 'f', -1, 64)
}

// Words is the quality as shelf.txt's Delivered line says it: "FLAC 24-bit
// 96 kHz", or just the codec for a lossy file.
func (q Quality) Words() string {
	codec := strings.ToUpper(q.Codec)
	if !q.lossless() {
		return codec
	}
	return codec + " " + strconv.Itoa(q.BitDepth) + "-bit " + KHz(q.SampleRate) + " kHz"
}

// Short is the quality in a track line: "FLAC 24/96", or the codec.
func (q Quality) Short() string {
	codec := strings.ToUpper(q.Codec)
	if !q.lossless() {
		return codec
	}
	return codec + " " + strconv.Itoa(q.BitDepth) + "/" + KHz(q.SampleRate)
}

// Better reports whether q is higher than o: lossless above lossy, then bit
// depth, then sample rate.
func (q Quality) Better(o Quality) bool {
	if q.lossless() != o.lossless() {
		return q.lossless()
	}
	if q.BitDepth != o.BitDepth {
		return q.BitDepth > o.BitDepth
	}
	if q.SampleRate != o.SampleRate {
		return q.SampleRate > o.SampleRate
	}
	return q.Codec != "" && o.Codec == ""
}
