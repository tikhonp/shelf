package applemusic

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/tikhonp/shelf/internal/source"
)

// Resource types in Apple's answers.
const (
	typeSongs     = "songs"
	typeAlbums    = "albums"
	typeArtists   = "artists"
	typePlaylists = "playlists"
)

// qualityFromTraits reads an item's audioTraits. Apple doesn't say an
// item's exact bit depth or sample rate, so the badge says the tier.
func qualityFromTraits(traits []string) source.Quality {
	if len(traits) == 0 {
		return source.Quality{}
	}
	q := source.Quality{Atmos: slices.Contains(traits, "atmos")}
	switch {
	case slices.Contains(traits, "hi-res-lossless"):
		q.Codec, q.HiRes, q.Badge = "alac", true, "Hi-Res Lossless"
	case slices.Contains(traits, "lossless"):
		q.Codec, q.Badge = "alac", "Lossless"
	default:
		q.Codec, q.Badge = "aac", "AAC"
	}
	return q
}

func year(date string) int {
	if len(date) < 4 {
		return 0
	}
	y, _ := strconv.Atoi(date[:4])
	return y
}

func seconds(ms int64) int { return int((ms + 500) / 1000) }

// genre is the first genre other than Apple's catch-all "Music".
func genre(names []string) string {
	for _, n := range names {
		if n != "Music" {
			return n
		}
	}
	return ""
}

func (s *Source) ref(kind source.Kind, id string) source.Ref {
	return source.Ref{Source: ID, Kind: kind, ID: id}
}

func (s *Source) albumItem(r resource) source.Item {
	a := r.Attributes
	return source.Item{
		Ref: s.ref(source.KindAlbum, r.ID), Title: a.Name, Artist: a.ArtistName,
		Year: year(a.ReleaseDate), ReleaseDate: a.ReleaseDate, Label: a.RecordLabel, UPC: a.UPC,
		Genre: genre(a.GenreNames), Explicit: a.ContentRating == "explicit", TrackCount: a.TrackCount,
		CoverURL: a.Artwork.cover(), URL: a.URL, Quality: qualityFromTraits(a.AudioTraits),
	}
}

func (s *Source) songItem(r resource) source.Item {
	a := r.Attributes
	return source.Item{
		Ref: s.ref(source.KindTrack, r.ID), Title: a.Name, Artist: a.ArtistName, Album: a.AlbumName,
		Year: year(a.ReleaseDate), ReleaseDate: a.ReleaseDate, ISRC: a.ISRC, Genre: genre(a.GenreNames),
		Explicit: a.ContentRating == "explicit", TrackCount: 1, Duration: seconds(a.DurationInMillis),
		CoverURL: a.Artwork.cover(), URL: a.URL, Quality: qualityFromTraits(a.AudioTraits),
	}
}

func (s *Source) artistItem(r resource) source.Item {
	a := r.Attributes
	return source.Item{
		Ref: s.ref(source.KindArtist, r.ID), Title: a.Name, Artist: a.Name, Genre: genre(a.GenreNames),
		CoverURL: a.Artwork.cover(), URL: a.URL,
	}
}

func (s *Source) playlistItem(r resource) source.Item {
	a := r.Attributes
	return source.Item{
		Ref: s.ref(source.KindPlaylist, r.ID), Title: a.Name, Artist: a.CuratorName,
		CoverURL: a.Artwork.cover(), URL: a.URL,
	}
}

func songTrack(r resource, position int) source.Track {
	a := r.Attributes
	return source.Track{
		ID: r.ID, Position: position, Disc: a.DiscNumber, Title: a.Name, Artist: a.ArtistName, ISRC: a.ISRC,
		Explicit: a.ContentRating == "explicit", Duration: seconds(a.DurationInMillis), Quality: qualityFromTraits(a.AudioTraits),
	}
}

// release is an item with its tracks, and how amdl numbers them: amdl
// counts every item of an album or playlist, music videos too, while shelf
// lists only the songs.
type release struct {
	item source.Item
	// positions maps amdl's item number to a track's position.
	positions map[int]int
}

// withTracks fills an album or playlist item with its songs.
func withTracks(it source.Item, items []resource) release {
	rel := release{item: it, positions: map[int]int{}}
	dur := 0
	for i, r := range items {
		if r.Type != typeSongs {
			continue
		}
		t := songTrack(r, len(rel.item.Tracks)+1)
		rel.item.Tracks = append(rel.item.Tracks, t)
		rel.positions[i+1] = t.Position
		dur += t.Duration
	}
	rel.item.Duration = dur
	rel.item.TrackCount = len(rel.item.Tracks)
	return rel
}

// Item reads an album or playlist with its songs, a song, or an artist with
// its albums in sections.
func (s *Source) Item(ctx context.Context, ref source.Ref) (source.Item, error) {
	if ref.Kind == source.KindArtist {
		return s.artist(ctx, ref.ID)
	}
	rel, err := s.release(ctx, ref)
	return rel.item, err
}

func (s *Source) release(ctx context.Context, ref source.Ref) (release, error) {
	switch ref.Kind {
	case source.KindAlbum:
		return s.album(ctx, ref.ID)
	case source.KindPlaylist:
		return s.playlist(ctx, ref.ID)
	case source.KindTrack:
		return s.song(ctx, ref.ID)
	}
	return release{}, source.ErrNotFound
}

func first(doc document) (resource, error) {
	if len(doc.Data) == 0 {
		return resource{}, source.ErrNotFound
	}
	return doc.Data[0], nil
}

func (s *Source) album(ctx context.Context, id string) (release, error) {
	var doc document
	if err := s.catalog.get(ctx, "albums/"+url.PathEscape(id), url.Values{"include": {"tracks,artists"}}, &doc); err != nil {
		return release{}, err
	}
	r, err := first(doc)
	if err != nil {
		return release{}, err
	}
	tracks, err := s.catalog.pages(ctx, r.Relationships["tracks"])
	if err != nil {
		return release{}, err
	}
	return withTracks(s.albumItem(r), tracks), nil
}

func (s *Source) playlist(ctx context.Context, id string) (release, error) {
	var doc document
	if err := s.catalog.get(ctx, "playlists/"+url.PathEscape(id), url.Values{"include": {"tracks"}}, &doc); err != nil {
		return release{}, err
	}
	r, err := first(doc)
	if err != nil {
		return release{}, err
	}
	tracks, err := s.catalog.pages(ctx, r.Relationships["tracks"])
	if err != nil {
		return release{}, err
	}
	return withTracks(s.playlistItem(r), tracks), nil
}

// song reads one song. amdl downloads it from its album, so its own item
// number is the album's; any of them is the one track.
func (s *Source) song(ctx context.Context, id string) (release, error) {
	var doc document
	if err := s.catalog.get(ctx, "songs/"+url.PathEscape(id), url.Values{"include": {"albums,artists"}}, &doc); err != nil {
		return release{}, err
	}
	r, err := first(doc)
	if err != nil {
		return release{}, err
	}
	it := s.songItem(r)
	if albums := r.Relationships["albums"].Data; len(albums) > 0 {
		it.Label = albums[0].Attributes.RecordLabel
	}
	t := songTrack(r, 1)
	it.Tracks = []source.Track{t}
	return release{item: it, positions: nil}, nil
}

// Artist views, in the order of an artist's sections.
var views = []struct{ view, id, title string }{
	{"full-albums", "albums", "Albums"},
	{"singles", "eps", "EPs and singles"},
	{"compilation-albums", "compilations", "Compilations"},
	{"appears-on-albums", "appears_on", "Appears on"},
}

func viewNames() string {
	names := make([]string, len(views))
	for i, v := range views {
		names[i] = v.view
	}
	return strings.Join(names, ",")
}

// artist reads an artist's albums through its views, or through
// /artists/{id}/albums when Apple refuses the views.
func (s *Source) artist(ctx context.Context, id string) (source.Item, error) {
	var doc document
	err := s.catalog.get(ctx, "artists/"+url.PathEscape(id), url.Values{"views": {viewNames()}}, &doc)
	var se *statusError
	if errors.As(err, &se) && (se.status == http.StatusBadRequest || se.status == http.StatusForbidden) {
		return s.artistFallback(ctx, id)
	}
	if err != nil {
		return source.Item{}, err
	}
	r, err := first(doc)
	if err != nil {
		return source.Item{}, err
	}
	if len(r.Views) == 0 {
		return s.artistFallback(ctx, id)
	}
	it := s.artistItem(r)
	n := 0
	for _, v := range views {
		data, err := s.catalog.pages(ctx, r.Views[v.view])
		if err != nil {
			return source.Item{}, err
		}
		sec := source.Section{ID: v.id, Title: v.title, Items: []source.Item{}}
		for _, a := range data {
			if a.Type == typeAlbums {
				sec.Items = append(sec.Items, s.albumItem(a))
			}
		}
		n += len(sec.Items)
		it.Sections = append(it.Sections, sec)
	}
	it.TrackCount = n
	return it, nil
}

// artistFallbackLimit is the page size of /artists/{id}/albums.
const artistFallbackLimit = 100

func (s *Source) artistFallback(ctx context.Context, id string) (source.Item, error) {
	var doc document
	if err := s.catalog.get(ctx, "artists/"+url.PathEscape(id), nil, &doc); err != nil {
		return source.Item{}, err
	}
	r, err := first(doc)
	if err != nil {
		return source.Item{}, err
	}
	it := s.artistItem(r)
	secs := map[string]*source.Section{}
	for _, v := range views[:3] {
		secs[v.id] = &source.Section{ID: v.id, Title: v.title, Items: []source.Item{}}
	}
	n := 0
	for offset := 0; ; offset += artistFallbackLimit {
		var page document
		q := url.Values{"limit": {strconv.Itoa(artistFallbackLimit)}, "offset": {strconv.Itoa(offset)}}
		if err := s.catalog.get(ctx, "artists/"+url.PathEscape(id)+"/albums", q, &page); err != nil {
			return source.Item{}, err
		}
		for _, a := range page.Data {
			sec := secs["albums"]
			switch {
			case a.Attributes.IsCompilation:
				sec = secs["compilations"]
			case a.Attributes.IsSingle:
				sec = secs["eps"]
			}
			sec.Items = append(sec.Items, s.albumItem(a))
			n++
		}
		if page.Next == "" || len(page.Data) == 0 || offset/artistFallbackLimit >= maxPages {
			break
		}
	}
	for _, v := range views[:3] {
		it.Sections = append(it.Sections, *secs[v.id])
	}
	it.TrackCount = n
	return it, nil
}
