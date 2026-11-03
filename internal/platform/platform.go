// Package platform builds what every module uses: configuration, the
// database, migrations and the HTTP listener. The platform is itself a
// module named "platform", which owns the unprefixed tables.
package platform

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"

	"github.com/labstack/echo/v5"
	"github.com/tikhonp/shelf/internal/platform/config"
	"github.com/tikhonp/shelf/internal/platform/db"
	"github.com/tikhonp/shelf/internal/platform/httpx"
	"github.com/tikhonp/shelf/internal/platform/migrations"
	"github.com/tikhonp/shelf/internal/platform/module"
	"github.com/tikhonp/shelf/internal/platform/obs"
	"github.com/tikhonp/shelf/internal/platform/ui"
)

// Name is the platform's module name.
const Name = "platform"

// App is the assembled platform with its modules.
type App struct {
	Cfg *config.Config
	Log *slog.Logger
	DB  *db.DB
	// UI is the Vue build that GET and HEAD paths outside /api serve.
	UI fs.FS
	// Modules are the platform followed by the registered modules, in the
	// order their migrations run.
	Modules []module.Module
}

// Open builds the platform for modules: it creates the data directory,
// opens the database and initializes every module. It does not migrate;
// call Migrate before serving.
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
	a := &App{Cfg: cfg, Log: log, DB: d, UI: ui.Dist, Modules: all}
	deps := module.Deps{Cfg: cfg, Log: log, DB: d}
	for _, m := range all {
		if in, ok := m.(module.Initializer); ok {
			if err := in.Init(deps); err != nil {
				_ = d.Close()
				return nil, fmt.Errorf("init %s: %w", m.Name(), err)
			}
		}
	}
	return a, nil
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

// Health backs /healthz. It fails when the database doesn't answer.
func (a *App) Health(ctx context.Context) error { return a.DB.Ping(ctx) }

// HTTP builds the HTTP app: /healthz, the /api group with every module's
// routes, and the UI for every other GET or HEAD.
func (a *App) HTTP() *echo.Echo {
	e := httpx.New(httpx.Options{Log: a.Log, PublicURL: a.Cfg.PublicURL, Health: a.Health})
	api := e.Group("/api")
	api.GET("/version", version)
	for _, m := range a.Modules {
		if rd, ok := m.(module.RouteDeclarer); ok {
			rd.Routes(api)
		}
	}
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

// Serve runs the HTTP listener until ctx ends and open requests finish. The
// caller closes the database.
func (a *App) Serve(ctx context.Context, onListen func(net.Addr)) error {
	return httpx.Run(ctx, a.HTTP(), a.Cfg.HTTPAddr, onListen)
}

// Close closes the database.
func (a *App) Close() error { return a.DB.Close() }

// core is the platform as a module.
type core struct{}

func (core) Name() string      { return Name }
func (core) Migrations() fs.FS { return migrations.FS }
