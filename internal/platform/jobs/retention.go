package jobs

import (
	"context"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/tikhonp/shelf/internal/platform/db"
)

const (
	JobRetention       = 30 * 24 * time.Hour
	FailedJobRetention = 90 * 24 * time.Hour
)

// RetentionType is the platform's daily clean-up of finished jobs.
const RetentionType = "platform.retention"

// PlatformTypes are the job types and schedules the jobs system itself owns.
func (s *System) PlatformTypes() ([]Type, []Schedule) {
	types := []Type{{
		Name: RetentionType, Queue: QueueSystem, MaxAttempts: 1,
		Steps: []Step{{Name: "jobs", Run: func(ctx context.Context, r *Run) error {
			n, err := s.PruneJobs(ctx)
			r.Log().Info("Removed %d finished jobs", n)
			return err
		}}},
	}}
	schedules := []Schedule{{
		Name: RetentionType, At: "05:00",
		Request: func(context.Context) (Request, error) { return Request{Type: RetentionType}, nil },
	}}
	return types, schedules
}

// PruneJobs deletes finished jobs past their retention, with their steps and
// lines: failed ones after 90 days, the others after 30, and clean quiet ones
// after a day.
func (s *System) PruneJobs(ctx context.Context) (n int64, err error) {
	now := s.Now()
	err = s.d.Write(ctx, func(tx *sqlx.Tx) error {
		res, err := tx.ExecContext(ctx, `
			DELETE FROM jobs WHERE
			  (state = 'failed' AND finished_at < ?)
			  OR (state IN ('succeeded', 'cancelled') AND finished_at < ?)
			  OR (quiet = 1 AND state = 'succeeded' AND attempt <= 1 AND finished_at < ?)`,
			db.At(now.Add(-FailedJobRetention)), db.At(now.Add(-JobRetention)), db.At(now.Add(-QuietRetention)))
		if err != nil {
			return err
		}
		n, _ = res.RowsAffected()
		return nil
	})
	return n, err
}
