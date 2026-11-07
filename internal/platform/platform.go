// Package platform builds what every module uses: configuration, the
// database, migrations, the jobs system, the event hub, the source registry
// and the HTTP listener. The platform is itself a module named "platform",
// which owns the unprefixed tables.
package platform

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"maps"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/labstack/echo/v5"
	"github.com/tikhonp/shelf/internal/platform/auth"
	"github.com/tikhonp/shelf/internal/platform/backup"
	"github.com/tikhonp/shelf/internal/platform/config"
	"github.com/tikhonp/shelf/internal/platform/db"
	"github.com/tikhonp/shelf/internal/platform/httpx"
	"github.com/tikhonp/shelf/internal/platform/hub"
	"github.com/tikhonp/shelf/internal/platform/jobs"
	"github.com/tikhonp/shelf/internal/platform/migrations"
	"github.com/tikhonp/shelf/internal/platform/module"
	"github.com/tikhonp/shelf/internal/platform/obs"
	"github.com/tikhonp/shelf/internal/platform/ui"
	"github.com/tikhonp/shelf/internal/source"
)

// Name is the platform's module name.
const Name = "platform"

// AgentsModule is the one module that may serve /api/agent.
const AgentsModule = "agents"

// App is the assembled platform with its modules.
type App struct {
	Auth    *auth.Service
	Cfg     *config.Config
	Log     *slog.Logger
	DB      *db.DB
	Jobs    *jobs.System
	Hub     *hub.Hub
	Sources *source.Registry
	Backup  *backup.Service
	// UI is the Vue build that GET and HEAD paths outside /api serve.
	UI fs.FS
	// Modules are the platform followed by the registered modules, in the
	// order their migrations run.
	Modules []module.Module

	closing   chan struct{}
	closeOnce sync.Once
	// routeOwners maps "METHOD path" to the module that registered it, as
	// of the last HTTP.
	routeOwners map[string]string
}

// RouteOwners maps each route of the last HTTP, as "METHOD path", to the
// module that registered it.
func (a *App) RouteOwners() map[string]string { return maps.Clone(a.routeOwners) }

// Open builds the platform for modules: it creates the data directory,
// opens the database, initializes every module, and registers their job
// types, schedules and sources. It does not migrate; call Migrate before
// serving.
func Open(cfg *config.Config, log *slog.Logger, modules ...module.Module) (*App, error) {
	all := append([]module.Module{core{}}, modules...)
	if err := module.Validate(all); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return nil, fmt.Errorf("data directory: %w", err)
	}
	d, err := db.Open(cfg.DatabasePath())
	if err != nil {
		return nil, err
	}
	a := &App{Cfg: cfg, Log: log, DB: d, UI: ui.Dist, Modules: all,
		Jobs: jobs.New(d, cfg.TZ, log), Hub: hub.New(), Sources: source.NewRegistry(), closing: make(chan struct{})}
	origin := ""
	if cfg.PublicURL != nil {
		origin = cfg.PublicURL.Scheme + "://" + cfg.PublicURL.Host
	}
	a.Auth, err = auth.New(d, cfg.AuthProvider == "simple", origin)
	if err != nil {
		_ = d.Close()
		return nil, err
	}
	a.Backup = backup.New(d, cfg.DataDir, cfg.TZ, log)
	if err := a.register(); err != nil {
		_ = d.Close()
		return nil, err
	}
	return a, nil
}

func (a *App) register() error {
	types, schedules := a.Jobs.PlatformTypes()
	errs := []error{
		a.Jobs.Register(Name, append(types, a.Backup.JobType())...),
		a.Jobs.RegisterSchedules(Name, append(schedules, a.Backup.Schedule())...),
	}
	deps := module.Deps{Cfg: a.Cfg, Log: a.Log, DB: a.DB, Jobs: a.Jobs, Hub: a.Hub, Sources: a.Sources, Accounts: a.Auth, Closing: a.closing}
	for _, m := range a.Modules {
		if in, ok := m.(module.Initializer); ok {
			if err := in.Init(deps); err != nil {
				return fmt.Errorf("init %s: %w", m.Name(), err)
			}
		}
	}
	for _, m := range a.Modules {
		if jd, ok := m.(module.JobDeclarer); ok {
			errs = append(errs, a.Jobs.Register(m.Name(), jd.JobTypes()...), a.Jobs.RegisterSchedules(m.Name(), jd.Schedules()...))
		}
		if sd, ok := m.(module.SourceDeclarer); ok {
			for _, s := range sd.Sources() {
				errs = append(errs, a.Sources.Add(s))
			}
		}
	}
	return errors.Join(errs...)
}

// Module returns a registered module by name.
func (a *App) Module(name string) (module.Module, bool) {
	for _, m := range a.Modules {
		if m.Name() == name {
			return m, true
		}
	}
	return nil, false
}

// Migrate applies every module's pending migrations, the platform's first,
// then runs the modules' AfterMigrate hooks in module order.
func (a *App) Migrate(ctx context.Context) error {
	if err := db.MigrateUp(ctx, a.DB, a.Log, a.migrationSources()...); err != nil {
		return err
	}
	for _, m := range a.Modules {
		if mg, ok := m.(module.Migrated); ok {
			if err := mg.AfterMigrate(ctx); err != nil {
				return fmt.Errorf("after migrate %s: %w", m.Name(), err)
			}
		}
	}
	return nil
}

func (a *App) migrationSources() []db.MigrationSource {
	out := make([]db.MigrationSource, len(a.Modules))
	for i, m := range a.Modules {
		out[i] = m
	}
	return out
}

// MigrationStatus lists every module's migrations.
func (a *App) MigrationStatus(ctx context.Context) ([]db.MigrationStatus, error) {
	return db.Status(ctx, a.DB, a.migrationSources()...)
}

// Health backs /healthz. It fails when the database doesn't answer or a
// worker pool has stopped.
func (a *App) Health(ctx context.Context) error {
	if err := a.DB.Ping(ctx); err != nil {
		return err
	}
	return a.Jobs.Health()
}

// HTTP builds the HTTP app: /healthz, the /api group with every module's
// routes, and the UI for every other GET or HEAD.
func (a *App) HTTP() *echo.Echo {
	limits := map[string]int64{}
	for _, m := range a.Modules {
		if bl, ok := m.(module.BodyLimiter); ok {
			maps.Copy(limits, bl.BodyLimits())
		}
	}
	e := httpx.New(httpx.Options{Log: a.Log, PublicURL: a.Cfg.PublicURL, Health: a.Health, BodyLimits: limits})
	e.Use(a.Auth.Middleware)
	api := e.Group("/api")
	a.Auth.Routes(api)
	api.GET("/version", version)
	owners := map[string]string{}
	claim := func(owner string) {
		for _, r := range e.Router().Routes() {
			key := r.Method + " " + r.Path
			if _, ok := owners[key]; !ok {
				owners[key] = owner
			}
		}
	}
	claim(Name)
	for _, m := range a.Modules {
		if rd, ok := m.(module.RouteDeclarer); ok {
			rd.Routes(api)
			claim(m.Name())
		}
	}
	// The auth middleware lets /api/agent through without a session, so
	// only the agents module, which checks its own tokens there, may serve
	// it.
	for key, owner := range owners {
		_, path, _ := strings.Cut(key, " ")
		if auth.AgentPath(path) && owner != AgentsModule {
			panic(fmt.Sprintf("module %s registers %s, which only the %s module may serve", owner, key, AgentsModule))
		}
	}
	a.routeOwners = owners
	// Without this, an unknown /api path with a method other than GET would
	// match the UI's GET route and answer 405. The error handler makes the
	// 404 JSON.
	api.Any("/*", func(*echo.Context) error { return echo.ErrNotFound })
	spa := ui.Handler(a.UI)
	e.GET("/*", spa)
	e.HEAD("/*", spa) // net/http drops the body of a HEAD answer
	return e
}

// VersionBody is the answer of GET /api/version.
type VersionBody struct {
	Version string `json:"version"`
}

func version(c *echo.Context) error {
	return c.JSON(http.StatusOK, VersionBody{Version: obs.AppVersion})
}

// Serve runs the HTTP listener and the jobs system until ctx ends. Both stop
// at once. Open requests drain while running steps get their grace; the
// jobs system marks what still runs after it interrupted, and it resumes on
// the next start. The caller closes the database.
func (a *App) Serve(ctx context.Context, onListen func(net.Addr)) error {
	// A listener that fails to start stops the jobs too.
	ctx, stop := context.WithCancel(ctx)
	defer stop()
	n, err := a.Auth.StartServe(ctx)
	if err != nil {
		return err
	}
	if n > 0 {
		a.Log.Info("disabled authentication cleared sessions", "count", n)
	}
	authDone := make(chan struct{})
	go func() { defer close(authDone); a.Auth.Run(ctx) }()
	jobsDone := make(chan error, 1)
	go func() { jobsDone <- a.Jobs.Start(ctx) }()
	go func() {
		<-ctx.Done()
		a.closeStreams()
	}()
	runners := a.startRunners(ctx)
	httpErr := httpx.Run(ctx, a.HTTP(), a.Cfg.HTTPAddr, onListen)
	stop()
	err = errors.Join(httpErr, <-jobsDone)
	runners.Wait()
	<-authDone
	return err
}

// startRunners starts every module's Run. A runner that fails stops only its
// own work, so a bot that can't reach Telegram doesn't stop downloads.
func (a *App) startRunners(ctx context.Context) *sync.WaitGroup {
	var wg sync.WaitGroup
	for _, m := range a.Modules {
		r, ok := m.(module.Runner)
		if !ok {
			continue
		}
		wg.Go(func() {
			if err := r.Run(ctx); err != nil && ctx.Err() == nil {
				a.Log.Error("module stopped", "module", m.Name(), "error", err)
			}
		})
	}
	return &wg
}

func (a *App) closeStreams() { a.closeOnce.Do(func() { close(a.closing) }) }

// Close closes the database.
func (a *App) Close() error { return a.DB.Close() }

var _ module.Accounts = (*auth.Service)(nil)

// core is the platform as a module.
type core struct{}

func (core) Name() string      { return Name }
func (core) Migrations() fs.FS { return migrations.FS }
