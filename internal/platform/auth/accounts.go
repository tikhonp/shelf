// Package auth implements optional account authentication for Shelf.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/jmoiron/sqlx"
	"github.com/tikhonp/shelf/internal/platform/db"
	"golang.org/x/crypto/argon2"
	"golang.org/x/time/rate"
)

var (
	ErrCredentials  = errors.New("credentials changed or incorrect; sign in again")
	ErrNotFound     = errors.New("account not found")
	ErrLimited      = errors.New("too many attempts; try again later")
	usernamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,32}$`)
)

const sessionLife = 30 * 24 * time.Hour
const proofLife = 5 * time.Minute
const cookieName = "__Host-shelf_session"
const ceremonyCookie = "__Host-shelf_ceremony"

// Account excludes credential material when encoded as JSON.
type Account struct {
	ID          int64   `db:"id" json:"id"`
	Username    string  `db:"username" json:"username"`
	Hash        string  `db:"password_hash" json:"-"`
	Handle      []byte  `db:"user_handle" json:"-"`
	Gen         int64   `db:"credential_gen" json:"-"`
	CreatedAt   db.Time `db:"created_at" json:"created_at"`
	credentials []webauthn.Credential
}

func (a Account) WebAuthnID() []byte                         { return a.Handle }
func (a Account) WebAuthnName() string                       { return a.Username }
func (a Account) WebAuthnDisplayName() string                { return a.Username }
func (a Account) WebAuthnCredentials() []webauthn.Credential { return a.credentials }

const accountCols = `id, username, password_hash, user_handle, credential_gen, created_at`

type session struct {
	Account
	TokenHash string  `db:"token_hash"`
	Verified  db.Time `db:"verified_at"`
	Expires   db.Time `db:"expires_at"`
}
type attempt struct {
	limiter *rate.Limiter
	touched time.Time
}
type ceremony struct {
	Generations       map[int64]int64
	Data              webauthn.SessionData
	AccountID, Gen    int64
	SessionHash, Kind string
	Expires           time.Time
}

// Service owns accounts and the requests that their sessions authorize.
type Service struct {
	DB           *db.DB
	Enabled      bool
	webauthn     *webauthn.WebAuthn
	slots        chan struct{}
	passkeyLimit *rate.Limiter
	mu           sync.Mutex
	limits       map[string]attempt
	ceremonies   map[string]ceremony
	active       map[*activeRequest]struct{}
	now          func() time.Time
}

// New starts no goroutines and performs no database access.
func New(d *db.DB, enabled bool, origin string) (*Service, error) {
	s := &Service{DB: d, Enabled: enabled, slots: make(chan struct{}, 2), passkeyLimit: rate.NewLimiter(rate.Every(time.Second), 10), limits: map[string]attempt{}, ceremonies: map[string]ceremony{}, active: map[*activeRequest]struct{}{}, now: time.Now}
	if enabled {
		u, err := url.Parse(origin)
		if err != nil || u.Scheme != "https" || u.Hostname() == "" {
			return nil, errors.New("authentication needs an HTTPS origin")
		}
		host := u.Hostname()
		w, err := webauthn.New(&webauthn.Config{RPID: host, RPDisplayName: "Music Shelf", RPOrigins: []string{origin}, AuthenticatorSelection: protocol.AuthenticatorSelection{ResidentKey: protocol.ResidentKeyRequirementRequired, UserVerification: protocol.VerificationRequired}, AttestationPreference: protocol.PreferNoAttestation})
		if err != nil {
			return nil, err
		}
		s.webauthn = w
	}
	return s, nil
}

func randomToken() string { return base64.RawURLEncoding.EncodeToString(randomBytes(32)) }
func randomBytes(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return b
}
func tokenHash(t string) string { h := sha256.Sum256([]byte(t)); return hex.EncodeToString(h[:]) }

func passwordHash(password string) (string, error) {
	n := utf8.RuneCountInString(password)
	if !utf8.ValidString(password) || n < 8 || n > 128 {
		return "", errors.New("password must contain 8 to 128 Unicode characters")
	}
	salt := randomBytes(16)
	key := argon2.IDKey([]byte(password), salt, 3, 64*1024, 1, 32)
	return "$argon2id$v=19$m=65536,t=3,p=1$" + base64.RawStdEncoding.EncodeToString(salt) + "$" + base64.RawStdEncoding.EncodeToString(key), nil
}
func passwordMatches(hash, password string) bool {
	parts := strings.Split(hash, "$")
	// Only parameters written by this version are accepted. Never allocate from unchecked DB values.
	salt := make([]byte, 16)
	want := make([]byte, 32)
	valid := false
	if len(parts) == 6 && parts[1] == "argon2id" && parts[2] == "v=19" && parts[3] == "m=65536,t=3,p=1" {
		a, e1 := base64.RawStdEncoding.DecodeString(parts[4])
		b, e2 := base64.RawStdEncoding.DecodeString(parts[5])
		if e1 == nil && e2 == nil && len(a) == 16 && len(b) == 32 {
			salt = a
			want = b
			valid = true
		}
	}
	got := argon2.IDKey([]byte(password), salt, 3, 64*1024, 1, 32)
	return subtle.ConstantTimeCompare(got, want) == 1 && valid
}

func (s *Service) account(ctx context.Context, username string) (Account, error) {
	var a Account
	err := s.DB.R.GetContext(ctx, &a, `SELECT `+accountCols+` FROM accounts WHERE username = ?`, username)
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrNotFound
	}
	return a, err
}
func (s *Service) accountID(ctx context.Context, id int64) (Account, error) {
	var a Account
	err := s.DB.R.GetContext(ctx, &a, `SELECT `+accountCols+` FROM accounts WHERE id = ?`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrNotFound
	}
	return a, err
}

// Ready distinguishes a missing migration from a missing account.
func (s *Service) Ready(ctx context.Context) error {
	var n int
	if err := s.DB.R.GetContext(ctx, &n, `SELECT count(*) FROM accounts`); err != nil {
		return fmt.Errorf("account tables unavailable; run shelf manage migrate up first: %w", err)
	}
	return nil
}

// Add creates an account and never overwrites an existing username.
func (s *Service) Add(ctx context.Context, username, password string) error {
	if !usernamePattern.MatchString(username) {
		return errors.New("username must contain 1 to 32 ASCII letters, digits, dots, underscores, or hyphens")
	}
	hash, err := passwordHash(password)
	if err != nil {
		return err
	}
	_, err = s.DB.W.ExecContext(ctx, `INSERT INTO accounts(username,password_hash,user_handle,created_at) VALUES(?,?,?,?)`, username, hash, randomBytes(32), db.At(s.now()))
	if err != nil && strings.Contains(err.Error(), "UNIQUE") {
		return errors.New("username already exists")
	}
	return err
}

// List returns accounts in creation order.
func (s *Service) List(ctx context.Context) ([]Account, error) {
	a := []Account{}
	err := s.DB.R.SelectContext(ctx, &a, `SELECT `+accountCols+` FROM accounts ORDER BY id`)
	return a, err
}

// Reset changes credentials and revokes sessions atomically. An empty password is used only for passkey reset.
func (s *Service) Reset(ctx context.Context, username, password string, removePasskeys bool) error {
	a, err := s.account(ctx, username)
	if err != nil {
		return err
	}
	hash := ""
	if !removePasskeys {
		hash, err = passwordHash(password)
		if err != nil {
			return err
		}
	}
	err = s.DB.Write(ctx, func(tx *sqlx.Tx) error {
		if err := checkGeneration(ctx, tx, a); err != nil {
			return err
		}
		if removePasskeys {
			if _, err := tx.ExecContext(ctx, `DELETE FROM account_passkeys WHERE account_id=?`, a.ID); err != nil {
				return err
			}
		} else {
			if _, err := tx.ExecContext(ctx, `UPDATE accounts SET password_hash=? WHERE id=?`, hash, a.ID); err != nil {
				return err
			}
		}
		return revokeAccount(ctx, tx, a.ID)
	})
	if err == nil {
		s.cancelAccount(a.ID)
	}
	return err
}

// Delete leaves existing module histories intact.
func (s *Service) Delete(ctx context.Context, username string) error {
	a, err := s.account(ctx, username)
	if err != nil {
		return err
	}
	err = s.DB.Write(ctx, func(tx *sqlx.Tx) error {
		if err := checkGeneration(ctx, tx, a); err != nil {
			return err
		}
		if err := revokeAccount(ctx, tx, a.ID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `DELETE FROM accounts WHERE id=?`, a.ID)
		return err
	})
	if err == nil {
		s.cancelAccount(a.ID)
	}
	return err
}
func checkGeneration(ctx context.Context, tx *sqlx.Tx, a Account) error {
	var n int
	if err := tx.GetContext(ctx, &n, `SELECT count(*) FROM accounts WHERE id=? AND credential_gen=?`, a.ID, a.Gen); err != nil {
		return err
	}
	if n != 1 {
		return ErrCredentials
	}
	return nil
}
func revokeAccount(ctx context.Context, tx *sqlx.Tx, id int64) error {
	if _, err := tx.ExecContext(ctx, `UPDATE accounts SET credential_gen=credential_gen+1 WHERE id=?`, id); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `DELETE FROM account_sessions WHERE account_id=?`, id)
	return err
}
func (s *Service) createSession(ctx context.Context, tx *sqlx.Tx, a Account, token string) error {
	if err := checkGeneration(ctx, tx, a); err != nil {
		return err
	}
	now := s.now()
	if _, err := tx.ExecContext(ctx, `DELETE FROM account_sessions WHERE expires_at<=?`, db.At(now)); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO account_sessions(token_hash,account_id,credential_gen,verified_at,expires_at) VALUES(?,?,?,?,?)`, tokenHash(token), a.ID, a.Gen, db.At(now), db.At(now.Add(sessionLife)))
	return err
}
func (s *Service) lookupSession(ctx context.Context, hash string) (session, error) {
	var v session
	err := s.DB.R.GetContext(ctx, &v, `SELECT a.id,a.username,a.password_hash,a.user_handle,a.credential_gen,a.created_at,s.token_hash,s.verified_at,s.expires_at FROM account_sessions s JOIN accounts a ON a.id=s.account_id AND a.credential_gen=s.credential_gen WHERE s.token_hash=? AND s.expires_at>?`, hash, db.At(s.now()))
	return v, err
}
func (s *Service) allow(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	a, ok := s.limits[name]
	if !ok {
		if len(s.limits) >= 1024 {
			var oldest string
			var t time.Time
			for k, v := range s.limits {
				if t.IsZero() || v.touched.Before(t) {
					oldest = k
					t = v.touched
				}
			}
			delete(s.limits, oldest)
		}
		a = attempt{limiter: rate.NewLimiter(rate.Every(time.Minute/5), 5)}
	}
	a.touched = now
	s.limits[name] = a
	return a.limiter.AllowN(now, 1)
}

// AuthenticationEnabled reports provider simple, for module.Accounts. The
// Enabled field holds the same; a method of that name would collide with it.
func (s *Service) AuthenticationEnabled() bool { return s.Enabled }

// Exists reports whether an account ID is still in use, for module.Accounts.
func (s *Service) Exists(ctx context.Context, id int64) (bool, error) {
	var n int
	err := s.DB.R.GetContext(ctx, &n, `SELECT count(*) FROM accounts WHERE id = ?`, id)
	return n > 0, err
}
