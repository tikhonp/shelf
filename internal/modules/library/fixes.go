package library

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/tikhonp/shelf/internal/modules/library/loudness"
	"github.com/tikhonp/shelf/internal/modules/library/tagio"
)

// FixReplayGain is the choice of a run that measures and writes ReplayGain.
const FixReplayGain = "replaygain"

// runFixes are what a run offers, in the order they go through an album. A
// later fix sees what an earlier one did, so RELEASEDATE copies a date the
// tracks have just agreed on.
var runFixes = []string{string(TagsDisagree), string(ValuesDiffer), string(NoReleaseDate), string(VariousArtists), FixReplayGain}

// albumGains is what the analysis measured for an album, as tags hold it.
type albumGains struct {
	AlbumGain string `json:"album_gain"`
	AlbumPeak string `json:"album_peak"`
	// Tracks maps each file to its own gain. A silent track has none.
	Tracks map[string]trackGain `json:"tracks"`
}

type trackGain struct {
	Gain string `json:"gain"`
	Peak string `json:"peak"`
}

// albumPlan is a run's write for one album, from the files as they are now.
type albumPlan struct {
	row     albumRow
	indexed []*file
	stored  storedChange
	// before holds each file's tags, for the preview's samples.
	before map[string]map[string][]string
	// applied counts the fixes that change a tag.
	applied int
}

// planAlbum works out the chosen fixes from what the files hold. It asks no
// service: a run writes with nobody reading each album, so a Fix that leans on
// a suggestion takes the rule's value or leaves the tag alone.
func (m *Module) planAlbum(ctx context.Context, id int64, fixes []string, gains *albumGains) (*albumPlan, error) {
	row, err := m.store.album(ctx, id)
	if err != nil {
		return nil, err
	}
	indexed, err := m.store.albumFiles(ctx, id)
	if err != nil {
		return nil, err
	}
	rules := m.currentRules()
	var fresh []*file
	var tfs []tagio.File
	for _, old := range indexed {
		tf, err := tagio.Read(m.abs(old.Path))
		if err != nil {
			// As in the editor, a file shelf can't read is left out.
			continue
		}
		fresh = append(fresh, &file{ID: old.ID, Path: old.Path, Size: tf.Size, ModNS: tf.ModTime.UnixNano(), Revision: tf.Revision(),
			FirstSeen: old.FirstSeen, Facts: read(rules, tf, old.Path)})
		tfs = append(tfs, tf)
	}
	p := &albumPlan{row: row, indexed: indexed, before: map[string]map[string][]string{}}
	if len(fresh) == 0 {
		return p, nil
	}
	a, err := m.recheck(ctx, row, fresh, rules)
	if err != nil {
		return nil, err
	}
	ops := make([]map[string][]string, len(tfs))
	for i := range ops {
		ops[i] = map[string][]string{}
		p.before[fresh[i].Path] = tfs[i].Tags
	}
	for _, fix := range runFixes {
		if !slices.Contains(fixes, fix) {
			continue
		}
		edits := fixEdits(fix, a.Issues, tfs, fresh, gains)
		changed := false
		for i := range tfs {
			next, keys, err := tfs[i].Apply(edits[i])
			if err != nil {
				return nil, &tagio.FileError{Path: m.abs(fresh[i].Path), Err: err}
			}
			tfs[i] = next
			for k, v := range keys {
				ops[i][k] = v
				changed = true
			}
		}
		if changed {
			p.applied++
		}
	}
	for i, f := range fresh {
		if len(ops[i]) == 0 {
			continue
		}
		sf := storedFile{Path: f.Path, Revision: f.Revision}
		for _, k := range sortedKeys(ops[i]) {
			sf.Edits = append(sf.Edits, editJSON{Key: k, Values: ops[i][k]})
		}
		p.stored.Files = append(p.stored.Files, sf)
	}
	return p, nil
}

func sortedKeys(m map[string][]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// fixEdits is one fix's edits for each file. These are the editor's Fixes,
// without the suggestions they would rather use.
func fixEdits(fix string, issues []Issue, tfs []tagio.File, files []*file, gains *albumGains) [][]tagio.Edit {
	out := make([][]tagio.Edit, len(tfs))
	if fix == FixReplayGain {
		if gains != nil {
			for i, f := range files {
				out[i] = gainEdits(gains, f.Path)
			}
		}
		return out
	}
	i := slices.IndexFunc(issues, func(is Issue) bool { return string(is.Kind) == fix })
	if i < 0 {
		return out
	}
	fields := issues[i].Fields
	switch Kind(fix) {
	case NoReleaseDate:
		for i, tf := range tfs {
			out[i] = releaseDateEdits(tf)
		}
	case VariousArtists:
		for i := range tfs {
			out[i] = []tagio.Edit{{Field: "compilation", Values: []string{"1"}}}
		}
	case ValuesDiffer:
		for _, f := range fields {
			if v := commonValues(tfs, f); v != nil {
				for i := range tfs {
					out[i] = append(out[i], tagio.Edit{Field: f, Values: v})
				}
			}
		}
	case TagsDisagree:
		// A field edit writes every key that feeds the field, so even the value
		// the first key holds brings the others in line.
		for _, f := range fields {
			for i, tf := range tfs {
				if v := fieldValues(tf, f); len(v) > 0 {
					out[i] = append(out[i], tagio.Edit{Field: f, Values: v})
				}
			}
		}
	}
	return out
}

func gainEdits(g *albumGains, path string) []tagio.Edit {
	out := []tagio.Edit{{Field: "replaygain_album_gain", Values: []string{g.AlbumGain}}, {Field: "replaygain_album_peak", Values: []string{g.AlbumPeak}}}
	if t, ok := g.Tracks[path]; ok {
		out = append(out, tagio.Edit{Field: "replaygain_track_gain", Values: []string{t.Gain}}, tagio.Edit{Field: "replaygain_track_peak", Values: []string{t.Peak}})
	}
	return out
}

// releaseDateEdits keeps the year Navidrome shows. A key that already feeds
// the release date, such as YEAR, gives its value. Otherwise RELEASEDATE takes
// DATE, and DATE takes ORIGINALDATE, which is what the legacy rule showed.
func releaseDateEdits(tf tagio.File) []tagio.Edit {
	if len(tf.Tags["RELEASEDATE"]) > 0 {
		return nil
	}
	standard, _ := tf.Groups()
	for _, k := range standard {
		if k != "RELEASEDATE" && feeds(tf, k, "releasedate") {
			return []tagio.Edit{{Key: "RELEASEDATE", Values: tf.Tags[k]}}
		}
	}
	date := tf.Tags["DATE"]
	if len(date) == 0 {
		return nil
	}
	out := []tagio.Edit{{Key: "RELEASEDATE", Values: date}}
	if original := tf.Tags["ORIGINALDATE"]; len(original) > 0 {
		out = append(out, tagio.Edit{Key: "DATE", Values: original})
	}
	return out
}

func feeds(tf tagio.File, key, field string) bool {
	return slices.ContainsFunc(tagio.FieldsOf(tf.Format, key), func(f tagio.Field) bool { return f.Name == field })
}

// fieldValues is what the first key that feeds a field holds.
func fieldValues(tf tagio.File, field string) []string {
	standard, _ := tf.Groups()
	for _, k := range standard {
		if feeds(tf, k, field) {
			return tf.Tags[k]
		}
	}
	return nil
}

// commonValues is the value most tracks hold. On a tie no rule can tell which
// is right, and nobody reads a run's albums one by one, so it answers nil.
func commonValues(tfs []tagio.File, field string) []string {
	count := map[string]int{}
	values := map[string][]string{}
	for _, tf := range tfs {
		if v := fieldValues(tf, field); len(v) > 0 {
			k := strings.Join(v, "\x00")
			count[k]++
			values[k] = v
		}
	}
	best, tied := "", false
	for k, n := range count {
		switch {
		case n > count[best]:
			best, tied = k, false
		case n == count[best]:
			tied = true
		}
	}
	if tied {
		return nil
	}
	return values[best]
}

// measure finds an album's gains. Audio measured before, by this run or an
// earlier one, isn't decoded again.
func (m *Module) measure(ctx context.Context, albumID int64) (*albumGains, error) {
	files, err := m.store.albumFiles(ctx, albumID)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, errors.New("the album has no files shelf can read")
	}
	out := &albumGains{Tracks: map[string]trackGain{}}
	var all []loudness.Blocks
	peak := 0.0
	for _, f := range files {
		t, err := m.measureTrack(ctx, m.abs(f.Path))
		if err != nil {
			return nil, err
		}
		all = append(all, t.Blocks)
		peak = max(peak, t.Peak)
		if l, ok := t.Blocks.Integrated(); ok {
			out.Tracks[f.Path] = trackGain{Gain: loudness.FormatGain(loudness.Gain(l)), Peak: loudness.FormatPeak(t.Peak)}
		}
	}
	l, ok := loudness.Pool(all...).Integrated()
	if !ok {
		return nil, errors.New("the album is silent")
	}
	out.AlbumGain, out.AlbumPeak = loudness.FormatGain(loudness.Gain(l)), loudness.FormatPeak(peak)
	return out, nil
}

func (m *Module) measureTrack(ctx context.Context, abs string) (loudness.Track, error) {
	audio, err := tagio.AudioHash(ctx, m.FFmpeg, abs)
	if err != nil {
		return loudness.Track{}, err
	}
	if t, ok, err := m.store.gain(ctx, audio); ok || err != nil {
		return t, err
	}
	t, err := loudness.Analyze(ctx, m.FFmpeg, abs)
	if err != nil {
		return loudness.Track{}, err
	}
	return t, m.store.saveGain(ctx, audio, t)
}

func (s store) gain(ctx context.Context, audio string) (loudness.Track, bool, error) {
	var row struct {
		Peak   float64 `db:"peak"`
		Blocks string  `db:"blocks"`
	}
	if err := s.db.R.GetContext(ctx, &row, `SELECT peak, blocks FROM library_gains WHERE audio = ?`, audio); err != nil {
		if isNoRows(err) {
			return loudness.Track{}, false, nil
		}
		return loudness.Track{}, false, err
	}
	t := loudness.Track{Peak: row.Peak}
	return t, true, json.Unmarshal([]byte(row.Blocks), &t.Blocks)
}
