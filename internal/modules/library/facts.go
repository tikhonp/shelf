package library

import (
	"cmp"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/tikhonp/shelf/internal/modules/library/tagio"
	"github.com/tikhonp/shelf/internal/source"
)

// factsVersion changes whenever facts or the checks change, so the index
// reads every file again under the new code.
const factsVersion = 1

// facts is what the index keeps of one audio file: enough to group it into
// an album, list the album and run the checks without opening the file.
type facts struct {
	// AlbumID is the file's album ID in Navidrome.
	AlbumID string `json:"album_id"`
	Album   string `json:"album"`
	// AlbumKey is the album tag as Navidrome hashes it, so two albums in one
	// folder with the same name show as a split.
	AlbumKey    string `json:"album_key"`
	AlbumArtist string `json:"album_artist"`
	Artist      string `json:"artist"`
	Title       string `json:"title"`
	Track       int    `json:"track"`
	Disc        int    `json:"disc"`
	// Year is the year Navidrome shows.
	Year        int    `json:"year"`
	Compilation bool   `json:"compilation"`
	MBAlbumID   string `json:"mb_album_id"`
	HasPicture  bool   `json:"has_picture"`
	Codec       string `json:"codec"`
	BitDepth    int    `json:"bit_depth"`
	SampleRate  int    `json:"sample_rate"`
	LengthMS    int64  `json:"length_ms"`
	// Shared holds the values of the fields every track of an album should
	// agree on, joined.
	Shared map[string]string `json:"shared"`
	// Issues are what the checks found in this file alone.
	Issues []Issue `json:"issues"`
}

// Issue is one thing a check found. Fields name the tags involved.
type Issue struct {
	Kind   Kind     `json:"kind"`
	Fields []string `json:"fields,omitempty"`
}

// Kind names a check.
type Kind string

// The checks. Navidrome's tagging guide is the measure, and each issue is
// something Navidrome shows wrong or a tag the owner wants. Guide kinds are
// style only, so they never light a lamp.
const (
	MissingTag     Kind = "missing_tag"
	SplitAlbum     Kind = "split_album"
	ValuesDiffer   Kind = "values_differ"
	TagsDisagree   Kind = "tags_disagree"
	ReissueYear    Kind = "reissue_year"
	NoReleaseDate  Kind = "no_releasedate"
	VariousArtists Kind = "various_artists"
	ArtistSplit    Kind = "artist_split"
	TwoReleases    Kind = "two_releases"
	NoCover        Kind = "no_cover"

	GuideAlbumArtists Kind = "guide_albumartists"
	GuideReplayGain   Kind = "guide_replaygain"
)

// Kinds lists every check in the order the filter menu shows them.
var Kinds = []Kind{
	MissingTag, SplitAlbum, ValuesDiffer, TagsDisagree, ReissueYear, NoReleaseDate,
	VariousArtists, ArtistSplit, TwoReleases, NoCover, GuideAlbumArtists, GuideReplayGain,
}

// Guide reports whether a kind is style only.
func (k Kind) Guide() bool { return strings.HasPrefix(string(k), "guide_") }

// required are the fields without which Navidrome shows a placeholder or
// the file name.
var required = []string{"title", "artist", "album", "track"}

// shared are the fields every track of an album should agree on: the ones
// that feed the album ID or describe the release.
var shared = []string{
	"albumartist", "albumartists", "albumversion", "compilation", "releasedate", "originaldate",
	"disctotal", "recordlabel", "catalognumber", "barcode", "releasetype", "releasecountry",
	"releasestatus", "musicbrainz_albumartistid",
}

// releaseIDs are the fields that name one release. Two values in an album
// mean its tracks were tagged from two releases.
var releaseIDs = []string{"musicbrainz_albumid", "musicbrainz_releasegroupid"}

// read derives a file's facts as Navidrome reads it under rules.
func read(r *tagio.Rules, f tagio.File, rel string) facts {
	v := r.View(f, rel)
	date, original, _ := v.Dates()
	fc := facts{
		AlbumID: v.AlbumID(), Album: v.Album(), AlbumKey: tagio.Fold(v.First("album")),
		AlbumArtist: v.AlbumArtist(), Artist: v.Artist(), Title: v.Title(),
		Track: v.Track(), Disc: v.Disc(), Year: tagio.Year(date), Compilation: v.Compilation(),
		MBAlbumID: v.First("musicbrainz_albumid"), HasPicture: len(f.Pictures) > 0,
		Codec: cmp.Or(f.Audio.Codec, f.Format.Name), BitDepth: int(f.Audio.BitDepth),
		SampleRate: int(f.Audio.SampleRate), LengthMS: f.Audio.Length.Milliseconds(),
		Shared: map[string]string{},
	}
	for _, s := range append(slices.Clone(shared), releaseIDs...) {
		if vs := v.Tags[s]; len(vs) > 0 {
			fc.Shared[s] = strings.Join(vs, "\x1f")
		}
	}

	var missing []string
	for _, field := range required {
		if len(v.Tags[field]) == 0 {
			missing = append(missing, field)
		}
	}
	if missing != nil {
		fc.Issues = append(fc.Issues, Issue{Kind: MissingTag, Fields: missing})
	}
	if d := r.Disagreements(f); d != nil {
		fc.Issues = append(fc.Issues, Issue{Kind: TagsDisagree, Fields: d})
	}
	if original != "" && tagio.Year(date) > tagio.Year(original) {
		fc.Issues = append(fc.Issues, Issue{Kind: ReissueYear, Fields: []string{"originaldate", "recordingdate", "releasedate"}})
	}
	if !hasKey(f, "RELEASEDATE") {
		fc.Issues = append(fc.Issues, Issue{Kind: NoReleaseDate, Fields: []string{"releasedate"}})
	}
	var split []string
	if ids := v.Tags["musicbrainz_artistid"]; len(ids) > 0 && len(ids) != len(v.Artists()) {
		split = append(split, "artist")
	}
	if ids := v.Tags["musicbrainz_albumartistid"]; len(ids) > 0 && (len(v.Tags["albumartist"]) > 0 || len(v.Tags["albumartists"]) > 0) &&
		len(ids) != len(v.AlbumArtists()) {
		split = append(split, "albumartist")
	}
	if split != nil {
		fc.Issues = append(fc.Issues, Issue{Kind: ArtistSplit, Fields: split})
	}
	if len(v.Tags["albumartists"]) == 0 {
		fc.Issues = append(fc.Issues, Issue{Kind: GuideAlbumArtists, Fields: []string{"albumartists"}})
	}
	if len(v.Tags["replaygain_track_gain"]) == 0 && len(v.Tags["r128_track_gain"]) == 0 {
		fc.Issues = append(fc.Issues, Issue{Kind: GuideReplayGain, Fields: []string{"replaygain_track_gain"}})
	}
	return fc
}

func hasKey(f tagio.File, key string) bool {
	for k := range f.Tags {
		if strings.EqualFold(k, key) {
			return true
		}
	}
	return false
}

func (fc facts) quality() source.Quality {
	return source.Quality{Codec: fc.Codec, BitDepth: fc.BitDepth, SampleRate: fc.SampleRate}
}

// discFolder matches a disc's own folder inside an album's, such as "CD 2".
var discFolder = regexp.MustCompile(`(?i)^(cd|disc|disk)\s*\d+$`)

// releaseFolder is the folder a file's album lives in: its own folder, or
// the one above a disc folder.
func releaseFolder(rel string) string {
	dir := path.Dir(rel)
	if discFolder.MatchString(path.Base(dir)) {
		return path.Dir(dir)
	}
	return dir
}
