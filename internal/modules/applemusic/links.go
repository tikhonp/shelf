package applemusic

import (
	"net/url"
	"regexp"
	"strings"

	"github.com/tikhonp/shelf/internal/source"
)

// hosts are the sites whose links are Apple Music's.
var hosts = map[string]bool{
	"music.apple.com": true, "geo.music.apple.com": true, "beta.music.apple.com": true,
	"classical.music.apple.com": true, "itunes.apple.com": true,
}

var (
	numericID         = regexp.MustCompile(`^(?:id)?([0-9]+)$`)
	playlistID        = regexp.MustCompile(`^pl\.[A-Za-z0-9_-]+$`)
	storefrontPattern = regexp.MustCompile(`^[a-zA-Z]{2}$`)
)

// link splits an Apple Music link into its kind segment, its ID and its
// query. ok is false for another site.
func link(raw string) (kind, id string, q url.Values, ok bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || !hosts[strings.ToLower(u.Hostname())] {
		return "", "", nil, false
	}
	var segs []string
	for _, s := range strings.Split(u.Path, "/") {
		if s != "" {
			segs = append(segs, s)
		}
	}
	if len(segs) > 0 && storefrontPattern.MatchString(segs[0]) {
		segs = segs[1:]
	}
	// <kind>/<id> or <kind>/<slug>/<id>
	if len(segs) < 2 || len(segs) > 3 {
		return "", "", nil, false
	}
	return segs[0], segs[len(segs)-1], u.Query(), true
}

// ParseLink reads album, song, playlist and artist links. An album link
// with ?i= is its song.
func (s *Source) ParseLink(raw string) (source.Ref, bool) {
	kind, id, q, ok := link(raw)
	if !ok {
		return source.Ref{}, false
	}
	num := ""
	if m := numericID.FindStringSubmatch(id); m != nil {
		num = m[1]
	}
	switch kind {
	case "album":
		if num == "" {
			return source.Ref{}, false
		}
		if m := numericID.FindStringSubmatch(q.Get("i")); m != nil {
			return s.ref(source.KindTrack, m[1]), true
		}
		return s.ref(source.KindAlbum, num), true
	case "song":
		if num != "" {
			return s.ref(source.KindTrack, num), true
		}
	case "artist":
		if num != "" {
			return s.ref(source.KindArtist, num), true
		}
	case "playlist":
		if playlistID.MatchString(id) {
			return s.ref(source.KindPlaylist, id), true
		}
	}
	return source.Ref{}, false
}

// RefuseLink knows music video and station links, which shelf can't
// download yet.
func (s *Source) RefuseLink(raw string) bool {
	kind, _, _, ok := link(raw)
	return ok && (kind == "music-video" || kind == "station")
}

// downloadLink is the link amdl gets. It names the account's storefront,
// so amdl reads the catalog the account can play.
func downloadLink(sf string, ref source.Ref) string {
	segment := map[source.Kind]string{source.KindAlbum: "album", source.KindTrack: "song", source.KindPlaylist: "playlist"}[ref.Kind]
	return webBase + "/" + url.PathEscape(sf) + "/" + segment + "/" + url.PathEscape(ref.ID)
}
