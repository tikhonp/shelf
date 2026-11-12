package downloads

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/tikhonp/shelf/internal/platform/db"
	"github.com/tikhonp/shelf/internal/platform/identity"
	"github.com/tikhonp/shelf/internal/source"
)

// Status is where a download is: queued, then running, then done, partial,
// failed or canceled. Skipped is set at creation and never changes.
type Status string

const (
	Queued   Status = "queued"
	Running  Status = "running"
	Done     Status = "done"
	Partial  Status = "partial"
	Failed   Status = "failed"
	Canceled Status = "canceled"
	Skipped  Status = "skipped"
)

// Final reports whether the download has ended.
func (s Status) Final() bool { return s != Queued && s != Running }

// Retryable reports whether Retry accepts a download in this status.
func (s Status) Retryable() bool { return s == Failed || s == Partial || s == Canceled }

// Origins.
const (
	OriginWeb      = "web"
	OriginTelegram = "telegram"
	OriginCLI      = "cli"
)

// OriginRef is what the bot needs to edit its status message. It never
// holds tokens.
type OriginRef struct {
	ChatID           int64 `json:"chat_id,omitempty"`
	RequestMessageID int   `json:"request_message_id,omitempty"`
	StatusMessageID  int   `json:"status_message_id,omitempty"`
	// StatusIsText marks a status message sent as text, because the cover
	// could not be sent. Its edits change the text, not a caption.
	StatusIsText bool   `json:"status_is_text,omitempty"`
	UserID       int64  `json:"user_id,omitempty"`
	Username     string `json:"username,omitempty"`
}

// Download is one request to fetch an item.
type Download struct {
	AuthorID            *string
	Author              string
	ID, ParentID, JobID int64
	Ref                 source.Ref
	URL                 string
	Format              string
	Force               bool
	Title, Artist       string
	CoverURL            string
	Status              Status
	// Error is why the download failed, or a note on how it ended, such as
	// "nothing to download".
	Error                                 string
	TracksTotal, TracksDone, TracksFailed int
	Bytes                                 int64
	// Folder is the release folder, relative to the music root.
	Folder    string
	Origin    string
	OriginRef OriginRef
	Notified  bool
	CreatedAt time.Time
	// StartedAt and FinishedAt are zero until set.
	StartedAt, FinishedAt time.Time
}

// Track is one track of a download.
type Track struct {
	Position   int
	Disc       int
	ItemID     string
	Title      string
	Artist     string
	ISRC       string
	Status     string // pending, running, done, skipped, failed
	Path       string
	Codec      string
	BitDepth   int
	SampleRate int
	Size       int64
	Error      string
}

func (t Track) quality() source.Quality {
	return source.Quality{Codec: t.Codec, BitDepth: t.BitDepth, SampleRate: t.SampleRate}
}

// Detail is a download with its tracks and children.
type Detail struct {
	Download
	Tracks   []Track
	Children []Download
	// ProvenancePath is the release folder's shelf.txt, relative to the music
	// root, once the download has written it.
	ProvenancePath string
}

// Filter narrows List.
type Filter struct {
	Statuses []Status
	Source   string
	// Folder keeps the downloads of one release folder, relative to the music
	// root. It takes in an artist's or label's albums, which hold the folders.
	Folder string
	// Before is an ID cursor: downloads older than this one.
	Before int64
	Limit  int
}

// Event is the hub's download event.
type Event struct {
	Author       string  `json:"author"`
	AuthorID     *string `json:"author_id"`
	ID           int64   `json:"id"`
	ParentID     *int64  `json:"parent_id"`
	Status       Status  `json:"status"`
	TracksDone   int     `json:"tracks_done"`
	TracksTotal  int     `json:"tracks_total"`
	TracksFailed int     `json:"tracks_failed"`
	// Current is the title of the track being fetched.
	Current string `json:"current"`
	Bytes   int64  `json:"bytes"`
	Error   string `json:"error"`
}

// Topic is the hub topic of download events.
const Topic = "download"

var (
	ErrUnknownLink   = errors.New("downloads: link not from a known source")
	ErrRefusedLink   = errors.New("downloads: refused link")
	ErrUnknownSource = errors.New("downloads: unknown or disabled source")
	ErrNotRetryable  = errors.New("downloads: only failed, partial or canceled downloads can be retried")
	ErrUnknownFormat = errors.New("downloads: the source has no such format")
	ErrBadRequest    = errors.New("downloads: bad request")
	ErrNotFound      = errors.New("downloads: no such download")
	ErrNotActive     = errors.New("downloads: the download has already ended")
)

// row is a downloads row as stored.
type row struct {
	AuthorID     *string       `db:"author_id"`
	Author       string        `db:"author"`
	ID           int64         `db:"id"`
	ParentID     sql.NullInt64 `db:"parent_id"`
	JobID        sql.NullInt64 `db:"job_id"`
	Source       string        `db:"source"`
	Kind         string        `db:"kind"`
	ItemID       string        `db:"item_id"`
	URL          string        `db:"url"`
	Format       string        `db:"format"`
	Force        bool          `db:"force"`
	Title        string        `db:"title"`
	Artist       string        `db:"artist"`
	CoverURL     string        `db:"cover_url"`
	Status       string        `db:"status"`
	Error        string        `db:"error"`
	TracksTotal  int           `db:"tracks_total"`
	TracksDone   int           `db:"tracks_done"`
	TracksFailed int           `db:"tracks_failed"`
	Bytes        int64         `db:"bytes"`
	Folder       string        `db:"folder"`
	Origin       string        `db:"origin"`
	OriginRef    string        `db:"origin_ref"`
	Notified     bool          `db:"notified"`
	CreatedAt    db.Time       `db:"created_at"`
	StartedAt    db.Time       `db:"started_at"`
	FinishedAt   db.Time       `db:"finished_at"`
}

const columns = `author_id, author, id, parent_id, job_id, source, kind, item_id, url, format, force, title, artist, cover_url, status,
	error, tracks_total, tracks_done, tracks_failed, bytes, folder, origin, origin_ref, notified, created_at,
	started_at, finished_at`

func (r row) download() Download {
	d := Download{AuthorID: r.AuthorID, Author: r.Author,
		ID: r.ID, ParentID: r.ParentID.Int64, JobID: r.JobID.Int64,
		Ref: source.Ref{Source: r.Source, Kind: source.Kind(r.Kind), ID: r.ItemID}, URL: r.URL, Format: r.Format,
		Force: r.Force, Title: r.Title, Artist: r.Artist, CoverURL: r.CoverURL, Status: Status(r.Status), Error: r.Error,
		TracksTotal: r.TracksTotal, TracksDone: r.TracksDone, TracksFailed: r.TracksFailed, Bytes: r.Bytes,
		Folder: r.Folder, Origin: r.Origin, Notified: r.Notified,
		CreatedAt: r.CreatedAt.Time, StartedAt: r.StartedAt.Time, FinishedAt: r.FinishedAt.Time,
	}
	_ = json.Unmarshal([]byte(r.OriginRef), &d.OriginRef)
	return d
}

type trackRow struct {
	Position   int    `db:"position"`
	Disc       int    `db:"disc"`
	ItemID     string `db:"item_id"`
	Title      string `db:"title"`
	Artist     string `db:"artist"`
	ISRC       string `db:"isrc"`
	Status     string `db:"status"`
	Path       string `db:"path"`
	Codec      string `db:"codec"`
	BitDepth   int    `db:"bit_depth"`
	SampleRate int    `db:"sample_rate"`
	Size       int64  `db:"size"`
	Error      string `db:"error"`
}

const trackColumns = `position, disc, item_id, title, artist, isrc, status, path, codec,
	COALESCE(bit_depth, 0) AS bit_depth, COALESCE(sample_rate, 0) AS sample_rate, COALESCE(size, 0) AS size, error`

// event is d as the hub carries it.
func (d Download) event(current string) Event {

	e := Event{Author: d.authorName(), AuthorID: d.AuthorID, ID: d.ID, Status: d.Status, TracksDone: d.TracksDone, TracksTotal: d.TracksTotal,
		TracksFailed: d.TracksFailed, Current: current, Bytes: d.Bytes, Error: d.Error}
	if d.ParentID != 0 {
		p := d.ParentID
		e.ParentID = &p
	}
	return e
}

// createdBy is the jobs' created_by for a request from origin.
func createdBy(origin string, ref OriginRef) string {
	if origin == OriginTelegram && ref.UserID != 0 {
		return "telegram:" + strconv.FormatInt(ref.UserID, 10)
	}
	return origin
}

// nullable stores zero as NULL, for columns where zero means unknown.
func nullable[T int | int64](v T) any {
	if v == 0 {
		return nil
	}
	return v
}

func cut(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

func (d Download) authorName() string {
	if d.Author != "" {
		return d.Author
	}
	if d.OriginRef.UserID != 0 {
		return identity.Telegram(d.OriginRef.UserID, d.OriginRef.Username).Name
	}
	return "Unknown"
}
