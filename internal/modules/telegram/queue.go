package telegram

// The send queue is vk2tg's internal/tg/queue.go. Edits go through it like
// sends; the editor in status.go keeps at most one edit per message in it,
// and each edit reads the download's state only when its turn comes.

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// Limits configures the send queue's token buckets.
type Limits struct {
	// PerChat and PerChatBurst limit sends into one chat (Telegram allows
	// about 20 messages per minute in a group).
	PerChat      rate.Limit
	PerChatBurst int

	// Global and GlobalBurst limit all sends together (about 30/s).
	Global      rate.Limit
	GlobalBurst int

	// MaxAttempts bounds how many times a job is run when Telegram keeps
	// answering 429.
	MaxAttempts int
}

// DefaultLimits are the Bot API limits for sending into groups.
func DefaultLimits() Limits {
	return Limits{
		PerChat:      rate.Every(3 * time.Second),
		PerChatBurst: 3,
		Global:       30,
		GlobalBurst:  30,
		MaxAttempts:  5,
	}
}

// Queue serializes Bot API calls per chat, in FIFO order, under a per-chat and
// a global token bucket, and retries calls Telegram rejected with 429 after
// the retry_after it asked for. It is in-memory only.
type Queue struct {
	limits Limits
	global *rate.Limiter
	log    *slog.Logger

	mu    sync.Mutex
	chats map[int64]*chatQueue
}

type chatQueue struct {
	limiter *rate.Limiter
	jobs    []*job
	running bool
}

type job struct {
	ctx  context.Context
	fn   func(ctx context.Context) error
	done chan error
}

func NewQueue(limits Limits, log *slog.Logger) *Queue {
	return &Queue{
		limits: limits,
		global: rate.NewLimiter(limits.Global, limits.GlobalBurst),
		log:    log,
		chats:  make(map[int64]*chatQueue),
	}
}

// Do runs fn, a single Bot API call into chatID, after every call queued
// before it for the same chat, and returns its error. If fn fails with 429 it
// is run again after retry_after, so fn must be safe to repeat.
func (q *Queue) Do(ctx context.Context, chatID int64, fn func(ctx context.Context) error) error {
	j := &job{ctx: ctx, fn: fn, done: make(chan error, 1)}

	q.mu.Lock()
	cq := q.chats[chatID]
	if cq == nil {
		// Kept for the process lifetime so the bucket survives idle periods;
		// the bot answers a few people, so the map stays small.
		cq = &chatQueue{limiter: rate.NewLimiter(q.limits.PerChat, q.limits.PerChatBurst)}
		q.chats[chatID] = cq
	}
	cq.jobs = append(cq.jobs, j)
	if !cq.running {
		cq.running = true
		go q.work(chatID, cq)
	}
	q.mu.Unlock()

	select {
	case err := <-j.done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// work drains the chat's jobs and exits when the queue is empty.
func (q *Queue) work(chatID int64, cq *chatQueue) {
	for {
		q.mu.Lock()
		if len(cq.jobs) == 0 {
			cq.running = false
			q.mu.Unlock()
			return
		}
		j := cq.jobs[0]
		cq.jobs[0] = nil
		cq.jobs = cq.jobs[1:]
		q.mu.Unlock()

		j.done <- q.run(chatID, cq, j)
	}
}

func (q *Queue) run(chatID int64, cq *chatQueue, j *job) error {
	for attempt := 1; ; attempt++ {
		if err := cq.limiter.Wait(j.ctx); err != nil {
			return err
		}
		if err := q.global.Wait(j.ctx); err != nil {
			return err
		}
		err := j.fn(j.ctx)
		wait, limited := retryAfter(err)
		if !limited || attempt >= q.limits.MaxAttempts {
			return err
		}
		q.log.Warn("telegram rate limited", "chat_id", chatID, "retry_after", wait.String(), "attempt", attempt)
		select {
		case <-time.After(wait):
		case <-j.ctx.Done():
			return j.ctx.Err()
		}
	}
}
