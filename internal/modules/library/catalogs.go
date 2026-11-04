package library

import (
	"cmp"
	"context"
	"errors"
	"math"
	"slices"
	"strings"
	"sync"

	"github.com/tikhonp/shelf/internal/source"
)

// How sure a catalog's album is: the one shelf.txt names, or the best of a
// search by the share of words its title and artist share with the
// album's.
const (
	pCatalogRef    = 0.85
	pCatalogSearch = 0.35 // plus up to 0.45 by the match
	minMatch       = 0.7
	catalogResults = 5
)

// catalogSuggestions asks every source that can search and read albums,
// each on its own. A source never learns which album shelf.txt names
// unless the block is its own.
func (m *Module) catalogSuggestions(ctx context.Context, s *subject, ref *provenance, col *collector) {
	if m.sources == nil || len(s.tracks) == 0 {
		return
	}
	var wg sync.WaitGroup
	for _, src := range m.sources.All() {
		if !source.IsEnabled(src) {
			continue
		}
		res, ok := src.(source.Resolver)
		if !ok {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			it, p, err := m.catalogAlbum(ctx, src, res, s, ref)
			if err != nil {
				msg := src.Name() + " doesn't answer, so its suggestions are missing."
				var un *source.Unavailable
				if errors.As(err, &un) {
					msg = src.Name() + ": " + un.Reason
				} else {
					m.log.Warn("library: catalog", "source", src.ID(), "album", s.row.ID, "error", err)
				}
				col.fail(src.Name(), msg)
				return
			}
			if it == nil {
				return
			}
			pairs, fit, ok := matchItem(s, it)
			if !ok {
				return
			}
			p *= fit
			col.add(itemSuggestions(s, it, pairs, src.Name(), p)...)
			m.offerCover(ctx, col, it.CoverURL, "", src.Name(), p, "")
		}()
	}
	wg.Wait()
}

// catalogAlbum finds the album in one source: by the ID its shelf.txt
// block names, else by a search for its artist and title.
func (m *Module) catalogAlbum(ctx context.Context, src source.Source, res source.Resolver, s *subject, ref *provenance) (*source.Item, float64, error) {
	if ref != nil && ref.Kind == string(source.KindAlbum) && ref.Source == src.Name() && ref.SourceID != "" {
		it, err := res.Item(ctx, source.Ref{Source: src.ID(), Kind: source.KindAlbum, ID: ref.SourceID})
		if err == nil {
			return &it, pCatalogRef, nil
		}
		if !errors.Is(err, source.ErrNotFound) {
			return nil, 0, err
		}
	}
	se, ok := src.(source.Searcher)
	if !ok || !slices.Contains(se.SearchKinds(), source.KindAlbum) {
		return nil, 0, nil
	}
	album, artist := s.album(), s.albumArtist()
	if album == "" {
		return nil, 0, nil
	}
	q := album
	if artist != "" && !s.compilation {
		q = artist + " " + album
	}
	page, err := se.Search(ctx, source.Query{Kind: source.KindAlbum, Text: q, Limit: catalogResults})
	if err != nil {
		return nil, 0, err
	}
	best, bestScore := source.Item{}, 0.0
	for _, h := range page.Items {
		if h.TrackCount != 0 && h.TrackCount != len(s.tracks) {
			continue
		}
		title := h.Title
		score := similarity(title, album)
		if h.Version != "" {
			score = math.Max(score, similarity(title+" "+h.Version, album))
		}
		if !s.compilation {
			score = 0.6*score + 0.4*similarity(h.Artist, artist)
		}
		if score > bestScore {
			best, bestScore = h, score
		}
	}
	if bestScore < minMatch {
		return nil, 0, nil
	}
	it, err := res.Item(ctx, best.Ref)
	if errors.Is(err, source.ErrNotFound) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	return &it, pCatalogSearch + 0.45*bestScore, nil
}

// matchItem pairs the album's tracks with a catalog album's, in order,
// and checks their lengths as an anchor's are.
func matchItem(s *subject, it *source.Item) (map[int64]source.Track, float64, bool) {
	if len(it.Tracks) != len(s.tracks) {
		return nil, 0, false
	}
	tracks := slices.Clone(it.Tracks)
	slices.SortStableFunc(tracks, func(a, b source.Track) int {
		return cmp.Or(cmp.Compare(max(a.Disc, 1), max(b.Disc, 1)), cmp.Compare(a.Position, b.Position))
	})
	pairs := map[int64]source.Track{}
	var sum float64
	n := 0
	for i, t := range s.tracks {
		ct := tracks[i]
		pairs[t.ID] = ct
		if ct.Duration == 0 || t.LengthMS == 0 {
			continue
		}
		d := math.Abs(float64(ct.Duration) - float64(t.LengthMS)/1000)
		// Catalogs give whole seconds.
		d = math.Max(0, d-1)
		if d > maxTrackDiff {
			return nil, 0, false
		}
		sum += d
		n++
	}
	mean := 0.0
	if n > 0 {
		mean = sum / float64(n)
	}
	if mean > maxMeanDiff {
		return nil, 0, false
	}
	return pairs, math.Max(0.85, 1-0.05*mean), true
}

// itemSuggestions are a catalog album's fields. Its release fields stay
// behind an anchor's, so a matched MusicBrainz release always wins them.
func itemSuggestions(s *subject, it *source.Item, pairs map[int64]source.Track, name string, p float64) []cand {
	var out []cand
	album := func(field string, v string) {
		out = append(out, cand{field: field, values: []string{v}, source: name, p: p})
	}
	album("album", it.Title)
	if it.Version != "" {
		album("albumversion", it.Version)
	}
	album("albumartist", it.Artist)
	album("releasedate", it.ReleaseDate)
	album("recordlabel", it.Label)
	album("barcode", it.UPC)
	album("genre", it.Genre)
	for _, t := range s.tracks {
		ct := pairs[t.ID]
		title := ct.Title
		if ct.Version != "" && !strings.Contains(foldText(title), foldText(ct.Version)) {
			title += " (" + ct.Version + ")"
		}
		out = append(out,
			cand{track: t.ID, field: "title", values: []string{title}, source: name, p: p},
			cand{track: t.ID, field: "artist", values: []string{ct.Artist}, source: name, p: p},
			cand{track: t.ID, field: "isrc", values: []string{ct.ISRC}, source: name, p: p})
	}
	return out
}
