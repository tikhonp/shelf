package agents

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/tikhonp/shelf/internal/libraryport"
	"github.com/tikhonp/shelf/internal/platform/db"
	"github.com/tikhonp/shelf/internal/platform/identity"
)

// querier is a read connection or a write transaction.
type querier interface {
	GetContext(ctx context.Context, dest any, query string, args ...any) error
	SelectContext(ctx context.Context, dest any, query string, args ...any) error
}

type proposalRow struct {
	ID                 int64         `db:"id"`
	ConnectionID       int64         `db:"connection_id"`
	RunID              int64         `db:"run_id"`
	AlbumID            int64         `db:"album_id"`
	Folder             string        `db:"folder"`
	Title              string        `db:"title"`
	Artist             string        `db:"artist"`
	GroupKey           string        `db:"group_key"`
	FollowUpOf         sql.NullInt64 `db:"follow_up_of"`
	Head               int           `db:"head"`
	Pending            int           `db:"pending"`
	ChangesRequestedAt db.Time       `db:"changes_requested_at"`
	WithdrawnAt        db.Time       `db:"withdrawn_at"`
	CreatedAt          db.Time       `db:"created_at"`
	UpdatedAt          db.Time       `db:"updated_at"`
}

const proposalCols = `id, connection_id, run_id, album_id, folder, title, artist, group_key, follow_up_of, head, pending,
	changes_requested_at, withdrawn_at, created_at, updated_at`

func (p proposalRow) withdrawn() bool { return !p.WithdrawnAt.IsZero() }
func (p proposalRow) state() string   { return proposalState(p.Pending, p.withdrawn()) }

func proposalState(pending int, withdrawn bool) string {
	switch {
	case withdrawn:
		return "withdrawn"
	case pending > 0:
		return "pending"
	}
	return "reviewed"
}

type revisionRow struct {
	ProposalID    int64         `db:"proposal_id"`
	Revision      int           `db:"revision"`
	AuthorKind    string        `db:"author_kind"`
	ConnectionID  sql.NullInt64 `db:"connection_id"`
	RunID         sql.NullInt64 `db:"run_id"`
	AuthorID      string        `db:"author_id"`
	Author        string        `db:"author"`
	Title         string        `db:"title"`
	Reason        string        `db:"reason"`
	Uncertainties string        `db:"uncertainties"`
	Confidence    sql.NullInt64 `db:"confidence"`
	Evidence      string        `db:"evidence"`
	Base          string        `db:"base"`
	Ops           string        `db:"ops"`
	Preview       string        `db:"preview"`
	OpDigests     string        `db:"op_digests"`
	CreatedAt     db.Time       `db:"created_at"`
}

const revisionCols = `proposal_id, revision, author_kind, connection_id, run_id, author_id, author, title, reason, uncertainties,
	confidence, evidence, base, ops, preview, op_digests, created_at`

func (r revisionRow) ops() []Op { return decodeOps(r.Ops) }
func (r revisionRow) base() []BaseTrack {
	b := []BaseTrack{}
	_ = json.Unmarshal([]byte(r.Base), &b)
	return b
}
func (r revisionRow) evidence() []Evidence {
	e := []Evidence{}
	_ = json.Unmarshal([]byte(r.Evidence), &e)
	return e
}
func (r revisionRow) digests() map[string]string {
	d := map[string]string{}
	_ = json.Unmarshal([]byte(r.OpDigests), &d)
	return d
}
func (r revisionRow) preview() libraryport.Preview {
	p := emptyPreview()
	_ = json.Unmarshal([]byte(r.Preview), &p)
	return p
}

type opRow struct {
	ProposalID   int64          `db:"proposal_id"`
	OpID         string         `db:"op_id"`
	Type         string         `db:"type"`
	Fingerprint  string         `db:"fingerprint"`
	Decision     string         `db:"decision"`
	DecidedRev   sql.NullInt64  `db:"decided_rev"`
	AcceptanceID sql.NullInt64  `db:"acceptance_id"`
	DecidedAt    db.Time        `db:"decided_at"`
	DecidedByID  sql.NullString `db:"decided_by_id"`
	DecidedBy    sql.NullString `db:"decided_by"`
}

const opCols = `proposal_id, op_id, type, fingerprint, decision, decided_rev, acceptance_id, decided_at, decided_by_id, decided_by`

type acceptanceRow struct {
	ID         int64   `db:"id"`
	ProposalID int64   `db:"proposal_id"`
	Revision   int     `db:"revision"`
	OpIDs      string  `db:"op_ids"`
	IdemKey    string  `db:"idem_key"`
	Attempt    int     `db:"attempt"`
	Digest     string  `db:"digest"`
	Preview    string  `db:"preview"`
	Confirm    string  `db:"confirm"`
	ChangeID   int64   `db:"change_id"`
	AuthorID   string  `db:"author_id"`
	Author     string  `db:"author"`
	CreatedAt  db.Time `db:"created_at"`
	Outcome    string  `db:"outcome"`
	Final      bool    `db:"final"`
	ViaSave    bool    `db:"via_save"`
}

const acceptanceCols = `id, proposal_id, revision, op_ids, idem_key, attempt, digest, preview, confirm, change_id, author_id, author, created_at, outcome, final, via_save`

func (a acceptanceRow) ops() []string {
	out := []string{}
	_ = json.Unmarshal([]byte(a.OpIDs), &out)
	return out
}
func (a acceptanceRow) confirm() []string {
	out := []string{}
	_ = json.Unmarshal([]byte(a.Confirm), &out)
	return out
}
func (a acceptanceRow) outcome() outcome {
	var o outcome
	_ = json.Unmarshal([]byte(a.Outcome), &o)
	if o.State == "" {
		o.State = "queued"
	}
	return o
}

// outcome is the last ChangeStatus seen for an acceptance's change.
type outcome struct {
	State   string `json:"state"`
	Reason  string `json:"reason,omitempty"`
	Error   string `json:"error,omitempty"`
	Undone  bool   `json:"undone"`
	Written int    `json:"written"`
	Files   int    `json:"files"`
	Expired bool   `json:"expired,omitempty"`
}

func outcomeOf(s libraryport.ChangeStatus) outcome {
	return outcome{State: s.State, Reason: s.Reason, Error: s.Error, Undone: s.Undone, Written: s.Written, Files: s.Files}
}

func (o outcome) retryable() bool {
	return !o.Expired && !o.Undone && o.State == "failed" && o.Written == 0
}

func getProposal(ctx context.Context, q querier, id int64) (proposalRow, error) {
	var p proposalRow
	err := q.GetContext(ctx, &p, `SELECT `+proposalCols+` FROM agents_proposals WHERE id = ?`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return p, notFound("proposal")
	}
	return p, err
}

func getRevision(ctx context.Context, q querier, id int64, rev int) (revisionRow, error) {
	var r revisionRow
	err := q.GetContext(ctx, &r, `SELECT `+revisionCols+` FROM agents_revisions WHERE proposal_id = ? AND revision = ?`, id, rev)
	if errors.Is(err, sql.ErrNoRows) {
		return r, notFound("revision")
	}
	return r, err
}

func getOps(ctx context.Context, q querier, id int64) (map[string]opRow, error) {
	rows := []opRow{}
	if err := q.SelectContext(ctx, &rows, `SELECT `+opCols+` FROM agents_ops WHERE proposal_id = ? ORDER BY rowid`, id); err != nil {
		return nil, err
	}
	out := make(map[string]opRow, len(rows))
	for _, r := range rows {
		out[r.OpID] = r
	}
	return out, nil
}

func getAcceptances(ctx context.Context, q querier, proposal int64) ([]acceptanceRow, error) {
	rows := []acceptanceRow{}
	err := q.SelectContext(ctx, &rows, `SELECT `+acceptanceCols+` FROM agents_acceptances WHERE proposal_id = ? ORDER BY id`, proposal)
	return rows, err
}

// snapshot is what a review read before preparing, compared again inside
// the write so a concurrent action is caught.
type snapshot struct {
	head      int
	decisions map[string]string
	accepted  map[string]int64
}

func snap(p proposalRow, ops map[string]opRow) snapshot {
	s := snapshot{head: p.Head, decisions: map[string]string{}, accepted: map[string]int64{}}
	for id, o := range ops {
		s.decisions[id] = o.Decision
		s.accepted[id] = o.AcceptanceID.Int64
	}
	return s
}

// recheck reloads the proposal inside tx and refuses if the head moved or a
// decision changed since s.
func recheck(ctx context.Context, tx *sqlx.Tx, id int64, s snapshot) (proposalRow, error) {
	p, err := getProposal(ctx, tx, id)
	if err != nil {
		return p, err
	}
	if p.withdrawn() {
		return p, conflict("decided_op", "the proposal was withdrawn")
	}
	if p.Head != s.head {
		return p, staleRevision(p.Head)
	}
	ops, err := getOps(ctx, tx, id)
	if err != nil {
		return p, err
	}
	now := snap(p, ops)
	for opID, d := range s.decisions {
		if now.decisions[opID] != d || now.accepted[opID] != s.accepted[opID] {
			return p, conflict("decided_op", "op "+opID+" was decided meanwhile; reload the proposal", "op", opID)
		}
	}
	return p, nil
}

func staleRevision(head int) *apiError {
	return conflict("stale_revision", "the proposal has a newer revision", "head", head)
}

// event is one row of the durable feed.
type event struct {
	Kind       string
	Proposal   int64
	Revision   int
	Acceptance int64
	Memory     int64
	ActorKind  string
	ActorID    string
	Actor      string
	Connection int64
	Run        int64
	Body       string
	Data       any
}

func nz(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}

func (m *Module) insertEvent(ctx context.Context, tx *sqlx.Tx, e event) (int64, error) {
	data := "{}"
	if e.Data != nil {
		data = mustJSON(e.Data)
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO agents_events (at, kind, proposal_id, revision, acceptance_id, memory_id, actor_kind, actor_id, actor,
		connection_id, run_id, body, data) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.now(), e.Kind, nz(e.Proposal), nz(int64(e.Revision)), nz(e.Acceptance), nz(e.Memory), e.ActorKind, e.ActorID, e.Actor,
		nz(e.Connection), nz(e.Run), e.Body, data)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// userActor is the reviewer of a human request.
func userActor(ctx context.Context) identity.Author {
	a := identity.From(ctx)
	if a.ID == "" {
		a = identity.Author{ID: "anonymous", Name: "Anonymous"}
	}
	a.Name = a.Label()
	return a
}

func agentEvent(conn Connection, run int64, kind string) event {
	return event{Kind: kind, ActorKind: "agent", ActorID: agentID(conn.ID), Actor: conn.Name, Connection: conn.ID, Run: run}
}

func userEvent(a identity.Author, kind string) event {
	return event{Kind: kind, ActorKind: "user", ActorID: a.ID, Actor: a.Name}
}

func agentID(id int64) string { return "agent:" + itoa(id) }

func setPending(ctx context.Context, tx *sqlx.Tx, id int64, now db.Time) error {
	_, err := tx.ExecContext(ctx, `UPDATE agents_proposals SET pending = (SELECT count(*) FROM agents_ops WHERE proposal_id = ? AND decision = 'pending'),
		updated_at = ? WHERE id = ?`, id, now, id)
	return err
}

// checkRun refuses a run of another connection and marks the run seen.
func (m *Module) checkRun(ctx context.Context, tx *sqlx.Tx, conn Connection, run int64) error {
	var owner int64
	err := tx.GetContext(ctx, &owner, `SELECT connection_id FROM agents_runs WHERE id = ?`, run)
	if errors.Is(err, sql.ErrNoRows) || err == nil && owner != conn.ID {
		return fail(http.StatusNotFound, "not_found", "run not found for this connection; start one with POST /runs")
	}
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE agents_runs SET last_seen_at = ? WHERE id = ?`, m.now(), run)
	return err
}

// checkRunRead is checkRun before a write starts, on the read pool.
func (m *Module) checkRunRead(ctx context.Context, conn Connection, run int64) error {
	var owner int64
	err := m.db.R.GetContext(ctx, &owner, `SELECT connection_id FROM agents_runs WHERE id = ?`, run)
	if errors.Is(err, sql.ErrNoRows) || err == nil && owner != conn.ID {
		return fail(http.StatusNotFound, "not_found", "run not found for this connection; start one with POST /runs")
	}
	return err
}

func day(t time.Time) string { return t.UTC().Format("2006-01-02") }

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
