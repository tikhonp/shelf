package agents

import "context"

// outstandingLimit bounds each list of a run's start, so a backlog can't
// make POST /runs slow. The agent pages the rest through /events.
const outstandingLimit = 100

type changeRequest struct {
	Proposal int64  `db:"id" json:"proposal"`
	URL      string `db:"-" json:"url"`
	Album    int64  `db:"album_id" json:"album"`
	Event    int64  `db:"event" json:"comment_event"`
}

type unanswered struct {
	Proposal int64 `db:"proposal_id" json:"proposal"`
	Event    int64 `db:"event" json:"event"`
}

type outcomeNote struct {
	Proposal   int64  `db:"proposal_id" json:"proposal"`
	Acceptance int64  `db:"acceptance_id" json:"acceptance"`
	State      string `db:"state" json:"state"`
	Undone     bool   `db:"undone" json:"undone"`
	Event      int64  `db:"id" json:"event"`
}

// outstanding is the work a run of one connection starts with: its change
// requests, user comments it hasn't answered, and outcomes above the
// learning cursor. Other connections' proposals stay out, because the
// prompt has an agent answer only its own.
func (m *Module) outstanding(ctx context.Context, conn, cursor int64, review string) (map[string]any, error) {
	ids := []int64{}
	if e := m.db.R.SelectContext(ctx, &ids, `SELECT id FROM agents_proposals WHERE connection_id=? AND pending>0 AND withdrawn_at IS NULL ORDER BY updated_at DESC LIMIT ?`, conn, outstandingLimit); e != nil {
		return nil, e
	}
	var pending int
	if e := m.db.R.GetContext(ctx, &pending, `SELECT count(*) FROM agents_proposals WHERE connection_id=? AND pending>0 AND withdrawn_at IS NULL`, conn); e != nil {
		return nil, e
	}
	changes := []changeRequest{}
	if e := m.db.R.SelectContext(ctx, &changes, `SELECT p.id, p.album_id,
		coalesce((SELECT max(e.id) FROM agents_events e WHERE e.proposal_id=p.id AND e.kind='changes_requested'), 0) AS event
		FROM agents_proposals p WHERE p.connection_id=? AND p.changes_requested_at IS NOT NULL AND p.pending>0 AND p.withdrawn_at IS NULL
		ORDER BY p.id LIMIT ?`, conn, outstandingLimit); e != nil {
		return nil, e
	}
	for i := range changes {
		changes[i].URL = review + itoa(changes[i].Proposal)
	}
	comments := []unanswered{}
	if e := m.db.R.SelectContext(ctx, &comments, `SELECT e.proposal_id, max(e.id) AS event FROM agents_events e
		JOIN agents_proposals p ON p.id=e.proposal_id
		WHERE p.connection_id=? AND p.withdrawn_at IS NULL AND e.kind='comment' AND e.actor_kind='user'
		AND NOT EXISTS (SELECT 1 FROM agents_events a WHERE a.proposal_id=e.proposal_id AND a.actor_kind='agent' AND a.id>e.id)
		GROUP BY e.proposal_id ORDER BY e.proposal_id LIMIT ?`, conn, outstandingLimit); e != nil {
		return nil, e
	}
	outcomes := []outcomeNote{}
	if e := m.db.R.SelectContext(ctx, &outcomes, `SELECT e.id, e.proposal_id, coalesce(e.acceptance_id, 0) AS acceptance_id,
		coalesce(json_extract(e.data, '$.state'), '') AS state, coalesce(json_extract(e.data, '$.undone'), 0) AS undone
		FROM agents_events e JOIN agents_proposals p ON p.id=e.proposal_id
		WHERE p.connection_id=? AND e.kind='outcome' AND e.id>? ORDER BY e.id LIMIT ?`, conn, cursor, outstandingLimit); e != nil {
		return nil, e
	}
	return map[string]any{"proposals": ids, "pending": pending, "changes_requested": changes, "unanswered_comments": comments, "outcomes": outcomes}, nil
}
