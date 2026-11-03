package jobs_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/tikhonp/shelf/internal/platform/jobs"
	"github.com/tikhonp/shelf/internal/platform/testwait"
)

func provisioning(name string, run func(ctx context.Context, r *jobs.Run) error) jobs.Type {
	return jobs.Type{Name: name, Queue: jobs.QueueQobuz, MaxAttempts: 1, Steps: []jobs.Step{{Name: "run", Run: run}}}
}

func TestQueueConcurrency(t *testing.T) {
	h := newH(t)
	// Every shelf queue runs one job at a time; the pool works for more.
	jobs.Concurrency[jobs.QueueQobuz] = 2
	t.Cleanup(func() { jobs.Concurrency[jobs.QueueQobuz] = 1 })
	g := newGate()
	reg(t, h.Sys, provisioning("test.provision", func(ctx context.Context, r *jobs.Run) error { return g.wait(ctx) }))
	h.Start(h.Sys)
	for i := range 3 {
		enq(t, h, jobs.Request{Type: "test.provision", ResourceKey: "server:" + string(rune('1'+i))})
	}
	waitInt(t, h, "two running", g.reached.Load, 2)
	time.Sleep(100 * time.Millisecond)
	if n := count(t, h, `SELECT count(*) FROM jobs WHERE state = 'running'`); n != 2 {
		t.Fatalf("%d running, want 2", n)
	}
	if n := count(t, h, `SELECT count(*) FROM jobs WHERE state = 'queued'`); n != 1 {
		t.Fatalf("%d queued, want 1", n)
	}
	g.release()
	h.Drain()
	if n := count(t, h, `SELECT count(*) FROM jobs WHERE state = 'succeeded'`); n != 3 {
		t.Fatalf("%d succeeded", n)
	}
}

func TestRetryableFailureResumesAtFailedStep(t *testing.T) {
	h := newH(t)
	var a, b atomic.Int32
	reg(t, h.Sys, jobs.Type{Name: "test.sync", Queue: jobs.QueueAppleMusic, MaxAttempts: 2, Backoff: []time.Duration{time.Minute},
		Steps: []jobs.Step{
			{Name: "first", Run: func(_ context.Context, r *jobs.Run) error { a.Add(1); r.Log().Info("first ran"); return nil }},
			{Name: "second", Run: func(_ context.Context, r *jobs.Run) error {
				if b.Add(1) == 1 {
					return errors.New("router unreachable")
				}
				r.Log().Info("second ran")
				return nil
			}},
		}})
	h.Start(h.Sys)
	e := enq(t, h, jobs.Request{Type: "test.sync", ResourceKey: "router:3"})
	h.Drain()
	j, _ := h.Sys.Job(bg, e.ID)
	if j.State != jobs.Queued || j.Attempt != 1 {
		t.Fatalf("after the first attempt: %s attempt %d", j.State, j.Attempt)
	}
	if !j.RunAfter.After(h.Clock.Now().Add(59 * time.Second)) {
		t.Fatalf("run after %v", j.RunAfter)
	}
	h.Clock.Advance(2 * time.Minute)
	h.Drain()
	if st := h.State(e.ID); st != jobs.Succeeded {
		t.Fatalf("state %s", st)
	}
	if a.Load() != 1 || b.Load() != 2 {
		t.Fatalf("first ran %d, second %d", a.Load(), b.Load())
	}
	var attempts []int
	if err := h.DB.R.Select(&attempts, `SELECT DISTINCT attempt FROM job_log_lines WHERE job_id = ? AND level = 'step' ORDER BY attempt`, e.ID); err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 2 || attempts[0] != 1 || attempts[1] != 2 {
		t.Fatalf("log attempts %v", attempts)
	}
}

func TestCancelQueuedJob(t *testing.T) {
	h := newH(t)
	var ran atomic.Int32
	reg(t, h.Sys, simple("test.sync", func(context.Context, *jobs.Run) error { ran.Add(1); return nil }))
	e := enq(t, h, jobs.Request{Type: "test.sync", Delay: time.Hour})
	if err := h.Sys.Cancel(bg, e.ID, "admin"); err != nil {
		t.Fatal(err)
	}
	if st := h.State(e.ID); st != jobs.Cancelled {
		t.Fatalf("state %s", st)
	}
	h.Clock.Advance(2 * time.Hour)
	h.Start(h.Sys)
	h.Drain()
	if ran.Load() != 0 {
		t.Fatal("a cancelled job ran")
	}
	if err := h.Sys.Cancel(bg, e.ID, "admin"); !errors.Is(err, jobs.ErrNotActive) {
		t.Fatalf("second cancel: %v", err)
	}
}

func TestCancelRunningJobStopsAtStepBoundary(t *testing.T) {
	h := newH(t)
	g := newGate()
	var second, onCancelled atomic.Int32
	var by string
	reg(t, h.Sys, jobs.Type{Name: "test.provision", Queue: jobs.QueueQobuz, MaxAttempts: 1,
		Steps: []jobs.Step{
			{Name: "install", Run: func(ctx context.Context, r *jobs.Run) error { return g.wait(ctx) }},
			{Name: "configure", Run: func(context.Context, *jobs.Run) error { second.Add(1); return nil }},
		},
		OnCancelled: func(_ context.Context, _ *sqlx.Tx, _ jobs.Info, who string) error {
			onCancelled.Add(1)
			by = who
			return nil
		}})
	h.Start(h.Sys)
	e := enq(t, h, jobs.Request{Type: "test.provision", ResourceKey: "server:12"})
	waitInt(t, h, "install running", g.reached.Load, 1)
	if err := h.Sys.Cancel(bg, e.ID, "admin"); err != nil {
		t.Fatal(err)
	}
	g.release() // the step finishes; the job stops before the next one
	h.Drain()
	if st := h.State(e.ID); st != jobs.Cancelled {
		t.Fatalf("state %s", st)
	}
	if second.Load() != 0 || onCancelled.Load() != 1 || by != "admin" {
		t.Fatalf("second=%d onCancelled=%d by=%q", second.Load(), onCancelled.Load(), by)
	}
	if !contains(logs(t, h, e.ID), "Cancelled by admin") {
		t.Fatalf("log: %q", logs(t, h, e.ID))
	}
}

func TestStepCanStopOnCancel(t *testing.T) {
	h := newH(t)
	started := make(chan struct{})
	reg(t, h.Sys, simple("test.long", func(ctx context.Context, r *jobs.Run) error {
		close(started)
		select {
		case <-r.Cancelling():
			return jobs.ErrCancelled
		case <-ctx.Done():
			return ctx.Err()
		}
	}))
	h.Start(h.Sys)
	e := enq(t, h, jobs.Request{Type: "test.long"})
	<-started
	if err := h.Sys.Cancel(bg, e.ID, "admin"); err != nil {
		t.Fatal(err)
	}
	h.Drain()
	if st := h.State(e.ID); st != jobs.Cancelled {
		t.Fatalf("state %s", st)
	}
}

// twoStepType blocks its second step the first time it runs, until ctx ends or
// the gate opens.
func twoStepType(g *gate, first, second *atomic.Int32) jobs.Type {
	return jobs.Type{Name: "test.sync", Queue: jobs.QueueAppleMusic, MaxAttempts: 1, Steps: []jobs.Step{
		{Name: "prepare", Run: func(context.Context, *jobs.Run) error { first.Add(1); return nil }},
		{Name: "push", Run: func(ctx context.Context, r *jobs.Run) error {
			if second.Add(1) == 1 {
				return g.wait(ctx)
			}
			r.Log().Info("pushed")
			return nil
		}},
	}}
}

func TestRestartInterruptsAndResumes(t *testing.T) {
	h := newH(t)
	g := newGate()
	var first, second atomic.Int32
	reg(t, h.Sys, twoStepType(g, &first, &second))
	h.Start(h.Sys)
	e := enq(t, h, jobs.Request{Type: "test.sync", ResourceKey: "router:3"})
	waitInt(t, h, "push running", g.reached.Load, 1)

	// A second life of the process: it finds the first one's running job.
	b := h.NewSystem()
	reg(t, b, twoStepType(g, &first, &second))
	h.Start(b)
	h.WaitFor("job succeeded", func() bool { return h.State(e.ID) == jobs.Succeeded })

	if first.Load() != 1 || second.Load() != 2 {
		t.Fatalf("prepare ran %d times, push %d", first.Load(), second.Load())
	}
	lines := logs(t, h, e.ID)
	if !contains(lines, "Interrupted by restart") || !contains(lines, "pushed") {
		t.Fatalf("log: %q", lines)
	}
	if j, _ := h.Sys.Job(bg, e.ID); j.Attempt != 1 {
		t.Fatalf("attempt %d: an interruption must not count", j.Attempt)
	}
	// The old runner wakes up late and must not touch the job.
	g.release()
	time.Sleep(100 * time.Millisecond)
	if st := h.State(e.ID); st != jobs.Succeeded {
		t.Fatalf("state %s after the old runner returned", st)
	}
}

func TestRetryCreatesLinkedJobFromFailedStep(t *testing.T) {
	h := newH(t)
	var first, second atomic.Int32
	fail := atomic.Bool{}
	fail.Store(true)
	reg(t, h.Sys, jobs.Type{Name: "test.provision", Queue: jobs.QueueQobuz, MaxAttempts: 1, Steps: []jobs.Step{
		{Name: "connect", Run: func(context.Context, *jobs.Run) error { first.Add(1); return nil }},
		{Name: "install", Run: func(context.Context, *jobs.Run) error {
			second.Add(1)
			if fail.Load() {
				return errors.New("apt failed")
			}
			return nil
		}},
	}})
	h.Start(h.Sys)
	e := enq(t, h, jobs.Request{Type: "test.provision", ResourceKey: "server:1", Payload: map[string]string{"k": "v"}})
	h.Drain()
	if st := h.State(e.ID); st != jobs.Failed {
		t.Fatalf("state %s", st)
	}
	if _, err := h.Sys.Retry(bg, 999, "admin"); !errors.Is(err, jobs.ErrNotFound) {
		t.Fatalf("retry of nothing: %v", err)
	}
	fail.Store(false)
	id, err := h.Sys.Retry(bg, e.ID, "admin")
	if err != nil {
		t.Fatal(err)
	}
	h.Drain()
	j, _ := h.Sys.Job(bg, id)
	if j.State != jobs.Succeeded || j.RetryOf != e.ID || string(j.Payload) != `{"k":"v"}` {
		t.Fatalf("%+v", j)
	}
	if first.Load() != 1 || second.Load() != 2 {
		t.Fatalf("connect ran %d, install %d", first.Load(), second.Load())
	}
	steps, _ := h.Sys.Steps(bg, id)
	if steps[0].CarriedFrom != e.ID || steps[1].CarriedFrom != 0 {
		t.Fatalf("%+v", steps)
	}
	// A job that succeeded can't be retried.
	if _, err := h.Sys.Retry(bg, id, "admin"); !errors.Is(err, jobs.ErrNotRetryable) {
		t.Fatalf("err = %v", err)
	}
}

func TestOneRunningJobPerResourceKey(t *testing.T) {
	h := newH(t)
	var mu sync.Mutex
	live := map[string]int{}
	var overlap atomic.Bool
	reg(t, h.Sys, simple("test.sync", func(ctx context.Context, r *jobs.Run) error {
		key := r.Info().ResourceKey
		mu.Lock()
		live[key]++
		if live[key] > 1 {
			overlap.Store(true)
		}
		mu.Unlock()
		time.Sleep(30 * time.Millisecond)
		mu.Lock()
		live[key]--
		mu.Unlock()
		return nil
	}))
	h.Start(h.Sys)
	for range 4 {
		enq(t, h, jobs.Request{Type: "test.sync", ResourceKey: "router:3"})
	}
	other := enq(t, h, jobs.Request{Type: "test.sync", ResourceKey: "router:4"})
	h.Drain()
	if overlap.Load() {
		t.Fatal("two jobs of one resource key ran at once")
	}
	if st := h.State(other.ID); st != jobs.Succeeded {
		t.Fatal(st)
	}
	if n := count(t, h, `SELECT count(*) FROM jobs WHERE state = 'succeeded'`); n != 5 {
		t.Fatalf("%d succeeded", n)
	}
}

func TestFailureErrorIsRedacted(t *testing.T) {
	h := newH(t)
	gone := errors.New("exit status 1")
	var sawCause bool
	var onFailedText string
	typ := jobs.Type{Name: "test.sync", Queue: jobs.QueueAppleMusic, MaxAttempts: 2, Backoff: []time.Duration{0},
		Steps: []jobs.Step{{Name: "push", Run: func(_ context.Context, r *jobs.Run) error {
			r.Log().Redact("hunter2-root-password", "uuid-added-by-module")
			return fmt.Errorf("stderr: login hunter2-root-password, client uuid-added-by-module: %w", gone)
		}}},
		OnFailed: func(_ context.Context, _ *sqlx.Tx, _ jobs.Info, err error) error {
			sawCause = errors.Is(err, gone)
			onFailedText = err.Error()
			return nil
		}}
	reg(t, h.Sys, typ)
	h.Start(h.Sys)
	e := enq(t, h, jobs.Request{Type: "test.sync", ResourceKey: "router:3"})
	h.Drain()
	j, _ := h.Sys.Job(bg, e.ID)
	want := "stderr: login •••, client •••: exit status 1"
	if j.State != jobs.Failed || j.Error != want {
		t.Fatalf("%s %q", j.State, j.Error)
	}
	if !sawCause || onFailedText != want {
		t.Fatalf("OnFailed saw %q, cause kept %v", onFailedText, sawCause)
	}
	var stepErr string
	if err := h.DB.R.Get(&stepErr, `SELECT error FROM job_steps WHERE job_id = ?`, e.ID); err != nil || stepErr != want {
		t.Fatalf("step error %q %v", stepErr, err)
	}
	for _, l := range logs(t, h, e.ID) {
		if strings.Contains(l, "hunter2") || strings.Contains(l, "uuid-added") {
			t.Fatalf("log: %q", l)
		}
	}
}

func TestOnFailedRunsInTheFailingTransaction(t *testing.T) {
	h := newH(t)
	if _, err := h.DB.W.Exec(`CREATE TABLE test_marks (job_id INTEGER, error TEXT) STRICT`); err != nil {
		t.Fatal(err)
	}
	typ := simple("test.sync", func(context.Context, *jobs.Run) error { return errors.New("down") })
	typ.OnFailed = func(ctx context.Context, tx *sqlx.Tx, j jobs.Info, err error) error {
		_, e := tx.ExecContext(ctx, `INSERT INTO test_marks VALUES (?, ?)`, j.ID, err.Error())
		return e
	}
	reg(t, h.Sys, typ)
	h.Start(h.Sys)
	e := enq(t, h, jobs.Request{Type: "test.sync"})
	h.Drain()
	if st := h.State(e.ID); st != jobs.Failed {
		t.Fatalf("state %s", st)
	}
	if n := count(t, h, `SELECT count(*) FROM test_marks WHERE job_id = ? AND error = 'down'`, e.ID); n != 1 {
		t.Fatalf("%d marks", n)
	}
}

// Cancel ends the step's context, so a step that watches only its context
// stops too, and the job ends cancelled, not failed.
func TestCancelEndsTheStepContext(t *testing.T) {
	h := newH(t)
	started := make(chan struct{})
	var cause atomic.Value
	var after atomic.Int32
	reg(t, h.Sys, jobs.Type{Name: "test.long", Queue: jobs.QueueSystem, MaxAttempts: 3, Steps: []jobs.Step{
		{Name: "wait", Run: func(ctx context.Context, r *jobs.Run) error {
			close(started)
			<-ctx.Done()
			cause.Store(ctx.Err())
			return fmt.Errorf("transfer: %w", ctx.Err())
		}},
		{Name: "after", Run: func(context.Context, *jobs.Run) error { after.Add(1); return nil }},
	}})
	h.Start(h.Sys)
	e := enq(t, h, jobs.Request{Type: "test.long"})
	<-started
	if err := h.Sys.Cancel(bg, e.ID, "web"); err != nil {
		t.Fatal(err)
	}
	h.Drain()
	j, _ := h.Sys.Job(bg, e.ID)
	if j.State != jobs.Cancelled || j.Attempt != 1 || j.CancelRequestedBy != "web" {
		t.Fatalf("%s attempt %d by %q", j.State, j.Attempt, j.CancelRequestedBy)
	}
	if err, _ := cause.Load().(error); !errors.Is(err, context.Canceled) {
		t.Fatalf("the step's context ended with %v", err)
	}
	if after.Load() != 0 {
		t.Fatal("a step after the cancel ran")
	}
}

func TestDeferDoesNotCountAttempt(t *testing.T) {
	h := newH(t)
	var runs atomic.Int32
	reg(t, h.Sys, jobs.Type{Name: "test.send", Queue: jobs.QueueSystem, MaxAttempts: 1, Steps: []jobs.Step{{Name: "send", Run: func(context.Context, *jobs.Run) error {
		if runs.Add(1) == 1 {
			return jobs.Defer(time.Minute, "rate limited")
		}
		return nil
	}}}})
	h.Start(h.Sys)
	e := enq(t, h, jobs.Request{Type: "test.send"})
	h.Drain()
	j, _ := h.Sys.Job(bg, e.ID)
	if j.State != jobs.Queued || j.Attempt != 0 {
		t.Fatalf("%s attempt %d", j.State, j.Attempt)
	}
	h.Clock.Advance(2 * time.Minute)
	h.Drain()
	j, _ = h.Sys.Job(bg, e.ID)
	if j.State != jobs.Succeeded || j.Attempt != 1 {
		t.Fatalf("%s attempt %d", j.State, j.Attempt)
	}
}

func TestShutdownInterruptsRunningJobs(t *testing.T) {
	h := newH(t)
	g := newGate()
	reg(t, h.Sys, simple("test.long", func(ctx context.Context, r *jobs.Run) error { return g.wait(ctx) }))
	stop := h.Start(h.Sys)
	e := enq(t, h, jobs.Request{Type: "test.long"})
	waitInt(t, h, "running", g.reached.Load, 1)
	stop()
	if st := h.State(e.ID); st != jobs.Interrupted {
		t.Fatalf("state %s", st)
	}
	if !contains(logs(t, h, e.ID), "Interrupted by restart") {
		t.Fatal(logs(t, h, e.ID))
	}
}

func TestNoResumeTypeFailsAfterRestart(t *testing.T) {
	h := newH(t)
	g := newGate()
	mk := func() jobs.Type {
		typ := simple("test.once", func(ctx context.Context, r *jobs.Run) error { return g.wait(ctx) })
		typ.NoResume = true
		return typ
	}
	reg(t, h.Sys, mk())
	h.Start(h.Sys)
	e := enq(t, h, jobs.Request{Type: "test.once"})
	waitInt(t, h, "running", g.reached.Load, 1)
	b := h.NewSystem()
	reg(t, b, mk())
	h.Start(b)
	h.WaitFor("failed", func() bool { return h.State(e.ID) == jobs.Failed })
	j, _ := h.Sys.Job(bg, e.ID)
	if j.Error != "interrupted by restart" {
		t.Fatalf("error %q", j.Error)
	}
}

func TestStuckRunnerIsReaped(t *testing.T) {
	h := newH(t)
	g := newGate()
	reg(t, h.Sys, simple("test.stuck", func(ctx context.Context, r *jobs.Run) error { return g.wait(ctx) }))
	h.Start(h.Sys)
	e := enq(t, h, jobs.Request{Type: "test.stuck"})
	waitInt(t, h, "running", g.reached.Load, 1)
	if err := h.Sys.Health(); err != nil {
		t.Fatal(err)
	}
	h.Clock.Advance(2 * time.Minute) // the lease ran out and nobody renewed it
	if err := h.Sys.Health(); err == nil {
		t.Fatal("healthz must fail while a lease is expired")
	}
	if err := h.Sys.Reap(bg); err != nil {
		t.Fatal(err)
	}
	if !contains(logs(t, h, e.ID), "Interrupted: the runner stopped responding") {
		t.Fatal(logs(t, h, e.ID))
	}
	// The job is claimed again with a fresh lease; the stuck runner is ignored.
	h.WaitFor("healthy again", func() bool { return h.Sys.Health() == nil })
}

func TestResumeMatchesStepsByName(t *testing.T) {
	h := newH(t)
	var a, b, c atomic.Int32
	step := func(n *atomic.Int32) func(context.Context, *jobs.Run) error {
		return func(context.Context, *jobs.Run) error { n.Add(1); return nil }
	}
	v1 := jobs.Type{Name: "test.sync", Queue: jobs.QueueAppleMusic, MaxAttempts: 2, Backoff: []time.Duration{time.Minute}, Steps: []jobs.Step{
		{Name: "a", Run: step(&a)},
		{Name: "b", Run: func(context.Context, *jobs.Run) error {
			if b.Add(1) == 1 {
				return errors.New("first time")
			}
			return nil
		}},
	}}
	reg(t, h.Sys, v1)
	stop := h.Start(h.Sys)
	e := enq(t, h, jobs.Request{Type: "test.sync", ResourceKey: "router:3"})
	h.Drain()
	stop()

	// A new release adds a step in the middle.
	next := h.NewSystem()
	v2 := v1
	v2.Steps = []jobs.Step{{Name: "a", Run: step(&a)}, {Name: "c", Run: step(&c)}, v1.Steps[1]}
	reg(t, next, v2)
	h.Clock.Advance(2 * time.Minute)
	h.Start(next)
	h.Drain()
	if st := h.State(e.ID); st != jobs.Succeeded {
		t.Fatalf("state %s", st)
	}
	if a.Load() != 1 || c.Load() != 1 || b.Load() != 2 {
		t.Fatalf("a=%d b=%d c=%d", a.Load(), b.Load(), c.Load())
	}
}

func TestHealthFailsWhenPoolStalls(t *testing.T) {
	h := newH(t)
	h.Sys.SetTick("queue:checks", h.Clock.Now())
	if err := h.Sys.Health(); err != nil {
		t.Fatal(err)
	}
	h.Clock.Advance(31 * time.Second)
	if err := h.Sys.Health(); err == nil {
		t.Fatal("a pool that stopped looping must fail the health check")
	}
	h.Sys.SetTick("queue:checks", h.Clock.Now())
	if err := h.Sys.Health(); err != nil {
		t.Fatal(err)
	}
}

func TestRegisterRefusesBadTypes(t *testing.T) {
	h := newH(t)
	for name, typ := range map[string]jobs.Type{
		"wrong prefix": simple("other.x", nop),
		"no steps":     {Name: "test.x", Queue: jobs.QueueLibrary, MaxAttempts: 1},
		"no attempts":  {Name: "test.x", Queue: jobs.QueueLibrary, Steps: simple("", nop).Steps},
		"bad queue":    {Name: "test.x", Queue: "nope", MaxAttempts: 1, Steps: simple("", nop).Steps},
	} {
		if err := h.Sys.Register("test", typ); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	reg(t, h.Sys, simple("test.ok", nop))
	if err := h.Sys.Register("test", simple("test.ok", nop)); err == nil {
		t.Error("duplicate accepted")
	}
}

func TestRetryWithPatchesPayload(t *testing.T) {
	h := newH(t)
	var seen sync.Map
	fail := atomic.Bool{}
	fail.Store(true)
	reg(t, h.Sys, jobs.Type{Name: "test.provision", Queue: jobs.QueueQobuz, MaxAttempts: 1, Steps: []jobs.Step{
		{Name: "run", Run: func(_ context.Context, r *jobs.Run) error {
			var p struct {
				A string `json:"a"`
				B bool   `json:"b"`
			}
			if err := r.Payload(&p); err != nil {
				return err
			}
			seen.Store(r.Info().ID, p.A+"|"+boolString(p.B))
			if fail.Load() {
				return errors.New("no")
			}
			return nil
		}},
	}})
	h.Start(h.Sys)
	e := enq(t, h, jobs.Request{Type: "test.provision", ResourceKey: "server:1", Payload: map[string]any{"a": "kept", "b": false}})
	h.Drain()

	// a retry that fails to commit leaves no job behind
	boom := errors.New("boom")
	before := count(t, h, `SELECT count(*) FROM jobs`)
	err := h.DB.Write(bg, func(tx *sqlx.Tx) error {
		if _, err := h.Sys.RetryWithTx(bg, tx, e.ID, "admin", jobs.RetryOptions{}); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) || count(t, h, `SELECT count(*) FROM jobs`) != before {
		t.Fatalf("a rolled-back retry left a job: %v", err)
	}

	fail.Store(false)
	var id int64
	err = h.DB.Write(bg, func(tx *sqlx.Tx) (err error) {
		id, err = h.Sys.RetryWithTx(bg, tx, e.ID, "admin", jobs.RetryOptions{Payload: map[string]any{"b": true}})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	h.Sys.Kick()
	h.Drain()
	if st := h.State(id); st != jobs.Succeeded {
		t.Fatalf("state %s", st)
	}
	got, _ := seen.Load(id)
	if got != "kept|true" {
		t.Fatalf("the retry saw %q, want the patched payload", got)
	}
}

func boolString(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// With a poll that never fires, only kicks start jobs: an enqueue must wake
// the pool of every queue, and a finished job the one waiting for its key.
func TestKickWakesEveryPoolAndKeyWaiters(t *testing.T) {
	h := newH(t)
	h.Sys.Poll = time.Hour
	reg(t, h.Sys,
		simple("test.check", func(ctx context.Context, r *jobs.Run) error { time.Sleep(20 * time.Millisecond); return nil }),
		provisioning("test.provision", func(ctx context.Context, r *jobs.Run) error { return nil }))
	h.Start(h.Sys)
	time.Sleep(50 * time.Millisecond) // every pool has had its first look and waits
	ids := []int64{
		enq(t, h, jobs.Request{Type: "test.check", ResourceKey: "server:1"}).ID,
		enq(t, h, jobs.Request{Type: "test.check", ResourceKey: "server:1"}).ID,
		enq(t, h, jobs.Request{Type: "test.provision"}).ID,
	}
	deadline := testwait.Deadline(5 * time.Second)
	for _, id := range ids {
		for h.State(id) != jobs.Succeeded {
			if time.Now().After(deadline) {
				t.Fatalf("job %d is %s: nothing woke its pool", id, h.State(id))
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
}
