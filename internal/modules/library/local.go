package library

import (
	"bufio"
	"bytes"
	"os"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Local sources fill gaps: a file or folder name suggests a field only
// when the tracks lack it, since names are cut and cleaned versions of the
// tags. Other tracks suggest the values most of the album holds, and
// shelf.txt what the catalog said when the album was downloaded.
const (
	pNameTrack  = 0.6
	pNameDisc   = 0.5
	pNameTitle  = 0.45
	pNameArtist = 0.35
	pFolder     = 0.5
	pFolderArt  = 0.4
	pSibling    = 0.4 // plus up to 0.5 by the share of tracks that hold the value
	pShelf      = 0.85
)

// localResult is what the local sources found for the lookups that follow.
type localResult struct {
	// ref is the album's download in shelf.txt, which a catalog can read
	// by its ID.
	ref *provenance
}

func (m *Module) localSuggestions(s *subject, col *collector) localResult {
	for _, t := range s.tracks {
		col.add(nameSuggestions(t)...)
	}
	col.add(folderSuggestions(s)...)
	col.add(siblingSuggestions(s)...)
	var out localResult
	if b, err := os.ReadFile(m.abs(path.Join(s.row.Folder, "shelf.txt"))); err == nil {
		if p := pickProvenance(parseProvenance(b)); p != nil {
			out.ref = p
			col.add(p.suggestions(s)...)
		}
	}
	return out
}

// trackName reads a file name such as "01 - Title", "01. Title" or
// "1-01 Title".
var trackName = regexp.MustCompile(`^(?:(\d)[-.](\d{1,3})|(\d{1,2}))(?:\s*[-.]\s*|\s+)(.+)$`)

func nameSuggestions(t *subjectTrack) []cand {
	base := strings.TrimSuffix(path.Base(t.Path), path.Ext(t.Path))
	m := trackName.FindStringSubmatch(base)
	var out []cand
	add := func(field, v string, p float64) {
		if v != "" && len(t.values(field)) == 0 {
			out = append(out, cand{track: t.ID, field: field, values: []string{v}, source: srcFileName, p: p})
		}
	}
	if d := discFolder.FindString(path.Base(path.Dir(t.Path))); d != "" {
		add("disc", strconv.Itoa(trailingNumber(d)), pNameDisc)
	}
	if m == nil {
		add("title", base, pNameTitle-0.1)
		return out
	}
	if m[1] != "" {
		add("disc", strconv.Itoa(atoi(m[1])), pNameDisc)
		add("track", strconv.Itoa(atoi(m[2])), pNameTrack)
	} else {
		add("track", strconv.Itoa(atoi(m[3])), pNameTrack)
	}
	title := m[4]
	if left, right, ok := strings.Cut(title, " - "); ok {
		switch artist := t.View.Artist(); {
		case len(t.values("artist")) == 0 && len(t.values("artists")) == 0:
			add("artist", left, pNameArtist)
			title = right
		case foldEq(left, artist):
			title = right
		}
	}
	add("title", title, pNameTitle)
	return out
}

func trailingNumber(s string) int {
	i := len(s)
	for i > 0 && s[i-1] >= '0' && s[i-1] <= '9' {
		i--
	}
	return atoi(s[i:])
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

func foldEq(a, b string) bool { return foldText(a) == foldText(b) }

// Folder names: "Artist - Album (1959) [24B-96kHz]", "[1959] Album", and
// the marks Qobuz adds while it works.
var (
	folderMark  = regexp.MustCompile(`^\[(?:IN PROGRESS|INCOMPLETE)\]\s*`)
	folderTail  = regexp.MustCompile(`\s*(?:\[[^\]]*\]|\{[^}]*\})$`)
	folderYear  = regexp.MustCompile(`\s*\((\d{4})\)$`)
	folderYear2 = regexp.MustCompile(`^[\[(]?\d{4}[\])]?\s*[-.]?\s*`)
)

func folderSuggestions(s *subject) []cand {
	name := folderMark.ReplaceAllString(path.Base(s.row.Folder), "")
	for {
		trimmed := folderTail.ReplaceAllString(name, "")
		if trimmed == name {
			break
		}
		name = trimmed
	}
	name = folderYear.ReplaceAllString(name, "")
	name = strings.TrimSpace(folderYear2.ReplaceAllString(name, ""))
	artist, album, ok := strings.Cut(name, " - ")
	if !ok {
		album, artist = name, ""
		if parent := path.Dir(s.row.Folder); strings.Contains(parent, "/") {
			artist = path.Base(parent)
		}
	}
	missing := func(fields ...string) bool {
		for _, t := range s.tracks {
			for _, f := range fields {
				if len(t.values(f)) > 0 {
					return false
				}
			}
		}
		return true
	}
	var out []cand
	if album = strings.TrimSpace(album); album != "" && missing("album") {
		out = append(out, cand{field: "album", values: []string{album}, source: srcFolder, p: pFolder})
	}
	if artist = strings.TrimSpace(artist); artist != "" && missing("albumartist", "albumartists") {
		out = append(out, cand{field: "albumartist", values: []string{artist}, source: srcFolder, p: pFolderArt})
	}
	return out
}

// siblingFields are the fields an album's tracks should share, and so
// the ones its other tracks can suggest.
var siblingFields = append([]string{"album", "genre"}, shared...)

// siblingSuggestions suggest, where tracks differ, each value some tracks
// hold to the whole album, more confidently the more tracks hold it.
func siblingSuggestions(s *subject) []cand {
	n := len(s.tracks)
	if n < 2 {
		return nil
	}
	var out []cand
	for _, f := range siblingFields {
		count := map[string]int{}
		vals := map[string][]string{}
		for _, t := range s.tracks {
			k := strings.Join(t.values(f), "\x1f")
			count[k]++
			vals[k] = t.values(f)
		}
		if len(count) < 2 {
			continue
		}
		keys := make([]string, 0, len(count))
		for k := range count {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		for _, k := range keys {
			if k == "" {
				continue
			}
			out = append(out, cand{field: f, values: vals[k], source: srcSiblings,
				p: pSibling + 0.5*float64(count[k])/float64(n)})
		}
	}
	return out
}

// provenance is one block of shelf.txt.
type provenance struct {
	Source, Kind, SourceID         string
	Title, Artist, Label, Released string
	UPC                            string
	// ISRCs are the tracks' codes by file name.
	ISRCs map[string]string
}

// parseProvenance reads the blocks of a shelf.txt, as provenance.md
// describes it. It skips keys it doesn't know.
func parseProvenance(b []byte) []provenance {
	var out []provenance
	var cur *provenance
	inTracks := false
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "Music Shelf download "):
			out = append(out, provenance{ISRCs: map[string]string{}})
			cur, inTracks = &out[len(out)-1], false
			continue
		case cur == nil || strings.TrimSpace(line) == "":
			inTracks = false
			continue
		}
		if inTracks && strings.HasPrefix(line, "  ") {
			cols := columns.Split(strings.TrimSpace(line), -1)
			if len(cols) < 2 {
				continue
			}
			for _, c := range cols[2:] {
				if isrc, ok := strings.CutPrefix(c, "ISRC "); ok {
					cur.ISRCs[cols[1]] = isrc
				}
			}
			continue
		}
		key, val, ok := strings.Cut(line, ": ")
		if !ok {
			inTracks = line == "Tracks:"
			continue
		}
		switch key {
		case "Source":
			cur.Source = val
		case "Kind":
			cur.Kind = val
		case "Source ID":
			cur.SourceID = val
		case "Title":
			cur.Title = val
		case "Artist":
			cur.Artist = val
		case "Label":
			cur.Label = val
		case "Release date":
			cur.Released = val
		case "UPC":
			cur.UPC = val
		}
	}
	return out
}

var columns = regexp.MustCompile(`\s{2,}`)

// pickProvenance is the latest album block, else the latest block.
func pickProvenance(ps []provenance) *provenance {
	for i := len(ps) - 1; i >= 0; i-- {
		if ps[i].Kind == "album" {
			return &ps[i]
		}
	}
	if len(ps) > 0 {
		return &ps[len(ps)-1]
	}
	return nil
}

func (p *provenance) suggestions(s *subject) []cand {
	var out []cand
	add := func(field, v string) {
		if v != "" {
			out = append(out, cand{field: field, values: []string{v}, source: srcShelf, p: pShelf})
		}
	}
	if p.Kind == "album" {
		add("album", p.Title)
		add("albumartist", p.Artist)
		add("recordlabel", p.Label)
		add("releasedate", p.Released)
		add("barcode", p.UPC)
	}
	for _, t := range s.tracks {
		for name, isrc := range p.ISRCs {
			if t.Path == name || strings.HasSuffix(t.Path, "/"+name) {
				out = append(out, cand{track: t.ID, field: "isrc", values: []string{isrc}, source: srcShelf, p: pShelf})
			}
		}
	}
	return out
}
