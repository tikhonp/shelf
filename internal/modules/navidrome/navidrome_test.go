package navidrome

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/tikhonp/shelf/internal/modules/downloads"
	"github.com/tikhonp/shelf/internal/modules/fakesource"
	"github.com/tikhonp/shelf/internal/platform/config"
	"github.com/tikhonp/shelf/internal/platform/db/dbtest"
	"github.com/tikhonp/shelf/internal/platform/hub"
	"github.com/tikhonp/shelf/internal/platform/jobs"
	"github.com/tikhonp/shelf/internal/platform/jobs/jobstest"
	"github.com/tikhonp/shelf/internal/platform/module"
	"github.com/tikhonp/shelf/internal/platform/testwait"
	"github.com/tikhonp/shelf/internal/platform/web"
	"github.com/tikhonp/shelf/internal/source"
)

var bg = context.Background()

const (
	user     = "admin"
	password = "s3cret-pass"
)

// fakeNavidrome is a Subsonic server with a scan state the test sets. It
// checks token authentication the way Navidrome does.
type fakeNavidrome struct {
	srv *httptest.Server

	mu       sync.Mutex
	scanning bool
	count    int
	lastScan time.Time
	starts   int
	statuses int
	queries  []url.Values
	// down answers every request with a 500.
	down bool
	// refuse answers every request as a wrong password.
	refuse bool
	// scanFor, when set, keeps a started scan running for that many status
	// reads.
	scanFor int
}

func newFake(t *testing.T) *fakeNavidrome {
	f := &fakeNavidrome{count: 1200, lastScan: time.Date(2026, 3, 9, 8, 0, 0, 0, time.UTC)}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeNavidrome) url(t *testing.T) *url.URL {
	u, err := url.Parse(f.srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func (f *fakeNavidrome) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	q := r.URL.Query()
	f.queries = append(f.queries, q)
	if f.down {
		http.Error(w, "down", http.StatusInternalServerError)
		return
	}
	resp := map[string]any{"status": "ok", "version": "1.16.1", "type": "navidrome", "serverVersion": "0.58.0", "openSubsonic": true}
	sum := md5.Sum([]byte(password + q.Get("s")))
	if f.refuse || q.Get("u") != user || q.Get("t") != hex.EncodeToString(sum[:]) {
		resp["status"] = "failed"
		resp["error"] = map[string]any{"code": 40, "message": "Wrong username or password"}
		_ = json.NewEncoder(w).Encode(map[string]any{"subsonic-response": resp})
		return
	}
	switch strings.TrimPrefix(r.URL.Path, "/rest/") {
	case "startScan":
		f.starts++
		f.scanning = true
	case "getScanStatus":
		f.statuses++
		if f.scanning && f.scanFor > 0 {
			if f.scanFor--; f.scanFor == 0 {
				f.scanning = false
				f.count++
				f.lastScan = f.lastScan.Add(time.Hour)
			}
		}
	default:
		resp["status"] = "failed"
		resp["error"] = map[string]any{"code": 70, "message": "not found"}
	}
	resp["scanStatus"] = map[string]any{"scanning": f.scanning, "count": f.count, "folderCount": 10,
		"lastScan": f.lastScan.Format(time.RFC3339)}
	_ = json.NewEncoder(w).Encode(map[string]any{"subsonic-response": resp})
}

func (f *fakeNavidrome) get(fn func(f *fakeNavidrome)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(f)
}

func (f *fakeNavidrome) startCount() (n int) {
	f.get(func(f *fakeNavidrome) { n = f.starts })
	return n
}

// rig is the navidrome module wired to the downloads module and a fake
// source, as main wires them, on a migrated database.
type rig struct {
	t    *testing.T
	h    *jobstest.Harness
	nav  *Module
	dl   *downloads.Module
	fake *fakeNavidrome
	hub  *hub.Hub
}

func newRig(t *testing.T) *rig {
	t.Helper()
	fake := newFake(t)
	h := jobstest.New(t, downloads.New())
	r := &rig{t: t, h: h, fake: fake, hub: hub.New(), nav: New(), dl: downloads.New()}
	cfg := &config.Config{MusicDir: t.TempDir(), TZ: time.UTC, Navidrome: config.Navidrome{
		URL: fake.url(t), User: user, Password: password, ScanDelay: 30 * time.Second,
	}}
	reg := source.NewRegistry()
	if err := reg.Add(fakesource.NewSource("fake", "Fake")); err != nil {
		t.Fatal(err)
	}
	r.nav.PollEvery = 10 * time.Millisecond
	r.dl.Service().SetScanner(r.nav)
	d := module.Deps{Cfg: cfg, Log: dbtest.Discard, DB: h.DB, Jobs: h.Sys, Hub: r.hub, Sources: reg, Closing: make(chan struct{})}
	for _, m := range []module.Initializer{r.dl, r.nav} {
		if err := m.Init(d); err != nil {
			t.Fatal(err)
		}
	}
	r.dl.Service().Now = h.Clock.Now
	if err := h.Sys.Register(downloads.Name, r.dl.JobTypes()...); err != nil {
		t.Fatal(err)
	}
	if err := h.Sys.Register(Name, r.nav.JobTypes()...); err != nil {
		t.Fatal(err)
	}
	return r
}

func (r *rig) start() {
	r.h.Start(r.h.Sys)
	ctx, cancel := context.WithCancel(bg)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = r.nav.Run(ctx)
	}()
	r.t.Cleanup(func() { cancel(); <-done })
}

// scanJobs lists the scan jobs, oldest first.
func (r *rig) scanJobs() []jobs.Job {
	r.t.Helper()
	list, err := r.h.Sys.List(bg, jobs.Filter{Type: JobType, Limit: 50})
	if err != nil {
		r.t.Fatal(err)
	}
	for i, j := 0, len(list)-1; i < j; i, j = i+1, j-1 {
		list[i], list[j] = list[j], list[i]
	}
	return list
}

func (r *rig) do(method, path string) *httptest.ResponseRecorder {
	e := echo.New()
	e.HTTPErrorHandler = web.ErrorHandler
	r.nav.Routes(e.Group("/api"))
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

func TestSubsonicAuth(t *testing.T) {
	f := newFake(t)
	c := NewClient(f.url(t), user, password, http.DefaultClient)
	for range 2 {
		if _, err := c.ScanStatus(bg); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.StartScan(bg); err != nil {
		t.Fatal(err)
	}
	var qs []url.Values
	f.get(func(f *fakeNavidrome) { qs = f.queries })
	if len(qs) != 3 {
		t.Fatalf("%d requests", len(qs))
	}
	salts := map[string]bool{}
	for _, q := range qs {
		if q.Has("p") {
			t.Fatalf("the password travels: %v", q)
		}
		sum := md5.Sum([]byte(password + q.Get("s")))
		if q.Get("u") != user || q.Get("t") != hex.EncodeToString(sum[:]) || len(q.Get("s")) < 6 ||
			q.Get("v") != "1.16.1" || q.Get("c") != "shelf" || q.Get("f") != "json" {
			t.Fatalf("query %v", q)
		}
		salts[q.Get("s")] = true
	}
	if len(salts) != 3 {
		t.Fatalf("salts repeat: %v", salts)
	}
}

func TestSubsonicError(t *testing.T) {
	f := newFake(t)
	c := NewClient(f.url(t), user, "wrong", http.DefaultClient)
	_, err := c.ScanStatus(bg)
	var se *SubsonicError
	if !errors.As(err, &se) || se.Code != 40 || se.Message != "Wrong username or password" {
		t.Fatalf("error %v", err)
	}

	// An unreachable server's error leaves the token-bearing URL out.
	f.srv.Close()
	_, err = NewClient(f.url(t), user, password, http.DefaultClient).ScanStatus(bg)
	if err == nil || strings.Contains(err.Error(), "t=") || strings.Contains(err.Error(), "rest/") {
		t.Fatalf("error %v", err)
	}
}

func TestScanCoalesces(t *testing.T) {
	r := newRig(t)
	r.start()
	svc := r.dl.Service()
	for _, id := range []string{"album-1", "album-2", "album-3"} {
		if _, err := svc.Queue(bg, downloads.QueueRequest{Ref: source.Ref{Source: "fake", Kind: source.KindAlbum, ID: id}, Origin: downloads.OriginWeb}); err != nil {
			t.Fatal(err)
		}
	}
	r.h.Drain()
	list := r.scanJobs()
	if len(list) != 1 || list[0].Merged != 2 || list[0].State != jobs.Queued || list[0].Queue != jobs.QueueLibrary {
		t.Fatalf("scan jobs %+v", list)
	}
	start := r.h.Clock.Now()
	if !list[0].RunAfter.Equal(start.Add(30 * time.Second)) {
		t.Fatalf("runs at %v", list[0].RunAfter)
	}

	// A download that ends 20 seconds later moves the scan later.
	r.h.Clock.Advance(20 * time.Second)
	if err := r.nav.RequestScan(bg); err != nil {
		t.Fatal(err)
	}
	if list = r.scanJobs(); len(list) != 1 || !list[0].RunAfter.Equal(start.Add(50*time.Second)) {
		t.Fatalf("scan jobs %+v", list)
	}
	r.h.Clock.Advance(15 * time.Second)
	r.h.Drain()
	if n := r.fake.startCount(); n != 0 {
		t.Fatalf("%d scans before the delay", n)
	}
	r.h.Clock.Advance(16 * time.Second)
	r.h.Sys.Kick()
	r.h.Drain()
	if n := r.fake.startCount(); n != 1 {
		t.Fatalf("%d scans", n)
	}
	if st := r.h.State(list[0].ID); st != jobs.Succeeded {
		t.Fatalf("scan job %s", st)
	}
}

func TestScanDefers(t *testing.T) {
	r := newRig(t)
	r.fake.get(func(f *fakeNavidrome) { f.scanning = true })
	r.start()
	id, err := r.nav.ScanNow(bg, "test")
	if err != nil {
		t.Fatal(err)
	}
	r.h.WaitFor("deferred", func() bool {
		j, err := r.h.Sys.Job(bg, id)
		return err == nil && j.State == jobs.Queued && j.Attempt == 0 && j.RunAfter.After(r.h.Clock.Now())
	})
	j, _ := r.h.Sys.Job(bg, id)
	if !j.RunAfter.Equal(r.h.Clock.Now().Add(DeferFor)) || r.fake.startCount() != 0 {
		t.Fatalf("job %+v, %d scans", j, r.fake.startCount())
	}

	r.fake.get(func(f *fakeNavidrome) { f.scanning = false })
	r.h.Clock.Advance(DeferFor)
	r.h.Sys.Kick()
	r.h.Drain()
	if r.fake.startCount() != 1 || r.h.State(id) != jobs.Succeeded {
		t.Fatalf("%d scans, job %s", r.fake.startCount(), r.h.State(id))
	}
}

func TestScanRetries(t *testing.T) {
	r := newRig(t)
	r.fake.get(func(f *fakeNavidrome) { f.down = true })
	r.start()
	id, err := r.nav.ScanNow(bg, "test")
	if err != nil {
		t.Fatal(err)
	}
	waitDeferred := func(failures int) {
		t.Helper()
		r.h.WaitFor("a deferred scan", func() bool {
			j, err := r.h.Sys.Job(bg, id)
			return err == nil && j.State == jobs.Queued && j.Attempt == 0 && string(j.Payload) == `{"failures":`+itoa(failures)+`}`
		})
	}
	waitDeferred(1)
	// A download's request merges into the waiting scan.
	if err := r.nav.RequestScan(bg); err != nil {
		t.Fatal(err)
	}
	if list := r.scanJobs(); len(list) != 1 || list[0].ID != id {
		t.Fatalf("scan jobs %+v", list)
	}
	r.h.Clock.Advance(RetryAfter)
	r.h.Sys.Kick()
	waitDeferred(2)

	r.fake.get(func(f *fakeNavidrome) { f.down = false })
	r.h.Clock.Advance(RetryAfter)
	r.h.Sys.Kick()
	r.h.Drain()
	if r.fake.startCount() != 1 || r.h.State(id) != jobs.Succeeded {
		t.Fatalf("%d scans, job %s", r.fake.startCount(), r.h.State(id))
	}

	// Navidrome that refuses the password ends the scan at once.
	r.fake.get(func(f *fakeNavidrome) { f.refuse = true })
	id, err = r.nav.ScanNow(bg, "test")
	if err != nil {
		t.Fatal(err)
	}
	r.h.Drain()
	if j, _ := r.h.Sys.Job(bg, id); j.State != jobs.Failed || !strings.Contains(j.Error, "error 40") {
		t.Fatalf("job %+v", j)
	}
}

func TestScanGivesUp(t *testing.T) {
	r := newRig(t)
	r.fake.get(func(f *fakeNavidrome) { f.down = true })
	r.nav.RetryAfter = 0
	r.start()
	id, err := r.nav.ScanNow(bg, "test")
	if err != nil {
		t.Fatal(err)
	}
	r.h.Drain()
	var calls int
	r.fake.get(func(f *fakeNavidrome) { calls = len(f.queries) })
	if j, _ := r.h.Sys.Job(bg, id); j.State != jobs.Failed || calls != maxFailures {
		t.Fatalf("job %s after %d tries", j.State, calls)
	}
}

func itoa(n int) string { return strconv.Itoa(n) }

func TestScanNow(t *testing.T) {
	r := newRig(t)
	r.start()
	// A scan waits for the delay after a download.
	if err := r.nav.RequestScan(bg); err != nil {
		t.Fatal(err)
	}
	rec := r.do(http.MethodPost, "/api/library/scan")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	var body ScanQueuedJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	r.h.Drain()
	if n := r.fake.startCount(); n != 1 {
		t.Fatalf("%d scans", n)
	}
	if list := r.scanJobs(); len(list) != 1 || list[0].ID != body.JobID || list[0].State != jobs.Succeeded {
		t.Fatalf("scan jobs %+v", list)
	}

	// With nothing queued, it queues a scan that runs now.
	r.fake.get(func(f *fakeNavidrome) { f.scanning = false })
	if rec := r.do(http.MethodPost, "/api/library/scan"); rec.Code != http.StatusAccepted {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	r.h.Drain()
	if n := r.fake.startCount(); n != 2 {
		t.Fatalf("%d scans", n)
	}
}

func TestScanEvents(t *testing.T) {
	r := newRig(t)
	r.fake.get(func(f *fakeNavidrome) { f.scanFor = 3 })
	events, stop := r.hub.Subscribe(Topic)
	defer stop()
	r.start()
	if _, err := r.nav.ScanNow(bg, "test"); err != nil {
		t.Fatal(err)
	}
	next := func() Event {
		t.Helper()
		select {
		case e := <-events:
			return e.Data.(Event)
		case <-time.After(testwait.For(30 * time.Second)):
			t.Fatal("no scan event")
		}
		return Event{}
	}
	if e := next(); !e.Scanning || e.Count != 1200 || e.LastScan == nil {
		t.Fatalf("start event %+v", e)
	}
	e := next()
	if e.Scanning || e.Count != 1201 || !e.LastScan.Equal(time.Date(2026, 3, 9, 9, 0, 0, 0, time.UTC)) {
		t.Fatalf("end event %+v", e)
	}
	// Polling stops once the scan has ended.
	var n int
	r.fake.get(func(f *fakeNavidrome) { n = f.statuses })
	time.Sleep(100 * time.Millisecond)
	r.fake.get(func(f *fakeNavidrome) {
		if f.statuses != n {
			t.Errorf("%d more status reads after the scan ended", f.statuses-n)
		}
	})
}

func TestScanStatusRoute(t *testing.T) {
	r := newRig(t)
	rec := r.do(http.MethodGet, "/api/library/scan")
	var got ScanJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK || !got.Enabled || !got.OK || got.Scanning || got.Count != 1200 || got.LastScan == nil ||
		got.Address != r.fake.url(t).Host {
		t.Fatalf("%d %+v", rec.Code, got)
	}
	if strings.Contains(rec.Body.String(), password) {
		t.Fatal("the answer holds the password")
	}

	r.fake.get(func(f *fakeNavidrome) { f.down = true })
	rec = r.do(http.MethodGet, "/api/library/scan")
	got = ScanJSON{}
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if rec.Code != http.StatusOK || !got.Enabled || got.OK || got.Message != textNoAnswer {
		t.Fatalf("%d %+v", rec.Code, got)
	}
}

func TestScansOff(t *testing.T) {
	h := jobstest.New(t)
	m := New()
	d := module.Deps{Cfg: &config.Config{}, Log: dbtest.Discard, DB: h.DB, Jobs: h.Sys, Hub: hub.New()}
	if err := m.Init(d); err != nil {
		t.Fatal(err)
	}
	if err := h.Sys.Register(Name, m.JobTypes()...); err != nil {
		t.Fatal(err)
	}
	if err := m.RequestScan(bg); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := h.DB.R.Get(&n, `SELECT count(*) FROM jobs`); err != nil || n != 0 {
		t.Fatalf("%d jobs, %v", n, err)
	}
	r := &rig{t: t, nav: m}
	if rec := r.do(http.MethodPost, "/api/library/scan"); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	rec := r.do(http.MethodGet, "/api/library/scan")
	var got ScanJSON
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if rec.Code != http.StatusOK || got.Enabled || got.Message != textOff {
		t.Fatalf("%d %+v", rec.Code, got)
	}
	// Run has nothing to follow and returns.
	if err := m.Run(bg); err != nil {
		t.Fatal(err)
	}
}

func TestScanRequestNamesWhoAsked(t *testing.T) {
	r := newRig(t)
	if err := r.nav.RequestScanFor(bg, "library"); err != nil {
		t.Fatal(err)
	}
	if list := r.scanJobs(); len(list) != 1 || list[0].CreatedBy != "library" {
		t.Fatalf("scan jobs %+v", list)
	}
}
