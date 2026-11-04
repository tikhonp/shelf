package qobuz

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	qobuzdl "github.com/tikhonp/qobuz-dl-go"
	"github.com/tikhonp/qobuz-dl-go/qobuzdltest"
	"github.com/tikhonp/shelf/internal/modules/downloads"
	"github.com/tikhonp/shelf/internal/platform/config"
	"github.com/tikhonp/shelf/internal/platform/db/dbtest"
	"github.com/tikhonp/shelf/internal/platform/hub"
	"github.com/tikhonp/shelf/internal/platform/module"
	"github.com/tikhonp/shelf/internal/platform/testwait"
	"github.com/tikhonp/shelf/internal/source"
)

var bg = context.Background()

// memHistory is a History in a map.
type memHistory struct {
	mu   sync.Mutex
	rows map[source.Ref]downloads.Downloaded
	has  []source.Ref
	err  error
}

func newMemHistory() *memHistory { return &memHistory{rows: map[source.Ref]downloads.Downloaded{}} }

func (h *memHistory) IsDownloaded(_ context.Context, ref source.Ref) (bool, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.has = append(h.has, ref)
	_, ok := h.rows[ref]
	return ok, h.err
}

func (h *memHistory) RecordDownloaded(_ context.Context, d downloads.Downloaded) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.rows[d.Ref] = d
	return nil
}

func (h *memHistory) asked() []source.Ref {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.has)
}

func (h *memHistory) row(ref source.Ref) (downloads.Downloaded, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	d, ok := h.rows[ref]
	return d, ok
}

// rig is the module on a fake Qobuz. Nothing runs until start.
type rig struct {
	t       *testing.T
	srv     *qobuzdltest.Server
	m       *Module
	s       *Source
	cfg     *config.Config
	hub     *hub.Hub
	history *memHistory
	stop    context.CancelFunc
	done    chan struct{}
}

func newRig(t *testing.T, change ...func(*rig)) *rig {
	t.Helper()
	r := &rig{t: t, srv: qobuzdltest.New(t), history: newMemHistory(), hub: hub.New(),
		cfg: &config.Config{MusicDir: t.TempDir(), Qobuz: config.Qobuz{
			Token: "from-the-fake", Dir: "qobuz", Quality: 27, Fallback: true, Workers: 3,
		}}}
	r.m = New(r.history)
	r.m.Options = func(o *qobuzdl.Options) { r.srv.Configure(o) }
	for _, c := range change {
		c(r)
	}
	if err := r.m.Init(module.Deps{Cfg: r.cfg, Log: dbtest.Discard, Hub: r.hub}); err != nil {
		t.Fatal(err)
	}
	r.s = r.m.Source()
	return r
}

// run starts Run and stops it when the test ends.
func (r *rig) run() {
	r.t.Helper()
	ctx, cancel := context.WithCancel(bg)
	r.stop, r.done = cancel, make(chan struct{})
	go func() {
		defer close(r.done)
		if err := r.m.Run(ctx); err != nil {
			r.t.Errorf("Run: %v", err)
		}
	}()
	r.t.Cleanup(func() {
		cancel()
		<-r.done
	})
}

// up runs the module and waits until it is logged in.
func (r *rig) up() *rig {
	r.t.Helper()
	r.run()
	r.waitHealth(func(h source.Health) bool { return h.OK })
	return r
}

func (r *rig) waitHealth(ok func(source.Health) bool) source.Health {
	r.t.Helper()
	deadline := testwait.Deadline(10 * time.Second)
	for {
		h := r.s.Health(bg)
		if ok(h) {
			return h
		}
		if time.Now().After(deadline) {
			r.t.Fatalf("health stayed %+v", h)
		}
		time.Sleep(time.Millisecond)
	}
}

func (r *rig) item(kind source.Kind, id string) source.Item {
	r.t.Helper()
	it, err := r.s.Item(bg, ref(kind, id))
	if err != nil {
		r.t.Fatalf("%s %s: %v", kind, id, err)
	}
	return it
}

func (r *rig) download(kind source.Kind, id, format string, force bool) (*recorder, source.DownloadResult, error) {
	r.t.Helper()
	rec := newRecorder()
	res, err := r.s.Download(bg, r.request(kind, id, format, force), rec)
	return rec, res, err
}

func (r *rig) request(kind source.Kind, id, format string, force bool) source.DownloadRequest {
	return source.DownloadRequest{DownloadID: 1, Ref: ref(kind, id), Format: format, Force: force, MusicDir: r.cfg.MusicDir}
}

// calls counts the fake's requests to an endpoint, such as "catalog/search".
func (r *rig) calls(endpoint string) []qobuzdltest.Request {
	var out []qobuzdltest.Request
	for _, q := range r.srv.Requests() {
		if strings.HasSuffix(q.Path, "/"+endpoint) {
			out = append(out, q)
		}
	}
	return out
}

// exists reports whether path, relative to the music root, is there.
func (r *rig) exists(path string) bool {
	_, err := os.Stat(filepath.Join(r.cfg.MusicDir, path))
	return err == nil
}

// recorder is a Reporter that keeps everything it hears.
type recorder struct {
	mu      sync.Mutex
	plan    []source.Track
	starts  map[int]int
	written map[int]int64
	dones   map[int][]source.TrackResult
	lines   []string
	// onStart, when set, runs at each Start, outside the lock.
	onStart func()
}

func newRecorder() *recorder {
	return &recorder{starts: map[int]int{}, written: map[int]int64{}, dones: map[int][]source.TrackResult{}}
}

func (r *recorder) Plan(tracks []source.Track) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.plan = tracks
}

func (r *recorder) Start(pos int) {
	r.mu.Lock()
	r.starts[pos]++
	r.mu.Unlock()
	if r.onStart != nil {
		r.onStart()
	}
}

func (r *recorder) Progress(pos int, n int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.written[pos] = n
}

func (r *recorder) Done(pos int, t source.TrackResult) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.dones[pos] = append(r.dones[pos], t)
}

func (r *recorder) Log() source.Logger { return (*recLog)(r) }

// last is how a track ended, as a download's table would hold it after
// every Done: a later call keeps the path of an earlier one.
func (r *recorder) last(pos int) source.TrackResult {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out source.TrackResult
	for _, d := range r.dones[pos] {
		path := out.Path
		out = d
		if out.Path == "" {
			out.Path = path
		}
	}
	return out
}

type recLog recorder

func (l *recLog) add(level, format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, level+" "+fmt.Sprintf(format, args...))
}

func (l *recLog) Debug(f string, a ...any) { l.add("debug", f, a...) }
func (l *recLog) Info(f string, a ...any)  { l.add("info", f, a...) }
func (l *recLog) Warn(f string, a ...any)  { l.add("warn", f, a...) }
func (l *recLog) Error(f string, a ...any) { l.add("error", f, a...) }
