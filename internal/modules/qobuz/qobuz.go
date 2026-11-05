// Package qobuz is the Qobuz source, an adapter over qobuz-dl-go, the Go port
// of qobuz-dl. The port does the API calls, file names, tags and transfers;
// this package maps them onto the source interfaces. docs/sources/qobuz.md
// has the details.
package qobuz

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	qobuzdl "github.com/tikhonp/qobuz-dl-go"
	"github.com/tikhonp/shelf/internal/platform/config"
	"github.com/tikhonp/shelf/internal/platform/hub"
	"github.com/tikhonp/shelf/internal/platform/module"
	"github.com/tikhonp/shelf/internal/source"
)

// ID is the source's ID, and Name the module's.
const (
	ID   = "qobuz"
	Name = "qobuz"
)

// SourceTopic is the hub topic of source events.
const SourceTopic = "source"

const (
	// RetryEvery is how long a failed start waits before the next one.
	RetryEvery = 5 * time.Minute
	// StartWait bounds how long a call waits for the first start to end. A
	// download resumed after a restart reaches the source before the start
	// is over, and it must not fail for that.
	StartWait = time.Minute
)

// Module is the Qobuz module. It registers its source switched off when
// SHELF_QOBUZ_TOKEN is empty.
type Module struct {
	// RetryEvery and StartWait default to the constants. Options, when set,
	// changes what each qobuz-dl-go instance starts with. Tests change them
	// before Init.
	RetryEvery time.Duration
	StartWait  time.Duration
	Options    func(*qobuzdl.Options)

	src *Source
	log *slog.Logger
}

// New returns the module. history is the record of what shelf has
// downloaded.
func New(history History) *Module {
	return &Module{
		RetryEvery: RetryEvery, StartWait: StartWait,
		src: &Source{off: true, history: history, started: make(chan struct{}), dls: map[variant]*qobuzdl.DL{}},
	}
}

func (*Module) Name() string               { return Name }
func (*Module) Migrations() fs.FS          { return nil }
func (m *Module) Sources() []source.Source { return []source.Source{m.src} }

// Source is the module's source, for tests.
func (m *Module) Source() *Source { return m.src }

// Init reads the settings. It reaches no network and creates no folder:
// manage runs Init too, and qobuzdl.New creates the music folder, so Run
// does that.
func (m *Module) Init(d module.Deps) error {
	m.log = d.Log
	s := m.src
	s.log, s.hub, s.quality = d.Log, d.Hub, d.Cfg.Qobuz.Quality
	s.folder = filepath.ToSlash(d.Cfg.Qobuz.Dir)
	if !d.Cfg.Qobuz.Enabled() {
		return nil
	}
	root, err := filepath.Abs(d.Cfg.MusicDir)
	if err != nil {
		return fmt.Errorf("qobuz: music folder: %w", err)
	}
	s.cfg, s.root, s.dir = d.Cfg.Qobuz, root, filepath.Join(root, d.Cfg.Qobuz.Dir)
	s.http, s.tweak, s.startWait = httpClient(d.Cfg), m.Options, m.StartWait
	s.off = false
	return nil
}

// httpClient is the one client behind every Qobuz request. It has no
// Timeout, which would cut long transfers: qobuz-dl-go times each call
// itself. It goes through SHELF_PROXY_URL only when SHELF_QOBUZ_USE_PROXY is
// on, and ignores the HTTP_PROXY variables, so each module chooses for
// itself.
func httpClient(cfg *config.Config) *http.Client {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.Proxy = nil
	if cfg.Qobuz.UseProxy && cfg.Proxy != nil {
		t.Proxy = http.ProxyURL(cfg.Proxy)
	}
	return &http.Client{Transport: t}
}

// Run logs in, trying again every RetryEvery until it works, so shelf starts
// even when Qobuz is unreachable.
func (m *Module) Run(ctx context.Context) error {
	s := m.src
	if s.off {
		return nil
	}
	for {
		err := s.start(ctx)
		if ctx.Err() != nil {
			return nil
		}
		s.startEnded(err)
		if err == nil {
			return nil
		}
		m.log.Warn("qobuz: start failed", "error", err)
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(m.RetryEvery):
		}
	}
}

// variant is what a qobuz-dl-go instance can't change per call: the quality,
// and whether it has a downloads database. HandleID takes neither.
type variant struct {
	quality int
	noDB    bool
}

// Source is the Qobuz source.
type Source struct {
	off       bool
	cfg       config.Qobuz
	quality   int
	root, dir string
	// folder is dir inside the music folder, set while switched off too.
	folder    string
	http      *http.Client
	history   History
	tweak     func(*qobuzdl.Options)
	log       *slog.Logger
	hub       *hub.Hub
	startWait time.Duration

	// started closes when the first start ends, well or not.
	started chan struct{}
	once    sync.Once

	mu       sync.Mutex
	phase    phase
	api      *qobuzdl.Client
	appID    string
	secrets  []string
	lastSent *SourceEvent

	// dlMu covers dls. Only one Qobuz download runs at a time, so holding it
	// across a login holds nobody up.
	dlMu sync.Mutex
	dls  map[variant]*qobuzdl.DL
}

func (s *Source) ID() string     { return ID }
func (s *Source) Name() string   { return "Qobuz" }
func (s *Source) Enabled() bool  { return !s.off }
func (s *Source) Folder() string { return s.folder }

// options are the old bot's settings, listed in docs/sources/qobuz.md.
// DefaultOptions has the rest of them.
func (s *Source) options(v variant) qobuzdl.Options {
	o := qobuzdl.DefaultOptions()
	o.Directory = s.dir
	o.Quality = v.quality
	o.QualityFallback = s.cfg.Fallback
	o.Settings.MaxWorkers = s.cfg.Workers
	o.Settings.Delay = s.cfg.Delay
	o.Settings.UserAuthToken = s.cfg.Token.Reveal()
	o.HTTPClient = s.http
	o.Logger = s.log
	if !v.noDB && s.history != nil {
		o.DownloadsDB = downloadsDB{history: s.history, root: s.root}
	}
	if s.tweak != nil {
		s.tweak(&o)
	}
	return o
}

// start fetches the web player's bundle and logs in.
func (s *Source) start(ctx context.Context) error {
	v := variant{quality: s.quality}
	d, err := qobuzdl.New(s.options(v))
	if err != nil {
		return err
	}
	if err := d.GetTokens(ctx); err != nil {
		return err
	}
	if err := d.InitializeClient(ctx, "", "", d.AppID(), d.Secrets()); err != nil {
		return err
	}
	s.dlMu.Lock()
	s.dls[v] = d
	s.dlMu.Unlock()
	s.mu.Lock()
	s.api, s.appID, s.secrets = d.Client(), d.AppID(), d.Secrets()
	s.mu.Unlock()
	return nil
}

// ready waits for the first start and says whether Qobuz can be asked.
func (s *Source) ready(ctx context.Context) error {
	if s.off {
		return &source.Unavailable{Reason: textOff}
	}
	wait := time.NewTimer(s.startWait)
	defer wait.Stop()
	select {
	case <-s.started:
	case <-wait.C:
	case <-ctx.Done():
		return ctx.Err()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.phase != phaseUp {
		return &source.Unavailable{Reason: s.healthLocked().Message}
	}
	return nil
}

// client is the catalog client once Qobuz can be asked.
func (s *Source) client(ctx context.Context) (*qobuzdl.Client, error) {
	if err := s.ready(ctx); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.api, nil
}

// downloader is the instance for a quality, with no database when force is
// set. It logs in on first use, with the app ID and secrets of the start.
func (s *Source) downloader(ctx context.Context, quality int, force bool) (*qobuzdl.DL, error) {
	if err := s.ready(ctx); err != nil {
		return nil, err
	}
	v := variant{quality: quality, noDB: force}
	s.dlMu.Lock()
	defer s.dlMu.Unlock()
	if d, ok := s.dls[v]; ok {
		return d, nil
	}
	d, err := qobuzdl.New(s.options(v))
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	appID, secrets := s.appID, s.secrets
	s.mu.Unlock()
	if err := d.InitializeClient(ctx, "", "", appID, secrets); err != nil {
		return nil, err
	}
	s.dls[v] = d
	return d, nil
}
