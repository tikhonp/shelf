// Package library is the tag editor's module. Phase 2 builds it in steps.
// So far it sweeps the temp files a crashed write left in the music folder;
// the reading and writing of tags is in tagio.
package library

import (
	"context"
	"io/fs"
	"log/slog"
	"os"

	"github.com/tikhonp/shelf/internal/modules/library/tagio"
	"github.com/tikhonp/shelf/internal/platform/module"
)

// Name is the module's name.
const Name = "library"

// Module is the library module.
type Module struct {
	root string
	log  *slog.Logger
}

// New returns the module.
func New() *Module { return &Module{} }

func (*Module) Name() string      { return Name }
func (*Module) Migrations() fs.FS { return nil }

// Init keeps the music folder's path. The sweep walks the whole library, so
// it waits for Run, which only serve calls.
func (m *Module) Init(d module.Deps) error {
	m.root, m.log = d.Cfg.MusicDir, d.Log
	return nil
}

// Run removes the temp files a crash left in the middle of a write, once at
// start, and returns.
func (m *Module) Run(context.Context) error {
	removed, err := tagio.Sweep(m.root, os.Getuid())
	for _, p := range removed {
		m.log.Info("removed a temp file", "path", p)
	}
	if err != nil {
		// Folders shelf can't read hold none of its temp files.
		m.log.Warn("temp sweep", "error", err)
	}
	return nil
}
