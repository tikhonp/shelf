package qobuz

import (
	"net/url"
	"regexp"
	"strings"

	qobuzdl "github.com/tikhonp/qobuz-dl-go"
	"github.com/tikhonp/shelf/internal/source"
)

// hosts are the sites whose links are Qobuz's. GetURLInfo's regex matches a
// path on any site, so this check comes first.
var hosts = map[string]bool{
	"qobuz.com": true, "www.qobuz.com": true, "play.qobuz.com": true, "open.qobuz.com": true,
}

var (
	locale  = regexp.MustCompile(`^[a-z]{2}-[a-z]{2}$`)
	numeric = regexp.MustCompile(`^[0-9]+$`)
)

// ParseLink reads the forms GetURLInfo knows, and the site's three it
// doesn't: /interpreter/, /playlists/, and a label's
// /download-streaming-albums/. The ID must be the link's whole last segment,
// because GetURLInfo stops at a dash, so it reads
// /label/<slug>/download-streaming-albums as the ID download, and it matches
// a path anywhere in the text, the query included.
func (s *Source) ParseLink(raw string) (source.Ref, bool) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || !hosts[strings.ToLower(u.Hostname())] {
		return source.Ref{}, false
	}
	segs := segments(u.Path)
	if len(segs) < 2 {
		return source.Ref{}, false
	}
	last := segs[len(segs)-1]
	short := len(segs) <= 3 && numeric.MatchString(last)
	switch {
	case segs[0] == "interpreter" && short:
		return ref(source.KindArtist, last), true
	case segs[0] == "playlists" && short:
		return ref(source.KindPlaylist, last), true
	case len(segs) == 4 && segs[0] == "label" && segs[2] == "download-streaming-albums" && numeric.MatchString(last):
		return ref(source.KindLabel, last), true
	}
	kind, id, err := qobuzdl.GetURLInfo(raw)
	if err != nil || id != last {
		return source.Ref{}, false
	}
	return ref(source.Kind(kind), id), true
}

// segments splits a path and drops the locale.
func segments(path string) []string {
	var segs []string
	for s := range strings.SplitSeq(path, "/") {
		if s != "" {
			segs = append(segs, s)
		}
	}
	if len(segs) > 0 && locale.MatchString(segs[0]) {
		segs = segs[1:]
	}
	return segs
}

func ref(kind source.Kind, id string) source.Ref {
	return source.Ref{Source: ID, Kind: kind, ID: id}
}

// playLink is an item's page in the web player, for the items whose answers
// carry no URL of their own.
func playLink(kind source.Kind, id string) string {
	return "https://play.qobuz.com/" + string(kind) + "/" + id
}
