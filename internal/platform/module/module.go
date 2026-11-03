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
