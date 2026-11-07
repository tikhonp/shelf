package agents

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/jmoiron/sqlx"
)

// Daily budgets per connection, by usage kind.
var daily = map[string]int64{
	"lookup":       300,
	"run":          2000,
	"comment":      1000,
	"offer":        100,
	"upload":       100,
	"upload_bytes": 500 << 20,
	"proposal":     200,
	"revision":     500,
	"memory":       1000,
}

// Pending proposals allowed at once.
const (
	pendingPerConnection = 200
	pendingInAll         = 1000
)

// spend counts n of kind against conn's budget for today, in tx. Over the
// budget it counts nothing and answers 429.
func (m *Module) spend(ctx context.Context, tx *sqlx.Tx, conn int64, kind string, n int64) error {
	now := m.Now().UTC()
	d := day(now)
	var used int64
	if err := tx.GetContext(ctx, &used, `SELECT coalesce(max(n), 0) FROM agents_usage WHERE connection_id = ? AND day = ? AND kind = ?`, conn, d, kind); err != nil {
		return err
	}
	if used+n > daily[kind] {
		reset := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)
		return fail(http.StatusTooManyRequests, "budget", fmt.Sprintf("the daily %s budget of %d is spent", kind, daily[kind]),
			"kind", kind, "reset_at", reset.Format(time.RFC3339), "retry_after", int(reset.Sub(now).Seconds())+1)
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO agents_usage (connection_id, day, kind, n) VALUES (?, ?, ?, ?)
		ON CONFLICT (connection_id, day, kind) DO UPDATE SET n = n + excluded.n`, conn, d, kind, n)
	return err
}

func (m *Module) spendNow(ctx context.Context, conn int64, kind string, n int64) error {
	return m.db.Write(ctx, func(tx *sqlx.Tx) error { return m.spend(ctx, tx, conn, kind, n) })
}

func (m *Module) usage(ctx context.Context, conn int64) (map[string]int64, error) {
	rows := []struct {
		Kind string `db:"kind"`
		N    int64  `db:"n"`
	}{}
	if err := m.db.R.SelectContext(ctx, &rows, `SELECT kind, n FROM agents_usage WHERE connection_id = ? AND day = ?`, conn, day(m.Now())); err != nil {
		return nil, err
	}
	out := map[string]int64{}
	for k := range daily {
		out[k] = 0
	}
	for _, r := range rows {
		out[r.Kind] = r.N
	}
	return out, nil
}

// checkPendingQuota refuses a new proposal over the pending limits.
func checkPendingQuota(ctx context.Context, tx *sqlx.Tx, conn int64) error {
	var mine, all int64
	if err := tx.GetContext(ctx, &all, `SELECT count(*) FROM agents_proposals WHERE pending > 0 AND withdrawn_at IS NULL`); err != nil {
		return err
	}
	if err := tx.GetContext(ctx, &mine, `SELECT count(*) FROM agents_proposals WHERE pending > 0 AND withdrawn_at IS NULL AND connection_id = ?`, conn); err != nil {
		return err
	}
	if mine >= pendingPerConnection {
		return fail(http.StatusTooManyRequests, "budget", fmt.Sprintf("this connection has %d pending proposals; wait for reviews", mine), "kind", "pending_proposals")
	}
	if all >= pendingInAll {
		return fail(http.StatusTooManyRequests, "budget", fmt.Sprintf("the inbox has %d pending proposals; wait for reviews", all), "kind", "pending_proposals")
	}
	return nil
}

// lookup runs f through the agent-wide gate: one external lookup at a time,
// waiting at most LookupWait, so agents can't crowd out the editor.
func (m *Module) lookup(ctx context.Context, f func() error) error {
	t := time.NewTimer(m.LookupWait)
	defer t.Stop()
	select {
	case m.gate <- struct{}{}:
	case <-t.C:
		return fail(http.StatusTooManyRequests, "busy", "another agent lookup is running; try again shortly", "retry_after", 5)
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-m.gate }()
	return f()
}
