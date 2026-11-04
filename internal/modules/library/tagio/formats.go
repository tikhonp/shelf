package tagio

import (
	"path/filepath"
	"strings"
)

// Format is a kind of audio file shelf reads and writes.
type Format struct {
	// Name is short and lowercase, such as "flac" or "m4a".
	Name string
	// Label is what people read, such as "FLAC".
	Label string
	// Exts are the lowercase extensions the format goes by.
	Exts []string
	// Custom is false where TagLib drops keys it has no mapping for.
	Custom bool
	// Multi is false where TagLib loses the order of a multi-value tag.
	Multi bool
	// flac hashes the audio bytes in-process. Other formats ask ffmpeg.
	flac bool
	// mp4 pictures carry no type or description.
	mp4 bool
	// atoms maps a key TagLib reads to the raw frame or atom it comes from,
	// where Navidrome reads that frame into a different field than the key
	// names. M4A's ©day becomes TagLib's DATE but Navidrome's release date.
	atoms map[string]string
	// readOnly holds keys TagLib can neither change nor remove. A field
	// that holds one is read-only.
	readOnly []string
}

// ID3 tags in MP3, WAV and AIFF.
var id3Atoms = map[string]string{"INITIALKEY": "tkey"}

var formats = []*Format{
	{Name: "flac", Label: "FLAC", Exts: []string{".flac"}, Custom: true, Multi: true, flac: true},
	{
		Name: "m4a", Label: "M4A", Exts: []string{".m4a", ".m4b"}, Custom: true, Multi: true, mp4: true,
		atoms: map[string]string{"DATE": "©day", "INITIALKEY": "----:com.apple.itunes:initialkey"},
		// Picard's lowercase atoms. TagLib maps only the uppercase ones, so
		// it reads these under their raw names and can't write them back.
		readOnly: []string{"----:com.apple.itunes:originaldate", "----:com.apple.itunes:originalyear"},
	},
	{Name: "mp3", Label: "MP3", Exts: []string{".mp3"}, Custom: true, Multi: true, atoms: id3Atoms},
	{Name: "ogg", Label: "Ogg Vorbis", Exts: []string{".ogg", ".oga"}, Custom: true, Multi: true},
	{Name: "opus", Label: "Opus", Exts: []string{".opus"}, Custom: true, Multi: true},
	{Name: "wav", Label: "WAV", Exts: []string{".wav"}, Custom: true, Multi: true, atoms: id3Atoms},
	{Name: "aiff", Label: "AIFF", Exts: []string{".aiff", ".aif"}, Custom: true, Multi: true, atoms: id3Atoms},
	{
		Name: "wma", Label: "WMA", Exts: []string{".wma"},
		atoms: map[string]string{"DATE": "wm/year", "INITIALKEY": "wm/initialkey"},
	},
}

// FormatOf finds a file's format by its extension, or nil for a file that
// isn't audio to shelf. That includes .mp4, which in this library is amdl's
// animated artwork and which Navidrome skips.
func FormatOf(path string) *Format {
	ext := strings.ToLower(filepath.Ext(path))
	for _, f := range formats {
		for _, e := range f.Exts {
			if e == ext {
				return f
			}
		}
	}
	return nil
}

// ReadOnly reports whether TagLib can't change key in this format.
func (f *Format) ReadOnly(key string) bool {
	k := strings.ToLower(key)
	for _, r := range f.readOnly {
		if r == k {
			return true
		}
	}
	return false
}
