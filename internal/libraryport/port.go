// Package libraryport defines the library operations available to other modules.
package libraryport

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jmoiron/sqlx"
)

type Port interface {
	Albums(ctx context.Context, q AlbumQuery) (AlbumPage, error)
	// Album reads the files now, like GET /api/library/albums/:id, but never
	// runs ffmpeg: Issues has no bad_cover.
	Album(ctx context.Context, id int64) (Album, error)
	// AlbumsOf returns the current album of each path, 0 for a path the index
	// has lost.
	AlbumsOf(ctx context.Context, paths []string) (map[string]int64, error)
	// Suggestions is GET /api/library/albums/:id/suggestions. External false
	// gives only local suggestions. It never enqueues a fingerprint job.
	Suggestions(ctx context.Context, albumID int64, external bool) (json.RawMessage, error)
	AlbumCover(ctx context.Context, albumID int64) ([]byte, string, error) // bytes, MIME

	History(ctx context.Context, after int64, limit int) (HistoryPage, error)
	// Changes reports linked changes. A missing ID left the history.
	Changes(ctx context.Context, ids []int64) (map[int64]ChangeStatus, error)

	PutCover(ctx context.Context, b []byte) (Image, error)             // inspect + store, 32 MiB
	MaterializeOffer(ctx context.Context, offer string) (Image, error) // library_remote ID
	CoverInfo(ctx context.Context, id string) (Image, error)           // ErrNotFound if absent
	CoverBytes(ctx context.Context, id string) ([]byte, error)
	// Pin keeps covers through retention until Unpin. Both run in the
	// caller's transaction.
	Pin(ctx context.Context, tx *sqlx.Tx, holder string, ids []string) error
	Unpin(ctx context.Context, tx *sqlx.Tx, holder string) error

	// Plan runs the Save checks without writing: it reads the files now,
	// resolves the album, builds the stored change and the preview. A refusal
	// is in Plan.Refusal, not err.
	Plan(ctx context.Context, r PlanRequest) (Plan, error)
	// Enqueue inserts the change and its write job in tx. The author comes
	// from identity.From(ctx). after kicks the jobs and publishes the change;
	// call it after commit. ErrMissingConfirm if r.Confirm lacks a warning
	// kind; ErrOriginTaken if origin exists.
	Enqueue(ctx context.Context, tx *sqlx.Tx, p Plan, origin string) (changeID int64, after func(), err error)
}

var (
	ErrNotFound       = errors.New("libraryport: not found")
	ErrMissingConfirm = errors.New("libraryport: confirmation missing")
	ErrOriginTaken    = errors.New("libraryport: origin exists")
	ErrRefused        = errors.New("libraryport: refused") // bad image etc., message in wrapping error
)

type AlbumQuery struct {
	Search, Filter, Folder, Source, Sort string
	Desc                                 bool
	Limit, Offset                        int // Limit 1..100
}
type AlbumPage struct {
	Albums []AlbumSummary `json:"albums"`
	Total  int            `json:"total"`
}
type AlbumSummary struct {
	ID      int64   `json:"id"`
	Folder  string  `json:"folder"`
	Title   string  `json:"title"`
	Artist  string  `json:"artist"`
	Year    int     `json:"year"`
	Tracks  int     `json:"tracks"`
	Quality string  `json:"quality"`
	HasMBID bool    `json:"has_mbid"`
	Issues  []Issue `json:"issues"`
	Source  string  `json:"source"` // source ID or ""
}
type Issue struct {
	Kind   string   `json:"kind"`
	Fields []string `json:"fields,omitempty"`
}
type Album struct {
	AlbumSummary
	Stale  bool    `json:"stale"`
	Tracks []Track `json:"tracks"`
}
type Track struct {
	ID       int64   `json:"id"`
	Path     string  `json:"path"` // inside the music folder
	Revision string  `json:"revision"`
	Disc     int     `json:"disc"`
	Track    int     `json:"track"`
	Title    string  `json:"title"`
	Artist   string  `json:"artist"`
	Format   string  `json:"format"`
	Quality  string  `json:"quality"`
	Error    string  `json:"error,omitempty"`
	Issues   []Issue `json:"issues"`
	Tags     []Tag   `json:"tags"` // standard then extended, as the editor groups them
	HasCover bool    `json:"has_cover"`
}
type Tag struct {
	Key      string   `json:"key"`
	Fields   []string `json:"fields,omitempty"`
	Values   []string `json:"values"`
	ReadOnly bool     `json:"read_only,omitempty"`
	Extended bool     `json:"extended,omitempty"`
}

type PlanRequest struct {
	// Files are the tracks with their edits. Every file must be on one
	// current album. Revisions are read now; a caller never supplies them.
	Files []PlanFile
	Cover *PlanCover // adds every track of the album
	// Confirm lists the warning kinds the reviewer saw. Plan reports
	// Missing; Enqueue refuses when Missing is not empty.
	Confirm []string
}
type PlanFile struct {
	TrackID int64
	Edits   []Edit
}
type Edit struct {
	Field  string   `json:"field,omitempty"`
	Key    string   `json:"key,omitempty"`
	Values []string `json:"values"`
	Anchor string   `json:"anchor,omitempty"`
}
type PlanCover struct {
	Image      string // cover store ID; "" with Remove
	Remove     bool
	EveryTrack bool
}
type Plan struct {
	AlbumID int64
	Preview Preview
	Refusal *Refusal
	Missing []string // warning kinds not in Confirm
	// Stored is the library's stored change, opaque to callers. Enqueue
	// takes it as is.
	Stored json.RawMessage
}
type Preview struct {
	Album    PreviewAlbum   `json:"album"`
	Files    []PreviewFile  `json:"files"`    // sorted by path
	Folder   []PreviewCover `json:"folder"`   // folder cover files, sorted by path
	Warnings []Warning      `json:"warnings"` // sorted by kind
	// Digest is the hex SHA-256 of the canonical JSON of Album.ID-free
	// content: files, folder and warnings. Revisions are excluded, so an
	// unrelated write leaves it unchanged.
	Digest string `json:"digest"`
}
type PreviewAlbum struct {
	ID     int64  `json:"id"`
	Folder string `json:"folder"`
	Title  string `json:"title"`
	Artist string `json:"artist"`
}
type PreviewFile struct {
	TrackID     int64       `json:"track_id"`
	Path        string      `json:"path"`
	Keys        []KeyChange `json:"keys"`         // sorted by key; only keys that change
	CoverBefore string      `json:"cover_before"` // "" none; equal to after when untouched
	CoverAfter  string      `json:"cover_after"`
}
type KeyChange struct {
	Key    string   `json:"key"`
	Before []string `json:"before"`
	After  []string `json:"after"`
}
type PreviewCover struct {
	Path   string `json:"path"`
	Before string `json:"before"` // hash, "" none
	After  string `json:"after"`  // hash, "" removes
}
type Warning struct {
	Kind    string `json:"kind"` // split | merge | album_artist | shared_cover
	Message string `json:"message"`
}
type Refusal struct {
	Error   string `json:"error"`
	Reason  string `json:"reason"` // refused | conflict | locked | busy | regrouped
	TrackID int64  `json:"track_id,omitempty"`
}

type Image struct {
	ID     string `json:"id"`
	MIME   string `json:"mime"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Size   int    `json:"size"`
}

type ChangeStatus struct {
	ID         int64   `json:"id"`
	State      string  `json:"state"` // queued | writing | done | partial | failed
	Reason     string  `json:"reason"`
	Error      string  `json:"error"`
	Undone     bool    `json:"undone"`
	Files      int     `json:"files"`
	Written    int     `json:"written"`
	FinishedAt *string `json:"finished_at"`
}

type HistoryPage struct {
	Changes []HistoryChange `json:"changes"` // ascending ID, after the cursor
	Next    int64           `json:"next"`    // cursor for the next page; equals after when done
	// TrimmedThrough is the highest change ID retention or Clear removed.
	// A cursor below it has a gap.
	TrimmedThrough int64 `json:"trimmed_through"`
}
type HistoryChange struct {
	ID        int64         `json:"id"`
	Kind      string        `json:"kind"` // save | undo | redo | delete | run
	Target    *int64        `json:"target"`
	Origin    string        `json:"origin"` // "" or "proposal:<p>/acceptance:<a>"
	AuthorID  string        `json:"author_id"`
	Author    string        `json:"author"`
	Folder    string        `json:"folder"`
	Title     string        `json:"title"`
	Artist    string        `json:"artist"`
	State     string        `json:"state"`
	Undone    bool          `json:"undone"`
	CreatedAt string        `json:"created_at"`
	Files     []PreviewFile `json:"files"` // empty for run heads and deletes
	Truncated bool          `json:"truncated,omitempty"`
}
