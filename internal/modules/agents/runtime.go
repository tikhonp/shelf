package agents

import (
	"context"
	"math"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/tikhonp/shelf/internal/libraryport"
)

func (m *Module) reconcile(ctx context.Context) error {
	_, err := m.reconcileThrough(ctx, math.MaxInt64)
	return err
}

func (m *Module) reconcileThrough(ctx context.Context, through int64) (int64, error) {
	select {
	case m.reconcileGate <- struct{}{}:
		defer func() { <-m.reconcileGate }()
	case <-ctx.Done():
		return 0, ctx.Err()
	}
	lib, err := m.lib()
	if err != nil {
		return 0, err
	}
	cursor, err := m.reconcileSavesThrough(ctx, lib, through)
	if err != nil {
		return cursor, err
	}
	// Clear deletes the rows outcomes come from, so record them before
	// returning the cursor.
	return cursor, m.reconcileOutcomes(ctx, lib)
}

func (m *Module) reconcileOutcomes(ctx context.Context, lib libraryport.Port) error {
	var e error
	as := []acceptanceRow{}
	if e = m.db.R.SelectContext(ctx, &as, `SELECT `+acceptanceCols+` FROM agents_acceptances WHERE final=0 ORDER BY id`); e != nil {
		return e
	}
	// Keep ended changes in the comparison set: an undo can occur much later.
	for start := 0; start < len(as); start += 100 {
		batch := as[start:min(start+100, len(as))]
		ids := []int64{}
		for _, a := range batch {
			ids = append(ids, a.ChangeID)
		}
		status, e := lib.Changes(ctx, ids)
		if e != nil {
			return e
		}
		for _, a := range batch {
			s, exists := status[a.ChangeID]
			o := a.outcome()
			if exists {
				o = outcomeOf(s)
			} else {
				o.Expired = true
			}
			encoded := mustJSON(o)
			if encoded == mustJSON(a.outcome()) {
				continue
			}
			e = m.db.Write(ctx, func(tx *sqlx.Tx) error {
				res, e := tx.ExecContext(ctx, `UPDATE agents_acceptances SET outcome=?,final=? WHERE id=? AND outcome=?`, encoded, !exists, a.ID, a.Outcome)
				if e != nil {
					return e
				}
				n, e := res.RowsAffected()
				if e != nil || n == 0 {
					return e
				}
				ev := event{Kind: "outcome", Proposal: a.ProposalID, Acceptance: a.ID, Revision: a.Revision, ActorKind: "system", ActorID: "system", Actor: "Shelf", Data: map[string]any{"acceptance_id": a.ID, "change_id": a.ChangeID, "state": o.State, "reason": o.Reason, "undone": o.Undone, "written": o.Written, "files": o.Files, "expired": o.Expired}}
				_, e = m.insertEvent(ctx, tx, ev)
				return e
			})
			if e != nil {
				return e
			}
			m.publish(a.ProposalID)
		}
	}
	return nil
}
func (m *Module) retain(ctx context.Context) error {
	if !m.enabled {
		return nil
	}
	lib, e := m.lib()
	if e != nil {
		return e
	}
	cut := m.Now().Add(-365 * 24 * time.Hour)
	return m.db.Write(ctx, func(tx *sqlx.Tx) error {
		if _, e := tx.ExecContext(ctx, `DELETE FROM agents_events WHERE proposal_id IS NULL AND at<? AND id<=(SELECT events_cursor FROM agents_learning WHERE id=1)`, cut.UTC().Format("2006-01-02T15:04:05.000Z")); e != nil {
			return e
		}
		ids := []int64{}
		if e := tx.SelectContext(ctx, &ids, `SELECT p.id FROM agents_proposals p WHERE (p.pending=0 OR p.withdrawn_at IS NOT NULL) AND p.updated_at<? AND NOT EXISTS(SELECT 1 FROM agents_acceptances a WHERE a.proposal_id=p.id AND a.final=0) AND NOT EXISTS(SELECT 1 FROM agents_events e WHERE e.proposal_id=p.id AND e.id>(SELECT events_cursor FROM agents_learning WHERE id=1)) AND NOT EXISTS(SELECT 1 FROM agents_proposals f WHERE f.follow_up_of=p.id)`, cut.UTC().Format("2006-01-02T15:04:05.000Z")); e != nil {
			return e
		}
		for _, id := range ids {
			if e := lib.Unpin(ctx, tx, holder(id)); e != nil {
				return e
			}
			if _, e := tx.ExecContext(ctx, `DELETE FROM agents_proposals WHERE id=?`, id); e != nil {
				return e
			}
		}
		if _, e := tx.ExecContext(ctx, `DELETE FROM agents_runs WHERE started_at<? AND NOT EXISTS(SELECT 1 FROM agents_proposals p WHERE p.run_id=agents_runs.id) AND NOT EXISTS(SELECT 1 FROM agents_revisions r WHERE r.run_id=agents_runs.id)`, cut.UTC().Format("2006-01-02T15:04:05.000Z")); e != nil {
			return e
		}
		_, e := tx.ExecContext(ctx, `DELETE FROM agents_usage WHERE day<?`, day(m.Now().Add(-30*24*time.Hour)))
		return e
	})
}
