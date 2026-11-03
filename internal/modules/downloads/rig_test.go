package downloads

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/tikhonp/shelf/internal/modules/fakesource"
	"github.com/tikhonp/shelf/internal/platform/config"
	"github.com/tikhonp/shelf/internal/platform/db/dbtest"
	"github.com/tikhonp/shelf/internal/platform/hub"
	"github.com/tikhonp/shelf/internal/platform/jobs"
	"github.com/tikhonp/shelf/internal/platform/jobs/jobstest"
	"github.com/tikhonp/shelf/internal/platform/module"
	"github.com/tikhonp/shelf/internal/platform/web"
	"github.com/tikhonp/shelf/internal/source"
)

var bg = context.Background()

// rig is the downloads module on a migrated database with the fake source.
// The pools don't run until start.
type rig struct {
	t       *testing.T
	h       *jobstest.Harness
	m       *Module
	svc     *Service
	src     *fakesource.Source
	reg     *source.Registry
	hub     *hub.Hub
	cfg     *config.Config
	closing chan struct{}
	stop    func()
}

func newRig(t *testing.T) *rig {
	t.Helper()
	h := jobstest.New(t, New())
	moscow, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		t.Fatal(err)
	}
	r := &rig{t: t, h: h, reg: source.NewRegistry(), hub: hub.New(), closing: make(chan struct{}),
		src: fakesource.NewSource("fake", "Fake"),
		cfg: &config.Config{MusicDir: t.TempDir(), TZ: moscow}}
	if err := r.reg.Add(r.src); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		select {
		case <-r.closing:
		default:
			close(r.closing)
		}
	})
	r.m = r.module(h.Sys)
	r.svc = r.m.svc
	return r
}

// module builds the downloads module on sys, as a start of the process does.
func (r *rig) module(sys *jobs.System) *Module {
	r.t.Helper()
	m := New()
	m.KeepAlive = 20 * time.Millisecond
	err := m.Init(module.Deps{Cfg: r.cfg, Log: dbtest.Discard, DB: r.h.DB, Jobs: sys, Hub: r.hub, Sources: r.reg, Closing: r.closing})
	if err != nil {
		r.t.Fatal(err)
	}
	m.svc.Now = r.h.Clock.Now
	if err := sys.Register(Name, m.JobTypes()...); err != nil {
		r.t.Fatal(err)
	}
	return m
}

func (r *rig) start() { r.stop = r.h.Start(r.h.Sys) }

// restart stops the jobs system and starts a new one with a new module on
// the same database, as a restart of the process does.
func (r *rig) restart() {
	r.t.Helper()
	r.stop()
	sys := r.h.NewSystem()
	r.m = r.module(sys)
	r.svc = r.m.svc
	r.h.Sys = sys
	r.start()
}

func album(id string) source.Ref { return source.Ref{Source: "fake", Kind: source.KindAlbum, ID: id} }

func (r *rig) queue(q QueueRequest) Download {
	r.t.Helper()
	if q.Origin == "" {
		q.Origin = OriginWeb
	}
	d, err := r.svc.Queue(bg, q)
	if err != nil {
		r.t.Fatalf("queue: %v", err)
	}
	return d
}

func (r *rig) get(id int64) Detail {
	r.t.Helper()
	d, err := r.svc.Get(bg, id)
	if err != nil {
		r.t.Fatal(err)
	}
	return d
}

func (r *rig) waitStatus(id int64, want Status) Download {
	r.t.Helper()
	var d Download
	r.h.WaitFor("download "+string(want), func() bool {
		d = r.get(id).Download
		return d.Status == want
	})
	return d
}

func (r *rig) count(query string, args ...any) int {
	r.t.Helper()
	var n int
	if err := r.h.DB.R.Get(&n, query, args...); err != nil {
		r.t.Fatal(err)
	}
	return n
}

func (r *rig) job(id int64) jobs.Job {
	r.t.Helper()
	j, err := r.h.Sys.Job(bg, id)
	if err != nil {
		r.t.Fatal(err)
	}
	return j
}

// http serves the module's routes the way the platform mounts them.
func (r *rig) http() *echo.Echo {
	e := echo.New()
	e.HTTPErrorHandler = web.ErrorHandler
	r.m.Routes(e.Group("/api"))
	return e
}

func (r *rig) do(method, path string, body any) *httptest.ResponseRecorder {
	r.t.Helper()
	var b bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&b).Encode(body); err != nil {
			r.t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, &b)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.http().ServeHTTP(rec, req)
	return rec
}

func mustURL(t *testing.T, s string) *url.URL {
	t.Helper()
	u, err := url.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

// gate holds a fake download at a track until released or its context ends.
type gate struct {
	reached chan struct{}
	release chan struct{}
}

func newGate() *gate { return &gate{reached: make(chan struct{}, 16), release: make(chan struct{})} }

func (g *gate) wait(ctx context.Context) error {
	g.reached <- struct{}{}
	select {
	case <-g.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (g *gate) waitReached(t *testing.T) {
	t.Helper()
	select {
	case <-g.reached:
	case <-time.After(30 * time.Second):
		t.Fatal("the download never reached the gate")
	}
}

var _ = http.MethodGet
