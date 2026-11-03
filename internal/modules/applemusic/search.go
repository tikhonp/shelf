package applemusic

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/tikhonp/shelf/internal/source"
)

// searchMax is the most results Apple's search gives per type.
const searchMax = 25

var searchTypes = map[source.Kind]string{
	source.KindAlbum:    typeAlbums,
	source.KindTrack:    typeSongs,
	source.KindArtist:   typeArtists,
	source.KindPlaylist: typePlaylists,
}

// SearchKinds are albums, tracks, artists and playlists.
func (s *Source) SearchKinds() []source.Kind {
	return []source.Kind{source.KindAlbum, source.KindTrack, source.KindArtist, source.KindPlaylist}
}

// Search asks Apple's catalog search for one kind. Apple gives no total:
// while it has a next page, Total counts one more than the items so far.
func (s *Source) Search(ctx context.Context, q source.Query) (source.Page, error) {
	typ, ok := searchTypes[q.Kind]
	if !ok {
		return source.Page{}, fmt.Errorf("apple music: can't search for %q", q.Kind)
	}
	page := source.Page{Items: []source.Item{}, Limit: q.Limit, Offset: q.Offset, Total: q.Offset}
	text := strings.TrimSpace(q.Text)
	if text == "" {
		return page, nil
	}
	limit := q.Limit
	if limit <= 0 || limit > searchMax {
		limit = searchMax
	}
	var doc document
	err := s.catalog.get(ctx, "search", url.Values{
		"term": {text}, "types": {typ}, "limit": {strconv.Itoa(limit)}, "offset": {strconv.Itoa(q.Offset)},
	}, &doc)
	if err != nil {
		return source.Page{}, err
	}
	res := doc.Results[typ]
	for _, r := range res.Data {
		switch r.Type {
		case typeAlbums:
			page.Items = append(page.Items, s.albumItem(r))
		case typeSongs:
			page.Items = append(page.Items, s.songItem(r))
		case typeArtists:
			page.Items = append(page.Items, s.artistItem(r))
		case typePlaylists:
			page.Items = append(page.Items, s.playlistItem(r))
		}
	}
	page.Total = q.Offset + len(page.Items)
	if res.Next != "" {
		page.Total++
	}
	return page, nil
}
