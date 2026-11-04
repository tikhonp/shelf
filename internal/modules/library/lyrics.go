package library

import (
	"context"
	"errors"
	"math"
	"net/http"

	"github.com/labstack/echo/v5"
	"github.com/tikhonp/shelf/internal/modules/library/lrclib"
	"github.com/tikhonp/shelf/internal/modules/library/tagio"
)

// LRCLIB matches a track by its artist, title, album and length, so a hit
// is likely the track. Synced lyrics go first, since Navidrome shows them
// in time.
const (
	pSynced = 0.7
	pPlain  = 0.65
)

// trackSuggestionsJSON is what one track's own lookups found.
type trackSuggestionsJSON struct {
	Suggestions []Suggestion  `json:"suggestions"`
	Unavailable []unavailable `json:"unavailable"`
}

// trackSuggestions answers the lyrics of one track. The editor asks when
// it opens a track's lyrics, not with the album: LRCLIB can take seconds a
// track, and an album's worth of calls on every opening would hold the
// album's suggestions up and load a free service for lyrics nobody reads.
func (m *Module) trackSuggestions(c *echo.Context) error {
	id, err := idParam(c, "track")
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	f, err := m.store.fileByID(ctx, id)
	if errors.Is(err, errNotFound) {
		return echo.NewHTTPError(http.StatusNotFound, "no such track")
	}
	if err != nil {
		return err
	}
	tf, err := tagio.Read(m.abs(f.Path))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "the track's file can't be read")
	}
	t := &subjectTrack{ID: f.ID, Path: f.Path, LengthMS: tf.Audio.Length.Milliseconds(), View: m.currentRules().View(tf, f.Path)}
	col := &collector{}
	m.lyricsOf(ctx, t, col)
	out := trackSuggestionsJSON{Suggestions: []Suggestion{}, Unavailable: []unavailable{}}
	out.Unavailable = append(out.Unavailable, col.unavailable...)
	for _, sg := range merge(&subject{tracks: []*subjectTrack{t}}, col.cands, nil) {
		out.Suggestions = append(out.Suggestions, sg.Suggestion)
	}
	return c.JSON(http.StatusOK, out)
}

// lyricsOf asks LRCLIB for a track's lyrics.
func (m *Module) lyricsOf(ctx context.Context, t *subjectTrack, col *collector) {
	title, artist := t.first("title"), t.View.Artist()
	if m.Lyrics == nil || title == "" || artist == "" || t.LengthMS == 0 {
		return
	}
	l, err := m.Lyrics.Get(ctx, artist, title, t.first("album"), int(math.Round(float64(t.LengthMS)/1000)))
	switch {
	case errors.Is(err, lrclib.ErrNotFound):
	case err != nil:
		m.log.Warn("library: LRCLIB", "track", t.ID, "error", err)
		col.fail(srcLRCLIB, "LRCLIB doesn't answer, so lyrics are missing.")
	case l.Instrumental:
	case l.Synced != "":
		col.add(cand{track: t.ID, field: "lyrics", values: []string{l.Synced}, source: srcLRCLIB, p: pSynced})
	case l.Plain != "":
		col.add(cand{track: t.ID, field: "lyrics", values: []string{l.Plain}, source: srcLRCLIB, p: pPlain})
	}
}
