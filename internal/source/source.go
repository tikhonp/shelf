// Package source holds what every source shares: catalog types, the
// interfaces a source implements, and the registry the other modules use to
// reach sources. Code outside a source's own module talks to it only through
// this package, so it never knows which source it talks to.
package source

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Kind is the kind of an item.
type Kind string

const (
	KindTrack    Kind = "track"
	KindAlbum    Kind = "album"
	KindPlaylist Kind = "playlist"
	KindArtist   Kind = "artist"
	KindLabel    Kind = "label"
)

// Kinds lists every kind.
var Kinds = []Kind{KindTrack, KindAlbum, KindPlaylist, KindArtist, KindLabel}

// Valid reports whether k is a known kind.
func (k Kind) Valid() bool {
	for _, v := range Kinds {
		if k == v {
			return true
		}
	}
	return false
}

// Collection reports whether k expands into one download per album.
func (k Kind) Collection() bool { return k == KindArtist || k == KindLabel }

// Ref names an item in a source's catalog.
type Ref struct {
	Source string `json:"source"` // "qobuz", "applemusic"
	Kind   Kind   `json:"kind"`
	ID     string `json:"id"`
}

func (r Ref) String() string { return r.Source + ":" + string(r.Kind) + ":" + r.ID }

// Valid reports whether every part of r is set and the kind is known.
func (r Ref) Valid() bool { return r.Source != "" && r.ID != "" && r.Kind.Valid() }

// ParseRef reads "source:kind:id". The ID may contain colons.
func ParseRef(s string) (Ref, error) {
	parts := strings.SplitN(s, ":", 3)
	if len(parts) != 3 {
		return Ref{}, fmt.Errorf("source: %q is not source:kind:id", s)
	}
	r := Ref{Source: parts[0], Kind: Kind(parts[1]), ID: parts[2]}
	if !r.Valid() {
		return Ref{}, fmt.Errorf("source: %q is not source:kind:id", s)
	}
	return r, nil
}

// ErrNotFound is what a Resolver answers for an item its catalog doesn't have.
var ErrNotFound = errors.New("source: no such item")

// Unavailable is a source that can't answer now, with a reason people may
// read. Other source errors stay in the logs, since they may hold a URL with
// a token.
type Unavailable struct{ Reason string }

func (e *Unavailable) Error() string { return e.Reason }

// Source is a music service shelf searches and downloads from. A source
// implements Source and any of the interfaces below.
type Source interface {
	// ID is short, lowercase and stable: it is stored in every download.
	ID() string
	// Name is what people read, such as "Apple Music".
	Name() string
	// Health answers from what the source already knows. It must not wait on
	// the network.
	Health(ctx context.Context) Health
}

// Switch is a source a module can register while it is switched off. shelf
// still recognises its links and refuses them with a clear message. A
// source without Switch is always on.
type Switch interface {
	Enabled() bool
}

// LinkParser recognises the source's links.
type LinkParser interface {
	ParseLink(raw string) (Ref, bool)
}

// LinkRefuser is a LinkParser that also knows links of its own that shelf
// can't download yet, such as music videos and stations. ParseLink doesn't
// claim them; RefuseLink does, so the bot can say why.
type LinkRefuser interface {
	RefuseLink(raw string) bool
}

// Searcher searches the source's catalog.
type Searcher interface {
	// Search finds items of q.Kind: album, track, artist or playlist.
	Search(ctx context.Context, q Query) (Page, error)
	// SearchKinds are the kinds Search accepts.
	SearchKinds() []Kind
}

// Resolver reads one item. An album or playlist comes with its tracks, an
// artist or label with its sections. ErrNotFound for an unknown item.
type Resolver interface {
	Item(ctx context.Context, ref Ref) (Item, error)
}

// Downloader writes items into the music folder.
type Downloader interface {
	// Formats lists what a download can ask for. One has Default set.
	Formats() []Format
	// Download fetches an album, playlist or track. It skips files that are
	// already complete, so it can run again after a restart. A failed track
	// goes to the Reporter; the error is for a download that could not run
	// at all, or whose context ended.
	Download(ctx context.Context, req DownloadRequest, rep Reporter) (DownloadResult, error)
}

// Library is the music server that serves the folder, such as Navidrome.
type Library interface {
	StartScan(ctx context.Context) error
	ScanStatus(ctx context.Context) (ScanStatus, error)
}

// ScanStatus is the library's scan state.
type ScanStatus struct {
	Scanning bool      `json:"scanning"`
	Count    int       `json:"count"`
	LastScan time.Time `json:"last_scan"`
}

// Health is a source's state for the Status page.
type Health struct {
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
	// Detail is source-specific, such as the Apple Music wrapper's state.
	Detail any `json:"detail,omitempty"`
}

// Query is a search.
type Query struct {
	Kind   Kind
	Text   string
	Limit  int
	Offset int
}

// Page is one page of search results.
type Page struct {
	Items  []Item `json:"items"`
	Total  int    `json:"total"`
	Limit  int    `json:"limit"`
	Offset int    `json:"offset"`
}

// Item is what the UI and shelf.txt show of an item.
type Item struct {
	Ref         Ref    `json:"ref"`
	Title       string `json:"title"`
	Version     string `json:"version,omitempty"`
	Artist      string `json:"artist,omitempty"`
	Album       string `json:"album,omitempty"` // a track's album
	Year        int    `json:"year,omitempty"`
	ReleaseDate string `json:"release_date,omitempty"` // 2006-01-02
	Label       string `json:"label,omitempty"`
	UPC         string `json:"upc,omitempty"`
	ISRC        string `json:"isrc,omitempty"`
	Genre       string `json:"genre,omitempty"`
	Explicit    bool   `json:"explicit,omitempty"`
	TrackCount  int    `json:"track_count,omitempty"`
	Duration    int    `json:"duration,omitempty"` // seconds
	CoverURL    string `json:"cover_url,omitempty"`
	// URL is the item's page on the source's site.
	URL     string  `json:"url,omitempty"`
	Quality Quality `json:"quality"`
	// Tracks of an album or playlist, in release order.
	Tracks []Track `json:"tracks,omitempty"`
	// Sections of an artist or label: albums, EPs and singles, and so on.
	Sections []Section `json:"sections,omitempty"`
	// Status is shelf's own: what it knows of the item's downloads. Sources
	// leave it nil.
	Status *Status `json:"status,omitempty"`
}

// Track is one track of an album or playlist.
type Track struct {
	ID       string  `json:"id"`
	Position int     `json:"position"` // 1..n in release order
	Disc     int     `json:"disc,omitempty"`
	Title    string  `json:"title"`
	Version  string  `json:"version,omitempty"`
	Artist   string  `json:"artist,omitempty"`
	ISRC     string  `json:"isrc,omitempty"`
	Explicit bool    `json:"explicit,omitempty"`
	Duration int     `json:"duration,omitempty"` // seconds
	Quality  Quality `json:"quality"`
	Status   *Status `json:"status,omitempty"`
}

// Section is a named group of an artist's or label's items.
type Section struct {
	ID    string `json:"id"` // "albums", "eps", "compilations", "appears_on"
	Title string `json:"title"`
	Items []Item `json:"items"`
}

// Status is what shelf knows of an item's downloads.
type Status struct {
	// State is "in_library", "queued", "running", "partial" or "failed".
	State       string `json:"state"`
	DownloadID  int64  `json:"download_id,omitempty"`
	TracksDone  int    `json:"tracks_done,omitempty"`
	TracksTotal int    `json:"tracks_total,omitempty"`
}

// Format is what a download can ask a source for.
type Format struct {
	ID    string `json:"id"`    // "qobuz:27", "applemusic:alac"
	Label string `json:"label"` // "Max"
	// Description is the format in words, as shelf.txt records it: "Qobuz
	// 27, FLAC up to 24-bit 192 kHz".
	Description string `json:"description"`
	Default     bool   `json:"default,omitempty"`
}

// DefaultFormat is the format marked Default, or the first one.
func DefaultFormat(formats []Format) (Format, bool) {
	for _, f := range formats {
		if f.Default {
			return f, true
		}
	}
	if len(formats) > 0 {
		return formats[0], true
	}
	return Format{}, false
}

// FindFormat returns the format with that ID.
func FindFormat(formats []Format, id string) (Format, bool) {
	for _, f := range formats {
		if f.ID == id {
			return f, true
		}
	}
	return Format{}, false
}

// DownloadRequest asks a source for one item.
type DownloadRequest struct {
	DownloadID int64
	Ref        Ref
	Format     string
	// Force asks again for what the source's own records call downloaded.
	Force bool
	// MusicDir is the music root. The source writes under its own folder
	// there and reports paths relative to it.
	MusicDir string
}

// Reporter hears a download's progress. Positions are 1..n in release
// order. Its methods may be called from several goroutines.
type Reporter interface {
	// Plan lists the tracks the source is about to fetch, replacing those
	// the download showed before.
	Plan(tracks []Track)
	// Start says the track at position began.
	Start(position int)
	// Progress says how many bytes of the track at position are written.
	Progress(position int, written int64)
	// Done says the track at position ended.
	Done(position int, r TrackResult)
	// Log is the download's log, which the UI streams.
	Log() Logger
}

// Logger writes the download's log.
type Logger interface {
	Debug(format string, args ...any)
	Info(format string, args ...any)
	Warn(format string, args ...any)
	Error(format string, args ...any)
}

// TrackResult is how one track ended.
type TrackResult struct {
	// Path is the file, relative to the music root.
	Path    string
	Quality Quality
	Size    int64
	// Skipped means the file was already complete.
	Skipped bool
	// Err is set when the track failed.
	Err error
}

// DownloadResult is what a download wrote.
type DownloadResult struct {
	// Folders are the release folders, relative to the music root: the album
	// folder, the playlist folder, or the folder a single track landed in.
	Folders []string
	// Files are every file written, relative to the music root.
	Files []string
	// Tool is what wrote the files, such as "amdl 0.1.4".
	Tool string
	// TrackNoun is what the catalog calls a track, for shelf.txt: "track"
	// when empty, "song" for Apple Music.
	TrackNoun string
}
