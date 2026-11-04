package tagio

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// goldenCases are files whose album IDs, artists and dates came from a real
// Navidrome 0.64.2 that scanned them. To refresh them, run
//
//	SHELF_GOLDEN_DIR=/tmp/golden go test ./internal/modules/library/tagio -run TestGoldenNavidrome
//
// point a Navidrome container's /music at that folder, scan it, and read
// each file's albumId and artists from the Subsonic API.
var goldenCases = []struct {
	name, fixture, rel string
	tags               map[string][]string
	album, artist      string
	albumID            string
	date, release      string
}{
	{
		name: "plain", fixture: "flac", rel: "jazz/Kind of Blue/01.flac",
		tags:  map[string][]string{"TITLE": {"So What"}, "ARTIST": {"Miles Davis"}, "ALBUMARTIST": {"Miles Davis"}, "ALBUM": {"Kind of Blue"}, "DATE": {"1959-08-17"}, "TRACKNUMBER": {"1"}},
		album: "Kind of Blue", artist: "Miles Davis", date: "1959-08-17",
		albumID: "51CVFEWY8IZLiutQXsAOBF",
	},
	{
		name: "musicbrainz", fixture: "flac", rel: "jazz/Kind of Blue MB/01.flac",
		tags: map[string][]string{"TITLE": {"So What"}, "ARTIST": {"Miles Davis"}, "ALBUM": {"Kind of Blue"},
			"MUSICBRAINZ_ALBUMID": {"{8D2338A8-5E0E-4E3B-9F3E-6B1E4B8F6C11}"}, "TRACKNUMBER": {"1"}},
		album: "Kind of Blue", artist: "Miles Davis",
		albumID: "0HjJLHrRjDrx7IoNWfHfv9",
	},
	{
		name: "feat", fixture: "flac", rel: "pop/Feat/01.flac",
		tags:  map[string][]string{"TITLE": {"Song"}, "ARTIST": {"Alpha feat. Beta"}, "ALBUM": {"Feat"}, "TRACKNUMBER": {"1"}},
		album: "Feat", artist: "Alpha",
		albumID: "3DMN80lvN3zpWhy7NUrRcn",
	},
	{
		name: "compilation", fixture: "flac", rel: "pop/Hits/01.flac",
		tags:  map[string][]string{"TITLE": {"Hit"}, "ARTIST": {"Gamma"}, "ALBUM": {"Hits"}, "COMPILATION": {"1"}, "TRACKNUMBER": {"1"}},
		album: "Hits", artist: VariousArtists,
		albumID: "469fhiRWGwBzX4YSMY5C5t",
	},
	{
		name: "typographic", fixture: "flac", rel: "rock/Appetite/01.flac",
		tags: map[string][]string{"TITLE": {"Welcome"}, "ARTIST": {"Guns N’ Roses"}, "ALBUMARTIST": {"Guns N’ Roses"}, "ALBUM": {"Appetite — Deluxe"},
			"RELEASEDATE": {"1987-07-21"}, "ALBUMVERSION": {"Remaster"}, "TRACKNUMBER": {"1"}},
		album: "Appetite — Deluxe", artist: "Guns N’ Roses", date: "1987-07-21", release: "1987-07-21",
		albumID: "4aOnfshWfbMoBfgxqrIOx6",
	},
	{
		name: "legacy dates", fixture: "flac", rel: "jazz/Moanin/01.flac",
		tags: map[string][]string{"TITLE": {"Moanin'"}, "ARTIST": {"Art Blakey"}, "ALBUMARTIST": {"Art Blakey"}, "ALBUM": {"Moanin'"},
			"DATE": {"2006"}, "ORIGINALDATE": {"1958"}, "TRACKNUMBER": {"1"}},
		album: "Moanin'", artist: "Art Blakey", date: "1958", release: "2006",
		albumID: "3hxOAQ7sVIvSfrLBZV3cv5",
	},
	{
		name: "year turns the legacy rule off", fixture: "flac", rel: "jazz/Moanin 1984/01.flac",
		tags: map[string][]string{"TITLE": {"Moanin'"}, "ARTIST": {"Art Blakey"}, "ALBUMARTIST": {"Art Blakey"}, "ALBUM": {"Moanin'"},
			"DATE": {"1984"}, "YEAR": {"1984"}, "ORIGINALDATE": {"1958"}, "TRACKNUMBER": {"1"}},
		album: "Moanin'", artist: "Art Blakey", date: "1984", release: "1984",
		albumID: "7uJ5iC6QVYhjdHhRlOkoC4",
	},
	{
		name: "m4a day", fixture: "alac", rel: "apple-music/Album/01.m4a",
		tags:  map[string][]string{"TITLE": {"Track"}, "ARTIST": {"Delta"}, "ALBUMARTIST": {"Delta"}, "ALBUM": {"Album"}, "DATE": {"2020-05-01"}, "TRACKNUMBER": {"1"}},
		album: "Album", artist: "Delta", date: "2020-05-01", release: "2020-05-01",
		albumID: "3p69LVPQe6mqpInr1qgfT3",
	},
	{
		name: "no tags", fixture: "flac", rel: "misc/Untagged/01 Something.flac",
		tags:  nil,
		album: UnknownAlbum, artist: UnknownArtist,
		albumID: "69IwB2p7tQDejD3lowUIFo",
	},
}

func TestGoldenNavidrome(t *testing.T) {
	out := os.Getenv("SHELF_GOLDEN_DIR")
	r := DefaultRules()
	for _, c := range goldenCases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			if out != "" {
				dir = filepath.Join(out, filepath.Dir(c.rel))
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			p := fixture(t, dir, fixtures[c.fixture], filepath.Base(c.rel))
			seed(t, p, c.tags)
			f, err := Read(p)
			if err != nil {
				t.Fatal(err)
			}
			v := r.View(f, c.rel)
			date, _, release := v.Dates()
			t.Logf("%s album ID %s", c.rel, v.AlbumID())
			if v.Album() != c.album || v.AlbumArtist() != c.artist || date != c.date || release != c.release {
				t.Errorf("album %q, album artist %q, date %q, release %q; want %q, %q, %q, %q",
					v.Album(), v.AlbumArtist(), date, release, c.album, c.artist, c.date, c.release)
			}
			if v.AlbumID() != c.albumID {
				t.Errorf("album ID %s, Navidrome's is %s", v.AlbumID(), c.albumID)
			}
		})
	}
}

func TestViewMergesAliasesAndSplits(t *testing.T) {
	f := File{Format: FormatOf("a.flac"), Tags: map[string][]string{
		"ALBUMARTIST":         {"One"},
		"ALBUM ARTIST":        {"Two"},
		"GENRE":               {"Jazz; Bop", "Jazz"},
		"ARTIST":              {"Simon & Garfunkel feat. Art"},
		"MUSICBRAINZ_ALBUMID": {"not a uuid"},
		"TRACKNUMBER":         {"3/9"},
		"DATE":                {"1999-13-40"},
	}}
	v := DefaultRules().View(f, "a/b.flac")
	if got := v.Tags["albumartist"]; !slices.Equal(got, []string{"One", "Two"}) {
		t.Errorf("albumartist %q: Navidrome merges the aliases", got)
	}
	if got := v.Tags["genre"]; !slices.Equal(got, []string{"Jazz", "Bop"}) {
		t.Errorf("genre %q", got)
	}
	if got := v.Artists(); !slices.Equal(got, []string{"Simon & Garfunkel", "Art"}) {
		t.Errorf("artists %q", got)
	}
	if v.First("musicbrainz_albumid") != "" {
		t.Error("an ID that isn't a UUID is dropped")
	}
	if v.Track() != 3 {
		t.Errorf("track %d", v.Track())
	}
	if d, _, _ := v.Dates(); d != "1999" {
		t.Errorf("date %q: a bad day keeps the year", d)
	}
	if v.AlbumArtist() != "One • Two" {
		t.Errorf("album artist %q", v.AlbumArtist())
	}
}

func TestRulesFollowNavidromeSettings(t *testing.T) {
	f := File{Format: FormatOf("a.flac"), Tags: map[string][]string{
		"ARTIST": {"Earth, Wind & Fire feat. Someone"},
		"GENRE":  {"Rock|Pop"},
		"MOOD":   {"Calm"},
	}}
	r := NewRules(NavidromeConfig{
		ArtistSplitExceptions: []string{"Earth, Wind & Fire"},
		Tags: map[string]TagConfig{
			"artist": {Split: []string{",", " feat. "}},
			"Mood":   {Ignore: true},
		},
		GenreSeparators: "|",
		AlbumPID:        "folder",
	})
	v := r.View(f, "x/y/z.flac")
	if got := v.Artists(); !slices.Equal(got, []string{"Earth, Wind & Fire", "Someone"}) {
		t.Errorf("artists %q", got)
	}
	if got := v.Tags["genre"]; !slices.Equal(got, []string{"Rock", "Pop"}) {
		t.Errorf("genre %q", got)
	}
	if _, ok := v.Tags["mood"]; ok {
		t.Error("an ignored tag is read")
	}
	other := r.View(f, "x/y/other.flac")
	if v.AlbumID() != other.AlbumID() {
		t.Error("a folder spec gives files of one folder one album")
	}
	if r.ID() == DefaultRules().ID() {
		t.Error("other settings, same rules ID")
	}
	if !DefaultRules().EmbeddedCovers() || !slices.Equal(DefaultRules().CoverNames(), []string{"cover.*", "folder.*", "front.*"}) {
		t.Errorf("cover priority %q", DefaultRules().CoverNames())
	}
}

func TestDisagreements(t *testing.T) {
	r := DefaultRules()
	f := File{Format: FormatOf("a.flac"), Tags: map[string][]string{
		"ALBUMARTIST": {"One"}, "ALBUM ARTIST": {"Two"},
		"DISC": {"1"}, "DISCNUMBER": {"1"},
		"LYRICS": {"a"}, "LYRICS:ENG": {"b"},
	}}
	if got := r.Disagreements(f); !slices.Equal(got, []string{"albumartist"}) {
		t.Errorf("FLAC: %q; agreeing aliases and lyrics in two languages are fine", got)
	}
	// M4A's DATE is also the release date, so RELEASEDATE beside it can
	// disagree.
	m := File{Format: FormatOf("a.m4a"), Tags: map[string][]string{"DATE": {"1984"}, "RELEASEDATE": {"2001"}}}
	if got := r.Disagreements(m); !slices.Equal(got, []string{"releasedate"}) {
		t.Errorf("M4A: %q", got)
	}
}
