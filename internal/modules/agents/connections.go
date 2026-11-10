package agents

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jmoiron/sqlx"
	"github.com/labstack/echo/v5"
	"github.com/tikhonp/shelf/internal/platform/db"
	"github.com/tikhonp/shelf/internal/platform/identity"
	"golang.org/x/time/rate"
)

// The operations a connection may hold.
const (
	OpLibraryRead    = "library.read"
	OpLibraryLookup  = "library.lookup"
	OpHistoryRead    = "history.read"
	OpCoversUpload   = "covers.upload"
	OpProposalsWrite = "proposals.write"
	OpMemoryWrite    = "memory.write"
)

// Operations lists every operation with what it grants, in order.
var Operations = []struct{ Name, Summary string }{
	{OpLibraryRead, "album list, album detail, local suggestions, album cover"},
	{OpLibraryLookup, "external suggestions (MusicBrainz, sources, LRCLIB) and cover offers"},
	{OpHistoryRead, "the library change history, with authors"},
	{OpCoversUpload, "uploading cover images for proposals"},
	{OpProposalsWrite, "creating, revising, commenting on and withdrawing proposals"},
	{OpMemoryWrite, "memory commits and learning commits"},
}

// AllOperations is the default set of a new connection.
func AllOperations() []string {
	out := make([]string, len(Operations))
	for i, o := range Operations {
		out[i] = o.Name
	}
	return out
}

var (
	// ErrConnectionNotFound is a connection ID that doesn't exist.
	ErrConnectionNotFound = errors.New("agents: connection not found")
	// ErrNameTaken is a name an unrevoked connection already has.
	ErrNameTaken = errors.New("agents: an active connection already has this name")
	// ErrAlreadyRevoked is a second revocation.
	ErrAlreadyRevoked = errors.New("agents: connection already revoked")
)

// Connection is an agent's connection as listed. It never holds the token.
type Connection struct {
	ID           int64    `json:"id"`
	Name         string   `json:"name"`
	Operations   []string `json:"operations"`
	CreatedByID  string   `json:"-"`
	CreatedBy    string   `json:"created_by"`
	CreatedAt    string   `json:"created_at"`
	LastUsedAt   *string  `json:"last_used_at"`
	RevokedAt    *string  `json:"revoked_at"`
	RevokeReason string   `json:"revoke_reason"`
	RevokedBy    string   `json:"revoked_by"`
	LastRunAt    *string  `json:"last_run_at"`
	Proposals    int      `json:"proposals"`
	Rules        int      `json:"rules"`
	AccountID    *int64   `json:"-"`
}

type connRow struct {
	ID            int64          `db:"id"`
	Name          string         `db:"name"`
	TokenHash     string         `db:"token_hash"`
	Operations    string         `db:"operations"`
	AccountID     sql.NullInt64  `db:"account_id"`
	CreatedByID   string         `db:"created_by_id"`
	CreatedBy     string         `db:"created_by"`
	CreatedAt     db.Time        `db:"created_at"`
	LastUsedAt    db.Time        `db:"last_used_at"`
	RevokedAt     db.Time        `db:"revoked_at"`
	RevokedBy     sql.NullString `db:"revoked_by"`
	RevokeReason  string         `db:"revoke_reason"`
	RevokedByName string         `db:"revoked_by_name"`
}

const connCols = `id, name, token_hash, operations, account_id, created_by_id, created_by, created_at, last_used_at, revoked_at, revoked_by, revoke_reason, revoked_by_name`

func (r connRow) conn() Connection {
	c := Connection{ID: r.ID, Name: r.Name, CreatedByID: r.CreatedByID, CreatedBy: r.CreatedBy,
		CreatedAt: stamp(r.CreatedAt), LastUsedAt: optStamp(r.LastUsedAt), RevokedAt: optStamp(r.RevokedAt), RevokeReason: r.RevokeReason, RevokedBy: r.RevokedByName}
	_ = json.Unmarshal([]byte(r.Operations), &c.Operations)
	if c.Operations == nil {
		c.Operations = []string{}
	}
	if r.AccountID.Valid {
		id := r.AccountID.Int64
		c.AccountID = &id
	}
	return c
}

func (c Connection) can(op string) bool { return slices.Contains(c.Operations, op) }

func stamp(t db.Time) string { return t.UTC().Format(time.RFC3339Nano) }
func optStamp(t db.Time) *string {
	if t.IsZero() {
		return nil
	}
	s := stamp(t)
	return &s
}

const tokenPrefix = "shelf_agent_"

var tokenPattern = regexp.MustCompile(`^shelf_agent_[A-Za-z0-9_-]{43}$`)

func newToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return tokenPrefix + base64.RawURLEncoding.EncodeToString(b)
}

func hashToken(t string) string {
	h := sha256.Sum256([]byte(t))
	return hex.EncodeToString(h[:])
}

func checkName(name string) error {
	n := utf8.RuneCountInString(name)
	if n < 1 || n > 64 || !utf8.ValidString(name) {
		return errors.New("a connection name has 1 to 64 characters")
	}
	for _, r := range name {
		if !unicode.IsPrint(r) {
			return errors.New("a connection name has only printable characters")
		}
	}
	if strings.TrimSpace(name) != name {
		return errors.New("a connection name doesn't start or end with a space")
	}
	return nil
}

func checkOperations(ops []string) ([]string, error) {
	if ops == nil {
		return AllOperations(), nil
	}
	all := AllOperations()
	out := []string{}
	for _, o := range ops {
		if !slices.Contains(all, o) {
			return nil, fmt.Errorf("unknown operation %q; known: %s", o, strings.Join(all, ", "))
		}
		if !slices.Contains(out, o) {
			out = append(out, o)
		}
	}
	// Keep the documented order, so lists read the same everywhere.
	slices.SortFunc(out, func(a, b string) int { return slices.Index(all, a) - slices.Index(all, b) })
	return out, nil
}

// AddConnection creates a connection and returns it with its token, which
// is shown this once. The author comes from ctx; without one it is the CLI.
// ops nil means every operation.
func (m *Module) AddConnection(ctx context.Context, name string, ops []string) (Connection, string, error) {
	a := identity.From(ctx)
	if a.ID == "" {
		a = identity.Author{ID: "cli", Name: "CLI"}
	}
	return m.createConnection(ctx, name, ops, a)
}

func accountOf(a identity.Author) *int64 {
	s, ok := strings.CutPrefix(a.ID, "user:")
	if !ok {
		return nil
	}
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return nil
	}
	return &id
}

func (m *Module) createConnection(ctx context.Context, name string, ops []string, a identity.Author) (Connection, string, error) {
	if err := checkName(name); err != nil {
		return Connection{}, "", err
	}
	ops, err := checkOperations(ops)
	if err != nil {
		return Connection{}, "", err
	}
	token := newToken()
	var id int64
	err = m.db.Write(ctx, func(tx *sqlx.Tx) error {
		var taken int
		if err := tx.GetContext(ctx, &taken, `SELECT count(*) FROM agents_connections WHERE name = ? AND revoked_at IS NULL`, name); err != nil {
			return err
		}
		if taken > 0 {
			return ErrNameTaken
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO agents_connections (name, token_hash, operations, account_id, created_by_id, created_by, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)`, name, hashToken(token), mustJSON(ops), accountOf(a), a.ID, a.Label(), m.now())
		if err != nil {
			return err
		}
		id, err = res.LastInsertId()
		return err
	})
	if err != nil {
		return Connection{}, "", err
	}
	c, err := m.connection(ctx, id)
	return c, token, err
}

func (m *Module) connection(ctx context.Context, id int64) (Connection, error) {
	var r connRow
	err := m.db.R.GetContext(ctx, &r, `SELECT `+connCols+` FROM agents_connections WHERE id = ?`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return Connection{}, ErrConnectionNotFound
	}
	return r.conn(), err
}

// ListConnections lists every connection, revoked ones included, by ID.
func (m *Module) ListConnections(ctx context.Context) ([]Connection, error) {
	rows := []struct {
		connRow
		LastRunAt db.Time `db:"last_run_at"`
		Proposals int     `db:"proposals"`
		Rules     int     `db:"rules"`
	}{}
	if err := m.db.R.SelectContext(ctx, &rows, `SELECT c.*,
		r.last_run_at, coalesce(p.proposals, 0) AS proposals, coalesce(m.rules, 0) AS rules
		FROM agents_connections c
		LEFT JOIN (SELECT connection_id, max(started_at) AS last_run_at FROM agents_runs GROUP BY connection_id) r ON r.connection_id = c.id
		LEFT JOIN (SELECT connection_id, count(*) AS proposals FROM agents_proposals WHERE pending > 0 AND withdrawn_at IS NULL GROUP BY connection_id) p ON p.connection_id = c.id
		LEFT JOIN (SELECT connection_id, count(*) AS rules FROM agents_memory WHERE layer = 'rule' AND status = 'active' GROUP BY connection_id) m ON m.connection_id = c.id
		ORDER BY c.id`); err != nil {
		return nil, err
	}
	out := make([]Connection, len(rows))
	for i, r := range rows {
		out[i] = r.conn()
		out[i].LastRunAt = optStamp(r.LastRunAt)
		out[i].Proposals = r.Proposals
		out[i].Rules = r.Rules
	}
	return out, nil
}

// RevokeConnection revokes a connection for good. The author comes from
// ctx; without one it is the CLI. Re-enabling the feature never revives it.
func (m *Module) RevokeConnection(ctx context.Context, id int64) error {
	a := identity.From(ctx)
	reason := "user"
	if a.ID == "" || a.ID == "cli" {
		a, reason = identity.Author{ID: "cli", Name: "CLI"}, "cli"
	}
	return m.revoke(ctx, id, a.ID, a.Name, reason)
}

func (m *Module) revoke(ctx context.Context, id int64, by, name, reason string) error {
	return m.db.Write(ctx, func(tx *sqlx.Tx) error {
		var revoked db.Time
		err := tx.GetContext(ctx, &revoked, `SELECT revoked_at FROM agents_connections WHERE id = ?`, id)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrConnectionNotFound
		}
		if err != nil {
			return err
		}
		if !revoked.IsZero() {
			return ErrAlreadyRevoked
		}
		_, err = tx.ExecContext(ctx, `UPDATE agents_connections SET revoked_at = ?, revoked_by = ?, revoked_by_name = ?, revoke_reason = ? WHERE id = ?`,
			m.now(), by, name, reason, id)
		return err
	})
}

// limiter holds each connection's token bucket and when its last use was
// stored.
type limiter struct {
	mu      sync.Mutex
	buckets map[int64]*rate.Limiter
	used    map[int64]time.Time
}

func newLimiter() *limiter {
	return &limiter{buckets: map[int64]*rate.Limiter{}, used: map[int64]time.Time{}}
}

func (l *limiter) allow(id int64) bool {
	l.mu.Lock()
	b := l.buckets[id]
	if b == nil {
		b = rate.NewLimiter(10, 30)
		l.buckets[id] = b
	}
	l.mu.Unlock()
	return b.Allow()
}

// touch reports whether last_used_at is due a write, at most once a minute.
func (l *limiter) touch(id int64, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if t, ok := l.used[id]; ok && now.Sub(t) < time.Minute {
		return false
	}
	l.used[id] = now
	return true
}

const connKey = "agents.connection"

func current(c *echo.Context) Connection { v, _ := c.Get(connKey).(Connection); return v }

// agentAuth authenticates every /api/agent/v1 request by its bearer token.
// Session cookies play no part.
func (m *Module) agentAuth(next echo.HandlerFunc) echo.HandlerFunc {
	return m.h(func(c *echo.Context) error {
		ctx := c.Request().Context()
		h := c.Request().Header.Get("Authorization")
		scheme, token, ok := strings.Cut(h, " ")
		if !ok || !strings.EqualFold(scheme, "Bearer") || !tokenPattern.MatchString(token) {
			return fail(http.StatusUnauthorized, "unauthorized", "send Authorization: Bearer <agent token>")
		}
		var r connRow
		err := m.db.R.GetContext(ctx, &r, `SELECT `+connCols+` FROM agents_connections WHERE token_hash = ?`, hashToken(token))
		if errors.Is(err, sql.ErrNoRows) {
			return fail(http.StatusUnauthorized, "unauthorized", "unknown agent token")
		}
		if err != nil {
			return err
		}
		if !r.RevokedAt.IsZero() {
			return fail(http.StatusUnauthorized, "revoked", "this connection was revoked")
		}
		conn := r.conn()
		if conn.AccountID != nil && m.accounts != nil {
			exists, err := m.accounts.Exists(ctx, *conn.AccountID)
			if err != nil {
				return err
			}
			if !exists {
				if err := m.revoke(ctx, conn.ID, "system", "", "account_deleted"); err != nil && !errors.Is(err, ErrAlreadyRevoked) {
					return err
				}
				return fail(http.StatusUnauthorized, "revoked", "the account that created this connection was deleted")
			}
		}
		if !m.limiter.allow(conn.ID) {
			return fail(http.StatusTooManyRequests, "rate_limited", "over 10 requests a second", "retry_after", 1)
		}
		if now := m.Now(); m.limiter.touch(conn.ID, now) {
			if err := m.db.Write(ctx, func(tx *sqlx.Tx) error {
				_, err := tx.ExecContext(ctx, `UPDATE agents_connections SET last_used_at = ? WHERE id = ?`, db.At(now), conn.ID)
				return err
			}); err != nil {
				m.log.Warn("agents: last use", "connection", conn.ID, "error", err)
			}
		}
		c.Set(connKey, conn)
		c.SetRequest(c.Request().WithContext(identity.With(ctx, identity.Author{ID: "agent:" + strconv.FormatInt(conn.ID, 10), Name: conn.Name})))
		return next(c)
	})
}

// need wraps a handler that requires op.
func (m *Module) need(op string, f echo.HandlerFunc) echo.HandlerFunc {
	return m.h(func(c *echo.Context) error {
		if !current(c).can(op) {
			return fail(http.StatusForbidden, "forbidden_operation", "this connection lacks "+op, "operation", op)
		}
		return f(c)
	})
}
