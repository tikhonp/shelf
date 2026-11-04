package qobuz

import (
	"testing"

	"github.com/tikhonp/shelf/internal/source"
)

func TestParseLinkForms(t *testing.T) {
	s := New(nil).Source()
	for _, tc := range []struct {
		link string
		kind source.Kind
		id   string
	}{
		{"https://play.qobuz.com/album/0634904080853", source.KindAlbum, "0634904080853"},
		{"https://open.qobuz.com/album/evz7usmruz35a", source.KindAlbum, "evz7usmruz35a"},
		{"https://play.qobuz.com/track/20273753", source.KindTrack, "20273753"},
		{"https://open.qobuz.com/playlist/2082798", source.KindPlaylist, "2082798"},
		{"https://play.qobuz.com/artist/50782", source.KindArtist, "50782"},
		{"https://open.qobuz.com/label/572190", source.KindLabel, "572190"},
		{"https://www.qobuz.com/album/com-lag-2-25-radiohead/0634904080853", source.KindAlbum, "0634904080853"},
		{"https://www.qobuz.com/us-en/album/com-lag-2-25-radiohead/0634904080853", source.KindAlbum, "0634904080853"},
		{"https://www.qobuz.com/fr-fr/track/so-what/20273753", source.KindTrack, "20273753"},
		{"https://www.qobuz.com/gb-en/playlist/top-10-operas/2082798", source.KindPlaylist, "2082798"},
		{"https://www.qobuz.com/us-en/artist/tord-gustavsen-trio/50782", source.KindArtist, "50782"},
		{"https://www.qobuz.com/us-en/label/naked-jain-records/572190", source.KindLabel, "572190"},
		// The site's forms that upstream's regex misses.
		{"https://www.qobuz.com/us-en/interpreter/tord-gustavsen-trio/50782", source.KindArtist, "50782"},
		{"https://www.qobuz.com/interpreter/tord-gustavsen-trio/50782", source.KindArtist, "50782"},
		{"https://www.qobuz.com/us-en/playlists/top-10-operas/2082798", source.KindPlaylist, "2082798"},
		{"https://www.qobuz.com/playlists/2082798", source.KindPlaylist, "2082798"},
		{"https://www.qobuz.com/us-en/label/naked-jain-records/download-streaming-albums/572190", source.KindLabel, "572190"},
		// What people paste.
		{"  https://play.qobuz.com/album/0634904080853\n", source.KindAlbum, "0634904080853"},
		{"https://play.qobuz.com/album/0634904080853/", source.KindAlbum, "0634904080853"},
		{"https://play.qobuz.com/album/0634904080853?utm_source=share#top", source.KindAlbum, "0634904080853"},
		{"http://WWW.Qobuz.com/album/com-lag/0634904080853", source.KindAlbum, "0634904080853"},
		{"https://qobuz.com/album/com-lag/0634904080853", source.KindAlbum, "0634904080853"},
	} {
		got, ok := s.ParseLink(tc.link)
		if want := ref(tc.kind, tc.id); !ok || got != want {
			t.Errorf("%q = %+v, %v, want %+v", tc.link, got, ok, want)
		}
	}
}

func TestParseLinkOtherSites(t *testing.T) {
	s := New(nil).Source()
	for _, link := range []string{
		"",
		"not a link",
		"/album/slug/0634904080853",
		"ftp://www.qobuz.com/album/slug/0634904080853",
		"https://music.apple.com/ru/album/in-rainbows/1109714933",
		// GetURLInfo's regex alone would claim each of these.
		"https://example.com/album/slug/0634904080853",
		"https://www.qobuz.com.example.com/album/0634904080853",
		"https://example.com/www.qobuz.com/album/0634904080853",
		"https://example.com/?u=https://play.qobuz.com/album/0634904080853",
		// Qobuz's site, but no item.
		"https://www.qobuz.com/",
		"https://www.qobuz.com/us-en/discover",
		"https://www.qobuz.com/us-en/shop?next=/album/slug/0634904080853",
		"https://www.qobuz.com/us-en/album/slug/0634904080853/reviews",
		"https://www.qobuz.com/us-en/label/slug/download-streaming-albums",
		"https://www.qobuz.com/us-en/interpreter/slug/download-streaming-albums",
		"https://www.qobuz.com/us-en/playlists/category/classical",
	} {
		if got, ok := s.ParseLink(link); ok {
			t.Errorf("%q claimed as %+v", link, got)
		}
	}
}
