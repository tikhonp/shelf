package tagio

import (
	"cmp"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Navidrome is the test of every write, so shelf reads a file the way
// Navidrome's scanner does: its mappings, changed by the owner's Navidrome
// settings, its artist split, its date rules and its album ID. The code
// follows Navidrome 0.64.2's model/metadata and model/tag_mappings.go.

// NavidromeConfig holds the Navidrome settings that change how it reads
// tags. Navidrome's GET /api/config/ returns them.
type NavidromeConfig struct {
	// Tags is Navidrome's Tags setting: changes to the mappings, keyed by
	// field name.
	Tags map[string]TagConfig
	// ArtistSplitExceptions are names Navidrome never splits.
	ArtistSplitExceptions []string
	// ArtistJoiner joins several artists into one display name.
	ArtistJoiner string
	// GenreSeparators is the deprecated Scanner.GenreSeparators.
	GenreSeparators string
	// AlbumPID is the album ID spec, PID.Album.
	AlbumPID string
	// CoverArtPriority lists where Navidrome looks for an album's cover.
	CoverArtPriority string
}

// TagConfig is one entry of Navidrome's Tags setting.
type TagConfig struct {
	Ignore    bool
	Aliases   []string
	Type      string
	MaxLength int
	Split     []string
	Album     bool
}

// Navidrome's defaults and fixed names.
const (
	DefaultAlbumPID         = "musicbrainz_albumid|albumartistid,album,albumversion,releasedate"
	DefaultArtistJoiner     = " • "
	DefaultCoverArtPriority = "cover.*, folder.*, front.*, embedded, external"
	UnknownArtist           = "[Unknown Artist]"
	UnknownAlbum            = "[Unknown Album]"
	VariousArtists          = "Various Artists"
	// libraryID is the one library the music folder is in Navidrome.
	libraryID = 1
	// defaultMaxLength is where Navidrome cuts a value of a field without
	// its own limit.
	defaultMaxLength = 1024
)

// Rules are Navidrome's mappings with the owner's settings applied.
type Rules struct {
	fields     map[string]*rule
	artistRx   *regexp.Regexp
	exceptRx   *regexp.Regexp
	joiner     string
	albumPID   string
	coverNames []string
	embedded   bool
	id         string
}

type rule struct {
	aliases []string
	typ     string
	maxLen  int
	splitRx *regexp.Regexp
}

// DefaultRules are Navidrome's rules with no settings changed.
func DefaultRules() *Rules { return NewRules(NavidromeConfig{}) }

// NewRules applies Navidrome's settings to its mappings, as its
// loadTagMappings does.
func NewRules(c NavidromeConfig) *Rules {
	r := &Rules{
		fields:   make(map[string]*rule, len(fields)),
		artistRx: splitRx(artistSplit),
		exceptRx: exceptionsRx(c.ArtistSplitExceptions),
		joiner:   cmp.Or(c.ArtistJoiner, DefaultArtistJoiner),
		albumPID: cmp.Or(strings.TrimSpace(c.AlbumPID), DefaultAlbumPID),
	}
	base := map[string]TagConfig{}
	for _, f := range fields {
		base[f.Name] = TagConfig{Aliases: f.Aliases, Type: f.Type, MaxLength: f.MaxLength, Split: f.Split, Album: f.Album}
	}
	if c.GenreSeparators != "" && len(c.Tags["genre"].Aliases) == 0 {
		g := base["genre"]
		g.Split = strings.Split(c.GenreSeparators, "")
		base["genre"] = g
	}
	for name, tc := range c.Tags {
		name = strings.ToLower(name)
		if tc.Ignore {
			delete(base, name)
			continue
		}
		old := base[name]
		next := TagConfig{
			Aliases: old.Aliases, Split: old.Split, Type: cmp.Or(tc.Type, old.Type),
			MaxLength: cmp.Or(tc.MaxLength, old.MaxLength), Album: tc.Album || old.Album,
		}
		if len(tc.Aliases) > 0 {
			next.Aliases = lower(tc.Aliases)
		}
		if tc.Split != nil {
			next.Split = tc.Split
		}
		base[name] = next
	}
	for name, tc := range base {
		ru := &rule{aliases: tc.Aliases, typ: tc.Type, maxLen: cmp.Or(tc.MaxLength, defaultMaxLength)}
		// Navidrome splits only text fields.
		if tc.Type == "" || tc.Type == "string" {
			ru.splitRx = splitRx(tc.Split)
		}
		r.fields[name] = ru
	}
	for _, p := range strings.Split(cmp.Or(c.CoverArtPriority, DefaultCoverArtPriority), ",") {
		switch p = strings.ToLower(strings.TrimSpace(p)); p {
		case "embedded":
			r.embedded = true
		case "external", "":
		default:
			r.coverNames = append(r.coverNames, p)
		}
	}
	b, _ := json.Marshal(c)
	h := sha256.Sum256(append(b, mappingsYAML...))
	r.id = hex.EncodeToString(h[:8])
	return r
}

// ID names the rules, so a reading made under other rules can be redone.
func (r *Rules) ID() string { return r.id }

// AlbumPID is the album ID spec in force.
func (r *Rules) AlbumPID() string { return r.albumPID }

// splitRx matches any of the separators, ignoring case.
func splitRx(seps []string) *regexp.Regexp {
	var q []string
	for _, s := range seps {
		if s != "" {
			q = append(q, regexp.QuoteMeta(s))
		}
	}
	if len(q) == 0 {
		return nil
	}
	return regexp.MustCompile("(?i)(" + strings.Join(q, "|") + ")")
}

// exceptionsRx matches the longest exception first, since Go's alternation
// takes the leftmost match.
func exceptionsRx(names []string) *regexp.Regexp {
	var ns []string
	for _, n := range names {
		if n = strings.TrimSpace(n); n != "" {
			ns = append(ns, n)
		}
	}
	if len(ns) == 0 {
		return nil
	}
	slices.SortFunc(ns, func(a, b string) int {
		if d := cmp.Compare(len(b), len(a)); d != 0 {
			return d
		}
		return cmp.Compare(a, b)
	})
	for i, n := range ns {
		ns[i] = regexp.QuoteMeta(n)
	}
	return regexp.MustCompile("(?i)(" + strings.Join(ns, "|") + ")")
}

// participant fields hold artist names, where the exceptions apply.
var participant = map[string]bool{
	"artist": true, "artists": true, "artistsort": true, "artistssort": true,
	"albumartist": true, "albumartists": true, "albumartistsort": true, "albumartistssort": true,
}

func (r *Rules) split(rx *regexp.Regexp, name, v string) []string {
	if rx == nil {
		return []string{v}
	}
	var protected [][]int
	if participant[name] && r.exceptRx != nil {
		for _, sp := range r.exceptRx.FindAllStringIndex(v, -1) {
			if wordBounded(v, sp[0], sp[1]) {
				protected = append(protected, sp)
			}
		}
	}
	var parts []string
	start := 0
	for _, sep := range rx.FindAllStringIndex(v, -1) {
		if slices.ContainsFunc(protected, func(p []int) bool { return sep[0] < p[1] && p[0] < sep[1] }) {
			continue
		}
		parts = append(parts, strings.TrimSpace(v[start:sep[0]]))
		start = sep[1]
	}
	return append(parts, strings.TrimSpace(v[start:]))
}

func wordBounded(s string, start, end int) bool {
	word := func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }
	before, _ := utf8.DecodeLastRuneInString(s[:start])
	after, _ := utf8.DecodeRuneInString(s[end:])
	return !word(before) && !word(after)
}

// View is a file as Navidrome reads it.
type View struct {
	r *Rules
	// Tags maps each field to its values after Navidrome's cleaning: aliases
	// merged in order, values split, duplicates and empties dropped, dates
	// and IDs parsed.
	Tags map[string][]string
	// Path is the file's path inside the music folder, with slashes.
	Path       string
	HasPicture bool
}

// View reads f as Navidrome would, at rel inside the music folder.
func (r *Rules) View(f File, rel string) View {
	raw := make(map[string][]string, len(f.Tags))
	for k, vs := range f.Tags {
		raw[strings.ToLower(k)] = vs
		if f.Format != nil {
			if atom, ok := f.Format.atoms[strings.ToUpper(k)]; ok {
				raw[atom] = vs
			}
		}
	}
	v := View{r: r, Tags: map[string][]string{}, Path: path.Clean(strings.ReplaceAll(rel, "\\", "/")), HasPicture: len(f.Pictures) > 0}
	for name, ru := range r.fields {
		var vals []string
		if ru.typ == "pair" {
			vals = pairs(name, ru, raw)
		} else {
			for _, a := range ru.aliases {
				for _, s := range raw[a] {
					vals = append(vals, r.split(ru.splitRx, name, s)...)
				}
			}
		}
		var clean []string
		for _, s := range vals {
			if s = sanitize(ru, s); s != "" && !slices.Contains(clean, s) {
				clean = append(clean, s)
			}
		}
		if len(clean) > 0 {
			v.Tags[name] = clean
		}
	}
	return v
}

// pairs reads a pair field such as lyrics. Only its values matter to shelf,
// so the keys Navidrome pairs them with are left out.
func pairs(name string, ru *rule, raw map[string][]string) []string {
	var out []string
	keys := make([]string, 0, len(raw))
	for k := range raw {
		if strings.HasPrefix(k, name+":") {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	for _, k := range keys {
		out = append(out, raw[k]...)
	}
	for _, a := range ru.aliases {
		out = append(out, raw[a]...)
	}
	return out
}

var yearRx = regexp.MustCompile(`([12]\d\d\d)`)

func sanitize(ru *rule, v string) string {
	if len(v) > ru.maxLen {
		v = v[:ru.maxLen]
		for range 3 {
			if r, size := utf8.DecodeLastRuneInString(v); r != utf8.RuneError || size != 1 {
				break
			}
			v = v[:len(v)-1]
		}
	}
	switch ru.typ {
	case "date":
		return parseDate(v)
	case "int":
		if _, err := strconv.Atoi(v); err != nil {
			return ""
		}
	case "float":
		if _, err := strconv.ParseFloat(v, 64); err != nil {
			return ""
		}
	case "uuid":
		u, err := uuid.Parse(v)
		if err != nil {
			return ""
		}
		return u.String()
	}
	return v
}

// parseDate keeps a full date or a year and month, and otherwise the
// first year in the value.
func parseDate(v string) string {
	if len(v) < 4 {
		return ""
	}
	m := yearRx.FindStringSubmatch(v)
	if m == nil {
		return ""
	}
	if len(v) < 5 {
		return m[1]
	}
	v = v[:min(10, len(v))]
	for _, layout := range []string{"2006-01-02", "2006-01"} {
		if _, err := time.Parse(layout, v); err == nil {
			return v
		}
	}
	return m[1]
}

// First is a field's first value, which is what Navidrome shows of a
// single-value field.
func (v View) First(field string) string {
	if vs := v.Tags[field]; len(vs) > 0 {
		return vs[0]
	}
	return ""
}

// Title is the title, or the file name without its extension.
func (v View) Title() string {
	if t := v.First("title"); t != "" {
		return t
	}
	b := path.Base(v.Path)
	return strings.TrimSuffix(b, path.Ext(b))
}

// Album is the album's name, or Navidrome's placeholder.
func (v View) Album() string { return cmp.Or(v.First("album"), UnknownAlbum) }

// Compilation is the compilation flag as Navidrome parses it.
func (v View) Compilation() bool {
	b, _ := strconv.ParseBool(v.First("compilation"))
	return b
}

// Track and Disc are the numbers before any "/total".
func (v View) Track() int { return number(v.First("track")) }
func (v View) Disc() int  { return number(v.First("disc")) }

func number(s string) int {
	s, _, _ = strings.Cut(s, "/")
	n, err := strconv.ParseInt(s, 10, 32)
	if err != nil || n < 0 {
		return 0
	}
	return int(n)
}

// Artists are the track's artists: the multi-value tag, or the single tag
// split by Navidrome's separators.
func (v View) Artists() []string {
	if a := v.artistValues("artist", "artists"); len(a) > 0 {
		return a
	}
	return []string{UnknownArtist}
}

// AlbumArtists are the album's artists. Without a tag they are the track's
// artists, or Various Artists on a compilation.
func (v View) AlbumArtists() []string {
	if a := v.artistValues("albumartist", "albumartists"); len(a) > 0 {
		return a
	}
	if v.Compilation() {
		return []string{VariousArtists}
	}
	return v.Artists()
}

func (v View) artistValues(single, multi string) []string {
	if m := v.Tags[multi]; len(m) > 0 {
		return m
	}
	s := v.Tags[single]
	if len(s) != 1 {
		return s
	}
	rx := v.r.artistRx
	if ru := v.r.fields[single]; ru != nil && ru.splitRx != nil {
		rx = ru.splitRx
	}
	var out []string
	for _, p := range v.r.split(rx, single, s[0]) {
		if p != "" && !slices.Contains(out, p) {
			out = append(out, p)
		}
	}
	return out
}

// Artist is the track artist's display name.
func (v View) Artist() string {
	return cmp.Or(v.joined("artist"), v.joined("artists"), UnknownArtist)
}

// AlbumArtist is the album artist's display name, which feeds the album ID.
func (v View) AlbumArtist() string {
	fallback := UnknownArtist
	if v.Compilation() {
		fallback = VariousArtists
	}
	return cmp.Or(v.joined("albumartist"), v.joined("albumartists"), v.AlbumArtists()[0], fallback)
}

func (v View) joined(field string) string { return strings.Join(v.Tags[field], v.r.joiner) }

// Dates are the date Navidrome shows, the original date and the release
// date. With an original date, no release date and a date no earlier,
// Navidrome reads the date as the release date, because taggers long wrote the
// release date there.
func (v View) Dates() (date, original, release string) {
	date, original, release = v.First("recordingdate"), v.First("originaldate"), v.First("releasedate")
	if original != "" && release == "" && date >= original {
		return original, original, date
	}
	return cmp.Or(date, original, release), original, release
}

// Year is the year of a date, or 0.
func Year(date string) int {
	if len(date) < 4 {
		return 0
	}
	y, _ := strconv.Atoi(date[:4])
	return y
}

// AlbumID is the album's ID in Navidrome. The spec names fields in
// alternatives split by "|", and the first alternative with any value is
// hashed.
func (v View) AlbumID() string { return v.pid(v.r.albumPID) }

func (v View) pid(spec string) string {
	if spec == "album_legacy" {
		return v.legacyAlbumID()
	}
	pid := ""
	for alt := range strings.SplitSeq(spec, "|") {
		attrs := strings.Split(alt, ",")
		vals := make([]string, len(attrs))
		has := false
		for i, a := range attrs {
			vals[i] = v.pidAttr(strings.ToLower(strings.TrimSpace(a)), spec)
			has = has || vals[i] != ""
		}
		if has {
			pid = strings.Join(vals, "\\")
			break
		}
	}
	return hashID(strconv.Itoa(libraryID) + "\\" + pid)
}

func (v View) pidAttr(attr, spec string) string {
	switch attr {
	case "albumid":
		if spec == v.r.albumPID {
			return ""
		}
		return v.pid(v.r.albumPID)
	case "folder":
		return path.Dir(v.Path)
	case "albumartistid":
		return hashID(Fold(v.AlbumArtist()))
	case "title":
		return v.Title()
	case "album":
		return Fold(v.First("album"))
	}
	return v.First(attr)
}

func (v View) legacyAlbumID() string {
	various := ""
	if v.Compilation() {
		various = VariousArtists
	}
	artist := cmp.Or(v.First("albumartist"), various, v.First("artist"), UnknownArtist)
	key := strings.ToLower(artist + "\\" + v.Album())
	if _, _, rel := v.Dates(); rel != "" {
		key += "\\" + rel
	}
	return encodeID(md5.Sum([]byte(key)))
}

// hashID is Navidrome's id.NewHash of one value.
func hashID(s string) string { return encodeID(md5.Sum([]byte(s + "\u200b"))) }

func encodeID(b [16]byte) string {
	return fmt.Sprintf("%022s", new(big.Int).SetBytes(b[:]).Text(62))
}

// Fold lowercases a name and turns typographic quotes and dashes into
// ASCII ones, as Navidrome does before hashing it.
func Fold(s string) string { return clearASCII(strings.ToLower(s)) }

// clearASCII is Navidrome's str.Clear.
var clearASCII = func() func(string) string {
	var pairs []string
	for ascii, from := range map[string]string{"'": "‘’‛′", `"`: "＂〃ˮײ᳓″‶˶ʺ“”˝‟", "-": "‐–—−―"} {
		for _, r := range from {
			pairs = append(pairs, string(r), ascii)
		}
	}
	return strings.NewReplacer(pairs...).Replace
}()

// CoverNames are the file name patterns Navidrome tries for an album's
// cover, lowercase, in order.
func (r *Rules) CoverNames() []string { return slices.Clone(r.coverNames) }

// EmbeddedCovers reports whether Navidrome shows embedded pictures.
func (r *Rules) EmbeddedCovers() bool { return r.embedded }

// Disagreements lists the fields that two keys of f feed with different
// values, such as a stale ALBUM ARTIST beside ALBUMARTIST. Navidrome takes
// the first alias of a single value and merges the aliases of a
// multi-value tag, so either way one of them shows wrong. In a date field, a
// year agrees with a full date in that year, such as Picard's
// ORIGINALYEAR=1997 beside ORIGINALDATE=1997-07-01.
func (r *Rules) Disagreements(f File) []string {
	feeds := map[string][]string{}
	for k := range f.Tags {
		names := []string{strings.ToLower(k)}
		if f.Format != nil {
			if atom, ok := f.Format.atoms[strings.ToUpper(k)]; ok {
				names = append(names, atom)
			}
		}
		for field, ru := range r.fields {
			if ru.typ == "pair" {
				continue
			}
			if slices.ContainsFunc(names, func(n string) bool { return slices.Contains(ru.aliases, n) }) {
				feeds[field] = append(feeds[field], k)
			}
		}
	}
	var out []string
	for field, keys := range feeds {
		isDate := r.fields[field].typ == "date"
		// Agreement between dates is not transitive, so compare every pair. Then the
		// random order of the map's keys can't change the result.
	pairs:
		for i, a := range keys {
			for _, b := range keys[i+1:] {
				if !valuesAgree(f.Tags[a], f.Tags[b], isDate) {
					out = append(out, field)
					break pairs
				}
			}
		}
	}
	slices.Sort(out)
	return out
}

func valuesAgree(a, b []string, date bool) bool {
	if !date {
		return slices.Equal(a, b)
	}
	return slices.EqualFunc(a, b, datesAgree)
}

// datesAgree reports whether two dates are equal or the shorter one is a year
// or a year and month that starts the longer one.
func datesAgree(a, b string) bool {
	if a == b {
		return true
	}
	if len(a) > len(b) {
		a, b = b, a
	}
	return parseDate(a) == a && parseDate(b) == b && strings.HasPrefix(b, a+"-")
}
