package qobuz

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/tikhonp/qobuz-dl-go/qobuzdltest"
	"github.com/tikhonp/shelf/internal/source"
)

func TestResolveKinds(t *testing.T) {
	r := newRig(t).up()

	a := r.item(source.KindAlbum, qobuzdltest.AlbumMultiDisc)
	if a.Title != "Ummagumma" || a.Artist != "Pink Floyd" || a.Label != "Pink Floyd Records" || a.UPC != "0886445613223" ||
		a.ReleaseDate != "1969-10-25" || a.Year != 1969 || a.Genre != "Rock" || a.TrackCount != 16 ||
		a.Quality.Badge != "Hi-Res 24/192" || !strings.HasPrefix(a.URL, "https://www.qobuz.com/") || a.CoverURL == "" {
		t.Errorf("album %+v", a)
	}
	if len(a.Tracks) != 16 {
		t.Fatalf("%d tracks", len(a.Tracks))
	}
	first, last := a.Tracks[0], a.Tracks[15]
	if first.Position != 1 || first.Disc != 1 || first.ID != "47683510" || first.ISRC != "GBN9Y1100044" || first.Version != "Live" ||
		first.Artist != "Pink Floyd" || first.Quality.Badge != "Hi-Res 24/192" {
		t.Errorf("first track %+v", first)
	}
	if last.Position != 16 || last.Disc != 2 {
		t.Errorf("last track %+v", last)
	}

	tr := r.item(source.KindTrack, qobuzdltest.TrackSingle)
	if tr.Title != "So What" || tr.Version != "Mono Version" || tr.Artist != "Miles Davis" || tr.Album != "Kind of Blue (Mono)" ||
		tr.ISRC != "USSM11304993" || tr.Label != "Columbia/Legacy" || tr.ReleaseDate != "1959-08-17" ||
		tr.URL != "https://play.qobuz.com/track/20273753" || tr.CoverURL == "" {
		t.Errorf("track %+v", tr)
	}

	p := r.item(source.KindPlaylist, qobuzdltest.Playlist)
	if p.Title != "Top 10 Operas" || p.Artist != "gramophonemagazine" || p.TrackCount != 10 || len(p.Tracks) != 10 ||
		p.Tracks[9].Position != 10 || p.Tracks[0].Disc != 0 || p.Tracks[0].Artist != "Véronique Gens" {
		t.Errorf("playlist %+v", p)
	}
	if !strings.HasSuffix(p.CoverURL, "_300.jpg") {
		t.Errorf("playlist cover %s", p.CoverURL)
	}

	long := r.item(source.KindPlaylist, qobuzdltest.PlaylistLong)
	if len(long.Tracks) != 120 || long.Tracks[119].Position != 120 {
		t.Errorf("a long playlist has %d tracks", len(long.Tracks))
	}

	for _, tc := range []struct {
		kind  source.Kind
		id    string
		title string
		n     int
	}{
		{source.KindArtist, qobuzdltest.ArtistAlbums, "Norah Jones", 3},
		{source.KindArtist, qobuzdltest.Artist, "Tord Gustavsen Trio", 8},
		{source.KindArtist, qobuzdltest.ArtistLong, "", 120},
		{source.KindLabel, qobuzdltest.Label, "Naked Jain Records", 15},
	} {
		it := r.item(tc.kind, tc.id)
		if tc.title != "" && it.Title != tc.title {
			t.Errorf("%s %s title %q", tc.kind, tc.id, it.Title)
		}
		if len(it.Sections) != 1 || it.Sections[0].ID != "albums" || len(it.Sections[0].Items) != tc.n {
			t.Fatalf("%s %s sections %+v", tc.kind, tc.id, it.Sections)
		}
		items := it.Sections[0].Items
		if items[0].Ref.Source != ID || items[0].Ref.Kind != source.KindAlbum {
			t.Errorf("%s %s album %+v", tc.kind, tc.id, items[0].Ref)
		}
		dates := make([]string, len(items))
		for i := range items {
			dates[i] = items[i].ReleaseDate
		}
		if !slices.IsSortedFunc(dates, func(a, b string) int { return strings.Compare(b, a) }) {
			t.Errorf("%s %s dates %v", tc.kind, tc.id, dates)
		}
	}
}

func TestResolveEmptyCollection(t *testing.T) {
	r := newRig(t).up()
	for _, tc := range []struct {
		kind source.Kind
		id   string
	}{{source.KindArtist, qobuzdltest.ArtistEmpty}, {source.KindLabel, qobuzdltest.LabelEmpty}} {
		it := r.item(tc.kind, tc.id)
		if it.Ref != ref(tc.kind, tc.id) || len(it.Sections) != 1 || it.Sections[0].ID != "albums" || len(it.Sections[0].Items) != 0 {
			t.Errorf("%s: %+v", tc.kind, it)
		}
	}
	it := r.item(source.KindPlaylist, qobuzdltest.PlaylistEmpty)
	if it.Ref != ref(source.KindPlaylist, qobuzdltest.PlaylistEmpty) || len(it.Tracks) != 0 {
		t.Errorf("playlist: %+v", it)
	}
}

func TestResolveUnknownItem(t *testing.T) {
	r := newRig(t).up()
	for _, k := range source.Kinds {
		if _, err := r.s.Item(bg, ref(k, "nothing-here")); !errors.Is(err, source.ErrNotFound) {
			t.Errorf("%s: %v", k, err)
		}
	}
}
