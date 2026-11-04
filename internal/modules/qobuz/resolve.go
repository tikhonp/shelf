package qobuz

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"

	qobuzdl "github.com/tikhonp/qobuz-dl-go"
	"github.com/tikhonp/shelf/internal/source"
)

// hz is a Qobuz sampling rate, which is in kHz.
func hz(khz float64) int { return int(math.Round(khz * 1000)) }

// quality is what an item offers. Qobuz's maximums count only for what it
// calls hi-res; everything else is CD quality.
func quality(hires bool, bits int, khz float64) source.Quality {
	if !hires || bits <= 16 || khz <= 0 {
		return source.Quality{Codec: "flac", BitDepth: 16, SampleRate: 44100, Badge: "CD 16/44.1"}
	}
	rate := hz(khz)
	return source.Quality{Codec: "flac", BitDepth: bits, SampleRate: rate, HiRes: true,
		Badge: "Hi-Res " + strconv.Itoa(bits) + "/" + source.KHz(rate)}
}

func year(date string) int {
	if len(date) < 4 {
		return 0
	}
	y, _ := strconv.Atoi(date[:4])
	return y
}

func cover(i qobuzdl.Image) string { return cmp.Or(i.Large, i.Small, i.Thumbnail) }

func albumItem(a *qobuzdl.Album) source.Item {
	return source.Item{
		Ref: ref(source.KindAlbum, a.ID), Title: a.Title, Version: a.Version, Artist: a.Artist.Name,
		Year: year(a.ReleaseDateOriginal), ReleaseDate: a.ReleaseDateOriginal, Label: a.Label.Name, UPC: a.UPC,
		Genre: a.Genre, Explicit: a.ParentalWarning, TrackCount: a.TracksCount, Duration: a.Duration,
		CoverURL: cover(a.Image), URL: cmp.Or(a.URL, playLink(source.KindAlbum, a.ID)),
		Quality: quality(a.HiresStreamable, a.MaximumBitDepth, a.MaximumSamplingRate),
	}
}

func trackItem(t *qobuzdl.Track) source.Item {
	it := source.Item{
		Ref: ref(source.KindTrack, t.ID), Title: t.Title, Version: t.Version, Artist: t.Performer.Name,
		Year: year(t.ReleaseDateOriginal), ReleaseDate: t.ReleaseDateOriginal, ISRC: t.ISRC,
		Explicit: t.ParentalWarning, TrackCount: 1, Duration: t.Duration, URL: playLink(source.KindTrack, t.ID),
		Quality: quality(t.HiresStreamable, t.MaximumBitDepth, t.MaximumSamplingRate),
	}
	if a := t.Album; a != nil {
		it.Album, it.Label, it.Genre, it.CoverURL = a.Title, a.Label.Name, a.Genre, cover(a.Image)
		it.Artist = cmp.Or(it.Artist, a.Artist.Name)
		it.ReleaseDate = cmp.Or(it.ReleaseDate, a.ReleaseDateOriginal)
		it.Year = year(it.ReleaseDate)
	}
	return it
}

func artistItem(a *qobuzdl.Artist) source.Item {
	return source.Item{
		Ref: ref(source.KindArtist, a.ID), Title: a.Name, Artist: a.Name, CoverURL: cover(a.Image),
		URL: playLink(source.KindArtist, a.ID),
	}
}

func playlistItem(p *qobuzdl.Playlist) source.Item {
	return source.Item{
		Ref: ref(source.KindPlaylist, p.ID), Title: p.Name, Artist: p.Owner, TrackCount: p.TracksCount,
		Duration: p.Duration, CoverURL: playlistCover(p), URL: playLink(source.KindPlaylist, p.ID),
	}
}

// playlistCover prefers the answer's images300: the view's Images are 50
// pixels wide.
func playlistCover(p *qobuzdl.Playlist) string {
	var raw struct {
		Images []string `json:"images300"`
	}
	if json.Unmarshal(p.Raw, &raw) == nil && len(raw.Images) > 0 {
		return raw.Images[0]
	}
	if len(p.Images) > 0 {
		return p.Images[0]
	}
	return ""
}

func labelItem(l *qobuzdl.Label) source.Item {
	return source.Item{Ref: ref(source.KindLabel, l.ID), Title: l.Name, URL: playLink(source.KindLabel, l.ID)}
}

// sourceTrack is a track at its position in a release, with fallback as the
// artist of a track that names none.
func sourceTrack(t *qobuzdl.Track, pos int, fallback string) source.Track {
	return source.Track{
		ID: t.ID, Position: pos, Disc: t.MediaNumber, Title: t.Title, Version: t.Version,
		Artist: cmp.Or(t.Performer.Name, fallback), ISRC: t.ISRC, Explicit: t.ParentalWarning, Duration: t.Duration,
		Quality: quality(t.HiresStreamable, t.MaximumBitDepth, t.MaximumSamplingRate),
	}
}

// albumTracks numbers tracks as qobuz-dl-go does: by their place in the
// album's list, across discs.
func albumTracks(a *qobuzdl.Album) []source.Track {
	out := make([]source.Track, len(a.Tracks))
	for i := range a.Tracks {
		out[i] = sourceTrack(&a.Tracks[i], i+1, a.Artist.Name)
	}
	return out
}

// playlistTracks leaves the disc out: a playlist is flat, and a track's disc
// is that of its album.
func playlistTracks(p *qobuzdl.Playlist) []source.Track {
	out := make([]source.Track, len(p.Tracks))
	for i := range p.Tracks {
		out[i] = sourceTrack(&p.Tracks[i], i+1, "")
		out[i].Disc = 0
	}
	return out
}

// section puts an artist's or label's albums in one section, newest first.
func section(albums []qobuzdl.Album) []source.Section {
	items := make([]source.Item, len(albums))
	for i := range albums {
		items[i] = albumItem(&albums[i])
	}
	slices.SortStableFunc(items, func(a, b source.Item) int { return cmp.Compare(b.ReleaseDate, a.ReleaseDate) })
	return []source.Section{{ID: "albums", Title: "Albums", Items: items}}
}

// Item reads one item.
func (s *Source) Item(ctx context.Context, r source.Ref) (source.Item, error) {
	c, err := s.client(ctx)
	if err != nil {
		return source.Item{}, err
	}
	switch r.Kind {
	case source.KindAlbum:
		a, err := c.GetAlbumMeta(ctx, r.ID)
		if err != nil {
			return source.Item{}, s.failed(err)
		}
		it := albumItem(a)
		it.Tracks = albumTracks(a)
		return it, nil
	case source.KindTrack:
		t, err := c.GetTrackMeta(ctx, r.ID)
		if err != nil {
			return source.Item{}, s.failed(err)
		}
		return trackItem(t), nil
	case source.KindPlaylist:
		p, err := c.GetPlaylistMeta(ctx, r.ID)
		if errors.Is(err, qobuzdl.ErrEmptyContent) {
			return source.Item{Ref: r, URL: playLink(r.Kind, r.ID)}, nil
		}
		if err != nil {
			return source.Item{}, s.failed(err)
		}
		it := playlistItem(p)
		it.Tracks = playlistTracks(p)
		return it, nil
	case source.KindArtist:
		a, err := c.GetArtistMeta(ctx, r.ID)
		if errors.Is(err, qobuzdl.ErrEmptyContent) {
			return emptyCollection(r), nil
		}
		if err != nil {
			return source.Item{}, s.failed(err)
		}
		it := artistItem(a)
		it.Sections = section(a.Albums)
		return it, nil
	case source.KindLabel:
		l, err := c.GetLabelMeta(ctx, r.ID)
		if errors.Is(err, qobuzdl.ErrEmptyContent) {
			return emptyCollection(r), nil
		}
		if err != nil {
			return source.Item{}, s.failed(err)
		}
		it := labelItem(l)
		it.Sections = section(l.Albums)
		return it, nil
	}
	return source.Item{}, fmt.Errorf("qobuz: can't read a %s", r.Kind)
}

// emptyCollection is an artist or label without albums. The port drops the
// answer that named it, so the item has no title.
func emptyCollection(r source.Ref) source.Item {
	return source.Item{Ref: r, URL: playLink(r.Kind, r.ID), Sections: section(nil)}
}
