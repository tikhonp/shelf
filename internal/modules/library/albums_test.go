package library

import (
	"slices"
	"testing"
	"time"

	"github.com/tikhonp/shelf/internal/modules/library/tagio"
)

// tagged is a well-tagged FLAC track of album "Blue" in tags, with the
// overrides applied. A nil value removes the key.
func tagged(over map[string][]string) map[string][]string {
	t := map[string][]string{
		"TITLE": {"Track"}, "ARTIST": {"Joni Mitchell"}, "ALBUMARTIST": {"Joni Mitchell"}, "ALBUMARTISTS": {"Joni Mitchell"},
		"ALBUM": {"Blue"}, "TRACKNUMBER": {"1"}, "DATE": {"1971"}, "RELEASEDATE": {"1971-06-22"},
		"REPLAYGAIN_TRACK_GAIN": {"-6 dB"},
	}
	for k, v := range over {
		if v == nil {
			delete(t, k)
		} else {
			t[k] = v
		}
	}
	return t
}

func factsOf(t *testing.T, rel string, tags map[string][]string) facts {
	t.Helper()
	return read(tagio.DefaultRules(), tagio.File{Format: tagio.FormatOf(rel), Tags: tags}, rel)
}

func kinds(is []Issue) []Kind {
	var out []Kind
	for _, i := range is {
		out = append(out, i.Kind)
	}
	return out
}

func issue(is []Issue, k Kind) (Issue, bool) {
	for _, i := range is {
		if i.Kind == k {
			return i, true
		}
	}
	return Issue{}, false
}

func TestFactsOfAWellTaggedFile(t *testing.T) {
	fc := factsOf(t, "folk/Blue/01.flac", tagged(nil))
	if fc.Issues != nil {
		t.Errorf("issues %+v", fc.Issues)
	}
	if fc.Album != "Blue" || fc.AlbumArtist != "Joni Mitchell" || fc.Year != 1971 || fc.Track != 1 || fc.AlbumKey != "blue" {
		t.Errorf("facts %+v", fc)
	}
}

func TestFileChecks(t *testing.T) {
	cases := []struct {
		name   string
		tags   map[string][]string
		want   Kind
		fields []string
	}{
		{"missing tags", tagged(map[string][]string{"TITLE": nil, "TRACKNUMBER": nil}), MissingTag, []string{"title", "track"}},
		{"stale alias", tagged(map[string][]string{"ALBUM ARTIST": {"Joni"}}), TagsDisagree, []string{"albumartist"}},
		{"reissue year", tagged(map[string][]string{"DATE": {"1984"}, "ORIGINALDATE": {"1958"}}),
			ReissueYear, []string{"originaldate", "recordingdate", "releasedate"}},
		{"no release date", tagged(map[string][]string{"RELEASEDATE": nil}), NoReleaseDate, []string{"releasedate"}},
		{"artist over-split", tagged(map[string][]string{"ARTIST": {"Crosby; Stills"}, "MUSICBRAINZ_ARTISTID": {"6b4ab5f6-3a2f-4b2e-9d1a-0a1c3a5e2f11"}}),
			ArtistSplit, []string{"artist"}},
		{"album artists without their IDs", tagged(map[string][]string{"ALBUMARTISTS": {"A", "B"},
			"MUSICBRAINZ_ALBUMARTISTID": {"6b4ab5f6-3a2f-4b2e-9d1a-0a1c3a5e2f11"}}), ArtistSplit, []string{"albumartist"}},
		{"no album artists tag", tagged(map[string][]string{"ALBUMARTISTS": nil}), GuideAlbumArtists, []string{"albumartists"}},
		{"no replaygain", tagged(map[string][]string{"REPLAYGAIN_TRACK_GAIN": nil}), GuideReplayGain, []string{"replaygain_track_gain"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			is := factsOf(t, "a/b.flac", c.tags).Issues
			got, ok := issue(is, c.want)
			if !ok || !slices.Equal(got.Fields, c.fields) || len(is) != 1 {
				t.Errorf("issues %+v, want only %s with %q", is, c.want, c.fields)
			}
		})
	}
	// The legacy date rule shows the original year, so it's no reissue.
	fc := factsOf(t, "a/b.flac", tagged(map[string][]string{"DATE": {"2006"}, "ORIGINALDATE": {"1958"}, "RELEASEDATE": nil}))
	if _, ok := issue(fc.Issues, ReissueYear); ok || fc.Year != 1958 {
		t.Errorf("legacy dates: year %d, issues %+v", fc.Year, fc.Issues)
	}
	// MusicBrainz IDs that match the split are fine.
	fc = factsOf(t, "a/b.flac", tagged(map[string][]string{"ARTIST": {"Alpha feat. Beta"},
		"MUSICBRAINZ_ARTISTID": {"6b4ab5f6-3a2f-4b2e-9d1a-0a1c3a5e2f11", "7b4ab5f6-3a2f-4b2e-9d1a-0a1c3a5e2f11"}}))
	if fc.Issues != nil {
		t.Errorf("a right split: %+v", fc.Issues)
	}
}

// indexed builds index files from tags, all readable.
func indexed(t *testing.T, tracks map[string]map[string][]string) []*file {
	t.Helper()
	var out []*file
	for rel, tags := range tracks {
		out = append(out, &file{Path: rel, Size: 100, FirstSeen: time.Unix(int64(len(out)), 0).UTC(), Facts: factsOf(t, rel, tags)})
	}
	return out
}

var cover = map[string][]string{}

func TestGroupMergesDiscFolders(t *testing.T) {
	files := indexed(t, map[string]map[string][]string{
		"folk/Blue/CD 1/01.flac": tagged(nil),
		"folk/Blue/CD 1/02.flac": tagged(map[string][]string{"TRACKNUMBER": {"2"}}),
		"folk/Blue/CD2/01.flac":  tagged(map[string][]string{"DISCNUMBER": {"2"}}),
	})
	albums := group(files, map[string][]string{"folk/Blue": {"cover.jpg"}}, tagio.DefaultRules())
	if len(albums) != 1 {
		t.Fatalf("%d albums", len(albums))
	}
	a := albums[0]
	if a.Folder != "folk/Blue" || len(a.Files) != 3 || a.Files[2].Path != "folk/Blue/CD2/01.flac" || a.Issues != nil {
		t.Errorf("album %+v, issues %+v", a, a.Issues)
	}
	if a.Title != "Blue" || a.Artist != "Joni Mitchell" || a.Year != 1971 || a.Size != 300 || !a.Added.Equal(time.Unix(0, 0)) {
		t.Errorf("summary %+v", a)
	}
}

func TestGroupFindsSplits(t *testing.T) {
	mb := func(id string) map[string][]string { return tagged(map[string][]string{"MUSICBRAINZ_ALBUMID": {id}}) }
	files := indexed(t, map[string]map[string][]string{
		// Disc 2 lost its album artist, so Navidrome shows two albums.
		"folk/Blue/CD 1/01.flac": tagged(nil),
		"folk/Blue/CD 2/01.flac": tagged(map[string][]string{"ALBUMARTIST": {"Joni"}, "ALBUMARTISTS": {"Joni"}, "DISCNUMBER": {"2"}}),
		// Two releases in one folder.
		"rock/Ten/01.flac": mb("11111111-1111-1111-1111-111111111111"),
		"rock/Ten/02.flac": mb("22222222-2222-2222-2222-222222222222"),
		// A playlist folder holds many albums, and none is split.
		"playlists/Mix/01.flac": tagged(map[string][]string{"ALBUM": {"One"}}),
		"playlists/Mix/02.flac": tagged(map[string][]string{"ALBUM": {"Two"}}),
	})
	albums := group(files, cover, tagio.DefaultRules())
	if len(albums) != 6 {
		t.Fatalf("%d albums", len(albums))
	}
	for _, a := range albums {
		_, split := issue(a.Issues, SplitAlbum)
		_, two := issue(a.Issues, TwoReleases)
		switch a.Folder {
		case "folk/Blue":
			if !split || two {
				t.Errorf("Blue: %v", kinds(a.Issues))
			}
		case "rock/Ten":
			if !split || !two {
				t.Errorf("Ten: %v", kinds(a.Issues))
			}
		case "playlists/Mix":
			if split {
				t.Errorf("Mix: %v", kinds(a.Issues))
			}
		}
	}
}

func TestAlbumChecks(t *testing.T) {
	r := tagio.DefaultRules()
	files := indexed(t, map[string]map[string][]string{
		"pop/Hits/01.flac": tagged(map[string][]string{"ALBUMARTIST": {"Various Artists"}, "ALBUMARTISTS": nil, "LABEL": {"A"},
			"MUSICBRAINZ_RELEASEGROUPID": {"11111111-1111-1111-1111-111111111111"}}),
		"pop/Hits/02.flac": tagged(map[string][]string{"ALBUMARTIST": {"Various Artists"}, "ALBUMARTISTS": nil, "LABEL": {"B"},
			"COMPILATION": {"1"}, "TRACKNUMBER": {"2"}, "MUSICBRAINZ_RELEASEGROUPID": {"22222222-2222-2222-2222-222222222222"}}),
	})
	albums := group(files, cover, r)
	if len(albums) != 1 {
		t.Fatalf("%d albums", len(albums))
	}
	a := albums[0]
	want := []Kind{ValuesDiffer, VariousArtists, TwoReleases, NoCover, GuideAlbumArtists}
	if !slices.Equal(kinds(a.Issues), want) {
		t.Errorf("issues %v, want %v", kinds(a.Issues), want)
	}
	if is, _ := issue(a.Issues, ValuesDiffer); !slices.Equal(is.Fields, []string{"compilation", "recordlabel"}) {
		t.Errorf("differ %q", is.Fields)
	}
	if !a.lamp() {
		t.Error("no lamp")
	}
}

func TestCovers(t *testing.T) {
	plain := indexed(t, map[string]map[string][]string{"folk/Blue/01.flac": tagged(nil)})
	embedded := indexed(t, map[string]map[string][]string{"folk/Blue/01.flac": tagged(nil)})
	embedded[0].Facts.HasPicture = true
	noEmbedded := tagio.NewRules(tagio.NavidromeConfig{CoverArtPriority: "folder.*, external"})
	cases := []struct {
		name   string
		files  []*file
		images map[string][]string
		rules  *tagio.Rules
		want   bool
	}{
		{"cover file", plain, map[string][]string{"folk/Blue": {"cover.jpg"}}, tagio.DefaultRules(), true},
		{"an image no pattern names", plain, map[string][]string{"folk/Blue": {"back.jpg"}}, tagio.DefaultRules(), false},
		{"embedded", embedded, nil, tagio.DefaultRules(), true},
		{"embedded but not in the priority", embedded, nil, noEmbedded, false},
		{"folder.png under the owner's priority", plain, map[string][]string{"folk/Blue": {"folder.png"}}, noEmbedded, true},
		{"cover.jpg under a priority without it", plain, map[string][]string{"folk/Blue": {"cover.jpg"}}, noEmbedded, false},
	}
	for _, c := range cases {
		albums := group(c.files, c.images, c.rules)
		if _, none := issue(albums[0].Issues, NoCover); none == c.want {
			t.Errorf("%s: no_cover %v", c.name, none)
		}
	}
}

func TestGroupSkipsUnreadableAndSummarizes(t *testing.T) {
	files := indexed(t, map[string]map[string][]string{
		"folk/Blue/01.flac": tagged(nil),
		"folk/Blue/02.flac": tagged(map[string][]string{"TRACKNUMBER": {"2"}}),
		"folk/Blue/03.flac": tagged(map[string][]string{"TRACKNUMBER": {"3"}}),
	})
	for _, f := range files {
		f.Facts.Codec, f.Facts.BitDepth, f.Facts.SampleRate, f.Facts.LengthMS = "flac", 24, 96000, 1000
		if f.Path == "folk/Blue/03.flac" {
			f.Facts.BitDepth, f.Facts.SampleRate = 16, 44100
		}
	}
	files = append(files, &file{Path: "folk/Blue/04.flac", Error: "broken"})
	albums := group(files, map[string][]string{"folk/Blue": {"cover.jpg"}}, tagio.DefaultRules())
	a := albums[0]
	if len(a.Files) != 3 || a.Quality != "FLAC 24/96" || a.LengthMS != 3000 {
		t.Errorf("album %+v", a)
	}
	if a.lamp() {
		t.Errorf("issues %v", kinds(a.Issues))
	}
}
