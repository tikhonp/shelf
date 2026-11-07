package auth

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/labstack/echo/v5"
	"github.com/tikhonp/shelf/internal/platform/db"
	"github.com/tikhonp/shelf/internal/platform/identity"
)

type activeRequest struct {
	hash    string
	account int64
	cancel  context.CancelFunc
}
type revokedWriter struct {
	http.ResponseWriter
	ctx context.Context
}

func (w *revokedWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *revokedWriter) Write(p []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	return w.ResponseWriter.Write(p)
}
func (w *revokedWriter) WriteHeader(code int) {
	if w.ctx.Err() != nil {
		return
	}
	if strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") && w.Header().Get("Cache-Control") == "" {
		w.Header().Set("Cache-Control", "no-store")
	}
	w.ResponseWriter.WriteHeader(code)
}
func (w *revokedWriter) FlushError() error {
	if err := w.ctx.Err(); err != nil {
		return err
	}
	return http.NewResponseController(w.ResponseWriter).Flush()
}
func (s *Service) cancelAccount(id int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for r := range s.active {
		if r.account == id {
			r.cancel()
		}
	}
}
func (s *Service) cancelToken(hash string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for r := range s.active {
		if r.hash == hash {
			r.cancel()
		}
	}
}

// Run checks sessions of requests that remain open after authentication.
func (s *Service) Run(ctx context.Context) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			s.mu.Lock()
			for r := range s.active {
				r.cancel()
			}
			s.mu.Unlock()
			return
		case <-t.C:
			s.revalidate(ctx)
		}
	}
}
func (s *Service) revalidate(ctx context.Context) {
	s.mu.Lock()
	list := make([]*activeRequest, 0, len(s.active))
	for r := range s.active {
		list = append(list, r)
	}
	s.mu.Unlock()
	for start := 0; start < len(list); start += 500 {
		end := min(start+500, len(list))
		hashes := []string{}
		for _, r := range list[start:end] {
			hashes = append(hashes, r.hash)
		}
		query, args, err := sqlx.In(`SELECT s.token_hash FROM account_sessions s JOIN accounts a ON a.id=s.account_id AND a.credential_gen=s.credential_gen WHERE s.expires_at>? AND s.token_hash IN (?)`, db.At(s.now()), hashes)
		found := []string{}
		if err == nil {
			err = s.DB.R.SelectContext(ctx, &found, query, args...)
		}
		valid := map[string]bool{}
		for _, h := range found {
			valid[h] = true
		}
		for _, r := range list[start:end] {
			if err != nil || !valid[r.hash] {
				r.cancel()
			}
		}
	}
}

// StartServe purges sessions only when the server explicitly starts without auth.
func (s *Service) StartServe(ctx context.Context) (int64, error) {
	if s.Enabled {
		return 0, nil
	}
	res, err := s.DB.W.ExecContext(ctx, `DELETE FROM account_sessions`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
func publicRoute(path, method string) bool {
	return method == http.MethodGet && path == "/api/auth" || method == http.MethodPost && (path == "/api/auth/login" || path == "/api/auth/passkey/begin" || path == "/api/auth/passkey/finish")
}

// AgentPath reports a path of the agent API, which authenticates with its
// own bearer tokens and never with a session.
func AgentPath(path string) bool {
	return path == "/api/agent" || strings.HasPrefix(path, "/api/agent/")
}

// bearer reports an Authorization header with the Bearer scheme, in any
// case.
func bearer(h string) bool {
	scheme, _, _ := strings.Cut(strings.TrimSpace(h), " ")
	return strings.EqualFold(scheme, "Bearer")
}

func current(c *echo.Context) session { v, _ := c.Get("account.session").(session); return v }

// Middleware protects all module routes, including media and streams.
func (s *Service) Middleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		path := c.Request().URL.Path
		if path != "/api" && !strings.HasPrefix(path, "/api/") {
			return next(c)
		}
		// An agent's token works only on the agent API, which checks it
		// itself. Anywhere else it is refused, not ignored, so it never
		// passes as an anonymous request in provider none.
		agent := AgentPath(path)
		if !agent && bearer(c.Request().Header.Get("Authorization")) {
			c.Response().Header().Set("Cache-Control", "no-store")
			return echo.NewHTTPError(http.StatusUnauthorized, "agent credentials only work under /api/agent")
		}
		if agent {
			return next(c)
		}
		if strings.HasPrefix(path, "/api/auth") {
			c.Response().Header().Set("Cache-Control", "no-store")
		}
		if !s.Enabled {
			c.SetRequest(c.Request().WithContext(identity.With(c.Request().Context(), identity.Author{ID: "anonymous", Name: "Anonymous"})))
			return next(c)
		}
		var v session
		err := sql.ErrNoRows
		if cookie, e := c.Cookie(cookieName); e == nil && len(cookie.Value) == 43 {
			v, err = s.lookupSession(c.Request().Context(), tokenHash(cookie.Value))
		}
		if err != nil {
			if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			if publicRoute(path, c.Request().Method) {
				return next(c)
			}
			c.Response().Header().Set("Cache-Control", "no-store")
			return echo.NewHTTPError(401, "Sign in to Shelf")
		}
		c.Set("account.session", v)
		ctx, cancel := context.WithCancel(identity.With(c.Request().Context(), identity.Account(v.ID, v.Username)))
		defer cancel()
		r := &activeRequest{hash: v.TokenHash, account: v.ID, cancel: cancel}
		s.mu.Lock()
		s.active[r] = struct{}{}
		s.mu.Unlock()
		defer func() { s.mu.Lock(); delete(s.active, r); s.mu.Unlock() }()
		c.SetRequest(c.Request().WithContext(ctx))
		// Recheck after registration so a concurrent reset cannot miss this request.
		if _, err := s.lookupSession(ctx, v.TokenHash); err != nil {
			c.Response().Header().Set("Cache-Control", "no-store")
			return echo.NewHTTPError(401, "Sign in to Shelf")
		}
		original := c.Response()
		c.SetResponse(&revokedWriter{ResponseWriter: original, ctx: ctx})
		defer c.SetResponse(original)
		return next(c)
	}
}

// Routes registers account endpoints behind the global cross-origin checks.
func (s *Service) Routes(api *echo.Group) {
	api.GET("/auth", s.status)
	g := api.Group("/auth", func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			if !s.Enabled {
				return echo.ErrNotFound
			}
			return next(c)
		}
	})
	g.POST("/login", s.login)
	g.POST("/logout", s.logout)
	g.POST("/password", s.changePassword)
	g.POST("/verify", s.verify)
	g.GET("/passkeys", s.listPasskeys)
	g.POST("/passkeys/begin", s.registerBegin)
	g.POST("/passkeys/finish", s.registerFinish)
	g.POST("/passkeys/:id/name", s.renamePasskey)
	g.DELETE("/passkeys/:id", s.removePasskey)
	g.POST("/passkey/begin", s.loginBegin)
	g.POST("/passkey/finish", s.loginFinish)
}
func (s *Service) status(c *echo.Context) error {
	var a any
	setup := false
	if s.Enabled {
		v := current(c)
		if v.ID != 0 {
			a = struct {
				ID       int64  `json:"id"`
				Username string `json:"username"`
			}{v.ID, v.Username}
		}
		var n int
		if err := s.DB.R.GetContext(c.Request().Context(), &n, `SELECT count(*) FROM accounts`); err != nil {
			return err
		}
		setup = n == 0
	}
	provider := "none"
	if s.Enabled {
		provider = "simple"
	}
	return c.JSON(200, map[string]any{"provider": provider, "account": a, "setup_needed": setup})
}
func authError(err error) error {
	if errors.Is(err, ErrCredentials) || errors.Is(err, ErrNotFound) {
		return echo.NewHTTPError(401, "Incorrect credentials or credentials changed; sign in again")
	}
	if errors.Is(err, ErrLimited) {
		return echo.NewHTTPError(429, ErrLimited.Error())
	}
	return err
}
func (s *Service) checkPassword(ctx context.Context, username, password string) (Account, error) {
	if len(username) > 32 || len(password) > 512 {
		return Account{}, ErrCredentials
	}
	if !s.allow("user:" + username) {
		return Account{}, ErrLimited
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	select {
	case <-ctx.Done():
		return Account{}, ctx.Err()
	default:
	}
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return Account{}, ErrLimited
		}
		return Account{}, ctx.Err()
	}
	a, err := s.account(ctx, username)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return a, err
	}
	if !passwordMatches(a.Hash, password) || err != nil {
		return Account{}, ErrCredentials
	}
	return a, nil
}
func (s *Service) login(c *echo.Context) error {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.ErrBadRequest
	}
	a, err := s.checkPassword(c.Request().Context(), req.Username, req.Password)
	if err != nil {
		return authError(err)
	}
	token := randomToken()
	err = s.DB.Write(c.Request().Context(), func(tx *sqlx.Tx) error { return s.createSession(c.Request().Context(), tx, a, token) })
	if err != nil {
		return authError(err)
	}
	setCookie(c, cookieName, token, sessionLife)
	return s.statusFor(c, a)
}
func (s *Service) statusFor(c *echo.Context, a Account) error {
	c.Set("account.session", session{Account: a})
	return s.status(c)
}
func setCookie(c *echo.Context, name, value string, life time.Duration) {
	age := int(life.Seconds())
	if life < 0 {
		age = -1
	}
	c.SetCookie(&http.Cookie{Name: name, Value: value, Path: "/", HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode, MaxAge: age, Expires: time.Now().Add(life)})
}
func (s *Service) logout(c *echo.Context) error {
	v := current(c)
	_, err := s.DB.W.ExecContext(c.Request().Context(), `DELETE FROM account_sessions WHERE token_hash=?`, v.TokenHash)
	if err != nil {
		return err
	}
	setCookie(c, cookieName, "", -time.Hour)
	err = c.JSON(200, map[string]bool{"ok": true})
	s.cancelToken(v.TokenHash)
	return err
}
func (s *Service) verify(c *echo.Context) error {
	v := current(c)
	var req struct {
		Password string `json:"password"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.ErrBadRequest
	}
	a, err := s.checkPassword(c.Request().Context(), v.Username, req.Password)
	if err != nil {
		return authError(err)
	}
	if a.Gen != v.Gen {
		return authError(ErrCredentials)
	}
	err = s.DB.Write(c.Request().Context(), func(tx *sqlx.Tx) error {
		if err := checkGeneration(c.Request().Context(), tx, a); err != nil {
			return err
		}
		_, err := tx.ExecContext(c.Request().Context(), `UPDATE account_sessions SET verified_at=? WHERE token_hash=?`, db.At(s.now()), v.TokenHash)
		return err
	})
	if err != nil {
		return authError(err)
	}
	return c.JSON(200, map[string]bool{"ok": true})
}
func (s *Service) changePassword(c *echo.Context) error {
	v := current(c)
	var req struct {
		Current  string `json:"current_password"`
		Password string `json:"password"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.ErrBadRequest
	}
	a, err := s.checkPassword(c.Request().Context(), v.Username, req.Current)
	if err != nil {
		return authError(err)
	}
	if a.Gen != v.Gen {
		return authError(ErrCredentials)
	}
	hash, err := passwordHash(req.Password)
	if err != nil {
		return echo.NewHTTPError(400, err.Error())
	}
	ctx := c.Request().Context()
	err = s.DB.Write(ctx, func(tx *sqlx.Tx) error {
		if err := checkGeneration(ctx, tx, a); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE accounts SET password_hash=? WHERE id=?`, hash, a.ID); err != nil {
			return err
		}
		return revokeAccount(ctx, tx, a.ID)
	})
	if err != nil {
		return authError(err)
	}
	setCookie(c, cookieName, "", -time.Hour)
	err = c.JSON(200, map[string]bool{"ok": true})
	s.cancelAccount(a.ID)
	return err
}
func (s *Service) recent(c *echo.Context) error {
	v := current(c)
	if v.ID == 0 {
		return echo.ErrUnauthorized
	}
	if s.now().Sub(v.Verified.Time) > proofLife {
		return echo.NewHTTPError(403, "Confirm your password or sign in with a passkey again")
	}
	return nil
}
