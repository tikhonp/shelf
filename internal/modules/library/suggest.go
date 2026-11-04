package library

import (
	"cmp"
	"context"
	"errors"
	"math"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/tikhonp/shelf/internal/modules/library/musicbrainz"
	"github.com/tikhonp/shelf/internal/modules/library/tagio"
)

// suggestTimeout bounds the lookups behind one album's suggestions. A
// service still busy then counts as not answering.
const suggestTimeout = 40 * time.Second

// maxPerField is how many suggestions a field shows.
const maxPerField = 3

// What a suggestion's source is called on screen.
const (
	srcFileName = "File name"
	srcFolder   = "Folder"
	srcSiblings = "Other tracks"
	srcShelf    = "shelf.txt"
	srcMB       = "MusicBrainz"
	srcCAA      = "Cover Art Archive"
	srcLRCLIB   = "LRCLIB"
)

// releaseFields name one release. Each shows one value, the anchor's when
// the album has one, so accepting fields one at a time can't mix two
// releases.
var releaseFields = []string{
	"musicbrainz_albumid", "musicbrainz_releasegroupid", "musicbrainz_albumartistid",
	"musicbrainz_trackid", "musicbrainz_recordingid", "musicbrainz_artistid",
	"barcode", "catalognumber", "recordlabel", "releasecountry", "releasestatus", "releasetype",
	"releasedate", "asin", "script", "disctotal", "tracktotal",
}

func isReleaseField(f string) bool { return slices.Contains(releaseFields, f) }

// Suggestion is a value for a field, with where it came from and how sure
// shelf is of it.
type Suggestion struct {
	Field string `json:"field"`
	// Values is the field's new values. Tracks instead fills every track
	// with its own value, as a release's per-track IDs do.
	Values []string           `json:"values,omitempty"`
	Tracks map[int64][]string `json:"tracks,omitempty"`
	// Source is the most confident source, and Sources every source that
	// gave the same value, best first.
	Source     string   `json:"source"`
	Sources    []string `json:"sources"`
	Confidence int      `json:"confidence"`
	// Anchor is the MusicBrainz release a release field came from. A Save
	// sends it back with the edit.
	Anchor string `json:"anchor,omitempty"`

	p float64
}

type trackSuggestions struct {
	ID          int64        `json:"id"`
	Suggestions []Suggestion `json:"suggestions"`
}

// coverSuggestion is a cover found elsewhere. Image names it under
// /api/library/remote.
type coverSuggestion struct {
	Image      string `json:"image"`
	Source     string `json:"source"`
	Confidence int    `json:"confidence"`
	Anchor     string `json:"anchor,omitempty"`
}

// note says where Navidrome will show something other than the field.
type note struct {
	Track   int64  `json:"track,omitempty"`
	Field   string `json:"field"`
	Message string `json:"message"`
}

// unavailable is a service that didn't answer, so its suggestions are
// missing.
type unavailable struct {
	Source  string `json:"source"`
	Message string `json:"message"`
}

type suggestionsJSON struct {
	Anchor      *anchorJSON        `json:"anchor"`
	Album       []Suggestion       `json:"album"`
	Tracks      []trackSuggestions `json:"tracks"`
	Covers      []coverSuggestion  `json:"covers"`
	Notes       []note             `json:"notes"`
	Unavailable []unavailable      `json:"unavailable"`
}

// subject is an album as read now, the thing suggestions are for.
type subject struct {
	row    albumRow
	tracks []*subjectTrack
	// Strategies change how shelf searches MusicBrainz and what it suggests.
	compilation, live, classical bool
}

type subjectTrack struct {
	ID       int64
	Path     string
	Disc     int
	Track    int
	LengthMS int64
	View     tagio.View
}

// values are a track's values of a field.
func (t *subjectTrack) values(field string) []string { return t.View.Tags[field] }

func (t *subjectTrack) first(field string) string { return t.View.First(field) }

// common is the album's value of a field when every track holds the same,
// and the most common one otherwise.
func (s *subject) common(field string) string {
	count := map[string]int{}
	best := ""
	for _, t := range s.tracks {
		v := t.first(field)
		count[v]++
		if v != "" && count[v] > count[best] {
			best = v
		}
	}
	return best
}

// albumArtist is the album artist as Navidrome shows it.
func (s *subject) albumArtist() string {
	if len(s.tracks) == 0 {
		return ""
	}
	return s.tracks[0].View.AlbumArtist()
}

func (s *subject) album() string { return s.common("album") }

// readSubject reads an album's files now. A file that can't be read is
// left out.
func (m *Module) readSubject(ctx context.Context, id int64) (*subject, error) {
	row, err := m.store.album(ctx, id)
	if err != nil {
		return nil, err
	}
	files, err := m.store.albumFiles(ctx, id)
	if err != nil {
		return nil, err
	}
	rules := m.currentRules()
	s := &subject{row: row}
	for _, f := range files {
		tf, err := tagio.Read(m.abs(f.Path))
		if err != nil {
			continue
		}
		v := rules.View(tf, f.Path)
		s.tracks = append(s.tracks, &subjectTrack{ID: f.ID, Path: f.Path, Disc: max(v.Disc(), 1), Track: v.Track(),
			LengthMS: tf.Audio.Length.Milliseconds(), View: v})
	}
	slices.SortFunc(s.tracks, func(a, b *subjectTrack) int {
		return cmp.Or(cmp.Compare(a.Disc, b.Disc), cmp.Compare(a.Track, b.Track), strings.Compare(a.Path, b.Path))
	})
	s.strategies()
	return s, nil
}

// strategies picks how to look the album up. shelf searches for a
// compilation or a classical album without its album artist, which
// MusicBrainz credits differently, and for a live album only among live
// releases.
func (s *subject) strategies() {
	if len(s.tracks) == 0 {
		return
	}
	s.compilation = tagio.Fold(s.albumArtist()) == tagio.Fold(tagio.VariousArtists) || s.tracks[0].View.Compilation()
	words := tagio.Fold(s.album() + " " + path.Base(s.row.Folder))
	s.live = liveWord.MatchString(words) || slices.Contains(s.tracks[0].values("releasetype"), "live")
	genre := tagio.Fold(strings.Join(s.tracks[0].values("genre"), " ") + " " + strings.SplitN(s.row.Folder, "/", 2)[0])
	s.classical = strings.Contains(genre, "classical")
}

// collector gathers suggestions from sources that run at once.
type collector struct {
	mu          sync.Mutex
	cands       []cand
	covers      []coverSuggestion
	unavailable []unavailable
	anchor      *anchor
}

// cand is one source's value for a field. track 0 is the album.
type cand struct {
	track  int64
	field  string
	values []string
	tracks map[int64][]string
	source string
	p      float64
	anchor string
}

func (c *collector) add(cs ...cand) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, x := range cs {
		if x.p <= 0 || (len(x.values) == 0 && len(x.tracks) == 0) {
			continue
		}
		if x.values != nil {
			x.values = cleanValues(x.values)
			if len(x.values) == 0 {
				continue
			}
		}
		c.cands = append(c.cands, x)
	}
}

func (c *collector) cover(cs coverSuggestion) {
	c.mu.Lock()
	c.covers = append(c.covers, cs)
	c.mu.Unlock()
}

func (c *collector) fail(source, message string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !slices.ContainsFunc(c.unavailable, func(u unavailable) bool { return u.Source == source }) {
		c.unavailable = append(c.unavailable, unavailable{Source: source, Message: message})
	}
}

func cleanValues(vs []string) []string {
	var out []string
	for _, v := range vs {
		if v = strings.TrimSpace(v); v != "" && !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	return out
}

// suggestions answers an album's suggestions. The editor asks for them
// after it opens the album, so a slow service never holds it up.
// ?anchor= names a MusicBrainz release pasted into the album ID field.
func (m *Module) suggestions(c *echo.Context) error {
	id, err := idParam(c, "album")
	if err != nil {
		return err
	}
	staged := ""
	if a := c.QueryParam("anchor"); a != "" {
		var ok bool
		if staged, ok = musicbrainz.ParseRelease(a); !ok {
			return echo.NewHTTPError(http.StatusBadRequest, "anchor is not a MusicBrainz release ID or link")
		}
	}
	ctx := c.Request().Context()
	s, err := m.readSubject(ctx, id)
	if errors.Is(err, errNotFound) {
		return echo.NewHTTPError(http.StatusNotFound, "no such album")
	}
	if err != nil {
		return err
	}
	out, err := m.suggest(ctx, s, staged)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, out)
}

// suggest runs every source on an album and merges what they found.
func (m *Module) suggest(ctx context.Context, s *subject, staged string) (suggestionsJSON, error) {
	col := &collector{}
	local := m.localSuggestions(s, col)
	ctx, cancel := context.WithTimeout(ctx, suggestTimeout)
	defer cancel()
	var wg sync.WaitGroup
	run := func(f func()) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			f()
		}()
	}
	run(func() { m.mbSuggestions(ctx, s, staged, col) })
	run(func() { m.catalogSuggestions(ctx, s, local.ref, col) })
	wg.Wait()

	out := suggestionsJSON{Album: []Suggestion{}, Tracks: []trackSuggestions{}, Covers: []coverSuggestion{},
		Notes: m.notes(s), Unavailable: []unavailable{}}
	out.Unavailable = append(out.Unavailable, col.unavailable...)
	if col.anchor != nil {
		out.Anchor = col.anchor.json()
	}
	merged := merge(s, col.cands, col.anchor)
	byTrack := map[int64][]Suggestion{}
	for _, sg := range merged {
		if sg.track == 0 {
			out.Album = append(out.Album, sg.Suggestion)
		} else {
			byTrack[sg.track] = append(byTrack[sg.track], sg.Suggestion)
		}
	}
	for _, t := range s.tracks {
		if sgs := byTrack[t.ID]; sgs != nil {
			out.Tracks = append(out.Tracks, trackSuggestions{ID: t.ID, Suggestions: sgs})
		}
	}
	covers := col.covers
	slices.SortStableFunc(covers, func(a, b coverSuggestion) int { return b.Confidence - a.Confidence })
	if len(covers) > maxPerField {
		covers = covers[:maxPerField]
	}
	out.Covers = append(out.Covers, covers...)
	return out, nil
}

// offerCover records a remote cover and offers it.
func (m *Module) offerCover(ctx context.Context, col *collector, full, preview, source string, p float64, anchor string) {
	if full == "" {
		return
	}
	id, err := m.store.offer(context.WithoutCancel(ctx), full, cmp.Or(preview, full))
	if err != nil {
		m.log.Warn("library: offer a cover", "error", err)
		return
	}
	col.cover(coverSuggestion{Image: id, Source: source, Confidence: percent(p), Anchor: anchor})
}

// notes say where Navidrome shows something other than the tags: an .lrc
// file beside a track hides its embedded lyrics.
func (m *Module) notes(s *subject) []note {
	out := []note{}
	for _, t := range s.tracks {
		lrc := strings.TrimSuffix(t.Path, path.Ext(t.Path)) + ".lrc"
		if st, err := os.Stat(filepath.Join(m.root, filepath.FromSlash(lrc))); err == nil && st.Mode().IsRegular() {
			out = append(out, note{Track: t.ID, Field: "lyrics", Message: "Navidrome shows the .lrc file, not these lyrics."})
		}
	}
	return out
}

// merged is a suggestion with the track it belongs to.
type merged struct {
	Suggestion
	track int64
}

// merge joins the candidates that give the same value, works out their
// confidence, drops what the tracks already hold, holds release fields to
// one value, and keeps the best few per field.
//
// Confidence: each source gives its own p between 0 and 1, and sources
// that agree combine as 1 - (1-p1)(1-p2)..., so agreement raises it. One
// source counts once per value, with its best p. A release field keeps
// only the anchor's value when MusicBrainz matched one, else only its most
// confident value.
func merge(s *subject, cands []cand, a *anchor) []merged {
	type group struct {
		cand
		best map[string]float64
	}
	var groups []*group
	byKey := map[string]*group{}
	for _, c := range cands {
		k := candKey(c)
		g := byKey[k]
		if g == nil {
			g = &group{cand: c, best: map[string]float64{}}
			byKey[k] = g
			groups = append(groups, g)
		}
		g.best[c.source] = math.Max(g.best[c.source], c.p)
		if c.anchor != "" {
			g.anchor = c.anchor
		}
	}
	var out []merged
	for _, g := range groups {
		if holds(s, g.cand) {
			continue
		}
		miss := 1.0
		var sources []string
		for src, p := range g.best {
			miss *= 1 - p
			sources = append(sources, src)
		}
		slices.SortFunc(sources, func(x, y string) int {
			return cmp.Or(cmp.Compare(g.best[y], g.best[x]), strings.Compare(x, y))
		})
		p := 1 - miss
		out = append(out, merged{track: g.track, Suggestion: Suggestion{Field: g.field, Values: g.values, Tracks: g.tracks,
			Source: sources[0], Sources: sources, Confidence: percent(p), Anchor: g.anchor, p: p}})
	}
	matched := a != nil && a.Matched
	slices.SortStableFunc(out, func(x, y merged) int {
		return cmp.Or(cmp.Compare(x.track, y.track), cmp.Compare(fieldOrder(x.Field), fieldOrder(y.Field)), cmp.Compare(y.p, x.p))
	})
	var kept []merged
	for _, sg := range out {
		n := 0
		for _, k := range kept {
			if k.track == sg.track && k.Field == sg.Field {
				n++
			}
		}
		if isReleaseField(sg.Field) {
			if matched && sg.Anchor != a.ID || n > 0 {
				continue
			}
		}
		if n < maxPerField {
			kept = append(kept, sg)
		}
	}
	return kept
}

func candKey(c cand) string {
	var b strings.Builder
	b.WriteString(c.field)
	b.WriteByte(0)
	b.WriteString(strconv.FormatInt(c.track, 10))
	for _, v := range c.values {
		b.WriteByte(0)
		b.WriteString(v)
	}
	if c.tracks != nil {
		ids := make([]int64, 0, len(c.tracks))
		for id := range c.tracks {
			ids = append(ids, id)
		}
		slices.Sort(ids)
		for _, id := range ids {
			b.WriteByte(1)
			b.WriteString(strconv.FormatInt(id, 10))
			for _, v := range c.tracks[id] {
				b.WriteByte(0)
				b.WriteString(v)
			}
		}
	}
	return b.String()
}

// holds reports whether the tracks already hold what a candidate
// suggests.
func holds(s *subject, c cand) bool {
	for _, t := range s.tracks {
		switch {
		case c.tracks != nil:
			if vs, ok := c.tracks[t.ID]; ok && !slices.Equal(t.values(c.field), vs) {
				return false
			}
		case c.track == 0 || c.track == t.ID:
			if !slices.Equal(t.values(c.field), c.values) {
				return false
			}
		}
	}
	return true
}

// fieldOrder sorts suggestions in the order of Navidrome's mappings.
func fieldOrder(name string) int {
	if i, ok := fieldIndex()[name]; ok {
		return i
	}
	return math.MaxInt
}

var fieldIndex = sync.OnceValue(func() map[string]int {
	out := map[string]int{}
	for i, f := range tagio.Fields() {
		out[f.Name] = i
	}
	return out
})

// percent shows a confidence, from 1 to 99: no hint is certain.
func percent(p float64) int {
	return int(math.Min(99, math.Max(1, math.Round(p*100))))
}
