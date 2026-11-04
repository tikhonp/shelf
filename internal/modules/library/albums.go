package library

import (
	"cmp"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/tikhonp/shelf/internal/modules/library/tagio"
)

// file is one audio file in the index.
type file struct {
	ID   int64
	Path string // inside the music folder, with slashes
	Size int64
	// ModNS is the mtime in nanoseconds. A file whose size and mtime match
	// the index isn't read again.
	ModNS    int64
	Revision string
	// Rules and Version name what the facts were derived under.
	Rules   string
	Version int
	// Error is why the file couldn't be read. Such a file joins no album.
	Error string
	// FirstSeen is the file's mtime when the index first saw it. It stays,
	// so an album keeps its place in "date added" after a retag.
	FirstSeen time.Time
	Facts     facts
}

// album is the files of one Navidrome album inside one release folder.
type album struct {
	Folder  string
	AlbumID string
	Files   []*file
	Title   string
	Artist  string
	Year    int
	Quality string
	// LengthMS and Size are sums over the tracks.
	LengthMS int64
	Size     int64
	Added    time.Time
	HasMBID  bool
	Issues   []Issue
}

// key names an album in the index.
type albumKey struct{ folder, id string }

// group sorts readable files into albums and runs the album checks. images
// maps each folder to the lowercase names of the image files in it.
func group(files []*file, images map[string][]string, r *tagio.Rules) []*album {
	by := map[albumKey]*album{}
	var out []*album
	for _, f := range files {
		if f.Error != "" {
			continue
		}
		k := albumKey{releaseFolder(f.Path), f.Facts.AlbumID}
		a := by[k]
		if a == nil {
			a = &album{Folder: k.folder, AlbumID: k.id}
			by[k] = a
			out = append(out, a)
		}
		a.Files = append(a.Files, f)
	}
	for _, a := range out {
		a.summarize()
		a.check(images, r)
	}
	splits(out)
	for _, a := range out {
		sortIssues(a.Issues)
	}
	slices.SortFunc(out, func(a, b *album) int {
		return cmp.Or(strings.Compare(a.Folder, b.Folder), strings.Compare(a.AlbumID, b.AlbumID))
	})
	return out
}

func (a *album) summarize() {
	slices.SortFunc(a.Files, func(x, y *file) int {
		return cmp.Or(cmp.Compare(x.Facts.Disc, y.Facts.Disc), cmp.Compare(x.Facts.Track, y.Facts.Track), strings.Compare(x.Path, y.Path))
	})
	first := a.Files[0].Facts
	a.Title, a.Artist = first.Album, first.AlbumArtist
	qualities := map[string]int{}
	best := ""
	for _, f := range a.Files {
		fc := f.Facts
		a.Year = max(a.Year, fc.Year)
		a.LengthMS += fc.LengthMS
		a.Size += f.Size
		a.HasMBID = a.HasMBID || fc.MBAlbumID != ""
		if a.Added.IsZero() || f.FirstSeen.Before(a.Added) {
			a.Added = f.FirstSeen
		}
		q := fc.quality().Short()
		qualities[q]++
		if best == "" || qualities[q] > qualities[best] {
			best = q
		}
	}
	a.Quality = best
}

func (a *album) check(images map[string][]string, r *tagio.Rules) {
	fields := map[Kind][]string{}
	has := map[Kind]bool{}
	add := func(k Kind, fs ...string) {
		has[k] = true
		for _, f := range fs {
			if !slices.Contains(fields[k], f) {
				fields[k] = append(fields[k], f)
			}
		}
	}
	for _, f := range a.Files {
		for _, is := range f.Facts.Issues {
			add(is.Kind, is.Fields...)
		}
	}
	for _, s := range shared {
		if differ(a.Files, s) {
			add(ValuesDiffer, s)
		}
	}
	for _, s := range releaseIDs {
		if differ(a.Files, s) {
			add(TwoReleases, s)
		}
	}
	if strings.EqualFold(a.Artist, tagio.VariousArtists) &&
		slices.ContainsFunc(a.Files, func(f *file) bool { return !f.Facts.Compilation }) {
		add(VariousArtists, "compilation")
	}
	if !a.hasCover(images, r) {
		add(NoCover)
	}
	for _, k := range Kinds {
		if has[k] {
			a.Issues = append(a.Issues, Issue{Kind: k, Fields: fields[k]})
		}
	}
}

// differ reports whether the tracks hold more than one value of a field. A
// track without the field counts as one more value.
func differ(files []*file, field string) bool {
	v := files[0].Facts.Shared[field]
	return slices.ContainsFunc(files[1:], func(f *file) bool { return f.Facts.Shared[field] != v })
}

// hasCover follows Navidrome's cover priority: an image file in one of the
// album's folders that matches a pattern, or an embedded picture.
func (a *album) hasCover(images map[string][]string, r *tagio.Rules) bool {
	if r.EmbeddedCovers() && slices.ContainsFunc(a.Files, func(f *file) bool { return f.Facts.HasPicture }) {
		return true
	}
	for _, dir := range a.folders() {
		for _, name := range images[dir] {
			for _, p := range r.CoverNames() {
				if ok, _ := path.Match(p, name); ok {
					return true
				}
			}
		}
	}
	return false
}

// folders are the album's release folder and every folder a track is in.
func (a *album) folders() []string { return albumFolders(a.Folder, pathsOf(a.Files)) }

// albumFolders are a release folder and the folders of its tracks, in the
// order Navidrome looks through them for an album's cover: the release
// folder, then the disc folders by name.
func albumFolders(release string, paths []string) []string {
	var discs []string
	for _, p := range paths {
		if d := path.Dir(p); d != release && !slices.Contains(discs, d) {
			discs = append(discs, d)
		}
	}
	slices.Sort(discs)
	return append([]string{release}, discs...)
}

func pathsOf(files []*file) []string {
	out := make([]string, len(files))
	for i, f := range files {
		out[i] = f.Path
	}
	return out
}

// splits marks albums of one release folder that share a name: Navidrome
// shows them apart. When their MusicBrainz album IDs differ, the folder
// also holds tracks of two releases.
func splits(albums []*album) {
	type nameKey struct{ folder, name string }
	by := map[nameKey][]*album{}
	for _, a := range albums {
		if n := a.Files[0].Facts.AlbumKey; n != "" {
			k := nameKey{a.Folder, n}
			by[k] = append(by[k], a)
		}
	}
	for _, group := range by {
		if len(group) < 2 {
			continue
		}
		ids := map[string]bool{}
		for _, a := range group {
			for _, f := range a.Files {
				if f.Facts.MBAlbumID != "" {
					ids[f.Facts.MBAlbumID] = true
				}
			}
		}
		for _, a := range group {
			a.addIssue(Issue{Kind: SplitAlbum})
			if len(ids) > 1 {
				a.addIssue(Issue{Kind: TwoReleases, Fields: []string{"musicbrainz_albumid"}})
			}
		}
	}
}

func (a *album) addIssue(is Issue) {
	for i := range a.Issues {
		if a.Issues[i].Kind == is.Kind {
			for _, f := range is.Fields {
				if !slices.Contains(a.Issues[i].Fields, f) {
					a.Issues[i].Fields = append(a.Issues[i].Fields, f)
				}
			}
			return
		}
	}
	a.Issues = append(a.Issues, is)
}

func sortIssues(is []Issue) {
	slices.SortStableFunc(is, func(x, y Issue) int {
		return cmp.Compare(slices.Index(Kinds, x.Kind), slices.Index(Kinds, y.Kind))
	})
}

// lamp reports whether an album has an issue that isn't style only.
func (a *album) lamp() bool {
	return slices.ContainsFunc(a.Issues, func(is Issue) bool { return !is.Kind.Guide() })
}
