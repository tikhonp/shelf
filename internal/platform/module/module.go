// Package module defines what a shelf module is. A module is a Go package
// compiled into the binary, and main enables it by passing it to
// platform.Open. It owns its tables, migrations and routes, and talks to
// other modules only through ports and the event hub.
//
// Module is the minimum every module implements. A module declares the rest
// by also implementing the optional interfaces next to it; the platform
// finds them with a type assertion, so a module implements only what it
// has.
package module

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"regexp"

	"github.com/labstack/echo/v5"
	"github.com/tikhonp/shelf/internal/platform/config"
	"github.com/tikhonp/shelf/internal/platform/db"
	"github.com/tikhonp/shelf/internal/platform/hub"
	"github.com/tikhonp/shelf/internal/platform/jobs"
	"github.com/tikhonp/shelf/internal/source"
)

// Module is a registered part of shelf.
type Module interface {
	// Name is short and lowercase ("platform", "qobuz"). It names the
	// module's migration version table.
	Name() string
	// Migrations holds the module's goose *.sql files at its root, or is nil
	// when the module has no tables.
	Migrations() fs.FS
}

var namePattern = regexp.MustCompile(`^[a-z][a-z0-9]{1,23}$`)

// Validate checks that modules have valid, unique names.
func Validate(modules []Module) error {
	seen := map[string]bool{}
	var errs []error
	for _, m := range modules {
		n := m.Name()
		if !namePattern.MatchString(n) {
			errs = append(errs, fmt.Errorf("module name %q must match %s", n, namePattern))
		}
		if seen[n] {
			errs = append(errs, fmt.Errorf("module %q is registered twice", n))
		}
		seen[n] = true
	}
	return errors.Join(errs...)
}

// Deps is what a module may use of the platform.
type Deps struct {
	Cfg *config.Config
	Log *slog.Logger
	DB  *db.DB
	// Jobs enqueues and controls background work.
	Jobs *jobs.System
	// Hub carries events between modules and to the event stream.
	Hub *hub.Hub
	// Sources holds every registered source. It fills after every Init, so
	// read it when serving, not in Init.
	Sources *source.Registry
	// Accounts tells whether account sign-in is on and whether an account
	// still exists. It never exposes the accounts table.
	Accounts Accounts
	// Closing is closed when serve starts to stop. A long-lived response,
	// such as an event stream, ends on it, because the HTTP server's
	// shutdown doesn't cancel a running request.
	Closing <-chan struct{}
}

// Accounts is the platform's account port.
type Accounts interface {
	// AuthenticationEnabled reports provider simple.
	AuthenticationEnabled() bool
	// Exists reports whether an account ID is still in use. IDs are never
	// reused, so a false answer stays false.
	Exists(ctx context.Context, id int64) (bool, error)
}

// Initializer is a module with setup work. platform.Open calls Init after
// the platform services exist and before the tables do.
type Initializer interface{ Init(d Deps) error }

// Migrated is a module with work that needs its tables. AfterMigrate runs in
// App.Migrate after every module's migrations, in module order, on every
// start, so it must be safe to repeat.
type Migrated interface {
	AfterMigrate(ctx context.Context) error
}

// RouteDeclarer is a module with endpoints. api is the /api group, where the
// error handler answers with JSON.
type RouteDeclarer interface{ Routes(api *echo.Group) }

// JobDeclarer is a module with job types and schedules. Their names start
// with the module's name. The platform registers them after Init.
type JobDeclarer interface {
	JobTypes() []jobs.Type
	Schedules() []jobs.Schedule
}

// BodyLimiter is a module with routes that take bodies over the platform's
// limit, such as an image upload. The keys are route paths as registered,
// with the /api prefix.
type BodyLimiter interface {
	BodyLimits() map[string]int64
}

// SourceDeclarer is a module that provides sources. The platform adds them
// to the registry after Init, switched on or not.
type SourceDeclarer interface {
	Sources() []source.Source
}

// Runner is a module with work that lasts as long as the server, such as
// the bot's long polling. Only serve calls Run, so work that logs in or
// reaches the network goes here and not in Init, which manage runs too. Run
// returns when ctx ends. The platform logs an error before then, and the
// server goes on without that work.
type Runner interface {
	Run(ctx context.Context) error
}
