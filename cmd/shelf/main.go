// Command shelf is the whole of Music Shelf in one binary:
//
//	shelf serve                     the HTTP server and everything behind it
//	shelf manage <command> [args]   one-off admin commands, run in the container's shell
//	shelf healthcheck               exit 0 when /healthz answers, 1 otherwise
//	shelf version
package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"
	_ "time/tzdata" // the alpine image may lack zone files for SHELF_TIMEZONE

	"github.com/tikhonp/shelf/internal/platform"
	"github.com/tikhonp/shelf/internal/platform/config"
	"github.com/tikhonp/shelf/internal/platform/db"
	"github.com/tikhonp/shelf/internal/platform/module"
	"github.com/tikhonp/shelf/internal/platform/obs"
)

// modules are the enabled modules, in migration order.
func modules() []module.Module {
	return nil
}

const usage = `Usage:
  shelf serve
  shelf manage migrate up                 apply pending migrations of every module
  shelf manage migrate down <module>      roll back the last migration of one module
  shelf manage migrate status             list every module's migrations
  shelf healthcheck                       exit 0 when the running server is healthy, 1 otherwise
  shelf version
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	return cli{lookup: os.LookupEnv, stdout: stdout, stderr: stderr, modules: modules}.run(ctx, args)
}

// cli is one run of the command line. Its environment, output and modules
// are fields, so tests run every command in-process.
type cli struct {
	lookup  func(string) (string, bool)
	stdout  io.Writer
	stderr  io.Writer
	modules func() []module.Module
	// listening, when set, hears the address serve listens on.
	listening func(net.Addr)
}

func (c cli) run(ctx context.Context, args []string) int {
	if len(args) == 0 {
		say(c.stderr, usage)
		return 2
	}
	switch args[0] {
	case "serve":
		if len(args) == 1 {
			return c.serve(ctx)
		}
	case "manage":
		return c.manage(ctx, args[1:])
	case "healthcheck":
		if len(args) == 1 {
			return c.healthcheck(ctx)
		}
	case "version", "-v", "--version":
		sayln(c.stdout, obs.AppVersion)
		return 0
	case "help", "-h", "--help":
		say(c.stdout, usage)
		return 0
	}
	sayf(c.stderr, "unknown command %q\n\n%s", strings.Join(args, " "), usage)
	return 2
}

func (c cli) serve(ctx context.Context) int {
	platform.SetUmask()
	cfg, err := config.Load(c.lookup)
	if err != nil {
		obs.SetupLogging(c.stdout, slog.LevelInfo).Error("invalid configuration", "problems", strings.Split(err.Error(), "\n"))
		return 1
	}
	log := obs.SetupLogging(c.stdout, cfg.LogLevel)
	for _, w := range cfg.Warnings() {
		log.Warn(w)
	}

	app, err := platform.Open(cfg, log, c.modules()...)
	if err != nil {
		log.Error("start", "error", err)
		return 1
	}
	defer func() { _ = app.Close() }()

	// A failing migration keeps the previous schema and stops the start.
	if err := app.Migrate(ctx); err != nil {
		log.Error("migrate", "error", err)
		return 1
	}

	log.Info("starting", "version", obs.AppVersion, "listen", cfg.HTTPAddr, "modules", len(app.Modules))
	err = app.Serve(ctx, func(a net.Addr) {
		log.Info("listening", "addr", a.String())
		if c.listening != nil {
			c.listening(a)
		}
	})
	if err != nil {
		log.Error("serve", "error", err)
		return 1
	}
	log.Info("stopped")
	return 0
}

// healthcheck asks the server in this container whether it is healthy. The
// image has no curl, so compose runs this. It reads only SHELF_HTTP_ADDR,
// so a broken configuration shows as an unhealthy server and not as a
// failing check.
func (c cli) healthcheck(ctx context.Context) int {
	addr, ok := c.lookup("SHELF_HTTP_ADDR")
	addr = strings.TrimSpace(addr)
	if !ok || addr == "" {
		addr = ":8080"
	}
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		sayf(c.stderr, "SHELF_HTTP_ADDR %q: %v\n", addr, err)
		return 1
	}
	ctx, cancel := context.WithTimeout(ctx, healthTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+net.JoinHostPort("127.0.0.1", port)+"/healthz", nil)
	if err != nil {
		sayln(c.stderr, err)
		return 1
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		sayln(c.stderr, err)
		return 1
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		sayf(c.stderr, "healthz answered %s\n", resp.Status)
		return 1
	}
	return 0
}

const healthTimeout = 3 * time.Second

func (c cli) manage(ctx context.Context, args []string) int {
	var cmd func(*platform.App, *slog.Logger) error
	switch {
	case len(args) == 2 && args[0] == "migrate" && args[1] == "up":
		cmd = func(app *platform.App, _ *slog.Logger) error { return app.Migrate(ctx) }
	case len(args) == 3 && args[0] == "migrate" && args[1] == "down":
		cmd = func(app *platform.App, log *slog.Logger) error {
			m, ok := app.Module(args[2])
			if !ok {
				return fmt.Errorf("no module %q", args[2])
			}
			return db.MigrateDown(ctx, app.DB, log, m)
		}
	case len(args) == 2 && args[0] == "migrate" && args[1] == "status":
		cmd = func(app *platform.App, _ *slog.Logger) error {
			st, err := app.MigrationStatus(ctx)
			if err == nil {
				printStatus(c.stdout, st, app.Modules)
			}
			return err
		}
	default:
		sayf(c.stderr, "unknown manage command\n\n%s", usage)
		return 2
	}

	cfg, err := config.Load(c.lookup)
	if err != nil {
		sayf(c.stderr, "invalid configuration:\n%v\n", err)
		return 1
	}
	log := slog.New(slog.NewTextHandler(c.stderr, &slog.HandlerOptions{Level: cfg.LogLevel}))
	app, err := platform.Open(cfg, log, c.modules()...)
	if err != nil {
		sayln(c.stderr, err)
		return 1
	}
	defer func() { _ = app.Close() }()
	if err := cmd(app, log); err != nil {
		sayln(c.stderr, err)
		return 1
	}
	return 0
}

// Output to the terminal is best effort.
func say(w io.Writer, a ...any)                 { _, _ = fmt.Fprint(w, a...) }
func sayln(w io.Writer, a ...any)               { _, _ = fmt.Fprintln(w, a...) }
func sayf(w io.Writer, format string, a ...any) { _, _ = fmt.Fprintf(w, format, a...) }

// printStatus lists every migration and names each module that has none, so
// the output shows every module even when no module has a migration.
func printStatus(w io.Writer, st []db.MigrationStatus, modules []module.Module) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	sayln(tw, "MODULE\tVERSION\tSTATE\tFILE")
	has := map[string]bool{}
	for _, s := range st {
		has[s.Module] = true
	}
	for _, m := range modules {
		if !has[m.Name()] {
			sayf(tw, "%s\t-\tno migrations\t\n", m.Name())
		}
		for _, s := range st {
			if s.Module != m.Name() {
				continue
			}
			state := "pending"
			if s.Applied {
				state = "applied"
			}
			sayf(tw, "%s\t%d\t%s\t%s\n", s.Module, s.Version, state, s.Path)
		}
	}
	_ = tw.Flush()
}
