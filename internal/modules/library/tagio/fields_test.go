package tagio

import (
	"slices"
	"testing"
)

func names(fs []Field) []string {
	var out []string
	for _, f := range fs {
		out = append(out, f.Name)
	}
	return out
}

func TestFieldsLoadMappings(t *testing.T) {
	fs := Fields()
	if len(fs) < 80 {
		t.Fatalf("%d fields, want Navidrome's 80 or more", len(fs))
	}
	if fs[0].Name != "title" || !fs[0].Main {
		t.Errorf("first field %+v, want title, main", fs[0])
	}
	if last := fs[len(fs)-1]; last.Main {
		t.Errorf("last field %s is main; additional fields come last", last.Name)
	}
	g, ok := FieldByName("genre")
	if !ok || !g.Album || !slices.Equal(g.Split, []string{";", "/", ","}) {
		t.Errorf("genre = %+v", g)
	}
	if d, _ := FieldByName("releasedate"); d.Type != "date" {
		t.Errorf("releasedate type %q", d.Type)
	}
}

// A key shelf adds must be one Navidrome reads into the same field. Vorbis
// comments carry keys as written, so FLAC shows it plainly.
func TestFieldKeyFeedsItsField(t *testing.T) {
	flac := FormatOf("x.flac")
	for _, f := range Fields() {
		if !f.feeds(flac, f.Key) {
			t.Errorf("%s: key %s is none of its aliases %v", f.Name, f.Key, f.Aliases)
		}
	}
}

func TestFieldTraps(t *testing.T) {
	flac := FormatOf("x.flac")
	m4a := FormatOf("x.m4a")
	cases := []struct {
		ft   *Format
		key  string
		want []string
	}{
		// The file's release track ID is Navidrome's track ID, and the
		// file's track ID is the recording.
		{flac, "MUSICBRAINZ_RELEASETRACKID", []string{"musicbrainz_trackid"}},
		{flac, "MUSICBRAINZ_TRACKID", []string{"musicbrainz_recordingid"}},
		{flac, "ALBUM ARTIST", []string{"albumartist"}},
		{flac, "DISC", []string{"disc"}},
		{flac, "TRACKNUMBER", []string{"track"}},
		{flac, "YEAR", []string{"releasedate"}},
		{flac, "DATE", []string{"recordingdate"}},
		// M4A's DATE is the ©day atom, which Navidrome reads as the release
		// date too.
		{m4a, "DATE", []string{"recordingdate", "releasedate"}},
		{m4a, "INITIALKEY", []string{"key"}},
		{flac, "INITIALKEY", nil},
		{flac, "lyrics:eng", []string{"lyrics"}},
		{flac, "PERFORMER:PIANO", []string{"performer"}},
		{flac, "LABEL", []string{"recordlabel"}},
		{flac, "ORIGINALYEAR", []string{"originaldate"}},
		// Keys that never reach Navidrome.
		{flac, "ENSEMBLE", nil},
		{flac, "UPC", nil},
		{flac, "TITLE:X", nil},
	}
	for _, c := range cases {
		if got := names(FieldsOf(c.ft, c.key)); !slices.Equal(got, c.want) {
			t.Errorf("%s %s feeds %v, want %v", c.ft.Name, c.key, got, c.want)
		}
	}
	if k, _ := FieldByName("musicbrainz_trackid"); k.Key != "MUSICBRAINZ_RELEASETRACKID" {
		t.Errorf("musicbrainz_trackid adds %s", k.Key)
	}
}

func TestFormatOf(t *testing.T) {
	cases := map[string]string{
		"a.flac": "flac", "A.FLAC": "flac", "b.m4a": "m4a", "c.mp3": "mp3", "d.oga": "ogg",
		"e.opus": "opus", "f.wav": "wav", "g.aif": "aiff", "h.wma": "wma",
		// amdl's animated artwork, which Navidrome skips.
		"cover.mp4": "", "notes.txt": "", "flac": "",
	}
	for path, want := range cases {
		got := ""
		if f := FormatOf(path); f != nil {
			got = f.Name
		}
		if got != want {
			t.Errorf("FormatOf(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestM4AReadOnlyKeys(t *testing.T) {
	m4a := FormatOf("x.m4a")
	if !m4a.ReadOnly("----:com.apple.iTunes:originaldate") || !m4a.ReadOnly("----:com.apple.iTunes:originalyear") {
		t.Error("Picard's lowercase atoms should be read-only")
	}
	if m4a.ReadOnly("ORIGINALDATE") || FormatOf("x.flac").ReadOnly("----:com.apple.iTunes:originaldate") {
		t.Error("only M4A's lowercase atoms are read-only")
	}
}
