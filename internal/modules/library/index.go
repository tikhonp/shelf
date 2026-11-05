package library

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/tikhonp/shelf/internal/modules/library/tagio"
	"github.com/tikhonp/shelf/internal/source"
)

// Timings of the index.
const (
	// IndexEvery is how often the index walks the music folder when nothing
	// asks sooner. A walk reads only files whose size or mtime changed.
	IndexEvery = 15 * time.Minute
	// saveEvery is how many read files a walk writes at once, so a long
	// first walk keeps its progress over a restart.
	saveEvery = 200
	// configTimeout bounds the read of Navidrome's settings.
	configTimeout = 10 * time.Second
)

// Topic is the hub topic of index events.
const Topic = "library"

// Status is the index's state, for the Status page and the event stream.
type Status struct {
	// Indexing is true while a walk runs.
	Indexing bool `json:"indexing"`
	// Ready is true once a walk has finished, here or before a restart.
	Ready  bool `json:"ready"`
	Albums int  `json:"albums"`
	Tracks int  `json:"tracks"`
	// NavidromeAlbums counts distinct Navidrome album IDs, the number to
	// hold against Navidrome's own album count.
	NavidromeAlbums int `json:"navidrome_albums"`
	Unreadable      int `json:"unreadable"`
	// Read counts the files the running or last walk read.
	Read      int        `json:"read"`
	LastIndex *time.Time `json:"last_index"`
	// Rules says where the Navidrome settings came from: "navidrome",
	// "cached" or "default".
	Rules string `json:"rules"`
	Error string `json:"error"`
	// Locked counts the files shelf can't write because another user owns
	// them or their group isn't one of shelf's.
	Locked int `json:"locked"`
	// Folders are the music folder's top folders.
	Folders []FolderState `json:"folders"`
}

// FolderState is one top folder, with whether the probe at start could
// write in it and how many of its files are locked.
type FolderState struct {
	Name     string `json:"name"`
	Writable bool   `json:"writable"`
	Error    string `json:"error,omitempty"`
	Locked   int    `json:"locked"`
}

// ConfigReader reads Navidrome's tag settings. The navidrome module is it.
type ConfigReader func(ctx context.Context) (tagio.NavidromeConfig, error)

// index walks the music folder into the store.
type index struct {
	root string
	// uid is the owner a file needs before shelf writes it.
	uid    int
	store  store
	config ConfigReader
	// recs reads the downloads' records, and sources is the registry the
	// walk names sources from. Either may be nil.
	recs    RecordsReader
	sources *source.Registry
	// lastRecs are the records last read, for a walk whose read fails.
	lastRecs []Record
	// publish sends a status to the hub.
	publish func(Status)
	log     interface {
		Info(string, ...any)
		Warn(string, ...any)
	}

	mu    sync.Mutex
	state Status
	cur   *tagio.Rules
	files map[string]*file
	// saved sums the albums last written, so a walk that finds the same
	// albums writes nothing.
	saved [32]byte
}

// current are the rules the last walk read under.
func (ix *index) current() *tagio.Rules {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	if ix.cur == nil {
		return tagio.DefaultRules()
	}
	return ix.cur
}

func (ix *index) status() Status {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	st := ix.state
	// Walks update the folders in place.
	st.Folders = append([]FolderState{}, st.Folders...)
	return st
}

func (ix *index) set(fn func(*Status)) {
	ix.mu.Lock()
	fn(&ix.state)
	ix.mu.Unlock()
	ix.publish(ix.status())
}

// load reads the index the last run left.
func (ix *index) load(ctx context.Context) error {
	files, err := ix.store.files(ctx)
	if err != nil {
		return err
	}
	ix.files = make(map[string]*file, len(files))
	for _, f := range files {
		ix.files[f.Path] = f
	}
	albums, err := ix.store.counts(ctx)
	if err != nil {
		return err
	}
	nd, err := ix.store.navidromeAlbums(ctx)
	if err != nil {
		return err
	}
	ix.set(func(s *Status) {
		s.Ready = len(files) > 0
		s.Albums, s.NavidromeAlbums, s.Tracks = albums.Albums, nd, 0
		for _, f := range files {
			if f.Error == "" {
				s.Tracks++
			} else {
				s.Unreadable++
			}
		}
	})
	return nil
}

// rules reads Navidrome's settings, or falls back to the ones last read,
// or to Navidrome's defaults.
func (ix *index) rules(ctx context.Context) (*tagio.Rules, string) {
	if ix.config != nil {
		cctx, cancel := context.WithTimeout(ctx, configTimeout)
		c, err := ix.config(cctx)
		cancel()
		if err == nil {
			if err := ix.store.cacheConfig(ctx, c); err != nil {
				ix.log.Warn("library: keep the Navidrome settings", "error", err)
			}
			return tagio.NewRules(c), "navidrome"
		}
		if ctx.Err() != nil {
			return tagio.DefaultRules(), "default"
		}
		ix.log.Warn("library: read the Navidrome settings", "error", err)
	}
	if c, ok, err := ix.store.cachedConfig(ctx); err == nil && ok {
		return tagio.NewRules(c), "cached"
	}
	return tagio.DefaultRules(), "default"
}

// records reads the downloads' records, or keeps the ones last read, so one
// failed read changes no album's source.
func (ix *index) records(ctx context.Context) []Record {
	if ix.recs == nil {
		return nil
	}
	recs, err := ix.recs(ctx)
	if err != nil {
		if ctx.Err() == nil {
			ix.log.Warn("library: read the downloads' records", "error", err)
		}
		return ix.lastRecs
	}
	ix.lastRecs = recs
	return recs
}

// entry is an audio file the walk found.
type entry struct {
	rel  string
	info fs.FileInfo
}

// walk brings the index up to date with the music folder. It reads only
// files whose size, mtime, rules or facts version changed, and touches no
// file.
func (ix *index) walk(ctx context.Context) error {
	ix.set(func(s *Status) { s.Indexing, s.Read, s.Error = true, 0, "" })
	err := ix.run(ctx)
	now := time.Now().UTC()
	ix.set(func(s *Status) {
		s.Indexing = false
		if err != nil && ctx.Err() == nil {
			s.Error = err.Error()
		} else if err == nil {
			s.Ready, s.LastIndex = true, &now
		}
	})
	return err
}

func (ix *index) run(ctx context.Context) error {
	rules, from := ix.rules(ctx)
	ix.mu.Lock()
	ix.cur = rules
	ix.mu.Unlock()
	ix.set(func(s *Status) { s.Rules = from })
	entries, images, notes, err := scan(ix.root, ix.log)
	if err != nil {
		return err
	}
	locked := map[string]int{}
	for _, e := range entries {
		if tagio.Locked(e.info, ix.uid) {
			top, _, _ := strings.Cut(e.rel, "/")
			locked[top]++
		}
	}
	seen := make(map[string]bool, len(entries))
	var batch []*file
	read := 0
	for _, e := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		seen[e.rel] = true
		old := ix.files[e.rel]
		if old != nil && old.Size == e.info.Size() && old.ModNS == e.info.ModTime().UnixNano() &&
			old.Rules == rules.ID() && old.Version == factsVersion {
			continue
		}
		f := readFile(rules, filepath.Join(ix.root, filepath.FromSlash(e.rel)), e.rel, e.info)
		if old != nil {
			f.ID, f.FirstSeen = old.ID, old.FirstSeen
		}
		ix.files[e.rel] = f
		batch = append(batch, f)
		read++
		if len(batch) >= saveEvery {
			if err := ix.store.saveFiles(ctx, batch, nil); err != nil {
				return err
			}
			batch = batch[:0]
			ix.set(func(s *Status) { s.Read = read })
		}
	}
	var gone []int64
	for p, f := range ix.files {
		if !seen[p] {
			if f.ID != 0 {
				gone = append(gone, f.ID)
			}
			delete(ix.files, p)
		}
	}
	if err := ix.store.saveFiles(ctx, batch, gone); err != nil {
		return err
	}
	all := make([]*file, 0, len(ix.files))
	unreadable := 0
	for _, f := range ix.files {
		all = append(all, f)
		if f.Error != "" {
			unreadable++
		}
	}
	albums := group(all, images, rules)
	tellSources(albums, ix.records(ctx), readBlocks(ix.root, notes), sourcesOf(ix.sources))
	if sum := albumsSum(albums); sum != ix.saved {
		if err := ix.store.saveAlbums(ctx, albums); err != nil {
			return err
		}
		ix.saved = sum
	}
	nd := map[string]bool{}
	for _, a := range albums {
		nd[a.AlbumID] = true
	}
	if read > 0 || len(gone) > 0 {
		ix.log.Info("library: indexed", "read", read, "gone", len(gone), "albums", len(albums), "unreadable", unreadable)
	}
	ix.set(func(s *Status) {
		s.Read, s.Albums, s.NavidromeAlbums = read, len(albums), len(nd)
		s.Tracks, s.Unreadable = len(all)-unreadable, unreadable
		s.Locked = 0
		for i := range s.Folders {
			s.Folders[i].Locked = locked[s.Folders[i].Name]
		}
		for _, n := range locked {
			s.Locked += n
		}
	})
	return nil
}

// probe writes and removes a temp file in each top folder of the music
// folder, so the Status page can name a folder shelf can't write. It runs
// once at start, because each probe moves the folder's mtime.
func (ix *index) probe() {
	entries, err := os.ReadDir(ix.root)
	if err != nil {
		ix.log.Warn("library: list the music folder", "error", err)
		return
	}
	out := []FolderState{}
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() || dotEntry(name) || slices.ContainsFunc(systemDirs, func(s string) bool { return strings.EqualFold(s, name) }) {
			continue
		}
		fs := FolderState{Name: name, Writable: true}
		if err := tagio.Probe(filepath.Join(ix.root, name)); err != nil {
			fs.Writable, fs.Error = false, strings.ReplaceAll(err.Error(), ix.root+string(filepath.Separator), "")
			ix.log.Warn("library: shelf can't write in a folder", "folder", name, "error", err)
		}
		out = append(out, fs)
	}
	ix.set(func(s *Status) { s.Folders = out })
}

// albumsSum sums everything saveAlbums writes.
func albumsSum(albums []*album) [32]byte {
	h := sha256.New()
	enc := json.NewEncoder(h)
	for _, a := range albums {
		ids := make([]int64, len(a.Files))
		for i, f := range a.Files {
			ids[i] = f.ID
		}
		_ = enc.Encode([]any{a.Folder, a.AlbumID, a.Title, a.Artist, a.Year, a.Quality, a.LengthMS, a.Size,
			a.Added, a.HasMBID, a.Issues, ids, a.Source})
	}
	var out [32]byte
	h.Sum(out[:0])
	return out
}

// readFile reads one file into the index, or records why it can't be read.
func readFile(rules *tagio.Rules, abs, rel string, info fs.FileInfo) *file {
	f := &file{Path: rel, Size: info.Size(), ModNS: info.ModTime().UnixNano(), Rules: rules.ID(),
		Version: factsVersion, FirstSeen: info.ModTime().UTC()}
	tf, err := tagio.Read(abs)
	if err != nil {
		// The error names the absolute path, which the row already holds.
		f.Error = strings.ReplaceAll(err.Error(), abs+": ", "")
		return f
	}
	f.Size, f.ModNS, f.Revision = tf.Size, tf.ModTime.UnixNano(), tf.Revision()
	f.Facts = read(rules, tf, rel)
	return f
}

// imageExts are the extensions Navidrome counts as images for covers.
var imageExts = map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true, ".bmp": true, ".avif": true}

// scan lists the audio files under root as Navidrome sees them, and the
// image files of each folder, and the folders that hold a shelf.txt. It skips what Navidrome skips: names with one
// leading dot, its list of system folders, and folders whose .ndignore holds
// no pattern. shelf also skips symlinks, and the census found none.
func scan(root string, log interface{ Warn(string, ...any) }) ([]entry, map[string][]string, map[string]bool, error) {
	if _, err := os.Stat(root); err != nil {
		return nil, nil, nil, err
	}
	var entries []entry
	images := map[string][]string{}
	notes := map[string]bool{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == root {
				return err
			}
			log.Warn("library: skip an unreadable folder", "path", p, "error", err)
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		name := d.Name()
		if p != root && (dotEntry(name) || d.IsDir() && slices.ContainsFunc(systemDirs, func(s string) bool { return strings.EqualFold(s, name) })) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if ignoreAll(p, log) {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if name == "shelf.txt" {
			notes[path.Dir(rel)] = true
			return nil
		}
		ext := strings.ToLower(path.Ext(name))
		if imageExts[ext] {
			dir := path.Dir(rel)
			images[dir] = append(images[dir], strings.ToLower(name))
			return nil
		}
		if tagio.FormatOf(name) == nil {
			return nil
		}
		info, err := d.Info()
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		entries = append(entries, entry{rel: rel, info: info})
		return nil
	})
	return entries, images, notes, err
}

// systemDirs are the folders Navidrome never scans.
var systemDirs = []string{"$RECYCLE.BIN", "#snapshot", "@Recycle", "@eaDir", "@Recently-Snapshot", ".git", ".streams", "lost+found"}

// dotEntry is a name with exactly one leading dot. Album folders may start
// with an ellipsis, and Navidrome scans those.
func dotEntry(name string) bool {
	return strings.HasPrefix(name, ".") && !strings.HasPrefix(name, "..")
}

// ignoreAll reports a folder whose .ndignore holds no pattern, which makes
// Navidrome skip all of it. shelf doesn't match patterns, so a .ndignore
// with some is logged and its folder indexed.
func ignoreAll(dir string, log interface{ Warn(string, ...any) }) bool {
	b, err := os.ReadFile(filepath.Join(dir, ".ndignore"))
	if err != nil {
		return false
	}
	for line := range strings.Lines(string(b)) {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
			log.Warn("library: a .ndignore with patterns is not followed", "path", dir)
			return false
		}
	}
	return true
}
