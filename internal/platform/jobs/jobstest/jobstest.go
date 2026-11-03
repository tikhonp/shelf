// Package jobstest gives tests a jobs.System on a migrated temporary database
// with a fake clock, and a way to run it until idle.
package jobstest

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/tikhonp/shelf/internal/platform/db"
	"github.com/tikhonp/shelf/internal/platform/db/dbtest"
	"github.com/tikhonp/shelf/internal/platform/jobs"
	"github.com/tikhonp/shelf/internal/platform/testwait"
)

// Clock is a clock tests move by hand.
type Clock struct {
	mu sync.Mutex
	t  time.Time
}

// Now implements jobs.System.Now.
func (c *Clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

// Advance moves the clock forward.
func (c *Clock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// Set moves the clock to t.
func (c *Clock) Set(t time.Time) {
	c.mu.Lock()
	c.t = t
	c.mu.Unlock()
}

// Harness is a System with what tests need around it.
type Harness struct {
	T     testing.TB
	DB    *db.DB
	Clock *Clock
	// Location is the schedules' time zone of every System the harness
	// builds. UTC unless a test changes it before NewSystem.
	Location *time.Location
	Sys      *jobs.System
}

// New returns a Harness on a database with the platform's migrations and
// those of extra. Its clock starts at 2026-03-10 12:00 UTC. The pools are
// not running: call Start.
func New(t testing.TB, extra ...db.MigrationSource) *Harness {
	t.Helper()
	h := &Harness{T: t, DB: dbtest.Open(t, extra...), Location: time.UTC,
		Clock: &Clock{t: time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)}}
	h.Sys = h.NewSystem()
	return h
}

// NewSystem builds another System on the same database, with its own boot id
// (a second process life).
func (h *Harness) NewSystem() *jobs.System {
	s := jobs.New(h.DB, h.Location, dbtest.Discard)
	s.Now = h.Clock.Now
	s.Poll = 5 * time.Millisecond
	s.SchedulerPoll = 20 * time.Millisecond
	s.Grace = 200 * time.Millisecond
	return s
}

// Start runs sys until the test ends or stop is called.
func (h *Harness) Start(sys *jobs.System) (stop func()) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := sys.Start(ctx); err != nil {
			h.T.Errorf("jobs: Start: %v", err)
		}
	}()
	var once sync.Once
	stop = func() { once.Do(func() { cancel(); <-done }) }
	h.T.Cleanup(stop)
	return stop
}

// Drain waits until no job is running or due.
func (h *Harness) Drain() {
	h.T.Helper()
	deadline := testwait.Deadline(60 * time.Second)
	calm := 0
	for time.Now().Before(deadline) {
		var n int
		err := h.DB.R.Get(&n, `
			SELECT count(*) FROM jobs
			WHERE state IN ('running', 'interrupted') OR (state = 'queued' AND run_after <= ?)`, db.At(h.Clock.Now()))
		if err != nil {
			h.T.Fatal(err)
		}
		if n == 0 {
			if calm++; calm >= 3 {
				return
			}
		} else {
			calm = 0
		}
		time.Sleep(10 * time.Millisecond)
	}
	h.T.Fatal("jobs did not become idle")
}

// WaitFor polls cond for up to 30 seconds.
func (h *Harness) WaitFor(what string, cond func() bool) {
	h.T.Helper()
	deadline := testwait.Deadline(30 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	h.T.Fatalf("timed out waiting for %s", what)
}

// State returns a job's state.
func (h *Harness) State(id int64) jobs.State {
	h.T.Helper()
	j, err := h.Sys.Job(context.Background(), id)
	if err != nil {
		h.T.Fatal(err)
	}
	return j.State
}
