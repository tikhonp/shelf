package jobs_test

import (
	"context"
	"testing"
	"time"

	"github.com/tikhonp/shelf/internal/platform/jobs"
)

func TestBusyMeansRunning(t *testing.T) {
	h := newH(t)
	g := newGate()
	reg(t, h.Sys, simple("test.hold", func(ctx context.Context, r *jobs.Run) error { return g.wait(ctx) }))
	h.Start(h.Sys)

	busy := func() bool {
		t.Helper()
		b, err := h.Sys.Busy(bg, "server:7")
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	if busy() {
		t.Fatal("busy with no job")
	}
	// Queued (delayed) does not count.
	enq(t, h, jobs.Request{Type: "test.hold", ResourceKey: "server:7", Delay: time.Hour})
	if busy() {
		t.Fatal("a queued job made the key busy")
	}
	// Running does; another key does not.
	enq(t, h, jobs.Request{Type: "test.hold", ResourceKey: "server:7"})
	waitInt(t, h, "job running", g.reached.Load, 1)
	if !busy() {
		t.Fatal("a running job did not make the key busy")
	}
	if b, _ := h.Sys.Busy(bg, "server:8"); b {
		t.Fatal("another key is busy")
	}
	g.release()
	h.WaitFor("key free", func() bool { return !busy() })
}

func TestHoldingListsUnfinishedJobsOfAKey(t *testing.T) {
	h := newH(t)
	g := newGate()
	reg(t, h.Sys, simple("test.hold", func(ctx context.Context, r *jobs.Run) error { return g.wait(ctx) }))
	h.Start(h.Sys)

	enq(t, h, jobs.Request{Type: "test.hold", ResourceKey: "server:7"})
	waitInt(t, h, "job running", g.reached.Load, 1)
	enq(t, h, jobs.Request{Type: "test.hold", ResourceKey: "server:7", Delay: time.Hour})
	enq(t, h, jobs.Request{Type: "test.hold", ResourceKey: "server:8"})

	held, err := h.Sys.Holding(bg, "server:7")
	if err != nil {
		t.Fatal(err)
	}
	if len(held) != 2 || held[0].State != jobs.Running || held[1].State != jobs.Queued || held[0].Type != "test.hold" {
		t.Fatalf("held: %+v", held)
	}
	if none, _ := h.Sys.Holding(bg, "server:9"); len(none) != 0 {
		t.Fatalf("another key: %+v", none)
	}
	g.release()
}
