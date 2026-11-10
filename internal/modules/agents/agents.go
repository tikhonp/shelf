// Package agents connects external agents to the library. An agent reads
// the library, keeps shared memory and files proposals; only a Shelf user
// accepts a proposal, and the library module writes it. The module owns the
// connections, proposals and their revisions, decisions, the durable
// feedback feed, memory and learning progress.
//
// The module is always registered, so its migrations run and the CLI can
// revoke connections while the feature is off. SHELF_AGENT_ENABLED decides
// whether /api/agent/v1, /api/inbox and the runner exist.
package agents

import (
	"context"
	"embed"
	"errors"
	"io/fs"
	"log/slog"
	"net/url"
	"sync"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/tikhonp/shelf/internal/libraryport"
	"github.com/tikhonp/shelf/internal/platform/db"
	"github.com/tikhonp/shelf/internal/platform/hub"
	"github.com/tikhonp/shelf/internal/platform/jobs"
	"github.com/tikhonp/shelf/internal/platform/module"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Name is the module's name.
const Name = "agents"

// Topic is the hub topic the inbox listens on.
const Topic = "inbox"

// changeTopic is the library's hub topic of change events.
const changeTopic = "change"

// RetentionJob removes old reviewed proposals, runs and usage rows.
const RetentionJob = "agents.retention"

// Accounts is the platform's account port, as module.Deps carries it.
type Accounts interface {
	AuthenticationEnabled() bool
	Exists(ctx context.Context, id int64) (bool, error)
}

// Module is the agents module.
type Module struct {
	// Now is the clock. Tests change it.
	Now func() time.Time
	// LookupWait is how long an external lookup waits for the gate.
	LookupWait time.Duration
	// ReconcileEvery and ReconcileDebounce pace the outcome reconciler.
	ReconcileEvery    time.Duration
	ReconcileDebounce time.Duration

	enabled  bool
	public   *url.URL
	log      *slog.Logger
	db       *db.DB
	hub      *hub.Hub
	accounts Accounts

	mu          sync.Mutex
	libraryPort libraryport.Port

	limiter *limiter
	gate    chan struct{}
	// reconcileGate keeps two reconciles from writing the same outcome. It is a
	// channel so Clear's catch-up can stop waiting at its deadline.
	reconcileGate chan struct{}
}

// New returns the module.
func New() *Module {
	return &Module{
		Now:               time.Now,
		LookupWait:        30 * time.Second,
		ReconcileEvery:    5 * time.Minute,
		ReconcileDebounce: 2 * time.Second,
		limiter:           newLimiter(),
		gate:              make(chan struct{}, 1),
		reconcileGate:     make(chan struct{}, 1),
	}
}

func (*Module) Name() string { return Name }

func (*Module) Migrations() fs.FS {
	sub, _ := fs.Sub(migrationFiles, "migrations")
	return sub
}

// SetLibrary wires the library port. main sets it before Init.
func (m *Module) SetLibrary(p libraryport.Port) {
	m.mu.Lock()
	m.libraryPort = p
	m.mu.Unlock()
}

func (m *Module) library() libraryport.Port {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.libraryPort
}

// Init keeps the platform services. It reaches no network.
func (m *Module) Init(d module.Deps) error {
	return m.init(deps{enabled: d.Cfg.AgentEnabled, public: d.Cfg.PublicURL, log: d.Log, db: d.DB, hub: d.Hub, accounts: d.Accounts})
}

type deps struct {
	enabled  bool
	public   *url.URL
	log      *slog.Logger
	db       *db.DB
	hub      *hub.Hub
	accounts Accounts
}

func (m *Module) init(d deps) error {
	if d.db == nil {
		return errors.New("agents: no database")
	}
	m.enabled, m.public, m.log, m.db, m.hub, m.accounts = d.enabled, d.public, d.log, d.db, d.hub, d.accounts
	if m.log == nil {
		m.log = slog.Default()
	}
	return nil
}

// Enabled reports whether SHELF_AGENT_ENABLED is on.
func (m *Module) Enabled() bool { return m.enabled }

// Routes serves /api/agent/v1 and /api/inbox when the feature is on. With
// it off nothing is registered, so both answer the platform's 404.
func (m *Module) Routes(api *echo.Group) {
	if !m.enabled {
		return
	}
	m.agentRoutes(api.Group("/agent/v1", m.agentAuth))
	m.inboxRoutes(api.Group("/inbox"))
}

// BodyLimits lets cover uploads and proposals past the platform's 1 MiB.
func (*Module) BodyLimits() map[string]int64 {
	return map[string]int64{
		"/api/agent/v1/covers":                  uploadLimit + 1<<20,
		"/api/agent/v1/proposals":               proposalBodyLimit,
		"/api/agent/v1/proposals/:id/revisions": proposalBodyLimit,
		"/api/inbox/proposals/:id/revisions":    proposalBodyLimit,
	}
}

// JobTypes registers retention in every mode, so a queued run still has a
// type after the feature is switched off.
func (m *Module) JobTypes() []jobs.Type {
	return []jobs.Type{{Name: RetentionJob, Queue: jobs.QueueSystem, MaxAttempts: 1,
		Steps: []jobs.Step{{Name: "trim", Run: func(ctx context.Context, _ *jobs.Run) error { return m.retain(ctx) }}}}}
}

// Schedules runs retention daily while the feature is on.
func (m *Module) Schedules() []jobs.Schedule {
	if !m.enabled {
		return nil
	}
	return []jobs.Schedule{{Name: RetentionJob, At: "05:30", Request: func(context.Context) (jobs.Request, error) {
		return jobs.Request{Type: RetentionJob}, nil
	}}}
}

// Run reconciles acceptance outcomes with the library's changes: at start,
// after each change event, and every ReconcileEvery. With the feature off
// there is no runner.
func (m *Module) Run(ctx context.Context) error {
	if !m.enabled {
		return nil
	}
	m.reconcileLogged(ctx)
	var events <-chan hub.Event
	if m.hub != nil {
		ch, stop := m.hub.Subscribe(changeTopic)
		defer stop()
		events = ch
	}
	tick := time.NewTicker(m.ReconcileEvery)
	defer tick.Stop()
	var debounce <-chan time.Time
	for {
		select {
		case <-ctx.Done():
			return nil
		case _, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			if debounce == nil {
				debounce = time.After(m.ReconcileDebounce)
			}
		case <-debounce:
			debounce = nil
			m.reconcileLogged(ctx)
		case <-tick.C:
			m.reconcileLogged(ctx)
		}
	}
}

func (m *Module) reconcileLogged(ctx context.Context) {
	if err := m.reconcile(ctx); err != nil && ctx.Err() == nil {
		m.log.Error("agents: reconcile outcomes", "error", err)
	}
}

func (m *Module) now() db.Time { return db.At(m.Now()) }

func (m *Module) publish(proposal int64) {
	if m.hub == nil {
		return
	}
	var p struct {
		Pending   int     `db:"pending"`
		Withdrawn *string `db:"withdrawn_at"`
	}
	_ = m.db.R.Get(&p, `SELECT pending, withdrawn_at FROM agents_proposals WHERE id = ?`, proposal)
	m.hub.Publish(Topic, map[string]any{"proposal": proposal, "state": proposalState(p.Pending, p.Withdrawn != nil), "pending": p.Pending})
}
