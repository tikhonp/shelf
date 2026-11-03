// Package downloads is the downloads module: every request to fetch an item,
// its job, its tracks, the registry of items already downloaded, and the
// provenance file each download writes. The web, the bot and manage commands
// all queue through Service.Queue.
package downloads

import (
	"context"
	"embed"
	"io/fs"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/tikhonp/shelf/internal/platform/jobs"
	"github.com/tikhonp/shelf/internal/platform/module"
	"github.com/tikhonp/shelf/internal/platform/web"
	"github.com/tikhonp/shelf/internal/source"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Name is the module's name.
const Name = "downloads"

// Module is the downloads module. Its Service exists from New, so main can
// wire the ports before Init; it works after Init.
type Module struct {
	// KeepAlive is how often the event and log streams send a comment while
	// idle. Tests make it short.
	KeepAlive time.Duration

	svc     *Service
	closing <-chan struct{}
}

// New returns the module.
func New() *Module { return &Module{KeepAlive: web.KeepAlive, svc: &Service{Now: time.Now}} }

func (*Module) Name() string { return Name }

func (*Module) Migrations() fs.FS {
	sub, _ := fs.Sub(migrationFiles, "migrations")
	return sub
}

// Init gives the service the platform.
func (m *Module) Init(d module.Deps) error {
	m.svc.init(d)
	m.closing = d.Closing
	return nil
}

// Service is the module's service, for the modules that queue downloads.
func (m *Module) Service() *Service { return m.svc }

// JobTypes are the download job and the notify job.
func (m *Module) JobTypes() []jobs.Type { return m.svc.jobTypes() }

// Schedules: none.
func (*Module) Schedules() []jobs.Schedule { return nil }

// Routes serves /api/downloads and /api/events.
func (m *Module) Routes(api *echo.Group) { m.routes(api) }

// Statuses is the catalog's port: what shelf knows of each item's downloads.
func (m *Module) Statuses(ctx context.Context, refs []source.Ref) (map[source.Ref]source.Status, error) {
	return m.svc.Statuses(ctx, refs)
}
