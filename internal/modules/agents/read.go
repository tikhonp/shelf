package agents

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"sort"
	"strings"

	"github.com/labstack/echo/v5"
	"github.com/tikhonp/shelf/internal/libraryport"
	"github.com/tikhonp/shelf/internal/platform/db"
)

type eventRow struct {
	ID         int64   `db:"id"`
	At         db.Time `db:"at"`
	Kind       string  `db:"kind"`
	Proposal   *int64  `db:"proposal_id"`
	Revision   *int    `db:"revision"`
	Acceptance *int64  `db:"acceptance_id"`
	Memory     *int64  `db:"memory_id"`
	ActorKind  string  `db:"actor_kind"`
	ActorID    string  `db:"actor_id"`
	Actor      string  `db:"actor"`
	Connection *int64  `db:"connection_id"`
	Run        *int64  `db:"run_id"`
	Body       string  `db:"body"`
	Data       string  `db:"data"`
}

const eventCols = `id, at, kind, proposal_id, revision, acceptance_id, memory_id, actor_kind, actor_id, actor, connection_id, run_id, body, data`

func (e eventRow) json() map[string]any {
	return map[string]any{"id": e.ID, "at": stamp(e.At), "kind": e.Kind, "proposal_id": e.Proposal, "revision": e.Revision, "acceptance_id": e.Acceptance, "memory_id": e.Memory, "actor_kind": e.ActorKind, "actor": e.Actor, "body": e.Body, "data": json.RawMessage(e.Data)}
}
func (m *Module) serverOrigin(c *echo.Context) string {
	if m.public != nil {
		return strings.TrimSuffix(m.public.String(), "/")
	}
	scheme := "http"
	if c.Request().TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + c.Request().Host
}
func (m *Module) reviewBase(c *echo.Context) string { return m.serverOrigin(c) + "/inbox/" }
func previewImages(p libraryport.Preview) []string {
	set := map[string]bool{}
	for _, f := range p.Files {
		set[f.CoverBefore] = true
		set[f.CoverAfter] = true
	}
	for _, f := range p.Folder {
		set[f.Before] = true
		set[f.After] = true
	}
	delete(set, "")
	ids := []string{}
	for id := range set {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
func acceptanceJSON(a acceptanceRow) map[string]any {
	return map[string]any{"id": a.ID, "revision": a.Revision, "ops": a.ops(), "change_id": a.ChangeID, "author": a.Author, "created_at": stamp(a.CreatedAt), "outcome": a.outcome()}
}
func (m *Module) detail(ctx context.Context, id int64, base string, live bool) (map[string]any, error) {
	p, err := getProposal(ctx, m.db.R, id)
	if err != nil {
		return nil, err
	}
	r, err := getRevision(ctx, m.db.R, id, p.Head)
	if err != nil {
		return nil, err
	}
	conn, err := m.connection(ctx, p.ConnectionID)
	if err != nil {
		return nil, err
	}
	rows, err := getOps(ctx, m.db.R, id)
	if err != nil {
		return nil, err
	}
	ops := []map[string]any{}
	counts := map[string]int{"pending": 0, "accepted": 0, "declined": 0, "superseded": 0}
	pending := []string{}
	keys := []string{}
	for id := range rows {
		keys = append(keys, id)
	}
	sort.Strings(keys)
	for _, id := range keys {
		o := rows[id]
		counts[o.Decision]++
		if o.Decision == "pending" {
			pending = append(pending, id)
		}
		ops = append(ops, map[string]any{"id": o.OpID, "type": o.Type, "decision": o.Decision, "decided_rev": nullableInt(o.DecidedRev.Int64, o.DecidedRev.Valid), "acceptance": nullableInt(o.AcceptanceID.Int64, o.AcceptanceID.Valid), "decided_by": o.DecidedBy.String, "decided_at": optStamp(o.DecidedAt)})
	}
	revisions := []struct {
		Revision   int    `db:"revision" json:"revision"`
		AuthorKind string `db:"author_kind" json:"author_kind"`
		Author     string `db:"author" json:"author"`
		CreatedAt  string `db:"created_at" json:"created_at"`
	}{}
	if err = m.db.R.SelectContext(ctx, &revisions, `SELECT revision, author_kind, author, created_at FROM agents_revisions WHERE proposal_id=? ORDER BY revision`, id); err != nil {
		return nil, err
	}
	as, err := getAcceptances(ctx, m.db.R, id)
	if err != nil {
		return nil, err
	}
	acceptances := []map[string]any{}
	for _, a := range as {
		acceptances = append(acceptances, acceptanceJSON(a))
	}
	events := []eventRow{}
	if err = m.db.R.SelectContext(ctx, &events, `SELECT `+eventCols+` FROM agents_events WHERE proposal_id=? ORDER BY id`, id); err != nil {
		return nil, err
	}
	es := []map[string]any{}
	for _, e := range events {
		es = append(es, e.json())
	}
	var currentID int64
	lib := m.library()
	if lib != nil {
		currentID, err = m.currentAlbumID(ctx, lib, id)
		if err != nil {
			return nil, err
		}
	}
	var conf any
	if r.Confidence.Valid {
		conf = r.Confidence.Int64
	}
	out := map[string]any{"id": p.ID, "url": base + itoa(id), "state": p.state(), "changes_requested": !p.ChangesRequestedAt.IsZero(), "connection": map[string]any{"id": conn.ID, "name": conn.Name}, "run_id": p.RunID, "album": map[string]any{"id": p.AlbumID, "current_id": currentID, "folder": p.Folder, "title": p.Title, "artist": p.Artist}, "group": p.GroupKey, "follow_up_of": nullableInt(p.FollowUpOf.Int64, p.FollowUpOf.Valid), "head": p.Head, "revisions": revisions, "current": map[string]any{"title": r.Title, "reason": r.Reason, "uncertainties": r.Uncertainties, "confidence": conf, "evidence": r.evidence(), "base": r.base(), "ops": r.ops(), "preview": r.preview()}, "ops": ops, "summary": counts, "acceptances": acceptances, "events": es, "updated_at": stamp(p.UpdatedAt)}
	if live {
		stale := map[string]bool{}
		var pv any
		var refusal any
		if lib == nil {
			refusal = &libraryport.Refusal{Reason: "unavailable", Error: "the library is not available"}
		} else if currentID == 0 {
			refusal = &libraryport.Refusal{Reason: "regrouped", Error: "the proposal's files are no longer on one album"}
		} else if len(pending) > 0 {
			plan, e := lib.Plan(ctx, requestWithBase(pick(r.ops(), pending), nil, r.base()))
			if e != nil {
				return nil, e
			}
			if plan.Refusal != nil {
				refusal = plan.Refusal
			} else {
				pv = plan.Preview
			}
			for _, id := range pending {
				digest, e := opDigest(ctx, lib, r.ops(), pending, id, r.base())
				if e != nil {
					return nil, e
				}
				stale[id] = digest == "" || digest != r.digests()[id]
			}
		}
		// before is each pending tag field's values in the files now, for the
		// Inbox's "Was ..." line. A failed read only leaves the line out.
		before := map[string]map[int64][]string{}
		if lib != nil && currentID != 0 && len(pending) > 0 {
			if al, e := lib.Album(ctx, currentID); e == nil {
				before = beforeValues(al, pick(r.ops(), pending))
			} else if ctx.Err() == nil {
				m.log.Warn("agents: read the album for the values now", "album", currentID, "error", e)
			}
		}
		out["live"] = map[string]any{"preview": pv, "refusal": refusal, "op_stale": stale, "before": before}
	}
	return out, nil
}
func nullableInt(n int64, valid bool) any {
	if valid {
		return n
	}
	return nil
}
func (m *Module) getDetail(c *echo.Context) error {
	id, e := idParam(c, "id")
	if e != nil {
		return e
	}
	d, e := m.detail(c.Request().Context(), id, m.reviewBase(c), current(c).ID == 0)
	if e != nil {
		return e
	}
	return c.JSON(200, d)
}
func (m *Module) listProposals(c *echo.Context) error {
	ctx := c.Request().Context()
	limit, e := intQuery(c, "limit", 50, 1, 50)
	if e != nil {
		return e
	}
	after, e := intQuery(c, "after", 0, 0, 1<<62)
	if e != nil {
		return e
	}
	before, e := intQuery(c, "before", 0, 0, 1<<62)
	if e != nil {
		return e
	}
	state := c.QueryParam("state")
	q := `SELECT id FROM agents_proposals WHERE 1=1`
	args := []any{}
	switch state {
	case "", "pending":
		q += ` AND pending>0 AND withdrawn_at IS NULL`
	case "reviewed":
		q += ` AND pending=0 AND withdrawn_at IS NULL`
	case "withdrawn":
		q += ` AND withdrawn_at IS NOT NULL`
	case "all":
	default:
		return invalid("unknown proposal state")
	}
	for _, filter := range []struct{ name, col string }{{"connection", "connection_id"}, {"album", "album_id"}} {
		n, e := intQuery(c, filter.name, 0, 0, 1<<62)
		if e != nil {
			return e
		}
		if n > 0 {
			q += ` AND ` + filter.col + `=?`
			args = append(args, n)
		}
	}
	kind := c.QueryParam("kind")
	if kind != "" {
		if kind != "tag" && kind != "cover" {
			return invalid("kind is tag or cover")
		}
		q += ` AND EXISTS(SELECT 1 FROM agents_ops o WHERE o.proposal_id=agents_proposals.id AND type=?)`
		args = append(args, kind)
	}
	machine := current(c).ID != 0
	if machine {
		if after > 0 {
			q += ` AND id>?`
			args = append(args, after)
		}
		q += ` ORDER BY id`
	} else {
		if before > 0 {
			q += ` AND id<?`
			args = append(args, before)
		}
		q += ` ORDER BY id DESC`
	}
	q += ` LIMIT ?`
	args = append(args, limit+1)
	ids := []int64{}
	if e = m.db.R.SelectContext(ctx, &ids, q, args...); e != nil {
		return e
	}
	var next any
	if int64(len(ids)) > limit {
		ids = ids[:limit]
		next = ids[len(ids)-1]
	}
	ps := []map[string]any{}
	for _, id := range ids {
		d, e := m.detail(ctx, id, m.reviewBase(c), false)
		if e != nil {
			return e
		}
		cur := d["current"].(map[string]any)
		kinds := []string{}
		// fields names what each op changes without its values, so a list row can say "Genre, Mood" and stay small.
		fields := []Op{}
		for _, op := range cur["ops"].([]Op) {
			found := false
			for _, k := range kinds {
				found = found || k == op.Type
			}
			if !found {
				kinds = append(kinds, op.Type)
			}
			fields = append(fields, Op{ID: op.ID, Type: op.Type, Field: op.Field, TagKey: op.TagKey, Action: op.Action})
		}
		ps = append(ps, map[string]any{"id": id, "state": d["state"], "changes_requested": d["changes_requested"], "title": cur["title"], "album": d["album"], "connection": d["connection"], "kinds": kinds, "fields": fields, "confidence": cur["confidence"], "summary": d["summary"], "updated_at": d["updated_at"], "stale": false})
	}
	return c.JSON(200, map[string]any{"proposals": ps, "next": next})
}
func (m *Module) getEvents(c *echo.Context) error {
	ctx := c.Request().Context()
	after, e := intQuery(c, "after", 0, 0, 1<<62)
	if e != nil {
		return e
	}
	limit, e := intQuery(c, "limit", 100, 1, 100)
	if e != nil {
		return e
	}
	proposal, e := intQuery(c, "proposal", 0, 0, 1<<62)
	if e != nil {
		return e
	}
	q := `SELECT ` + eventCols + ` FROM agents_events WHERE id>?`
	args := []any{after}
	if proposal > 0 {
		q += ` AND proposal_id=?`
		args = append(args, proposal)
	}
	args = append(args, limit)
	rs := []eventRow{}
	if e = m.db.R.SelectContext(ctx, &rs, q+` ORDER BY id LIMIT ?`, args...); e != nil {
		return e
	}
	next := after
	events := []map[string]any{}
	for _, r := range rs {
		events = append(events, r.json())
		next = r.ID
	}
	var highest int64
	if e = m.db.R.GetContext(ctx, &highest, `SELECT coalesce(max(id),0) FROM agents_events`); e != nil {
		return e
	}
	return c.JSON(200, map[string]any{"events": events, "next": next, "highest": highest})
}
func (m *Module) libraryError(err error) error {
	if errors.Is(err, libraryport.ErrNotFound) {
		return notFound("library item")
	}
	if errors.Is(err, libraryport.ErrRefused) {
		return fail(http.StatusUnprocessableEntity, "refused", "the library refused this request")
	}
	return err
}

// beforeValues gives each tag op the values its tracks hold now. It reads the
// raw key of a tag_key op, or the key that feeds the op's field, as the
// editor does.
func beforeValues(al libraryport.Album, ops []Op) map[string]map[int64][]string {
	out := map[string]map[int64][]string{}
	tracks := map[int64]libraryport.Track{}
	for _, t := range al.Tracks {
		tracks[t.ID] = t
	}
	for _, o := range ops {
		if o.Type != "tag" {
			continue
		}
		vals := map[int64][]string{}
		for _, ot := range o.Tracks {
			t, ok := tracks[ot.TrackID]
			if !ok || t.Error != "" {
				continue
			}
			vals[ot.TrackID] = tagValues(t, o)
		}
		out[o.ID] = vals
	}
	return out
}

func tagValues(t libraryport.Track, o Op) []string {
	for _, tag := range t.Tags {
		if (o.TagKey != "" && strings.EqualFold(tag.Key, o.TagKey)) || (o.Field != "" && !tag.Extended && slices.Contains(tag.Fields, o.Field)) {
			if tag.Values == nil {
				return []string{}
			}
			return tag.Values
		}
	}
	return []string{}
}
