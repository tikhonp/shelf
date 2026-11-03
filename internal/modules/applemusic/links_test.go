package applemusic

import (
	"testing"

	"github.com/tikhonp/shelf/internal/source"
)

func am(kind source.Kind, id string) source.Ref { return source.Ref{Source: ID, Kind: kind, ID: id} }

func TestParseLinkForms(t *testing.T) {
	s := &Source{}
	for link, want := range map[string]source.Ref{
		"https://music.apple.com/ru/album/low-tide-hymns/1440000000":                     am(source.KindAlbum, "1440000000"),
		"https://music.apple.com/ru/album/1440000000":                                    am(source.KindAlbum, "1440000000"),
		"https://music.apple.com/us/album/low-tide-hymns/1440000000?l=en-GB":             am(source.KindAlbum, "1440000000"),
		"https://music.apple.com/ru/album/low-tide-hymns/1440000000?i=1440000002":        am(source.KindTrack, "1440000002"),
		"https://music.apple.com/album/low-tide-hymns/1440000000/":                       am(source.KindAlbum, "1440000000"),
		"https://itunes.apple.com/ru/album/low-tide-hymns/id1440000000?i=1440000002":     am(source.KindTrack, "1440000002"),
		"http://itunes.apple.com/us/album/id1440000000":                                  am(source.KindAlbum, "1440000000"),
		"https://classical.music.apple.com/ru/album/1440000000":                          am(source.KindAlbum, "1440000000"),
		"  https://MUSIC.apple.com/ru/song/undertow/1440000002  ":                        am(source.KindTrack, "1440000002"),
		"https://music.apple.com/ru/song/1440000002":                                     am(source.KindTrack, "1440000002"),
		"https://music.apple.com/ru/playlist/ambient-essentials/pl.0123456789abcdef":     am(source.KindPlaylist, "pl.0123456789abcdef"),
		"https://music.apple.com/ru/playlist/my-mix/pl.u-AbC_d-1":                        am(source.KindPlaylist, "pl.u-AbC_d-1"),
		"https://music.apple.com/ru/artist/northern-static/111":                          am(source.KindArtist, "111"),
		"https://music.apple.com/ru/artist/111":                                          am(source.KindArtist, "111"),
		"https://beta.music.apple.com/gb/artist/northern-static/111?see-all=full-albums": am(source.KindArtist, "111"),
	} {
		got, ok := s.ParseLink(link)
		if !ok || got != want {
			t.Errorf("%s: %+v %v, want %+v", link, got, ok, want)
		}
	}
	for _, link := range []string{
		"https://open.qobuz.com/album/1440000000",
		"https://music.apple.com/ru/album/low-tide-hymns/",
		"https://music.apple.com/ru/album/low-tide-hymns/abc",
		"https://music.apple.com/ru/playlist/x/12345",
		"https://music.apple.com/ru/artist/x/pl.abc",
		"https://music.apple.com/ru/browse",
		"https://music.apple.com/ru/new",
		"https://music.apple.com/ru/curator/apple-music-ambient/976439528",
		"https://music.apple.com/ru/a/b/c/1",
		"ftp://music.apple.com/ru/album/1",
		"music.apple.com/ru/album/1",
		"https://evil.example/music.apple.com/ru/album/1",
		"https://music.apple.com.evil.example/ru/album/1",
	} {
		if ref, ok := s.ParseLink(link); ok {
			t.Errorf("%s claimed as %+v", link, ref)
		}
	}
}

func TestRefusedLinks(t *testing.T) {
	s := &Source{}
	for _, link := range []string{
		"https://music.apple.com/ru/music-video/shoreline/1500000001",
		"https://music.apple.com/ru/music-video/1500000001",
		"https://music.apple.com/ru/station/northern-static-station/ra.1440000000",
		"https://music.apple.com/station/ra.978194965",
	} {
		if _, ok := s.ParseLink(link); ok || !s.RefuseLink(link) {
			t.Errorf("%s: claimed %v, refused %v", link, ok, s.RefuseLink(link))
		}
	}
	for _, link := range []string{
		"https://music.apple.com/ru/album/1440000000",
		"https://example.com/ru/music-video/x/1",
		"https://music.apple.com/ru/music-video",
	} {
		if s.RefuseLink(link) {
			t.Errorf("%s refused", link)
		}
	}
	// amdl gets the account's storefront and the bare ID.
	for ref, want := range map[source.Ref]string{
		am(source.KindAlbum, "1440000000"):             "https://music.apple.com/ru/album/1440000000",
		am(source.KindTrack, "1440000002"):             "https://music.apple.com/ru/song/1440000002",
		am(source.KindPlaylist, "pl.0123456789abcdef"): "https://music.apple.com/ru/playlist/pl.0123456789abcdef",
	} {
		if got := downloadLink("ru", ref); got != want {
			t.Errorf("%s, want %s", got, want)
		}
	}
}
