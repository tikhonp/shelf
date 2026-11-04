package fakesource

import (
	"context"
	"fmt"
	"hash/fnv"
	"net/url"
	"strings"

	"github.com/tikhonp/shelf/internal/source"
)

type album struct {
	id, title, artist, label string
	year                     int
}

// albums are the canned albums search finds. Any other album ID resolves
// too, with a title made from the ID.
var albums = []album{
	{"album-1", "Low Tide Hymns", "Northern Static", "Example Label", 2019},
	{"album-2", "Glass Weather", "Northern Static", "Example Label", 2021},
	{"album-3", "Paper Lanterns", "Harbor Lights", "Quiet Records", 2016},
	{"album-4-slow", "Slow Current", "Harbor Lights", "Quiet Records", 2018},
	{"album-5-big", "Long Distance", "Field Recordings", "Quiet Records", 2020},
	{"album-6-fail", "Broken Signal", "Field Recordings", "Example Label", 2022},
}

type artist struct{ id, name string }

var artists = []artist{
	{"artist-1", "Northern Static"},
	{"artist-2", "Harbor Lights"},
	{"artist-3-empty", "Nobody Yet"},
}

var hiRes = source.Quality{Codec: "flac", BitDepth: 24, SampleRate: 96000, HiRes: true, Badge: "Hi-Res 24/96"}

// cover is a data URL, which the CSP allows, in a color that follows the ID.
func (s *Source) cover(id string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(s.id + id))
	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1 1"><title>%s</title><rect width="1" height="1" fill="hsl(%d 45%% 45%%)"/></svg>`, id, h.Sum32()%360)
	return "data:image/svg+xml," + url.PathEscape(svg)
}

func cannedAlbum(id string) album {
	for _, a := range albums {
		if a.id == id {
			return a
		}
	}
	return album{id: id, title: "Album " + id, artist: "Various Artists", label: "Example Label", year: 2024}
}

func trackCount(id string) int {
	if strings.HasSuffix(id, "-big") {
		return 40
	}
	return 3
}

var trackTitles = []string{"Shoreline", "Undertow", "Breakwater", "Salt", "Low Sun", "Driftwood"}

func (s *Source) albumItem(id string) source.Item {
	a := cannedAlbum(id)
	n := trackCount(id)
	it := source.Item{
		Ref: source.Ref{Source: s.id, Kind: source.KindAlbum, ID: id}, Title: a.title, Artist: a.artist,
		Year: a.year, ReleaseDate: fmt.Sprintf("%d-03-01", a.year), Label: a.label, UPC: "0000000" + fmt.Sprintf("%06d", len(id)),
		Genre: "Ambient", TrackCount: n, Duration: n * 180, CoverURL: s.cover(id), URL: s.Link(source.KindAlbum, id), Quality: hiRes,
	}
	for i := 1; i <= n; i++ {
		it.Tracks = append(it.Tracks, source.Track{
			ID: fmt.Sprintf("%s-t%d", id, i), Position: i, Disc: 1, Title: trackTitles[(i-1)%len(trackTitles)],
			Artist: a.artist, ISRC: fmt.Sprintf("XX00000%05d", i), Duration: 180, Quality: hiRes,
		})
	}
	return it
}

func (s *Source) trackItem(id string) source.Item {
	return source.Item{
		Ref: source.Ref{Source: s.id, Kind: source.KindTrack, ID: id}, Title: "Single " + id, Artist: "Northern Static",
		Album: "Singles", Year: 2023, ISRC: "XX0000099999", Duration: 200, CoverURL: s.cover(id),
		URL: s.Link(source.KindTrack, id), Quality: hiRes, TrackCount: 1,
	}
}

func (s *Source) playlistItem(id string) source.Item {
	it := source.Item{
		Ref: source.Ref{Source: s.id, Kind: source.KindPlaylist, ID: id}, Title: "Playlist " + id, Artist: "Shelf",
		CoverURL: s.cover(id), URL: s.Link(source.KindPlaylist, id), Quality: hiRes,
	}
	for i, a := range albums[:3] {
		it.Tracks = append(it.Tracks, source.Track{
			ID: a.id + "-t1", Position: i + 1, Disc: 1, Title: trackTitles[i], Artist: a.artist, Duration: 180, Quality: hiRes,
		})
	}
	it.TrackCount = len(it.Tracks)
	return it
}

// collection is an artist's or label's albums: the canned ones of a canned
// artist, two made up for any other ID, none for an ID ending in -empty.
func (s *Source) collection(kind source.Kind, id string) source.Item {
	name := "Collection " + id
	for _, a := range artists {
		if a.id == id {
			name = a.name
		}
	}
	it := source.Item{Ref: source.Ref{Source: s.id, Kind: kind, ID: id}, Title: name, URL: s.Link(kind, id)}
	if kind == source.KindArtist {
		it.Artist = name
	}
	sec := source.Section{ID: "albums", Title: "Albums", Items: []source.Item{}}
	if !strings.HasSuffix(id, "-empty") {
		var ids []string
		for _, a := range albums {
			if a.artist == name {
				ids = append(ids, a.id)
			}
		}
		if len(ids) == 0 {
			ids = []string{id + "-a1", id + "-a2"}
		}
		for _, aid := range ids {
			a := s.albumItem(aid)
			a.Tracks = nil
			sec.Items = append(sec.Items, a)
		}
	}
	it.Sections = []source.Section{sec}
	it.TrackCount = len(sec.Items)
	return it
}

// Item resolves any ID of a known kind, except one starting with missing.
func (s *Source) Item(_ context.Context, ref source.Ref) (source.Item, error) {
	s.items.Add(1)
	if s.Fail != nil {
		return source.Item{}, s.Fail
	}
	return s.item(ref)
}

func (s *Source) item(ref source.Ref) (source.Item, error) {
	if strings.HasPrefix(ref.ID, "missing") {
		return source.Item{}, source.ErrNotFound
	}
	switch ref.Kind {
	case source.KindAlbum:
		return s.albumItem(ref.ID), nil
	case source.KindTrack:
		return s.trackItem(ref.ID), nil
	case source.KindPlaylist:
		return s.playlistItem(ref.ID), nil
	case source.KindArtist, source.KindLabel:
		return s.collection(ref.Kind, ref.ID), nil
	}
	return source.Item{}, source.ErrNotFound
}

// SearchKinds are the kinds Search answers.
func (s *Source) SearchKinds() []source.Kind {
	return []source.Kind{source.KindAlbum, source.KindTrack, source.KindArtist, source.KindPlaylist}
}

// Search filters the canned items by the query, in title or artist.
func (s *Source) Search(_ context.Context, q source.Query) (source.Page, error) {
	if s.Fail != nil {
		return source.Page{}, s.Fail
	}
	var all []source.Item
	switch q.Kind {
	case source.KindAlbum:
		for _, a := range albums {
			it := s.albumItem(a.id)
			it.Tracks = nil
			all = append(all, it)
		}
	case source.KindTrack:
		for _, a := range albums {
			al := s.albumItem(a.id)
			for _, t := range al.Tracks[:min(2, len(al.Tracks))] {
				all = append(all, source.Item{
					Ref: source.Ref{Source: s.id, Kind: source.KindTrack, ID: t.ID}, Title: t.Title, Artist: t.Artist,
					Album: al.Title, Year: al.Year, ISRC: t.ISRC, Duration: t.Duration, CoverURL: al.CoverURL,
					URL: s.Link(source.KindTrack, t.ID), Quality: t.Quality, TrackCount: 1,
				})
			}
		}
	case source.KindArtist:
		for _, a := range artists {
			it := s.collection(source.KindArtist, a.id)
			it.Sections = nil
			all = append(all, it)
		}
	case source.KindPlaylist:
		it := s.playlistItem("playlist-1")
		it.Tracks = nil
		all = append(all, it)
	default:
		return source.Page{}, fmt.Errorf("fake: can't search for %q", q.Kind)
	}
	text := strings.ToLower(strings.TrimSpace(q.Text))
	var hits []source.Item
	for _, it := range all {
		if strings.Contains(strings.ToLower(it.Title), text) || strings.Contains(strings.ToLower(it.Artist), text) {
			hits = append(hits, it)
		}
	}
	page := source.Page{Items: []source.Item{}, Total: len(hits), Limit: q.Limit, Offset: q.Offset}
	if q.Offset < len(hits) {
		end := len(hits)
		if q.Limit > 0 {
			end = min(end, q.Offset+q.Limit)
		}
		page.Items = hits[q.Offset:end]
	}
	return page, nil
}
