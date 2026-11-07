package auth

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/jmoiron/sqlx"
	"github.com/labstack/echo/v5"
	"github.com/tikhonp/shelf/internal/platform/db/dbtest"
	"github.com/tikhonp/shelf/internal/platform/httpx"
	"github.com/tikhonp/shelf/internal/platform/identity"
)

const testPassword = "a sufficiently long password"
const testOrigin = "https://shelf.example.com"

var bg = context.Background()

type rig struct {
	s *Service
	e *echo.Echo
}

func newRig(t *testing.T) *rig {
	t.Helper()
	s, err := New(dbtest.Open(t), true, testOrigin)
	if err != nil {
		t.Fatal(err)
	}
	e := httpx.New(httpx.Options{Log: dbtest.Discard})
	e.Use(s.Middleware)
	api := e.Group("/api")
	s.Routes(api)
	api.GET("/secret", func(c *echo.Context) error { return c.JSON(200, identity.From(c.Request().Context())) })
	api.POST("/secret", func(c *echo.Context) error { return c.JSON(200, true) })
	api.GET("/cover", func(c *echo.Context) error {
		c.Response().Header().Set("Cache-Control", "private, max-age=86400")
		return c.String(200, "image")
	})
	return &rig{s, e}
}
func (r *rig) request(t *testing.T, method, path string, body any, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	var b bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&b).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, &b)
	req.Header.Set("Content-Type", "application/json")
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	r.e.ServeHTTP(rec, req)
	return rec
}
func cookie(t *testing.T, r *httptest.ResponseRecorder, name string) *http.Cookie {
	t.Helper()
	for _, c := range r.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("cookie %s missing: %d %s", name, r.Code, r.Body)
	return nil
}
func requireStatus(t *testing.T, r *httptest.ResponseRecorder, status int) {
	t.Helper()
	if r.Code != status {
		t.Fatalf("status %d, want %d: %s", r.Code, status, r.Body)
	}
}
func (r *rig) add(t *testing.T, name string) {
	t.Helper()
	if err := r.s.Add(bg, name, testPassword); err != nil {
		t.Fatal(err)
	}
}
func (r *rig) login(t *testing.T, name string) *http.Cookie {
	t.Helper()
	r1 := r.request(t, "POST", "/api/auth/login", map[string]string{"username": name, "password": testPassword})
	requireStatus(t, r1, 200)
	return cookie(t, r1, cookieName)
}

func TestAccountsAreCaseSensitiveAndDoNotOverwrite(t *testing.T) {
	r := newRig(t)
	r.add(t, "Alice")
	r.add(t, "alice")
	if err := r.s.Add(bg, "Alice", testPassword); err == nil {
		t.Fatal("duplicate accepted")
	}
	for _, name := range []string{"Alice", "alice"} {
		c := r.login(t, name)
		res := r.request(t, "GET", "/api/secret", nil, c)
		requireStatus(t, res, 200)
		if !strings.Contains(res.Body.String(), name) {
			t.Fatal(res.Body)
		}
	}
	requireStatus(t, r.request(t, "POST", "/api/auth/login", map[string]string{"username": "ALICE", "password": testPassword}), 401)
	for _, name := range []string{"", "bad name", "é", strings.Repeat("a", 33)} {
		if err := r.s.Add(bg, name, testPassword); err == nil {
			t.Fatalf("invalid name %q", name)
		}
	}
}
func TestPasswordBoundsAndExactBytes(t *testing.T) {
	for _, p := range []string{"", strings.Repeat("a", 7), strings.Repeat("é", 7), strings.Repeat("a", 129), strings.Repeat("é", 129), string([]byte{0xff})} {
		if _, err := passwordHash(p); err == nil {
			t.Fatal("invalid password accepted")
		}
	}
	for _, p := range []string{"12345678", strings.Repeat("é", 8), strings.Repeat("é", 128)} {
		h, err := passwordHash(p)
		if err != nil {
			t.Fatal(err)
		}
		if !passwordMatches(h, p) {
			t.Fatal("valid password did not match")
		}
	}
	p := "  exact spaces and Unicode é  "
	h, err := passwordHash(p)
	if err != nil {
		t.Fatal(err)
	}
	if !passwordMatches(h, p) || passwordMatches(h, strings.TrimSpace(p)) || passwordMatches(h, "  exact spaces and Unicode e\u0301  ") {
		t.Fatal("password normalized")
	}
}
func TestSessionsRestartResetExpiryAndNone(t *testing.T) {
	r := newRig(t)
	r.add(t, "Alice")
	c := r.login(t, "Alice")
	if !c.Secure || !c.HttpOnly || c.Path != "/" || c.Domain != "" || c.SameSite != http.SameSiteLaxMode || c.MaxAge != 2592000 {
		t.Fatalf("cookie %+v", c)
	}
	var stored string
	if err := r.s.DB.R.GetContext(bg, &stored, `SELECT token_hash FROM account_sessions`); err != nil {
		t.Fatal(err)
	}
	if stored == c.Value {
		t.Fatal("raw token stored")
	}
	s2, err := New(r.s.DB, true, testOrigin)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s2.lookupSession(bg, tokenHash(c.Value)); err != nil {
		t.Fatal("restart lost session", err)
	}
	none, _ := New(r.s.DB, false, "")
	if _, err := none.List(bg); err != nil {
		t.Fatal(err)
	}
	if _, err := s2.lookupSession(bg, stored); err != nil {
		t.Fatal("manage-like open revoked session")
	}
	if err := r.s.Reset(bg, "Alice", "another sufficiently long password", false); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, r.request(t, "GET", "/api/secret", nil, c), 401)
	c2 := r.request(t, "POST", "/api/auth/login", map[string]string{"username": "Alice", "password": "another sufficiently long password"})
	requireStatus(t, c2, 200)
	c = cookie(t, c2, cookieName)
	r.s.now = func() time.Time { return time.Now().Add(31 * 24 * time.Hour) }
	requireStatus(t, r.request(t, "GET", "/api/secret", nil, c), 401)
	n, err := none.StartServe(bg)
	if err != nil || n != 1 {
		t.Fatalf("purge %d %v", n, err)
	}
	if rows, err := none.List(bg); err != nil || len(rows) != 1 {
		t.Fatal("purge deleted accounts", err)
	}
}
func TestGenerationRejectsCheckedPasswordAfterResetOrDeletion(t *testing.T) {
	for _, remove := range []bool{false, true} {
		t.Run(map[bool]string{false: "reset", true: "delete"}[remove], func(t *testing.T) {
			r := newRig(t)
			r.add(t, "Alice")
			a, err := r.s.checkPassword(bg, "Alice", testPassword)
			if err != nil {
				t.Fatal(err)
			}
			if remove {
				err = r.s.Delete(bg, "Alice")
			} else {
				err = r.s.Reset(bg, "Alice", "new sufficiently long password", false)
			}
			if err != nil {
				t.Fatal(err)
			}
			err = r.s.DB.Write(bg, func(tx *sqlx.Tx) error { return r.s.createSession(bg, tx, a, randomToken()) })
			if !errors.Is(err, ErrCredentials) {
				t.Fatalf("stale sign-in accepted: %v", err)
			}
		})
	}
}
func TestAuthProtectionNoChallengesAndCache(t *testing.T) {
	r := newRig(t)
	for _, path := range []string{"/api/secret", "/api/version", "/api/events", "/api/library/tracks/1/audio", "/api/downloads/1/log"} {
		res := r.request(t, "GET", path, nil)
		requireStatus(t, res, 401)
		if res.Header().Get("WWW-Authenticate") != "" {
			t.Fatal("basic auth challenge")
		}
	}
	res := r.request(t, "GET", "/api/auth", nil)
	requireStatus(t, res, 200)
	if !strings.Contains(res.Body.String(), `"setup_needed":true`) {
		t.Fatal(res.Body)
	}
	r.add(t, "Alice")
	c := r.login(t, "Alice")
	requireStatus(t, r.request(t, "GET", "/api/cover", nil, c), 200)
	if got := r.request(t, "GET", "/api/cover", nil, c).Header().Get("Cache-Control"); got != "private, max-age=86400" {
		t.Fatal(got)
	}
	req := httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(`{}`))
	req.Header.Set("Origin", "https://evil.example")
	rec := httptest.NewRecorder()
	r.e.ServeHTTP(rec, req)
	requireStatus(t, rec, 403)
	r.s.Enabled = false
	res = r.request(t, "GET", "/api/secret", nil, c)
	requireStatus(t, res, 200)
	if !strings.Contains(res.Body.String(), "Anonymous") {
		t.Fatal(res.Body)
	}
	requireStatus(t, r.request(t, "POST", "/api/auth/login", map[string]string{}), 404)
}
func TestAttemptLimitAndBoundedStorage(t *testing.T) {
	r := newRig(t)
	for range 5 {
		if !r.s.allow("Alice") {
			t.Fatal("early limit")
		}
	}
	if r.s.allow("Alice") {
		t.Fatal("missing limit")
	}
	if !r.s.allow("alice") {
		t.Fatal("case-folded rate limit")
	}
	for i := range 1200 {
		r.s.allow(strings.Repeat("x", i+1))
	}
	if len(r.s.limits) > 1024 {
		t.Fatal("unbounded limiters")
	}
}
func TestRevocationCancelsOpenRequestsFromAnotherProcess(t *testing.T) {
	r := newRig(t)
	r.add(t, "Alice")
	c := r.login(t, "Alice")
	started := make(chan context.Context, 1)
	done := make(chan struct{})
	r.e.GET("/api/stream", func(c *echo.Context) error {
		started <- c.Request().Context()
		<-c.Request().Context().Done()
		if _, err := c.Response().Write([]byte("private bytes")); !errors.Is(err, context.Canceled) {
			t.Errorf("write after cancellation: %v", err)
		}
		return nil
	})
	go func() { defer close(done); r.request(t, "GET", "/api/stream", nil, c) }()
	<-started
	// A second service has no in-memory request registry, just like manage.
	other, _ := New(r.s.DB, false, "")
	if err := other.Reset(bg, "Alice", "new sufficiently long password", false); err != nil {
		t.Fatal(err)
	}
	r.s.revalidate(bg)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("stream not canceled")
	}
}
func TestReadyWithoutMigrations(t *testing.T) {
	s, _ := New(dbtest.OpenEmpty(t), false, "")
	if err := s.Ready(bg); err == nil || !strings.Contains(err.Error(), "migrate up") {
		t.Fatal(err)
	}
}

// virtualKey exercises protocol verification with a real P-256 signature.
type virtualKey struct {
	key    *ecdsa.PrivateKey
	id     []byte
	handle []byte
}

func newVirtualKey(t *testing.T) *virtualKey {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return &virtualKey{key: k, id: randomBytes(16)}
}
func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }
func clientData(challenge, kind, origin string) []byte {
	v, _ := json.Marshal(map[string]any{"type": kind, "challenge": challenge, "origin": origin, "crossOrigin": false})
	return v
}
func (v *virtualKey) authData(flags byte, counter uint32) []byte {
	h := sha256.Sum256([]byte("shelf.example.com"))
	b := append([]byte{}, h[:]...)
	b = append(b, flags, 0, 0, 0, 0)
	binary.BigEndian.PutUint32(b[33:], counter)
	return b
}
func (v *virtualKey) registration(t *testing.T, challenge, origin string) map[string]any {
	t.Helper()
	pub, err := cbor.Marshal(map[int]any{1: 2, 3: -7, -1: 1, -2: v.key.X.FillBytes(make([]byte, 32)), -3: v.key.Y.FillBytes(make([]byte, 32))})
	if err != nil {
		t.Fatal(err)
	}
	data := v.authData(0x45, 0)
	data = append(data, make([]byte, 16)...)
	data = append(data, 0, byte(len(v.id)))
	data = append(data, v.id...)
	data = append(data, pub...)
	att, err := cbor.Marshal(map[string]any{"fmt": "none", "attStmt": map[string]any{}, "authData": data})
	if err != nil {
		t.Fatal(err)
	}
	return map[string]any{"id": b64(v.id), "rawId": b64(v.id), "type": "public-key", "clientExtensionResults": map[string]any{}, "response": map[string]any{"clientDataJSON": b64(clientData(challenge, "webauthn.create", origin)), "attestationObject": b64(att), "transports": []string{"internal"}}}
}
func (v *virtualKey) assertion(t *testing.T, challenge, origin string, flags byte) map[string]any {
	t.Helper()
	cd := clientData(challenge, "webauthn.get", origin)
	ad := v.authData(flags, 1)
	h := sha256.Sum256(cd)
	signed := append(append([]byte{}, ad...), h[:]...)
	digest := sha256.Sum256(signed)
	sig, err := ecdsa.SignASN1(rand.Reader, v.key, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return map[string]any{"id": b64(v.id), "rawId": b64(v.id), "type": "public-key", "clientExtensionResults": map[string]any{}, "response": map[string]any{"clientDataJSON": b64(cd), "authenticatorData": b64(ad), "signature": b64(sig), "userHandle": b64(v.handle)}}
}
func challenge(t *testing.T, res *httptest.ResponseRecorder) string {
	t.Helper()
	var v struct {
		PublicKey struct {
			Challenge string `json:"challenge"`
		} `json:"publicKey"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	return v.PublicKey.Challenge
}
func (r *rig) enroll(t *testing.T, c *http.Cookie) *virtualKey {
	t.Helper()
	v := newVirtualKey(t)
	a, err := r.s.account(bg, "Alice")
	if err != nil {
		t.Fatal(err)
	}
	v.handle = a.Handle
	start := r.request(t, "POST", "/api/auth/passkeys/begin", nil, c)
	requireStatus(t, start, 200)
	end := r.request(t, "POST", "/api/auth/passkeys/finish?name=Phone", v.registration(t, challenge(t, start), testOrigin), c, cookie(t, start, ceremonyCookie))
	requireStatus(t, end, 200)
	return v
}
func TestPasskeyRegistrationLoginOwnershipAndRevocation(t *testing.T) {
	r := newRig(t)
	r.add(t, "Alice")
	r.add(t, "Bob")
	c := r.login(t, "Alice")
	v := r.enroll(t, c)
	keys := r.request(t, "GET", "/api/auth/passkeys", nil, c)
	requireStatus(t, keys, 200)
	if !strings.Contains(keys.Body.String(), `"last_used_at":null`) {
		t.Fatal(keys.Body)
	}
	begin := r.request(t, "POST", "/api/auth/passkey/begin", nil)
	requireStatus(t, begin, 200)
	proof := v.assertion(t, challenge(t, begin), testOrigin, 0x05)
	cc := cookie(t, begin, ceremonyCookie)
	res := r.request(t, "POST", "/api/auth/passkey/finish", proof, cc)
	requireStatus(t, res, 200)
	signed := cookie(t, res, cookieName)
	requireStatus(t, r.request(t, "GET", "/api/secret", nil, signed), 200)
	requireStatus(t, r.request(t, "POST", "/api/auth/passkey/finish", proof, cc), 401)
	bob := r.login(t, "Bob")
	id := b64(v.id)
	requireStatus(t, r.request(t, "DELETE", "/api/auth/passkeys/"+id, nil, bob), 404)
	requireStatus(t, r.request(t, "POST", "/api/auth/passkeys/"+id+"/name", map[string]string{"name": "Laptop"}, c), 200)
	requireStatus(t, r.request(t, "DELETE", "/api/auth/passkeys/"+id, nil, c), 200)
	requireStatus(t, r.request(t, "GET", "/api/secret", nil, signed), 401)
}
func TestPasskeyRejectsBadOriginMissingVerificationAndReset(t *testing.T) {
	for _, mode := range []string{"origin", "uv", "reset", "expired", "handle", "rpid", "challenge", "unknown"} {
		t.Run(mode, func(t *testing.T) {
			r := newRig(t)
			r.add(t, "Alice")
			c := r.login(t, "Alice")
			v := r.enroll(t, c)
			begin := r.request(t, "POST", "/api/auth/passkey/begin", nil)
			requireStatus(t, begin, 200)
			origin := testOrigin
			flags := byte(0x05)
			switch mode {
			case "origin":
				origin = "https://evil.example"
			case "uv":
				flags = 0x01
			case "reset":
				if err := r.s.Reset(bg, "Alice", "new sufficiently long password", false); err != nil {
					t.Fatal(err)
				}
			case "expired":
				r.s.now = func() time.Time { return time.Now().Add(6 * time.Minute) }
			case "handle":
				v.handle = randomBytes(32)
			}
			ch := challenge(t, begin)
			if mode == "challenge" {
				ch = b64(randomBytes(32))
			}
			proof := v.assertion(t, ch, origin, flags)
			if mode == "rpid" {
				response := proof["response"].(map[string]any)
				raw, err := base64.RawURLEncoding.DecodeString(response["authenticatorData"].(string))
				if err != nil {
					t.Fatal(err)
				}
				raw[0] ^= 0xff
				response["authenticatorData"] = b64(raw)
			}
			if mode == "unknown" {
				id := b64(randomBytes(16))
				proof["id"] = id
				proof["rawId"] = id
			}
			requireStatus(t, r.request(t, "POST", "/api/auth/passkey/finish", proof, cookie(t, begin, ceremonyCookie)), 401)
		})
	}
}
func TestCeremonyConsumptionIsAtomic(t *testing.T) {
	r := newRig(t)
	r.add(t, "Alice")
	c := r.login(t, "Alice")
	v := r.enroll(t, c)
	start := r.request(t, "POST", "/api/auth/passkey/begin", nil)
	proof := v.assertion(t, challenge(t, start), testOrigin, 0x05)
	cc := cookie(t, start, ceremonyCookie)
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for range 2 {
		wg.Go(func() { codes <- r.request(t, "POST", "/api/auth/passkey/finish", proof, cc).Code })
	}
	wg.Wait()
	close(codes)
	ok := 0
	for code := range codes {
		if code == 200 {
			ok++
		} else if code != 401 {
			t.Fatal(code)
		}
	}
	if ok != 1 {
		t.Fatalf("successful finishes: %d", ok)
	}
}

func TestPendingRegistrationCannotMoveBetweenSessionsOrSurviveReset(t *testing.T) {
	for _, mode := range []string{"other-session", "reset"} {
		t.Run(mode, func(t *testing.T) {
			r := newRig(t)
			r.add(t, "Alice")
			c := r.login(t, "Alice")
			v := newVirtualKey(t)
			start := r.request(t, "POST", "/api/auth/passkeys/begin", nil, c)
			requireStatus(t, start, 200)
			if mode == "other-session" {
				c = r.login(t, "Alice")
			} else {
				if err := r.s.Reset(bg, "Alice", testPassword, false); err != nil {
					t.Fatal(err)
				}
				c = r.login(t, "Alice")
			}
			requireStatus(t, r.request(t, "POST", "/api/auth/passkeys/finish?name=Phone", v.registration(t, challenge(t, start), testOrigin), c, cookie(t, start, ceremonyCookie)), 401)
		})
	}
}
func TestCredentialChangesRequireRecentProofAndEndCurrentSession(t *testing.T) {
	r := newRig(t)
	r.add(t, "Alice")
	c := r.login(t, "Alice")
	now := time.Now().Add(6 * time.Minute)
	r.s.now = func() time.Time { return now }
	requireStatus(t, r.request(t, "POST", "/api/auth/passkeys/begin", nil, c), 403)
	requireStatus(t, r.request(t, "POST", "/api/auth/verify", map[string]string{"password": testPassword}, c), 200)
	requireStatus(t, r.request(t, "POST", "/api/auth/passkeys/begin", nil, c), 200)
	requireStatus(t, r.request(t, "POST", "/api/auth/password", map[string]string{"current_password": testPassword, "password": "another sufficiently long password"}, c), 200)
	requireStatus(t, r.request(t, "GET", "/api/secret", nil, c), 401)
}
func TestPasskeyResetRevokesAllCredentialsAndSessions(t *testing.T) {
	r := newRig(t)
	r.add(t, "Alice")
	c := r.login(t, "Alice")
	r.enroll(t, c)
	if err := r.s.Reset(bg, "Alice", "", true); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, r.request(t, "GET", "/api/secret", nil, c), 401)
	c = r.login(t, "Alice")
	res := r.request(t, "GET", "/api/auth/passkeys", nil, c)
	requireStatus(t, res, 200)
	if !strings.Contains(res.Body.String(), `"passkeys":[]`) {
		t.Fatal(res.Body)
	}
}
func TestResponseControllerFlushStillWorks(t *testing.T) {
	ctx, cancel := context.WithCancel(bg)
	rec := httptest.NewRecorder()
	w := &revokedWriter{ResponseWriter: rec, ctx: ctx}
	if err := http.NewResponseController(w).Flush(); err != nil {
		t.Fatal(err)
	}
	if !rec.Flushed {
		t.Fatal("not flushed")
	}
	cancel()
	if err := http.NewResponseController(w).Flush(); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestPasswordChecksWaitForCapacityAndHonorCancellation(t *testing.T) {
	r := newRig(t)
	r.add(t, "Alice")
	for range cap(r.s.slots) {
		r.s.slots <- struct{}{}
	}
	done := make(chan error, 1)
	go func() { _, err := r.s.checkPassword(bg, "Alice", testPassword); done <- err }()
	select {
	case err := <-done:
		t.Fatalf("rejected instead of waiting: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	<-r.s.slots
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("password check did not get a turn")
	}
	r.s.slots <- struct{}{}
	ctx, cancel := context.WithCancel(bg)
	cancel()
	if _, err := r.s.checkPassword(ctx, "Alice", testPassword); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
	for range cap(r.s.slots) {
		<-r.s.slots
	}
}

func TestPasskeyBudgetIsSeparateFromPasswordUsernames(t *testing.T) {
	r := newRig(t)
	now := time.Now()
	r.s.now = func() time.Time { return now }
	for range 5 {
		if !r.s.allow("user:passkey") {
			t.Fatal("early password limit")
		}
	}
	if r.s.allow("user:passkey") {
		t.Fatal("password budget not consumed")
	}
	for range 10 {
		requireStatus(t, r.request(t, "POST", "/api/auth/passkey/begin", nil), 200)
	}
	requireStatus(t, r.request(t, "POST", "/api/auth/passkey/begin", nil), 429)
	now = now.Add(time.Second)
	requireStatus(t, r.request(t, "POST", "/api/auth/passkey/begin", nil), 200)
	if _, err := r.s.checkPassword(bg, "passkey", testPassword); !errors.Is(err, ErrLimited) {
		t.Fatalf("password limiter namespace: %v", err)
	}
}

func TestRegistrationCanFinishAfterProofExpiresWithinCeremonyLifetime(t *testing.T) {
	r := newRig(t)
	r.add(t, "Alice")
	c := r.login(t, "Alice")
	v := newVirtualKey(t)
	started := time.Now()
	now := started.Add(4*time.Minute + 40*time.Second)
	r.s.now = func() time.Time { return now }
	begin := r.request(t, "POST", "/api/auth/passkeys/begin", nil, c)
	requireStatus(t, begin, 200)
	now = started.Add(5*time.Minute + 10*time.Second)
	requireStatus(t, r.request(t, "POST", "/api/auth/passkeys/finish?name=Phone", v.registration(t, challenge(t, begin), testOrigin), c, cookie(t, begin, ceremonyCookie)), 200)
	// The proof expired, so it cannot authorize a new ceremony.
	requireStatus(t, r.request(t, "POST", "/api/auth/passkeys/begin", nil, c), 403)
}

func TestAgentBearerWorksOnlyUnderAgentAPI(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		t.Run(map[bool]string{true: "simple", false: "none"}[enabled], func(t *testing.T) {
			s, err := New(dbtest.Open(t), enabled, testOrigin)
			if err != nil {
				t.Fatal(err)
			}
			e := httpx.New(httpx.Options{Log: dbtest.Discard})
			e.Use(s.Middleware)
			api := e.Group("/api")
			s.Routes(api)
			ok := func(c *echo.Context) error {
				return c.JSON(200, map[string]any{"author": identity.From(c.Request().Context()), "session": current(c).ID})
			}
			api.GET("/secret", ok)
			api.POST("/library/albums/:id/save", ok)
			api.GET("/inbox", ok)
			api.POST("/agent/v1/runs", ok)
			if enabled {
				if err := s.Add(bg, "Alice", testPassword); err != nil {
					t.Fatal(err)
				}
			}
			send := func(method, path, authz string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
				req := httptest.NewRequest(method, path, strings.NewReader(`{}`))
				req.Header.Set("Content-Type", "application/json")
				if authz != "" {
					req.Header.Set("Authorization", authz)
				}
				for _, c := range cookies {
					req.AddCookie(c)
				}
				rec := httptest.NewRecorder()
				e.ServeHTTP(rec, req)
				return rec
			}
			for _, authz := range []string{"Bearer shelf_agent_x", "bearer shelf_agent_x", "BEARER x"} {
				for _, p := range [][2]string{{"GET", "/api/secret"}, {"POST", "/api/library/albums/1/save"}, {"GET", "/api/inbox"},
					{"GET", "/api/auth"}, {"POST", "/api/auth/login"}, {"GET", "/api/downloads"}} {
					res := send(p[0], p[1], authz)
					if res.Code != 401 || !strings.Contains(res.Body.String(), "agent credentials only work under /api/agent") {
						t.Fatalf("%s %s with %q: %d %s", p[0], p[1], authz, res.Code, res.Body)
					}
				}
			}
			// The gateway's basic auth header is not an agent credential.
			if !enabled {
				requireStatus(t, send("GET", "/api/secret", "Basic dXNlcjpwYXNz"), 200)
			}
			// Under /api/agent the platform sets no author and reads no
			// session; the agents module authenticates.
			var cookies []*http.Cookie
			if enabled {
				r := &rig{s, e}
				cookies = append(cookies, r.login(t, "Alice"))
			}
			for _, authz := range []string{"Bearer shelf_agent_x", ""} {
				res := send("POST", "/api/agent/v1/runs", authz, cookies...)
				requireStatus(t, res, 200)
				if !strings.Contains(res.Body.String(), `"author":{"id":"","name":""}`) || !strings.Contains(res.Body.String(), `"session":0`) {
					t.Fatalf("agent path saw a person: %s", res.Body)
				}
			}
		})
	}
}

func TestAccountsPort(t *testing.T) {
	r := newRig(t)
	if !r.s.AuthenticationEnabled() {
		t.Fatal("simple is not enabled")
	}
	none, _ := New(r.s.DB, false, "")
	if none.AuthenticationEnabled() {
		t.Fatal("none is enabled")
	}
	r.add(t, "Alice")
	rows, err := r.s.List(bg)
	if err != nil || len(rows) != 1 {
		t.Fatal(rows, err)
	}
	id := rows[0].ID
	if ok, err := r.s.Exists(bg, id); !ok || err != nil {
		t.Fatal(ok, err)
	}
	if err := r.s.Delete(bg, "Alice"); err != nil {
		t.Fatal(err)
	}
	r.add(t, "Alice")
	if ok, err := r.s.Exists(bg, id); ok || err != nil {
		t.Fatal("a deleted account's ID came back", ok, err)
	}
}
