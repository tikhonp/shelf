// Package library is the tag editor's module. Phase 2 builds it in steps.
// It keeps an index of the music folder, read the way Navidrome reads it,
// with the albums it forms and the checks they fail, and serves it under
// /api/library. It never writes a file yet; the reading and writing of tags
// is in tagio.
package library

import (
	"context"
	"embed"
	"io/fs"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/tikhonp/shelf/internal/modules/library/tagio"
	"github.com/tikhonp/shelf/internal/platform/module"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Name is the module's name.
const Name = "library"

// Module is the library module.
type Module struct {
	// Every is how often the index walks without being asked. Tests change
	// it.
	Every time.Duration

	root  string
	log   *slog.Logger
	store store
	ix    *index
	// kick asks for a walk. It holds one signal, so a burst asks once.
	kick chan struct{}

	mu     sync.Mutex
	config ConfigReader
	rules  *tagio.Rules
}

// New returns the module.
func New() *Module {
	return &Module{Every: IndexEvery, kick: make(chan struct{}, 1), rules: tagio.DefaultRules()}
}

func (*Module) Name() string { return Name }

func (*Module) Migrations() fs.FS {
	sub, _ := fs.Sub(migrationFiles, "migrations")
	return sub
}

// SetConfig wires the reader of Navidrome's tag settings. main sets it
// before Init. Without it the index follows Navidrome's defaults.
func (m *Module) SetConfig(c ConfigReader) {
	m.mu.Lock()
	m.config = c
	m.mu.Unlock()
}

// Init keeps the music folder's path and the database. The walk reads the
// whole library, so it waits for Run, which only serve calls.
func (m *Module) Init(d module.Deps) error {
	m.root, m.log = d.Cfg.MusicDir, d.Log
	m.store = store{db: d.DB}
	m.mu.Lock()
	config := m.config
	m.mu.Unlock()
	m.ix = &index{root: m.root, store: m.store, config: config, log: d.Log,
		publish: func(s Status) {
			if d.Hub != nil {
				d.Hub.Publish(Topic, s)
			}
		}}
	return nil
}

// Routes serves /api/library.
func (m *Module) Routes(api *echo.Group) { m.routes(api) }

// RequestScan is the downloads module's scanner port, beside Navidrome's.
// A finished download asks the index to walk soon.
func (m *Module) RequestScan(context.Context) error {
	m.Kick()
	return nil
}

// Kick asks for a walk now.
func (m *Module) Kick() {
	select {
	case m.kick <- struct{}{}:
	default:
	}
}

// Run removes the temp files a crash left in the middle of a write, then
// walks the music folder at start, every m.Every, and when asked.
func (m *Module) Run(ctx context.Context) error {
	removed, err := tagio.Sweep(m.root, os.Getuid())
	for _, p := range removed {
		m.log.Info("removed a temp file", "path", p)
	}
	if err != nil {
		// Folders shelf can't read hold none of its temp files.
		m.log.Warn("temp sweep", "error", err)
	}
	if err := m.ix.load(ctx); err != nil {
		return err
	}
	t := time.NewTimer(0)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
		case <-m.kick:
			if !t.Stop() {
				select {
				case <-t.C:
				default:
				}
			}
		}
		if err := m.ix.walk(ctx); err != nil && ctx.Err() == nil {
			m.log.Warn("library: index", "error", err)
		}
		m.mu.Lock()
		m.rules = m.ix.current()
		m.mu.Unlock()
		t.Reset(m.Every)
	}
}

// currentRules are the rules of the last walk. An opened album is read
// under them too.
func (m *Module) currentRules() *tagio.Rules {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.rules
}
