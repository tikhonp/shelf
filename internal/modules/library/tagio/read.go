package tagio

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
)

// ErrNotAudio is a file whose extension is no format shelf knows.
var ErrNotAudio = errors.New("not an audio file shelf knows")

// File is what an audio file holds, read in one go.
type File struct {
	Path    string
	Format  *Format
	Size    int64
	ModTime time.Time
	// Tags maps each key, as TagLib reads it, to its values in order.
	Tags     map[string][]string
	Pictures []Picture
	Audio    Audio
}

// Picture is an embedded picture without its bytes.
type Picture struct {
	// Type is TagLib's name, such as "Front Cover". M4A pictures have none.
	Type        string
	Description string
	MIME        string
}

// Audio holds the stream's properties.
type Audio struct {
	// Codec is TagLib's name of the codec inside a container, such as
	// "alac", or empty for a format that is its own codec.
	Codec      string
	Length     time.Duration
	Channels   uint
	SampleRate uint
	BitDepth   uint
	// BitRate is in kbit/s.
	BitRate uint
}

// Read reads a file's tags, pictures and audio properties.
func Read(path string) (File, error) {
	ft := FormatOf(path)
	if ft == nil {
		return File{}, fmt.Errorf("%s: %w", path, ErrNotAudio)
	}
	st, err := os.Stat(path)
	if err != nil {
		return File{}, err
	}
	if !st.Mode().IsRegular() {
		return File{}, fmt.Errorf("%s: not a regular file", path)
	}
	return readAs(path, ft, st)
}

func readAs(path string, ft *Format, st os.FileInfo) (File, error) {
	tags, err := readTags(path)
	if err != nil {
		return File{}, err
	}
	props, err := readProperties(path)
	if err != nil {
		return File{}, err
	}
	f := File{
		Path: path, Format: ft, Size: st.Size(), ModTime: st.ModTime(), Tags: tags,
		Audio: Audio{
			Codec: props.InnerCodec, Length: props.Length, Channels: props.Channels,
			SampleRate: props.SampleRate, BitDepth: props.BitDepth, BitRate: props.BitRate,
		},
	}
	for _, im := range props.Images {
		f.Pictures = append(f.Pictures, Picture{Type: im.Type, Description: im.Description, MIME: im.MIMEType})
	}
	return f, nil
}

// Revision names the state of a file: its size, its modification time, and
// a hash of its tags and picture list. A Save carries the revision the
// editor loaded, and a file whose revision has moved since is a conflict.
// That catches Picard and other tools, even one that keeps the mtime.
func (f File) Revision() string {
	return strconv.FormatInt(f.Size, 10) + "-" + strconv.FormatInt(f.ModTime.UnixNano(), 10) + "-" + tagHash(f.Tags, f.Pictures)
}

func tagHash(tags map[string][]string, pics []Picture) string {
	h := sha256.New()
	keys := make([]string, 0, len(tags))
	for k := range tags {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		h.Write([]byte(k))
		for _, v := range tags[k] {
			h.Write([]byte{0})
			h.Write([]byte(v))
		}
		h.Write([]byte{1})
	}
	for _, p := range pics {
		h.Write([]byte(p.Type + "\x00" + p.Description + "\x00" + p.MIME + "\x02"))
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// Groups splits a file's keys into the ones Navidrome reads, in field order,
// and the rest, sorted. The All fields switch groups them this way.
func (f File) Groups() (standard, extended []string) {
	for k := range f.Tags {
		if len(FieldsOf(f.Format, k)) > 0 {
			standard = append(standard, k)
		} else {
			extended = append(extended, k)
		}
	}
	rank := func(k string) int {
		fs := FieldsOf(f.Format, k)
		return slices.IndexFunc(fields, func(x Field) bool { return x.Name == fs[0].Name })
	}
	slices.SortFunc(standard, func(a, b string) int {
		if d := rank(a) - rank(b); d != 0 {
			return d
		}
		return strings.Compare(a, b)
	})
	slices.Sort(extended)
	return standard, extended
}
