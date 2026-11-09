package agents

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/jmoiron/sqlx"
	"github.com/labstack/echo/v5"
	"github.com/tikhonp/shelf/internal/libraryport"
)

type selectionBody struct {
	Revision int      `json:"revision"`
	Ops      []string `json:"ops"`
}
type acceptBody struct {
	selectionBody
	Digest    string   `json:"digest"`
	Confirm   []string `json:"confirm"`
	RequestID string   `json:"request_id"`
}

func selectedOps(r revisionRow, rows map[string]opRow, ids []string, retry, satisfied map[int64]bool) ([]Op, error) {
	if len(ids) == 0 || len(ids) > maxOps {
		return nil, invalid("select 1 to %d operations", maxOps)
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			return nil, invalid("operation selected twice")
		}
		seen[id] = true
		o, ok := rows[id]
		if !ok {
			return nil, invalid("unknown operation %q", id)
		}
		if o.Decision != "pending" && (o.Decision != "accepted" || !retry[o.AcceptanceID.Int64]) {
			return nil, conflict("decided_op", "operation already decided", "op", id)
		}
	}
	ops := pick(r.ops(), ids)
	if len(ops) != len(ids) {
		return nil, invalid("selection is not in this revision")
	}
	for _, o := range ops {
		for _, dep := range closure(r.ops(), o.Requires) {
			if !seen[dep] && (rows[dep].Decision != "accepted" || !satisfied[rows[dep].AcceptanceID.Int64]) {
				return nil, fail(422, "refused", "operation "+o.ID+" requires "+dep)
			}
		}
	}
	return ops, nil
}
func (m *Module) selection(ctx context.Context, id int64, b selectionBody, allowRetry bool) (proposalRow, revisionRow, map[string]opRow, []Op, error) {
	p, e := getProposal(ctx, m.db.R, id)
	if e != nil {
		return p, revisionRow{}, nil, nil, e
	}
	if p.Head != b.Revision {
		return p, revisionRow{}, nil, nil, staleRevision(p.Head)
	}
	if p.withdrawn() {
		return p, revisionRow{}, nil, nil, conflict("decided_op", "proposal withdrawn")
	}
	r, e := getRevision(ctx, m.db.R, id, p.Head)
	if e != nil {
		return p, r, nil, nil, e
	}
	rows, e := getOps(ctx, m.db.R, id)
	if e != nil {
		return p, r, nil, nil, e
	}
	retry := map[int64]bool{}
	satisfied := map[int64]bool{}
	{
		as, e := getAcceptances(ctx, m.db.R, id)
		if e != nil {
			return p, r, rows, nil, e
		}
		lib, e := m.lib()
		if e != nil {
			return p, r, rows, nil, e
		}
		ids := []int64{}
		for _, a := range as {
			ids = append(ids, a.ChangeID)
		}
		status, e := lib.Changes(ctx, ids)
		if e != nil {
			return p, r, rows, nil, e
		}
		for _, a := range as {
			// A change trimmed from the history can no longer be undone, so its
			// last recorded outcome stands; without it a done dependency would
			// block its dependents for good.
			o := a.outcome()
			if s, ok := status[a.ChangeID]; ok {
				o = outcomeOf(s)
				retry[a.ID] = allowRetry && o.retryable()
			}
			satisfied[a.ID] = !o.Undone && slices.Contains([]string{"queued", "writing", "done"}, o.State)
		}
	}
	ops, e := selectedOps(r, rows, b.Ops, retry, satisfied)
	if e != nil {
		return p, r, rows, ops, e
	}
	lib, e := m.lib()
	if e != nil {
		return p, r, rows, ops, e
	}
	if e = validateTargets(ctx, lib, ops, r.base()); e != nil {
		return p, r, rows, ops, e
	}
	return p, r, rows, ops, nil
}
func (m *Module) previewSelection(c *echo.Context) error {
	id, e := idParam(c, "id")
	if e != nil {
		return e
	}
	var b selectionBody
	if e = decode(c, 1<<20, &b); e != nil {
		return e
	}
	ctx := c.Request().Context()
	_, r, _, ops, e := m.selection(ctx, id, b, true)
	if e != nil {
		return e
	}
	lib, e := m.lib()
	if e != nil {
		return e
	}
	p, e := lib.Plan(ctx, requestWithBase(ops, nil, r.base()))
	if e != nil {
		return e
	}
	return c.JSON(200, map[string]any{"preview": p.Preview, "refusal": p.Refusal, "missing": p.Missing})
}
func sameAcceptance(a acceptanceRow, id int64, b acceptBody) bool {
	ops := slices.Clone(b.Ops)
	confirm := slices.Clone(b.Confirm)
	slices.Sort(ops)
	slices.Sort(confirm)
	old := a.ops()
	cs := a.confirm()
	slices.Sort(old)
	slices.Sort(cs)
	return a.ProposalID == id && a.Revision == b.Revision && a.Digest == b.Digest && slices.Equal(ops, old) && slices.Equal(confirm, cs)
}
func (m *Module) replay(ctx context.Context, key string, id int64, b acceptBody) (*acceptanceRow, error) {
	var a acceptanceRow
	e := m.db.R.GetContext(ctx, &a, `SELECT `+acceptanceCols+` FROM agents_acceptances WHERE idem_key=?`, key)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	if !sameAcceptance(a, id, b) {
		return nil, conflict("invalid", "request_id was already used for a different acceptance")
	}
	return &a, nil
}
func (m *Module) postAccept(c *echo.Context) error {
	id, e := idParam(c, "id")
	if e != nil {
		return e
	}
	var b acceptBody
	if e = decode(c, 1<<20, &b); e != nil {
		return e
	}
	if !opIDPattern.MatchString(b.RequestID) || len(b.Digest) != 64 {
		return invalid("request_id must be a new lowercase identifier up to 48 characters, and digest must be the reviewed preview hash")
	}
	ctx := c.Request().Context()
	key := "proposal:" + itoa(id) + ":" + b.RequestID
	if a, e := m.replay(ctx, key, id, b); e != nil {
		return e
	} else if a != nil {
		return c.JSON(200, map[string]any{"acceptance": acceptanceJSON(*a), "change": map[string]any{"id": a.ChangeID}})
	}
	prop, r, rows, ops, e := m.selection(ctx, id, b.selectionBody, true)
	if e != nil {
		return e
	}
	lib, e := m.lib()
	if e != nil {
		return e
	}
	plan, e := lib.Plan(ctx, requestWithBase(ops, b.Confirm, r.base()))
	if e != nil {
		return e
	}
	if plan.Refusal != nil {
		return refused(plan.Refusal)
	}
	if plan.Preview.Digest != b.Digest {
		return conflict("preview_changed", "the selected effects changed; review again", "preview", plan.Preview)
	}
	if len(plan.Missing) > 0 {
		return conflict("confirm", "confirm each warning before accepting", "confirm", plan.Preview.Warnings)
	}
	a := userActor(ctx)
	s := snap(prop, rows)
	var accepted acceptanceRow
	var after func()
	replayed := false
	e = m.db.Write(ctx, func(tx *sqlx.Tx) error {
		err := tx.GetContext(ctx, &accepted, `SELECT `+acceptanceCols+` FROM agents_acceptances WHERE idem_key=?`, key)
		if err == nil {
			if !sameAcceptance(accepted, id, b) {
				return conflict("invalid", "request_id reused for another payload")
			}
			replayed = true
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		cur, err := recheck(ctx, tx, id, s)
		if err != nil {
			return err
		}
		if cur.withdrawn() {
			return conflict("decided_op", "proposal withdrawn")
		}
		ids := slices.Clone(b.Ops)
		slices.Sort(ids)
		confirm := slices.Clone(b.Confirm)
		slices.Sort(confirm)
		now := m.now()
		res, err := tx.ExecContext(ctx, `INSERT INTO agents_acceptances(proposal_id,revision,op_ids,idem_key,digest,preview,confirm,change_id,author_id,author,created_at) VALUES(?,?,?,?,?,?,?,0,?,?,?)`, id, b.Revision, mustJSON(ids), key, b.Digest, mustJSON(plan.Preview), mustJSON(confirm), a.ID, a.Name, now)
		if err != nil {
			return err
		}
		aid, err := res.LastInsertId()
		if err != nil {
			return err
		}
		cid, kick, err := lib.Enqueue(ctx, tx, plan, "proposal:"+itoa(id)+"/acceptance:"+itoa(aid))
		if err != nil {
			return err
		}
		after = kick
		if _, err = tx.ExecContext(ctx, `UPDATE agents_acceptances SET change_id=? WHERE id=?`, cid, aid); err != nil {
			return err
		}
		for _, op := range ids {
			if _, err = tx.ExecContext(ctx, `UPDATE agents_ops SET decision='accepted',decided_rev=?,acceptance_id=?,decided_at=?,decided_by_id=?,decided_by=? WHERE proposal_id=? AND op_id=?`, b.Revision, aid, now, a.ID, a.Name, id, op); err != nil {
				return err
			}
		}
		if err = setPending(ctx, tx, id, now); err != nil {
			return err
		}
		ev := userEvent(a, "accepted")
		ev.Proposal = id
		ev.Revision = b.Revision
		ev.Acceptance = aid
		ev.Data = map[string]any{"acceptance_id": aid, "change_id": cid, "ops": ids, "revision": b.Revision, "values": ops}
		if _, err = m.insertEvent(ctx, tx, ev); err != nil {
			return err
		}
		if err = m.pin(ctx, tx, lib, id, previewImages(plan.Preview)); err != nil {
			return err
		}
		return tx.GetContext(ctx, &accepted, `SELECT `+acceptanceCols+` FROM agents_acceptances WHERE id=?`, aid)
	})
	if e != nil {
		return e
	}
	if after != nil && !replayed {
		after()
	}
	m.publish(id)
	status := http.StatusAccepted
	if replayed {
		status = 200
	}
	return c.JSON(status, map[string]any{"acceptance": acceptanceJSON(accepted), "change": map[string]any{"id": accepted.ChangeID, "state": "queued", "origin": "proposal:" + itoa(id) + "/acceptance:" + itoa(accepted.ID)}})
}
func (m *Module) postDecision(c *echo.Context, kind string) error {
	id, e := idParam(c, "id")
	if e != nil {
		return e
	}
	var b struct {
		Revision  int      `json:"revision"`
		Ops       []string `json:"ops"`
		Remaining bool     `json:"remaining"`
		Reason    string   `json:"reason"`
		Comment   string   `json:"comment"`
		Body      string   `json:"body"`
	}
	if e = decode(c, 1<<20, &b); e != nil {
		return e
	}
	if utf8.RuneCountInString(b.Reason) > maxBody {
		return invalid("reason too long")
	}
	text := b.Reason
	if kind == "changes_requested" {
		text = b.Comment
		if e = checkComment(strings.TrimSpace(text)); e != nil {
			return e
		}
	}
	if kind == "comment" {
		text = b.Body
		if e = checkComment(strings.TrimSpace(text)); e != nil {
			return e
		}
	}
	ctx := c.Request().Context()
	a := userActor(ctx)
	var eid int64
	e = m.db.Write(ctx, func(tx *sqlx.Tx) error {
		p, e := getProposal(ctx, tx, id)
		if e != nil {
			return e
		}
		if kind != "comment" && p.Head != b.Revision {
			return staleRevision(p.Head)
		}
		if kind != "comment" && p.withdrawn() {
			return conflict("decided_op", "proposal withdrawn")
		}
		if kind == "declined" {
			rows, e := getOps(ctx, tx, id)
			if e != nil {
				return e
			}
			if b.Remaining {
				b.Ops = []string{}
				for id, o := range rows {
					if o.Decision == "pending" {
						b.Ops = append(b.Ops, id)
					}
				}
			}
			if len(b.Ops) == 0 {
				return invalid("select pending operations")
			}
			for _, id := range b.Ops {
				if rows[id].Decision != "pending" {
					return conflict("decided_op", "operation already decided")
				}
			}
			for _, op := range b.Ops {
				if _, e = tx.ExecContext(ctx, `UPDATE agents_ops SET decision='declined',decided_rev=?,decided_at=?,decided_by_id=?,decided_by=? WHERE proposal_id=? AND op_id=?`, p.Head, m.now(), a.ID, a.Name, id, op); e != nil {
					return e
				}
			}
		}
		if kind == "changes_requested" {
			if p.Pending == 0 {
				return conflict("decided_op", "no pending operations")
			}
			if _, e = tx.ExecContext(ctx, `UPDATE agents_proposals SET changes_requested_at=? WHERE id=?`, m.now(), id); e != nil {
				return e
			}
		}
		if e = setPending(ctx, tx, id, m.now()); e != nil {
			return e
		}
		ev := userEvent(a, kind)
		ev.Proposal = id
		ev.Revision = p.Head
		ev.Body = text
		ev.Data = map[string]any{"ops": b.Ops, "revision": p.Head}
		eid, e = m.insertEvent(ctx, tx, ev)
		return e
	})
	if e != nil {
		return e
	}
	m.publish(id)
	return m.answerEvent(c, eid)
}

// File IDs alone are not enough after an index rebuild. The recorded paths
// bind the reviewed operations to the files the agent originally read.
func validateTargets(ctx context.Context, lib libraryport.Port, ops []Op, base []BaseTrack) error {
	expected := map[int64]string{}
	for _, b := range base {
		expected[b.TrackID] = b.Path
	}
	needed := map[int64]bool{}
	for _, o := range ops {
		if o.Type == "cover" {
			for id := range expected {
				needed[id] = true
			}
		}
		for _, t := range o.Tracks {
			needed[t.TrackID] = true
		}
	}
	paths := []string{}
	for id := range needed {
		p := expected[id]
		if p == "" {
			return conflict("stale_base", "a target has no recorded path")
		}
		paths = append(paths, p)
	}
	of, e := lib.AlbumsOf(ctx, paths)
	if e != nil {
		return e
	}
	var aid int64
	for _, p := range paths {
		if of[p] == 0 || aid != 0 && aid != of[p] {
			return conflict("stale_base", "selected files are no longer on one album")
		}
		aid = of[p]
	}
	if aid == 0 {
		return conflict("stale_base", "the target album is gone")
	}
	a, e := lib.Album(ctx, aid)
	if e != nil {
		return e
	}
	actual := map[int64]string{}
	for _, t := range a.Tracks {
		actual[t.ID] = t.Path
	}
	for id := range needed {
		if actual[id] != expected[id] {
			return conflict("stale_base", "a target file left the index; revise the proposal")
		}
	}
	return nil
}
