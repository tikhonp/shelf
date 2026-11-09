package agents

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strings"

	"github.com/jmoiron/sqlx"
	"github.com/labstack/echo/v5"
	"github.com/tikhonp/shelf/internal/platform/db"
)

type memoryRow struct {
	ID         int64  `db:"id" json:"id"`
	Key        string `db:"key" json:"key"`
	Layer      string `db:"layer" json:"layer"`
	Status     string `db:"status" json:"status"`
	Title      string `db:"title" json:"title"`
	Scope      string `db:"scope" json:"scope"`
	Body       string `db:"body" json:"body"`
	Confidence *int   `db:"confidence" json:"confidence"`
	Evidence   string `db:"evidence" json:"-"`
	Version    int64  `db:"version" json:"version"`
	AuthorKind string `db:"author_kind" json:"author_kind"`
	AuthorID   string `db:"author_id" json:"-"`
	Author     string `db:"author" json:"author"`
	Connection *int64 `db:"connection_id" json:"-"`
	Run        *int64 `db:"run_id" json:"-"`
	Corrected  *int64 `db:"corrected_event" json:"corrected_event"`
	Created    string `db:"created_at" json:"created_at"`
	Updated    string `db:"updated_at" json:"updated_at"`
}

func (r memoryRow) json() map[string]any {
	b, _ := json.Marshal(r)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	evidence := r.Evidence
	if evidence == "" {
		evidence = "[]"
	}
	m["evidence"] = json.RawMessage(evidence)
	return m
}

type learningRow struct {
	ID         int    `db:"id" json:"-"`
	Version    int64  `db:"version" json:"version"`
	Events     int64  `db:"events_cursor" json:"events_cursor"`
	History    int64  `db:"history_cursor" json:"history_cursor"`
	Checkpoint string `db:"checkpoint" json:"-"`
	Updated    string `db:"updated_at" json:"updated_at"`
	Connection *int64 `db:"connection_id" json:"connection_id"`
	Run        *int64 `db:"run_id" json:"run_id"`
}

func (r learningRow) json() map[string]any {
	return map[string]any{"version": r.Version, "events_cursor": r.Events, "history_cursor": r.History, "checkpoint": json.RawMessage(r.Checkpoint), "updated_at": r.Updated}
}
func learning(ctx context.Context, q querier) (learningRow, error) {
	var r learningRow
	e := q.GetContext(ctx, &r, `SELECT * FROM agents_learning WHERE id=1`)
	return r, e
}

type memEvidence struct {
	Event    int64  `json:"event,omitempty"`
	Change   int64  `json:"change,omitempty"`
	Proposal int64  `json:"proposal,omitempty"`
	Note     string `json:"note,omitempty"`
}
type memoryWrite struct {
	Op         string        `json:"op"`
	Key        string        `json:"key"`
	Expected   int64         `json:"expected_version"`
	Layer      string        `json:"layer"`
	Title      string        `json:"title"`
	Scope      string        `json:"scope"`
	Body       string        `json:"body"`
	Confidence *int          `json:"confidence"`
	Evidence   []memEvidence `json:"evidence"`
	Reconsider *struct {
		Reason string  `json:"reason"`
		Events []int64 `json:"evidence_events"`
	} `json:"reconsider"`
}
type learningWrite struct {
	Expected   int64           `json:"expected_version"`
	Events     int64           `json:"events_cursor"`
	History    int64           `json:"history_cursor"`
	Checkpoint json.RawMessage `json:"checkpoint"`
}

var memoryKey = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,119}$`)

// Versions survive physical deletion so stale writers cannot overwrite recreated keys.
func nextMemoryVersion(ctx context.Context, tx *sqlx.Tx) (int64, error) {
	var version int64
	err := tx.GetContext(ctx, &version, `UPDATE agents_state SET value=value+1 WHERE key='memory_version' RETURNING value`)
	return version, err
}

func (m *Module) memoryWrite(ctx context.Context, tx *sqlx.Tx, w memoryWrite, conn Connection, run int64) (memoryRow, error) {
	var old memoryRow
	e := tx.GetContext(ctx, &old, `SELECT * FROM agents_memory WHERE key=?`, w.Key)
	exists := e == nil
	if !exists && errors.Is(e, sql.ErrNoRows) {
		old = memoryRow{}
	}
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return old, e
	}
	if !memoryKey.MatchString(w.Key) {
		return old, invalid("invalid memory key")
	}
	if old.Version != w.Expected {
		return old, conflict("stale_memory", "memory changed", "current", old.json())
	}
	if exists && w.Layer != "" && old.Layer != w.Layer {
		return old, invalid("memory layer cannot change")
	}
	agent := conn.ID != 0
	a := userActor(ctx)
	kind := "user"
	var status string
	if agent {
		a.ID = agentID(conn.ID)
		a.Name = conn.Name
		kind = "agent"
	}
	if exists {
		if w.Layer == "" {
			w.Layer = old.Layer
		}
	}
	if !slices.Contains([]string{"rule", "preference", "context"}, w.Layer) {
		return old, invalid("layer is rule, preference or context")
	}
	if agent && exists && old.Layer == "rule" && old.Status == "active" {
		return old, conflict("user_corrected", "active user rules cannot be changed by an agent")
	}
	if agent && old.Corrected != nil {
		valid := false
		if w.Reconsider != nil && strings.TrimSpace(w.Reconsider.Reason) != "" && len(w.Reconsider.Reason) <= 4000 {
			for _, id := range w.Reconsider.Events {
				var n int
				if id > *old.Corrected {
					if e = tx.GetContext(ctx, &n, `SELECT count(*) FROM agents_events WHERE id=? AND actor_kind='user' AND kind IN ('comment','accepted','declined','changes_requested','proposal_edited','memory_changed')`, id); e != nil {
						return old, e
					}
					valid = valid || n == 1
				}
			}
		}
		if !valid {
			return old, conflict("user_corrected", "user-corrected memory needs newer user evidence and a reason", "current", old.json())
		}
	}
	if w.Op == "delete" {
		if !exists {
			return old, notFound("memory")
		}
		if agent && (old.Layer != "context" || old.AuthorKind != "agent") {
			return old, conflict("user_corrected", "an agent may only delete agent context")
		}
		status = "deleted"
		w.Title, w.Scope, w.Body, w.Confidence = old.Title, old.Scope, old.Body, old.Confidence
		_ = json.Unmarshal([]byte(old.Evidence), &w.Evidence)
		if old.Layer == "context" {
			w.Body, w.Scope, w.Evidence = "", "", nil
		}
	} else if w.Op != "" && w.Op != "put" {
		return old, invalid("memory op is put or delete")
	} else {
		status = "active"
		if agent && w.Layer == "rule" {
			status = "proposed"
		}
	}
	if len(w.Title) == 0 || len(w.Title) > 200 || len(w.Scope) > 500 || len(w.Body) > 65536 || len(w.Evidence) > 20 || w.Confidence != nil && (*w.Confidence < 0 || *w.Confidence > 100) {
		return old, invalid("memory fields exceed their limits")
	}
	if w.Evidence == nil {
		w.Evidence = []memEvidence{}
	}
	for _, ev := range w.Evidence {
		if len(ev.Note) > 4000 {
			return old, invalid("evidence note too long")
		}
	}
	now := m.now()
	var connID, runID any
	if agent {
		connID, runID = conn.ID, run
	}
	version, e := nextMemoryVersion(ctx, tx)
	if e != nil {
		return old, e
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO agents_memory(key,layer,status,title,scope,body,confidence,evidence,version,author_kind,author_id,author,connection_id,run_id,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(key) DO UPDATE SET status=excluded.status,title=excluded.title,scope=excluded.scope,body=excluded.body,confidence=excluded.confidence,evidence=excluded.evidence,version=excluded.version,author_kind=excluded.author_kind,author_id=excluded.author_id,author=excluded.author,connection_id=excluded.connection_id,run_id=excluded.run_id,updated_at=excluded.updated_at`, w.Key, w.Layer, status, w.Title, w.Scope, w.Body, w.Confidence, mustJSON(w.Evidence), version, kind, a.ID, a.Name, connID, runID, now, now)
	if e != nil {
		return old, e
	}
	var out memoryRow
	if e = tx.GetContext(ctx, &out, `SELECT * FROM agents_memory WHERE key=?`, w.Key); e != nil {
		return old, e
	}
	if !agent {
		ev := userEvent(a, "memory_changed")
		ev.Memory = out.ID
		ev.Data = map[string]any{"key": w.Key, "action": w.Op, "before": old.json(), "after": out.json()}
		id, e := m.insertEvent(ctx, tx, ev)
		if e != nil {
			return out, e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE agents_memory SET corrected_event=? WHERE id=?`, id, out.ID); e != nil {
			return out, e
		}
		out.Corrected = &id
	}
	var n, size int
	if e = tx.GetContext(ctx, &n, `SELECT count(*) FROM agents_memory`); e != nil {
		return out, e
	}
	if e = tx.GetContext(ctx, &size, `SELECT coalesce(sum(length(CAST(body AS BLOB))),0) FROM agents_memory`); e != nil {
		return out, e
	}
	if n > 2000 || size > 4<<20 {
		return out, fail(413, "quota", "shared memory quota exceeded")
	}
	return out, nil
}
func (m *Module) commitMemory(c *echo.Context) error {
	var b struct {
		Run      int64          `json:"run_id"`
		Writes   []memoryWrite  `json:"writes"`
		Learning *learningWrite `json:"learning"`
	}
	if e := decode(c, 1<<20, &b); e != nil {
		return e
	}
	if len(b.Writes) > 100 {
		return invalid("at most 100 memory writes per commit")
	}
	ctx := c.Request().Context()
	conn := current(c)
	docs := []map[string]any{}
	var state learningRow
	e := m.db.Write(ctx, func(tx *sqlx.Tx) error {
		if e := m.checkRun(ctx, tx, conn, b.Run); e != nil {
			return e
		}
		if e := m.spend(ctx, tx, conn.ID, "memory", int64(len(b.Writes))); e != nil {
			return e
		}
		seen := map[string]bool{}
		for _, w := range b.Writes {
			if seen[w.Key] {
				return invalid("memory key repeated in commit")
			}
			seen[w.Key] = true
			r, e := m.memoryWrite(ctx, tx, w, conn, b.Run)
			if e != nil {
				return e
			}
			docs = append(docs, r.json())
		}
		var e error
		state, e = learning(ctx, tx)
		if e != nil {
			return e
		}
		if b.Learning != nil {
			w := b.Learning
			if w.Expected != state.Version {
				return conflict("stale_learning", "learning progress changed", "current", state.json())
			}
			var high int64
			if e = tx.GetContext(ctx, &high, `SELECT coalesce(max(id),0) FROM agents_events`); e != nil {
				return e
			}
			var historyHigh int64
			if e = tx.GetContext(ctx, &historyHigh, `SELECT value FROM agents_state WHERE key='history_high'`); e != nil {
				return e
			}
			if w.Events < state.Events || w.Events > max(high, state.Events) || w.History < state.History || w.History > historyHigh {
				return invalid("learning cursors must advance within available feedback")
			}
			if len(w.Checkpoint) == 0 {
				w.Checkpoint = json.RawMessage(`{}`)
			}
			if len(w.Checkpoint) > 65536 || !json.Valid(w.Checkpoint) {
				return invalid("checkpoint must be JSON up to 64 KiB")
			}
			_, e = tx.ExecContext(ctx, `UPDATE agents_learning SET version=version+1,events_cursor=?,history_cursor=?,checkpoint=?,updated_at=?,connection_id=?,run_id=? WHERE id=1`, w.Events, w.History, string(w.Checkpoint), m.now(), conn.ID, b.Run)
			if e != nil {
				return e
			}
			state, e = learning(ctx, tx)
		}
		return e
	})
	if e != nil {
		return e
	}
	return c.JSON(200, map[string]any{"memory": docs, "learning": state.json()})
}
func (m *Module) getMemory(c *echo.Context) error {
	after, e := intQuery(c, "after", 0, 0, 1<<62)
	if e != nil {
		return e
	}
	limit, e := intQuery(c, "limit", 100, 1, 100)
	if e != nil {
		return e
	}
	q := `SELECT * FROM agents_memory WHERE id>?`
	args := []any{after}
	for _, f := range []string{"layer", "status"} {
		v := c.QueryParam(f)
		if v != "" && v != "all" {
			q += ` AND ` + f + `=?`
			args = append(args, v)
		}
	}
	q += ` ORDER BY id LIMIT ?`
	args = append(args, limit+1)
	rs := []memoryRow{}
	if e = m.db.R.SelectContext(c.Request().Context(), &rs, q, args...); e != nil {
		return e
	}
	var next any
	if int64(len(rs)) > limit {
		rs = rs[:limit]
		next = rs[len(rs)-1].ID
	}
	out := []map[string]any{}
	for _, r := range rs {
		out = append(out, r.json())
	}
	return c.JSON(200, map[string]any{"memory": out, "next": next})
}
func (m *Module) getMemoryKey(c *echo.Context) error {
	var r memoryRow
	e := m.db.R.GetContext(c.Request().Context(), &r, `SELECT * FROM agents_memory WHERE key=?`, c.Param("key"))
	if errors.Is(e, sql.ErrNoRows) {
		return notFound("memory")
	}
	if e != nil {
		return e
	}
	return c.JSON(200, r.json())
}
func (m *Module) getLearning(c *echo.Context) error {
	r, e := learning(c.Request().Context(), m.db.R)
	if e != nil {
		return e
	}
	out := r.json()
	var n int
	if e = m.db.R.GetContext(c.Request().Context(), &n, `SELECT count(*) FROM agents_events WHERE id>?`, r.Events); e != nil {
		return e
	}
	out["unprocessed_events"] = n
	return c.JSON(200, out)
}
func (m *Module) userMemory(c *echo.Context, action string) error {
	ctx := c.Request().Context()
	var w memoryWrite
	var before memoryRow
	if action == "create" {
		if e := decode(c, 1<<20, &w); e != nil {
			return e
		}
		if w.Layer != "rule" || w.Expected != 0 {
			return invalid("users create explicit rules")
		}
	} else {
		id, e := idParam(c, "id")
		if e != nil {
			return e
		}
		if e = m.db.R.GetContext(ctx, &before, `SELECT * FROM agents_memory WHERE id=?`, id); errors.Is(e, sql.ErrNoRows) {
			return notFound("memory")
		} else if e != nil {
			return e
		}
		w = memoryWrite{Key: before.Key, Layer: before.Layer, Title: before.Title, Scope: before.Scope, Body: before.Body, Confidence: before.Confidence}
		_ = json.Unmarshal([]byte(before.Evidence), &w.Evidence)
		if action == "delete" {
			v, e := intQuery(c, "expected_version", -1, 0, 1<<62)
			if e != nil || v < 0 {
				return invalid("expected_version is required")
			}
			w.Expected = v
			w.Op = "delete"
		} else {
			var b struct {
				Expected   int64           `json:"expected_version"`
				Title      *string         `json:"title"`
				Scope      *string         `json:"scope"`
				Body       *string         `json:"body"`
				Confidence json.RawMessage `json:"confidence"`
			}
			if e = decode(c, 1<<20, &b); e != nil {
				return e
			}
			w.Expected = b.Expected
			if b.Title != nil {
				w.Title = *b.Title
			}
			if b.Scope != nil {
				w.Scope = *b.Scope
			}
			if b.Body != nil {
				w.Body = *b.Body
			}
			if b.Confidence != nil {
				if e = json.Unmarshal(b.Confidence, &w.Confidence); e != nil {
					return invalid("confidence must be a number or null")
				}
			}
			if action == "confirm" && (before.Layer != "rule" || before.Status != "proposed") {
				return invalid("only proposed rules can be confirmed")
			}
		}
	}
	if action == "delete" && before.Layer != "preference" {
		e := m.db.Write(ctx, func(tx *sqlx.Tx) error {
			var now memoryRow
			if e := tx.GetContext(ctx, &now, `SELECT * FROM agents_memory WHERE id=?`, before.ID); e != nil {
				return e
			}
			if now.Version != w.Expected {
				return conflict("stale_memory", "memory changed", "current", now.json())
			}
			ev := userEvent(userActor(ctx), "memory_changed")
			ev.Memory = now.ID
			ev.Data = map[string]any{"action": "deleted", "key": now.Key}
			if _, e := m.insertEvent(ctx, tx, ev); e != nil {
				return e
			}
			_, e := tx.ExecContext(ctx, `DELETE FROM agents_memory WHERE id=?`, now.ID)
			return e
		})
		if e != nil {
			return e
		}
		return c.JSON(200, map[string]any{"deleted": true})
	}
	var out memoryRow
	e := m.db.Write(ctx, func(tx *sqlx.Tx) error { var e error; out, e = m.memoryWrite(ctx, tx, w, Connection{}, 0); return e })
	if e != nil {
		return e
	}
	return c.JSON(200, out.json())
}
func (m *Module) resetMemory(c *echo.Context) error {
	var b struct {
		Layers []string `json:"layers"`
	}
	if e := decode(c, 1<<20, &b); e != nil {
		return e
	}
	if len(b.Layers) == 0 || len(b.Layers) > 3 {
		return invalid("select memory layers")
	}
	for _, l := range b.Layers {
		if !slices.Contains([]string{"rule", "preference", "context"}, l) {
			return invalid("unknown memory layer")
		}
	}
	ctx := c.Request().Context()
	a := userActor(ctx)
	e := m.db.Write(ctx, func(tx *sqlx.Tx) error {
		q, args, e := sqlx.In(`SELECT * FROM agents_memory WHERE layer IN (?)`, b.Layers)
		if e != nil {
			return e
		}
		rs := []memoryRow{}
		if e = tx.SelectContext(ctx, &rs, q, args...); e != nil {
			return e
		}
		for _, r := range rs {
			ev := userEvent(a, "memory_changed")
			ev.Memory = r.ID
			ev.Data = map[string]any{"action": "reset", "key": r.Key}
			id, e := m.insertEvent(ctx, tx, ev)
			if e != nil {
				return e
			}
			if r.Layer == "preference" {
				version, err := nextMemoryVersion(ctx, tx)
				if err != nil {
					return err
				}
				_, e = tx.ExecContext(ctx, `UPDATE agents_memory SET status='deleted',version=?,corrected_event=?,updated_at=? WHERE id=?`, version, id, m.now(), r.ID)
			} else {
				_, e = tx.ExecContext(ctx, `DELETE FROM agents_memory WHERE id=?`, r.ID)
			}
			if e != nil {
				return e
			}
		}
		return nil
	})
	if e != nil {
		return e
	}
	return c.JSON(200, map[string]any{"reset": true})
}
func (m *Module) exportMemory(c *echo.Context) error {
	ctx := c.Request().Context()
	rs := []memoryRow{}
	if e := m.db.R.SelectContext(ctx, &rs, `SELECT * FROM agents_memory ORDER BY id`); e != nil {
		return e
	}
	docs := []map[string]any{}
	for _, r := range rs {
		docs = append(docs, r.json())
	}
	l, e := learning(ctx, m.db.R)
	if e != nil {
		return e
	}
	c.Response().Header().Set("Content-Disposition", `attachment; filename="shelf-memory.json"`)
	return c.JSON(200, map[string]any{"memory": docs, "learning": l.json(), "exported_at": stamp(db.At(m.Now()))})
}
