package library

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/tikhonp/shelf/internal/modules/library/musicbrainz"
	"github.com/tikhonp/shelf/internal/modules/library/tagio"
)

// How an anchor was found, strongest first, and how sure each way is.
const (
	byStaged  = "staged"
	byFiles   = "files"
	byBarcode = "barcode"
	byISRC    = "isrc"
	bySearch  = "search"
	byPrint   = "acoustid"

	pStaged  = 0.97
	pFiles   = 0.95
	pBarcode = 0.9
	pISRC    = 0.6 // plus up to 0.3 by the share of codes that agree
	pSearch  = 0.4 // plus up to 0.4 by the search's score
	pPrint   = 0.5 // plus up to 0.35 by the share of tracks that agree
)

// The threshold a release must pass to anchor an album: the same number
// of tracks, and lengths close enough.
const (
	maxTrackDiff = 10.0 // seconds, any one track
	maxMeanDiff  = 3.0  // seconds, over the album
	// searchLookups bounds the releases read for one search, since each
	// costs a second.
	searchLookups = 3
	// isrcLookups bounds the codes asked about.
	isrcLookups = 3
	minScore    = 60
)

var liveWord = regexp.MustCompile(`\blive\b`)

// anchor is the one MusicBrainz release an album's release fields come
// from.
type anchor struct {
	ID      string
	By      string
	P       float64
	Release musicbrainz.Release
	// Matched means the release passed the threshold. Without it the
	// release's fields stay hidden, and Reason says why.
	Matched bool
	Reason  string
	// Tracks maps each track of the album to its track on the release.
	Tracks map[int64]releaseTrack
	// fit is how closely the lengths agree, from 0.85 to 1.
	fit float64
}

type releaseTrack struct {
	Disc   int
	Medium musicbrainz.Medium
	Track  musicbrainz.Track
}

type anchorJSON struct {
	ID      string `json:"id"`
	By      string `json:"by"`
	Title   string `json:"title"`
	Artist  string `json:"artist"`
	Date    string `json:"date"`
	Country string `json:"country"`
	Tracks  int    `json:"tracks"`
	URL     string `json:"url"`
	Matched bool   `json:"matched"`
	Reason  string `json:"reason,omitempty"`
}

func (a *anchor) json() *anchorJSON {
	r := a.Release
	return &anchorJSON{ID: a.ID, By: a.By, Title: r.Title, Artist: r.ArtistCredit.String(), Date: r.Date,
		Country: r.Country, Tracks: r.TrackCount(), URL: "https://musicbrainz.org/release/" + a.ID,
		Matched: a.Matched, Reason: a.Reason}
}

// mbSuggestions finds the album's anchor and suggests its fields and
// cover.
func (m *Module) mbSuggestions(ctx context.Context, s *subject, staged string, col *collector) {
	if m.MB == nil || len(s.tracks) == 0 {
		return
	}
	a, err := m.findAnchor(ctx, s, staged)
	if err != nil {
		m.log.Warn("library: MusicBrainz", "album", s.row.ID, "error", err)
		col.fail(srcMB, "MusicBrainz doesn't answer, so its suggestions are missing.")
	}
	if a == nil {
		return
	}
	col.mu.Lock()
	col.anchor = a
	col.mu.Unlock()
	if !a.Matched {
		return
	}
	col.add(releaseSuggestions(s, a)...)
	cover, err := m.MB.FrontCover(ctx, a.ID)
	switch {
	case err == nil:
		m.offerCover(ctx, col, cover.Image, cover.Preview, srcCAA, a.P*a.fit, a.ID)
	case !errors.Is(err, musicbrainz.ErrNotFound):
		m.log.Warn("library: Cover Art Archive", "release", a.ID, "error", err)
		col.fail(srcCAA, "The Cover Art Archive doesn't answer.")
	}
}

// findAnchor tries each way of finding the release in turn. A release
// named by ID, staged or in the files, is the anchor even when it fails
// the threshold, because the owner or the files chose it. A release found
// any other way must pass.
func (m *Module) findAnchor(ctx context.Context, s *subject, staged string) (*anchor, error) {
	named := func(id, by string, p float64) (*anchor, error) {
		r, err := m.MB.Release(ctx, id, s.classical)
		if errors.Is(err, musicbrainz.ErrNotFound) {
			return &anchor{ID: id, By: by, P: p, Release: musicbrainz.Release{ID: id},
				Reason: "MusicBrainz has no release with this ID."}, nil
		}
		if err != nil {
			return nil, err
		}
		a := &anchor{ID: id, By: by, P: p, Release: r}
		a.match(s)
		return a, nil
	}
	if staged != "" {
		return named(staged, byStaged, pStaged)
	}
	if id := s.common("musicbrainz_albumid"); musicbrainz.IsID(id) {
		return named(id, byFiles, pFiles)
	}
	var errs []error
	try := func(by string, cands []scored) *anchor {
		for i, c := range cands {
			if i >= searchLookups {
				break
			}
			r, err := m.MB.Release(ctx, c.id, s.classical)
			if err != nil {
				if !errors.Is(err, musicbrainz.ErrNotFound) {
					errs = append(errs, err)
				}
				continue
			}
			a := &anchor{ID: c.id, By: by, P: c.p, Release: r}
			if a.match(s) {
				return a
			}
		}
		return nil
	}
	if bc := s.common("barcode"); bc != "" {
		hits, err := m.MB.SearchReleases(ctx, "barcode:"+musicbrainz.Phrase(bc), 5)
		if err != nil {
			errs = append(errs, err)
		}
		if a := try(byBarcode, rank(hits, s, func(musicbrainz.Hit) float64 { return pBarcode })); a != nil {
			return a, nil
		}
	}
	if a, err := m.byISRC(ctx, s, try); err != nil || a != nil {
		return a, err
	}
	if album := s.album(); album != "" {
		q := "release:" + musicbrainz.Phrase(album)
		if artist := s.albumArtist(); artist != "" && !s.compilation && !s.classical {
			q += " AND artist:" + musicbrainz.Phrase(artist)
		}
		if s.live {
			q += " AND secondarytype:live"
		}
		hits, err := m.MB.SearchReleases(ctx, q, 10)
		if err != nil {
			errs = append(errs, err)
		}
		hits = slices.DeleteFunc(hits, func(h musicbrainz.Hit) bool { return h.Score < minScore })
		if a := try(bySearch, rank(hits, s, func(h musicbrainz.Hit) float64 { return pSearch + 0.4*float64(h.Score)/100 })); a != nil {
			return a, nil
		}
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	// Audio is the last resort, and only once the text lookups have really
	// found nothing.
	return try(byPrint, m.printed(ctx, s)), nil
}

// byISRC asks for the releases of a few of the album's codes and tries
// them, most shared first.
func (m *Module) byISRC(ctx context.Context, s *subject, try func(string, []scored) *anchor) (*anchor, error) {
	var codes []string
	for _, t := range s.tracks {
		if c := t.first("isrc"); c != "" && !slices.Contains(codes, c) {
			codes = append(codes, c)
		}
		if len(codes) == isrcLookups {
			break
		}
	}
	if len(codes) == 0 {
		return nil, nil
	}
	count := map[string]int{}
	byID := map[string]musicbrainz.Hit{}
	for _, c := range codes {
		hits, err := m.MB.ISRC(ctx, c)
		if err != nil {
			return nil, err
		}
		seen := map[string]bool{}
		for _, h := range hits {
			if !seen[h.ID] {
				seen[h.ID] = true
				count[h.ID]++
				byID[h.ID] = h
			}
		}
	}
	var hits []musicbrainz.Hit
	for _, h := range byID {
		hits = append(hits, h)
	}
	slices.SortFunc(hits, func(a, b musicbrainz.Hit) int { return cmp.Or(count[b.ID]-count[a.ID], strings.Compare(a.ID, b.ID)) })
	return try(byISRC, rank(hits, s, func(h musicbrainz.Hit) float64 {
		return pISRC + 0.3*float64(count[h.ID])/float64(len(codes))
	})), nil
}

type scored struct {
	id string
	p  float64
}

// rank keeps the hits with the album's number of tracks, in their order.
// A search answers how many tracks each release has, so a release that
// can't pass costs no lookup.
func rank(hits []musicbrainz.Hit, s *subject, p func(musicbrainz.Hit) float64) []scored {
	var out []scored
	for _, h := range hits {
		if h.TrackCount != 0 && h.TrackCount != len(s.tracks) {
			continue
		}
		if !slices.ContainsFunc(out, func(x scored) bool { return x.id == h.ID }) {
			out = append(out, scored{id: h.ID, p: p(h)})
		}
	}
	return out
}

// match pairs the album's tracks with the release's, by disc and track
// number, else in order, and checks their lengths.
func (a *anchor) match(s *subject) bool {
	r := a.Release
	byNumber := map[[2]int]releaseTrack{}
	var all []releaseTrack
	for i, md := range r.Media {
		disc := cmp.Or(md.Position, i+1)
		for _, t := range md.Tracks {
			rt := releaseTrack{Disc: disc, Medium: md, Track: t}
			byNumber[[2]int{disc, t.Position}] = rt
			all = append(all, rt)
		}
	}
	if len(all) != len(s.tracks) {
		a.Reason = fmt.Sprintf("The release has %d tracks and the album %d.", len(all), len(s.tracks))
		return false
	}
	pairs := map[int64]releaseTrack{}
	used := map[string]bool{}
	for _, t := range s.tracks {
		rt, ok := byNumber[[2]int{t.Disc, t.Track}]
		if !ok || used[rt.Track.ID] {
			pairs = nil
			break
		}
		used[rt.Track.ID] = true
		pairs[t.ID] = rt
	}
	if pairs == nil {
		pairs = map[int64]releaseTrack{}
		for i, t := range s.tracks {
			pairs[t.ID] = all[i]
		}
	}
	var sum float64
	n := 0
	for _, t := range s.tracks {
		rt := pairs[t.ID]
		ms := cmp.Or(rt.Track.Length, rt.Track.Recording.Length)
		if ms == 0 || t.LengthMS == 0 {
			continue
		}
		d := math.Abs(float64(ms-t.LengthMS)) / 1000
		if d > maxTrackDiff {
			a.Reason = fmt.Sprintf("Track %d is %s seconds longer or shorter on the release.", t.Track, strconv.Itoa(int(math.Round(d))))
			return false
		}
		sum += d
		n++
	}
	mean := 0.0
	if n > 0 {
		mean = sum / float64(n)
	}
	if mean > maxMeanDiff {
		a.Reason = fmt.Sprintf("The tracks differ from the release's by %.1f seconds on average.", mean)
		return false
	}
	a.Tracks, a.Matched, a.Reason = pairs, true, ""
	a.fit = math.Max(0.85, 1-0.05*mean)
	return true
}

// releaseSuggestions are a matched release's fields. Release fields carry
// the anchor, so a Save can refuse two releases. The per-track IDs fill
// every track at once.
func releaseSuggestions(s *subject, a *anchor) []cand {
	r := a.Release
	p := a.P * a.fit
	var out []cand
	album := func(field string, vs ...string) {
		var anchor string
		if isReleaseField(field) {
			anchor = a.ID
		}
		out = append(out, cand{field: field, values: vs, source: srcMB, p: p, anchor: anchor})
	}
	album("album", r.Title)
	album("albumartist", r.ArtistCredit.String())
	album("albumartists", r.ArtistCredit.Names()...)
	album("albumartistsort", r.ArtistCredit.Sort())
	album("musicbrainz_albumartistid", r.ArtistCredit.IDs()...)
	album("musicbrainz_albumid", r.ID)
	album("musicbrainz_releasegroupid", r.ReleaseGroup.ID)
	album("releasedate", r.Date)
	album("originaldate", r.ReleaseGroup.FirstReleaseDate)
	album("recordlabel", r.Label())
	album("catalognumber", r.CatalogNumber())
	album("barcode", r.Barcode)
	album("releasecountry", r.Country)
	album("releasestatus", strings.ToLower(r.Status))
	album("releasetype", r.ReleaseGroup.Types()...)
	album("asin", r.ASIN)
	album("script", r.TextRepr.Script)
	album("genre", r.Genre())
	album("disctotal", strconv.Itoa(len(r.Media)))
	if slices.Contains(r.ArtistCredit.IDs(), musicbrainz.VariousArtists) {
		album("compilation", "1")
	}
	fills := map[string]map[int64][]string{}
	fill := func(field string, id int64, vs ...string) {
		vs = cleanValues(vs)
		if len(vs) == 0 {
			return
		}
		if fills[field] == nil {
			fills[field] = map[int64][]string{}
		}
		fills[field][id] = vs
	}
	for _, t := range s.tracks {
		rt := a.Tracks[t.ID]
		tr, rec := rt.Track, rt.Track.Recording
		credit := tr.ArtistCredit
		if len(credit) == 0 {
			credit = r.ArtistCredit
		}
		fill("musicbrainz_trackid", t.ID, tr.ID)
		fill("musicbrainz_recordingid", t.ID, rec.ID)
		fill("musicbrainz_artistid", t.ID, credit.IDs()...)
		fill("tracktotal", t.ID, strconv.Itoa(len(rt.Medium.Tracks)))
		track := func(field string, vs ...string) {
			out = append(out, cand{track: t.ID, field: field, values: vs, source: srcMB, p: p})
		}
		track("title", tr.Title)
		track("artist", credit.String())
		track("artists", credit.Names()...)
		track("track", strconv.Itoa(tr.Position))
		track("disc", strconv.Itoa(rt.Disc))
		track("isrc", rec.ISRCs...)
		if s.classical {
			track("composer", rec.Composers()...)
			track("work", rec.Works()...)
		}
	}
	for field, byTrack := range fills {
		// A fill that misses a track would leave it with the old release's
		// ID, so shelf offers it only whole.
		if len(byTrack) == len(s.tracks) {
			out = append(out, cand{field: field, tracks: byTrack, source: srcMB, p: p, anchor: a.ID})
		}
	}
	return out
}

// foldText makes names comparable: lowercase, plain quotes and dashes,
// letters and digits only, single spaces.
func foldText(s string) string {
	s = tagio.Fold(s)
	var b strings.Builder
	space := false
	for _, r := range s {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			if space && b.Len() > 0 {
				b.WriteByte(' ')
			}
			space = false
			b.WriteRune(r)
		default:
			space = true
		}
	}
	return b.String()
}

// similarity is the share of words two names have in common, from 0 to 1.
func similarity(a, b string) float64 {
	wa, wb := strings.Fields(foldText(a)), strings.Fields(foldText(b))
	if len(wa) == 0 || len(wb) == 0 {
		return 0
	}
	if slices.Equal(wa, wb) {
		return 1
	}
	inB := map[string]int{}
	for _, w := range wb {
		inB[w]++
	}
	common := 0
	for _, w := range wa {
		if inB[w] > 0 {
			inB[w]--
			common++
		}
	}
	return 2 * float64(common) / float64(len(wa)+len(wb))
}
