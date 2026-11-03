// Package jobs is the platform's job system: every long or remote piece of
// work is a stored job that runs in a worker pool of its queue, step by step,
// with a live log. Jobs survive a restart, can be cancelled and retried, and
// never overlap on one resource key. It is proxier's job system without the
// vault, the settings overrides and the events catalog.
package jobs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/tikhonp/shelf/internal/platform/db"
)

// Queue is a worker pool's name.
type Queue string

// The queues. A source whose ID names a queue downloads on it, so each
// source runs one download at a time and two sources run side by side.
const (
	QueueQobuz      Queue = "qobuz"
	QueueAppleMusic Queue = "applemusic"
	QueueLibrary    Queue = "library"
	QueueSystem     Queue = "system"
)

// Concurrency is how many jobs each queue runs at once.
var Concurrency = map[Queue]int{QueueQobuz: 1, QueueAppleMusic: 1, QueueLibrary: 1, QueueSystem: 1}

// State is where a job is in its life.
type State string

const (
	Queued      State = "queued"
	Running     State = "running"
	Succeeded   State = "succeeded"
	Failed      State = "failed"
	Cancelled   State = "cancelled"
	Interrupted State = "interrupted"
)

// Type is a kind of job a module registers at startup.
type Type struct {
	// Name is "<module>.<what>", e.g. "downloads.download"; the prefix must
	// be the registering module's name.
	Name        string
	Queue       Queue
	MaxAttempts int             // at least 1
	Backoff     []time.Duration // wait before attempt 2, 3, …; the last repeats
	Steps       []Step          // at least one, unique names
	// Merge combines a coalesced request's payload into the queued job's. Nil
	// is a shallow JSON-object merge where the incoming keys win.
	Merge func(queued, incoming json.RawMessage) (json.RawMessage, error)
	// OnFailed updates the module's state in the transaction that marks the
	// job failed.
	OnFailed func(ctx context.Context, tx *sqlx.Tx, j Info, err error) error
	// OnCancelled updates the module's state in the transaction that marks the
	// job cancelled.
	OnCancelled func(ctx context.Context, tx *sqlx.Tx, j Info, by string) error
	// NoResume fails the job after a restart instead of resuming it.
	NoResume bool
	// Quiet marks high-frequency work: a clean success is deleted after
	// QuietRetention.
	Quiet bool
}

// Step is one named, idempotent part of a job.
type Step struct {
	Name string // stable; stored and matched on resume, e.g. "fetch"
	Run  func(ctx context.Context, r *Run) error
}

// Info is what a job's callbacks learn about it.
type Info struct {
	ID          int64
	Type        string
	Queue       Queue
	Attempt     int // 1-based while running
	MaxAttempts int
	ResourceKey string
	CreatedBy   string
	RetryOf     int64
}

// Actor names work done by this job, for the created_by of the jobs it
// starts: "job:<id>".
func (i Info) Actor() string { return fmt.Sprintf("job:%d", i.ID) }

// Request asks for a job.
type Request struct {
	Type string
	// Queue, when set, runs the job on that queue instead of its type's.
	// One download type serves every source's queue.
	Queue         Queue
	Payload       any // marshalled to a JSON object; never secrets
	ResourceKey   string
	CoalescingKey string // unique within the type; any string
	Delay         time.Duration
	CreatedBy     string // required
	SkipIfBusy    bool
}

// QuietRetention is how long a clean quiet job is kept.
const QuietRetention = 24 * time.Hour

// Enqueued is the answer to a request.
type Enqueued struct {
	ID     int64
	Merged bool // merged into an existing queued job
}

var (
	ErrSkipped         = errors.New("jobs: skipped, the resource is busy")
	ErrCancelled       = errors.New("jobs: cancelled")
	ErrUnknownType     = errors.New("jobs: unknown job type")
	ErrUnknownQueue    = errors.New("jobs: unknown queue")
	ErrPayloadTooLarge = errors.New("jobs: payload over 64 KiB")
	ErrNotRetryable    = errors.New("jobs: only failed or cancelled jobs can be retried")
	ErrNotActive       = errors.New("jobs: the job has already finished")
	ErrNotFound        = errors.New("jobs: no such job")
)

// MaxPayload is the largest payload, in bytes.
const MaxPayload = 64 << 10

type permanent struct{ err error }

func (p permanent) Error() string { return p.err.Error() }
func (p permanent) Unwrap() error { return p.err }

// Permanent marks err as not worth another attempt.
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return permanent{err}
}

// IsPermanent reports whether err, or an error it wraps, was marked Permanent.
func IsPermanent(err error) bool {
	var p permanent
	return errors.As(err, &p)
}

type deferral struct {
	d      time.Duration
	reason string
}

func (d deferral) Error() string { return fmt.Sprintf("deferred for %s: %s", d.d, d.reason) }

// Defer queues the job again after d without counting the attempt.
func Defer(d time.Duration, reason string) error { return deferral{d, reason} }

// System owns the queues, workers and scheduler of one process.
type System struct {
	// Now is the clock; tests replace it.
	Now func() time.Time
	// Grace is how long running steps get to finish on shutdown.
	Grace time.Duration
	// Poll is how often an idle pool looks for work.
	Poll time.Duration
	// SchedulerPoll is how often the scheduler looks for due schedules.
	SchedulerPoll time.Duration
	// Location is the time zone of schedules that run at a time of day.
	Location *time.Location

	d      *db.DB
	log    *slog.Logger
	bootID string
	hub    *hub
	kickc  map[Queue]chan struct{} // one per pool: a kick wakes every pool

	mu        sync.Mutex
	types     map[string]Type
	schedules map[string]Schedule
	schedList []string
	running   map[int64]*Run // jobs this boot runs now
	ticks     map[string]time.Time
	started   bool
	jitter    func(max time.Duration) time.Duration
}

// New builds a System. Register types before Start. Steps get Grace to
// finish at shutdown; a download's fetch step never finishes in time, so it
// is cut and resumes on the next start. Docker stops a container after 10
// seconds, so the HTTP shutdown and the grace together must stay under that.
func New(d *db.DB, loc *time.Location, log *slog.Logger) *System {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	if loc == nil {
		loc = time.UTC
	}
	return &System{
		Now: time.Now, Grace: 3 * time.Second, Poll: 500 * time.Millisecond, SchedulerPoll: 5 * time.Second,
		Location: loc,
		d:        d, log: log, bootID: hex.EncodeToString(b), hub: newHub(), kickc: kickChans(),
		types: map[string]Type{}, schedules: map[string]Schedule{}, running: map[int64]*Run{},
		ticks: map[string]time.Time{},
		jitter: func(max time.Duration) time.Duration {
			if max <= 0 {
				return 0
			}
			var b [8]byte
			_, _ = rand.Read(b[:])
			n := int64(0)
			for _, x := range b {
				n = n<<8 | int64(x)
			}
			if n < 0 {
				n = -n
			}
			return time.Duration(n % int64(max))
		},
	}
}

func (s *System) now() db.Time { return db.At(s.Now()) }

// Register adds a module's job types.
func (s *System) Register(module string, types ...Type) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var errs []error
	for _, t := range types {
		names := map[string]bool{}
		switch {
		case !strings.HasPrefix(t.Name, module+"."):
			errs = append(errs, fmt.Errorf("job type %q must start with %q", t.Name, module+"."))
		case s.types[t.Name].Name != "":
			errs = append(errs, fmt.Errorf("job type %q registered twice", t.Name))
		case Concurrency[t.Queue] == 0:
			errs = append(errs, fmt.Errorf("job type %q: unknown queue %q", t.Name, t.Queue))
		case t.MaxAttempts < 1:
			errs = append(errs, fmt.Errorf("job type %q: MaxAttempts must be at least 1", t.Name))
		case len(t.Steps) == 0:
			errs = append(errs, fmt.Errorf("job type %q has no steps", t.Name))
		default:
			bad := false
			for _, st := range t.Steps {
				if st.Name == "" || st.Run == nil || names[st.Name] {
					errs = append(errs, fmt.Errorf("job type %q: step %q is empty or repeated", t.Name, st.Name))
					bad = true
					break
				}
				names[st.Name] = true
			}
			if !bad {
				s.types[t.Name] = t
			}
		}
	}
	return errors.Join(errs...)
}

func (s *System) typeOf(name string) (Type, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.types[name]
	return t, ok
}

// ScheduleActor is the created_by of a schedule's jobs.
func ScheduleActor(name string) string { return "schedule:" + name }

// ActorSystem is the actor of a cancel that no one asked for by name.
const ActorSystem = "system"

func cut(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
