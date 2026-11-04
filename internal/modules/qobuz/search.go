package qobuz

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	qobuzdl "github.com/tikhonp/qobuz-dl-go"
	"github.com/tikhonp/shelf/internal/source"
)

const (
	// minQuery is qobuz-dl's shortest search. The typed search doesn't check.
	minQuery = 3
	// searchLimit is the page size of a query that names none.
	searchLimit = 20
)

// SearchKinds are albums, tracks, artists and playlists.
func (s *Source) SearchKinds() []source.Kind {
	return []source.Kind{source.KindAlbum, source.KindTrack, source.KindArtist, source.KindPlaylist}
}

// Search asks Qobuz's catalog for one kind.
func (s *Source) Search(ctx context.Context, q source.Query) (source.Page, error) {
	switch q.Kind {
	case source.KindAlbum, source.KindTrack, source.KindArtist, source.KindPlaylist:
	default:
		return source.Page{}, fmt.Errorf("qobuz: can't search for %q", q.Kind)
	}
	q.Text = strings.TrimSpace(q.Text)
	if utf8.RuneCountInString(q.Text) < minQuery {
		return source.Page{Items: []source.Item{}, Limit: q.Limit, Offset: q.Offset}, nil
	}
	if q.Limit <= 0 {
		q.Limit = searchLimit
	}
	c, err := s.client(ctx)
	if err != nil {
		return source.Page{}, err
	}
	switch q.Kind {
	case source.KindAlbum:
		return search(ctx, s, q, c.SearchAlbums, albumItem)
	case source.KindTrack:
		return search(ctx, s, q, c.SearchTracks, trackItem)
	case source.KindArtist:
		return search(ctx, s, q, c.SearchArtists, artistItem)
	}
	return search(ctx, s, q, c.SearchPlaylists, playlistItem)
}

type finder[T any] func(ctx context.Context, query string, limit, offset int) (*qobuzdl.SearchPage[T], error)

func search[T any](ctx context.Context, s *Source, q source.Query, find finder[T], item func(*T) source.Item) (source.Page, error) {
	p, err := find(ctx, q.Text, q.Limit, q.Offset)
	if err != nil {
		return source.Page{}, s.failed(err)
	}
	page := source.Page{Items: make([]source.Item, len(p.Items)), Total: p.Total, Limit: q.Limit, Offset: q.Offset}
	for i := range p.Items {
		page.Items[i] = item(&p.Items[i])
	}
	return page, nil
}
