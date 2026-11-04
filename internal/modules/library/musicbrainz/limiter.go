package musicbrainz

import (
	"context"
	"sync"
	"time"
)

// Priority orders the callers that share the limiter. An editor waiting on
// an album goes ahead of a bulk run.
type Priority int

const (
	Interactive Priority = iota
	Bulk
)

type priorityKey struct{}

// WithPriority marks the calls made under ctx. Unmarked calls are
// interactive.
func WithPriority(ctx context.Context, p Priority) context.Context {
	return context.WithValue(ctx, priorityKey{}, p)
}

func priorityOf(ctx context.Context) Priority {
	p, _ := ctx.Value(priorityKey{}).(Priority)
	return p
}

// Limiter hands out one slot per Every, the rate MusicBrainz asks of every
// client. It starts no goroutine: a caller reserves the next free slot and
// sleeps until it. A bulk caller lets interactive ones that wait go first.
type Limiter struct {
	Every time.Duration
	// now and sleep are the clock. Tests replace them.
	now   func() time.Time
	sleep func(ctx context.Context, d time.Duration) error

	mu          sync.Mutex
	next        time.Time
	interactive int
}

// NewLimiter returns a limiter of one call per every.
func NewLimiter(every time.Duration) *Limiter {
	return &Limiter{Every: every, now: time.Now, sleep: sleepCtx}
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// Wait returns when the caller may send one request.
func (l *Limiter) Wait(ctx context.Context) error {
	p := priorityOf(ctx)
	l.mu.Lock()
	if p == Interactive {
		l.interactive++
		defer func() {
			l.mu.Lock()
			l.interactive--
			l.mu.Unlock()
		}()
	}
	for p == Bulk && l.interactive > 0 {
		l.mu.Unlock()
		if err := l.sleep(ctx, l.Every); err != nil {
			return err
		}
		l.mu.Lock()
	}
	now := l.now()
	slot := l.next
	if slot.Before(now) {
		slot = now
	}
	l.next = slot.Add(l.Every)
	l.mu.Unlock()
	return l.sleep(ctx, slot.Sub(now))
}
