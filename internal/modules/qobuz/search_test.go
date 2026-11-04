package qobuz

import (
	"strings"
	"testing"

	"github.com/tikhonp/shelf/internal/source"
)

func TestSearchMapsTypes(t *testing.T) {
	r := newRig(t).up()
	for _, tc := range []struct {
		kind     source.Kind
		typ      string
		id       string
		title    string
		artist   string
		quality  string
		urlStart string
	}{
		{source.KindAlbum, "albums", "5099749522428", "Kind Of Blue", "Miles Davis", "Hi-Res 24/192", "https://www.qobuz.com/"},
		{source.KindTrack, "tracks", "13176083", "So What", "Miles Davis", "Hi-Res 24/192", "https://play.qobuz.com/track/13176083"},
		{source.KindArtist, "artists", "6760", "Miles Davis", "Miles Davis", "", "https://play.qobuz.com/artist/6760"},
		{source.KindPlaylist, "playlists", "17495305", "Qobuz Club: Jazz For A Rainy Day", "Qobuz Club", "", "https://play.qobuz.com/playlist/17495305"},
	} {
		page, err := r.s.Search(bg, source.Query{Kind: tc.kind, Text: "  kind of blue ", Limit: 2, Offset: 1})
		if err != nil {
			t.Fatalf("%s: %v", tc.kind, err)
		}
		if page.Limit != 2 || page.Offset != 1 || page.Total == 0 || len(page.Items) == 0 {
			t.Fatalf("%s: %+v", tc.kind, page)
		}
		calls := r.calls("catalog/search")
		q := calls[len(calls)-1].Query
		if q.Get("type") != tc.typ || q.Get("query") != "kind of blue" || q.Get("limit") != "2" || q.Get("offset") != "1" {
			t.Errorf("%s: asked %v", tc.kind, q)
		}
		// The fake cuts its recorded page by offset, so look at a fresh one.
		page, err = r.s.Search(bg, source.Query{Kind: tc.kind, Text: "kind of blue"})
		if err != nil {
			t.Fatal(err)
		}
		it := page.Items[0]
		if page.Limit != searchLimit || it.Ref != ref(tc.kind, tc.id) || it.Title != tc.title || it.Artist != tc.artist ||
			it.Quality.Badge != tc.quality || !strings.HasPrefix(it.URL, tc.urlStart) || it.CoverURL == "" {
			t.Errorf("%s: %+v", tc.kind, it)
		}
	}
	if _, err := r.s.Search(bg, source.Query{Kind: source.KindLabel, Text: "columbia"}); err == nil {
		t.Error("a search for labels ran")
	}
	if got := r.s.SearchKinds(); len(got) != 4 {
		t.Errorf("SearchKinds = %v", got)
	}
}

func TestQualityBadges(t *testing.T) {
	cd := source.Quality{Codec: "flac", BitDepth: 16, SampleRate: 44100, Badge: "CD 16/44.1"}
	for _, tc := range []struct {
		hires bool
		bits  int
		khz   float64
		want  source.Quality
	}{
		{true, 24, 192, source.Quality{Codec: "flac", BitDepth: 24, SampleRate: 192000, HiRes: true, Badge: "Hi-Res 24/192"}},
		{true, 24, 96, source.Quality{Codec: "flac", BitDepth: 24, SampleRate: 96000, HiRes: true, Badge: "Hi-Res 24/96"}},
		{true, 24, 88.2, source.Quality{Codec: "flac", BitDepth: 24, SampleRate: 88200, HiRes: true, Badge: "Hi-Res 24/88.2"}},
		{true, 24, 44.1, source.Quality{Codec: "flac", BitDepth: 24, SampleRate: 44100, HiRes: true, Badge: "Hi-Res 24/44.1"}},
		{false, 24, 96, cd},
		{false, 16, 44.1, cd},
		{true, 16, 44.1, cd},
		{true, 0, 0, cd},
	} {
		if got := quality(tc.hires, tc.bits, tc.khz); got != tc.want {
			t.Errorf("%v %d %v = %+v, want %+v", tc.hires, tc.bits, tc.khz, got, tc.want)
		}
	}
}

func TestSearchShortQuery(t *testing.T) {
	r := newRig(t).up()
	for _, text := range []string{"", "a", "ab", "  ab  ", "ми"} {
		page, err := r.s.Search(bg, source.Query{Kind: source.KindAlbum, Text: text, Limit: 5, Offset: 10})
		if err != nil || page.Items == nil || len(page.Items) != 0 || page.Total != 0 || page.Limit != 5 || page.Offset != 10 {
			t.Errorf("%q: %+v, %v", text, page, err)
		}
	}
	if n := len(r.calls("catalog/search")); n != 0 {
		t.Fatalf("%d searches reached Qobuz", n)
	}
	// Three letters that take six bytes count as three.
	if _, err := r.s.Search(bg, source.Query{Kind: source.KindAlbum, Text: "мир"}); err != nil {
		t.Fatal(err)
	}
	if n := len(r.calls("catalog/search")); n != 1 {
		t.Fatalf("%d searches for three letters", n)
	}
}
