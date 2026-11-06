package auth

import (
	"bytes"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/jmoiron/sqlx"
	"github.com/labstack/echo/v5"
	"github.com/tikhonp/shelf/internal/platform/db"
)

type passkey struct {
	ID         string   `db:"id" json:"id"`
	Name       string   `db:"name" json:"name"`
	Credential string   `db:"credential" json:"-"`
	Created    db.Time  `db:"created_at" json:"created_at"`
	Used       *db.Time `db:"last_used_at" json:"last_used_at"`
}

func (s *Service) keys(c *echo.Context, id int64) ([]passkey, error) {
	rows := []passkey{}
	err := s.DB.R.SelectContext(c.Request().Context(), &rows, `SELECT id,name,credential,created_at,last_used_at FROM account_passkeys WHERE account_id=? ORDER BY created_at,id`, id)
	return rows, err
}
func (s *Service) loadCredentials(c *echo.Context, a *Account) error {
	keys, err := s.keys(c, a.ID)
	if err != nil {
		return err
	}
	for _, k := range keys {
		var cred webauthn.Credential
		if err := json.Unmarshal([]byte(k.Credential), &cred); err != nil {
			return err
		}
		a.credentials = append(a.credentials, cred)
	}
	return nil
}
func (s *Service) listPasskeys(c *echo.Context) error {
	keys, err := s.keys(c, current(c).ID)
	if err != nil {
		return err
	}
	return c.JSON(200, map[string]any{"passkeys": keys})
}
func (s *Service) saveCeremony(c *echo.Context, v ceremony) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	for k, x := range s.ceremonies {
		if !x.Expires.After(now) {
			delete(s.ceremonies, k)
		}
	}
	if len(s.ceremonies) >= 256 {
		return "", ErrLimited
	}
	// One pending ceremony per browser prevents replacing its cookie from leaving unbounded entries.
	if old, err := c.Cookie(ceremonyCookie); err == nil {
		delete(s.ceremonies, tokenHash(old.Value))
	}
	token := randomToken()
	v.Expires = now.Add(proofLife)
	s.ceremonies[tokenHash(token)] = v
	setCookie(c, ceremonyCookie, token, proofLife)
	return token, nil
}
func (s *Service) takeCeremony(c *echo.Context, kind string) (ceremony, error) {
	cookie, err := c.Cookie(ceremonyCookie)
	if err != nil {
		return ceremony{}, ErrCredentials
	}
	s.mu.Lock()
	v, ok := s.ceremonies[tokenHash(cookie.Value)]
	delete(s.ceremonies, tokenHash(cookie.Value))
	s.mu.Unlock()
	setCookie(c, ceremonyCookie, "", -time.Hour)
	if !ok || v.Kind != kind || !v.Expires.After(s.now()) {
		return ceremony{}, ErrCredentials
	}
	if kind == "register" && (current(c).ID != v.AccountID || current(c).TokenHash != v.SessionHash || current(c).Gen != v.Gen) {
		return ceremony{}, ErrCredentials
	}
	return v, nil
}
func (s *Service) registerBegin(c *echo.Context) error {
	if err := s.recent(c); err != nil {
		return err
	}
	a := current(c).Account
	if err := s.loadCredentials(c, &a); err != nil {
		return err
	}
	exclude := []protocol.CredentialDescriptor{}
	for _, cred := range a.credentials {
		exclude = append(exclude, cred.Descriptor())
	}
	options, data, err := s.webauthn.BeginRegistration(a, webauthn.WithExclusions(exclude))
	if err != nil {
		return err
	}
	_, err = s.saveCeremony(c, ceremony{Data: *data, AccountID: a.ID, Gen: a.Gen, SessionHash: current(c).TokenHash, Kind: "register"})
	if err != nil {
		return authError(err)
	}
	return c.JSON(200, options)
}

// The credential name travels in the query so the library can parse the standard WebAuthn response body.
func (s *Service) registerFinish(c *echo.Context) error {
	name := strings.TrimSpace(c.QueryParam("name"))
	if !validKeyName(name) {
		return echo.NewHTTPError(400, "Passkey name must contain 1 to 64 characters")
	}
	v, err := s.takeCeremony(c, "register")
	if err != nil {
		return authError(err)
	}
	a := current(c).Account
	if err := s.loadCredentials(c, &a); err != nil {
		return err
	}
	cred, err := s.webauthn.FinishRegistration(a, v.Data, c.Request())
	if err != nil {
		return echo.NewHTTPError(400, "Passkey registration failed; start again")
	}
	raw, err := json.Marshal(cred)
	if err != nil {
		return err
	}
	id := base64.RawURLEncoding.EncodeToString(cred.ID)
	ctx := c.Request().Context()
	err = s.DB.Write(ctx, func(tx *sqlx.Tx) error {
		if err := checkGeneration(ctx, tx, a); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO account_passkeys(id,account_id,name,credential,created_at) VALUES(?,?,?,?,?)`, id, a.ID, name, string(raw), db.At(s.now()))
		return err
	})
	if err != nil {
		return authError(err)
	}
	return c.JSON(200, map[string]bool{"ok": true})
}
func validKeyName(name string) bool {
	return utf8.ValidString(name) && utf8.RuneCountInString(name) >= 1 && utf8.RuneCountInString(name) <= 64
}
func (s *Service) renamePasskey(c *echo.Context) error {
	if err := s.recent(c); err != nil {
		return err
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.ErrBadRequest
	}
	req.Name = strings.TrimSpace(req.Name)
	if !validKeyName(req.Name) {
		return echo.NewHTTPError(400, "Passkey name must contain 1 to 64 characters")
	}
	v := current(c)
	ctx := c.Request().Context()
	err := s.DB.Write(ctx, func(tx *sqlx.Tx) error {
		if err := checkGeneration(ctx, tx, v.Account); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `UPDATE account_passkeys SET name=? WHERE id=? AND account_id=?`, req.Name, c.Param("id"), v.ID)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		if n != 1 {
			return echo.ErrNotFound
		}
		return nil
	})
	if err != nil {
		return authError(err)
	}
	return c.JSON(200, map[string]bool{"ok": true})
}
func (s *Service) removePasskey(c *echo.Context) error {
	if err := s.recent(c); err != nil {
		return err
	}
	v := current(c)
	ctx := c.Request().Context()
	err := s.DB.Write(ctx, func(tx *sqlx.Tx) error {
		if err := checkGeneration(ctx, tx, v.Account); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `DELETE FROM account_passkeys WHERE id=? AND account_id=?`, c.Param("id"), v.ID)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		if n != 1 {
			return echo.ErrNotFound
		}
		return revokeAccount(ctx, tx, v.ID)
	})
	if err != nil {
		return authError(err)
	}
	setCookie(c, cookieName, "", -time.Hour)
	err = c.JSON(200, map[string]bool{"ok": true})
	s.cancelAccount(v.ID)
	return err
}
func (s *Service) loginBegin(c *echo.Context) error {
	if !s.passkeyLimit.AllowN(s.now(), 1) {
		return authError(ErrLimited)
	}
	options, data, err := s.webauthn.BeginDiscoverableLogin()
	if err != nil {
		return err
	}
	// Unknown-user sign-in captures a generation snapshot, so a reset also invalidates pending sign-ins.
	rows, err := s.List(c.Request().Context())
	if err != nil {
		return err
	}
	gens := map[int64]int64{}
	for _, a := range rows {
		gens[a.ID] = a.Gen
	}
	_, err = s.saveCeremony(c, ceremony{Data: *data, Kind: "login", Generations: gens})
	if err != nil {
		return authError(err)
	}
	return c.JSON(200, options)
}
func (s *Service) loginFinish(c *echo.Context) error {
	v, err := s.takeCeremony(c, "login")
	if err != nil {
		return authError(err)
	}
	var a Account
	var stored string
	_, cred, err := s.webauthn.FinishPasskeyLogin(func(rawID, handle []byte) (webauthn.User, error) {
		id := base64.RawURLEncoding.EncodeToString(rawID)
		var accountID int64
		if err := s.DB.R.GetContext(c.Request().Context(), &accountID, `SELECT account_id FROM account_passkeys WHERE id=?`, id); err != nil {
			return nil, ErrCredentials
		}
		var err error
		a, err = s.accountID(c.Request().Context(), accountID)
		if err != nil {
			return nil, err
		}
		if !bytes.Equal(handle, a.Handle) || v.Generations[a.ID] != a.Gen {
			return nil, ErrCredentials
		}
		if err := s.loadCredentials(c, &a); err != nil {
			return nil, err
		}
		if err := s.DB.R.GetContext(c.Request().Context(), &stored, `SELECT credential FROM account_passkeys WHERE id=? AND account_id=?`, id, a.ID); err != nil {
			return nil, err
		}
		return a, nil
	}, v.Data, c.Request())
	if err != nil || cred == nil || cred.Authenticator.CloneWarning {
		return authError(ErrCredentials)
	}
	raw, err := json.Marshal(cred)
	if err != nil {
		return err
	}
	token := randomToken()
	ctx := c.Request().Context()
	err = s.DB.Write(ctx, func(tx *sqlx.Tx) error {
		if err := checkGeneration(ctx, tx, a); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `UPDATE account_passkeys SET credential=?,last_used_at=? WHERE id=? AND account_id=? AND credential=?`, string(raw), db.At(s.now()), base64.RawURLEncoding.EncodeToString(cred.ID), a.ID, stored)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		if n != 1 {
			return ErrCredentials
		}
		return s.createSession(ctx, tx, a, token)
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			err = ErrCredentials
		}
		return authError(err)
	}
	setCookie(c, cookieName, token, sessionLife)
	return s.statusFor(c, a)
}
