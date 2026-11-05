// Package library is the tag editor's module. It keeps an index of the
// music folder, read the way Navidrome reads it, with the albums it forms and
// the checks they fail, serves it under /api/library, and saves edits through
// jobs on the tags queue. The reading and writing of tags is in tagio.
package library

import (
	"context"
	"embed"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/tikhonp/shelf/internal/modules/library/acoustid"
	"github.com/tikhonp/shelf/internal/modules/library/lrclib"
	"github.com/tikhonp/shelf/internal/modules/library/musicbrainz"
	"github.com/tikhonp/shelf/internal/modules/library/tagio"
	"github.com/tikhonp/shelf/internal/platform/hub"
	"github.com/tikhonp/shelf/internal/platform/jobs"
	"github.com/tikhonp/shelf/internal/platform/module"
	"github.com/tikhonp/shelf/internal/platform/obs"
	"github.com/tikhonp/shelf/internal/source"
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
	// FFmpeg judges and repairs covers.
	FFmpeg string
	// MB, Lyrics and HTTP reach the services behind suggestions. Init
	// points them at the public services, and tests at their own.
	MB     *musicbrainz.Client
	Lyrics *lrclib.Client
	HTTP   *http.Client
	// Prints identifies audio. It stays nil without an AcoustID key.
	Prints *acoustid.Client
	// DeleteWait is how long a delete waits between looks at Navidrome's
	// missing files. Tests change it.
	DeleteWait time.Duration
	// afterSlice runs when a run's slice ends. Tests stop a run with it.
	afterSlice func()

	root  string
	log   *slog.Logger
	store store
	ix    *index
	jobs  *jobs.System
	hub   *hub.Hub
	// covers keeps uploads and the covers changes replaced.
	covers    coverStore
	sources   *source.Registry
	lookups   lookups
	userAgent string
	// kick asks for a walk. It holds one signal, so a burst asks once.
	kick chan struct{}
	mb   mbProbe

	mu     sync.Mutex
	config ConfigReader
	recs   RecordsReader
	rules  *tagio.Rules
	busy   func(dir string) bool
	scan   func(ctx context.Context) error
	nav    *Navidrome
	forget func(ctx context.Context, folder string) (int64, error)
}

// New returns the module.
func New() *Module {
	return &Module{Every: IndexEvery, DeleteWait: DeleteWait, FFmpeg: "ffmpeg", kick: make(chan struct{}, 1), rules: tagio.DefaultRules()}
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

// SetRecords wires the downloads' records, which tell each album's source.
// main sets it before Init. Without it only shelf.txt and the source
// folders tell.
func (m *Module) SetRecords(r RecordsReader) {
	m.mu.Lock()
	m.recs = r
	m.mu.Unlock()
}

// SetBusy wires the downloads module's report of the folders a download
// writes in. main sets it before Init. Without it no folder is busy.
func (m *Module) SetBusy(busy func(dir string) bool) {
	m.mu.Lock()
	m.busy = busy
	m.mu.Unlock()
}

// SetScanner wires the request for a Navidrome scan after a write.
func (m *Module) SetScanner(scan func(ctx context.Context) error) {
	m.mu.Lock()
	m.scan = scan
	m.mu.Unlock()
}

func (m *Module) scanner() func(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.scan
}

// Init keeps the music folder's path and the database. The walk reads the
// whole library, so it waits for Run, which only serve calls.
func (m *Module) Init(d module.Deps) error {
	m.root, m.log, m.jobs, m.hub = d.Cfg.MusicDir, d.Log, d.Jobs, d.Hub
	m.store = store{db: d.DB}
	m.covers = coverStore{dir: filepath.Join(d.Cfg.DataDir, "library", "covers")}
	m.sources = d.Sources
	m.userAgent = userAgent(d.Cfg.PublicURL)
	cache := lookups{db: d.DB, now: time.Now}
	m.MB = musicbrainz.New(m.userAgent, cache)
	m.Lyrics = lrclib.New(m.userAgent, cache)
	m.HTTP = &http.Client{}
	if d.Cfg.AcoustIDKey != "" {
		m.Prints = acoustid.New(d.Cfg.AcoustIDKey.Reveal(), m.userAgent)
	}
	m.lookups = cache
	m.mu.Lock()
	config, recs := m.config, m.recs
	m.mu.Unlock()
	m.ix = &index{root: m.root, uid: os.Getuid(), store: m.store, config: config, recs: recs, sources: d.Sources, log: d.Log,
		publish: func(s Status) {
			if d.Hub != nil {
				d.Hub.Publish(Topic, s)
			}
		}}
	return nil
}

// userAgent names shelf to MusicBrainz and LRCLIB, as both ask, with the
// address the owner set or the code's.
func userAgent(public *url.URL) string {
	contact := "https://code.tikhonnnnn.com/tikhon/shelf"
	if public != nil {
		contact = public.String()
	}
	return "MusicShelf/" + obs.AppVersion + " ( " + contact + " )"
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

// Run removes the temp files a crash left in the middle of a write,
// settles the changes it cut off, probes the top folders, then walks the
// music folder at start, every m.Every, and when asked.
func (m *Module) Run(ctx context.Context) error {
	removed, err := tagio.Sweep(m.root, os.Getuid())
	for _, p := range removed {
		m.log.Info("removed a temp file", "path", p)
	}
	if err != nil {
		// Folders shelf can't read hold none of its temp files.
		m.log.Warn("temp sweep", "error", err)
	}
	if err := m.reconcileAll(ctx); err != nil {
		m.log.Error("library: settle interrupted changes", "error", err)
	}
	if err := m.ix.load(ctx); err != nil {
		return err
	}
	m.ix.probe()
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
