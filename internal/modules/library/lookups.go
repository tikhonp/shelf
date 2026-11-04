package library

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/labstack/echo/v5"
	"github.com/tikhonp/shelf/internal/modules/library/tagio"
	"github.com/tikhonp/shelf/internal/platform/db"
)

// How long lookups stay.
const (
	// lookupKeep is how long a found answer serves. Releases change little,
	// and a stale answer only weakens a hint.
	lookupKeep = 30 * 24 * time.Hour
	// missKeep is how long "not found" serves, since new releases and
	// lyrics appear every day.
	missKeep = 24 * time.Hour
	// remoteKeep is how long an offered image can be fetched.
	remoteKeep = 30 * 24 * time.Hour
	// remoteLimit is the largest remote image, the same as an upload.
	remoteLimit = uploadLimit
	// remoteTimeout bounds one image fetch.
	remoteTimeout = 30 * time.Second
)

// lookups is the cache of the clients' answers, in library_lookups.
type lookups struct {
	db  *db.DB
	now func() time.Time
}

func (l lookups) Get(ctx context.Context, key string) (int, []byte, bool) {
	var row struct {
		Status    int     `db:"status"`
		Body      string  `db:"body"`
		FetchedAt db.Time `db:"fetched_at"`
	}
	err := l.db.R.GetContext(ctx, &row, `SELECT status, body, fetched_at FROM library_lookups WHERE url = ?`, key)
	if err != nil {
		return 0, nil, false
	}
	keep := lookupKeep
	if row.Status == http.StatusNotFound {
		keep = missKeep
	}
	if l.now().Sub(row.FetchedAt.Time) > keep {
		return 0, nil, false
	}
	return row.Status, []byte(row.Body), true
}

// Put keeps an answer. A cache that can't write only costs a request
// later, so Put drops a failure.
func (l lookups) Put(ctx context.Context, key string, status int, body []byte) {
	ctx = context.WithoutCancel(ctx)
	_ = l.db.Write(ctx, func(tx *sqlx.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO library_lookups (url, status, body, fetched_at) VALUES (?, ?, ?, ?)
			ON CONFLICT (url) DO UPDATE SET status = excluded.status, body = excluded.body, fetched_at = excluded.fetched_at`,
			key, status, string(body), db.At(l.now()))
		return err
	})
}

// trimLookups removes answers past their use and images no longer offered.
func (s store) trimLookups(ctx context.Context, now time.Time) (int64, error) {
	var n int64
	err := s.db.Write(ctx, func(tx *sqlx.Tx) error {
		r, err := tx.ExecContext(ctx, `DELETE FROM library_lookups WHERE fetched_at < ?`, db.At(now.Add(-lookupKeep)))
		if err != nil {
			return err
		}
		n, _ = r.RowsAffected()
		_, err = tx.ExecContext(ctx, `DELETE FROM library_remote WHERE created_at < ?`, db.At(now.Add(-remoteKeep)))
		return err
	})
	return n, err
}

// remote is an image a suggestion offered.
type remote struct {
	ID      string `db:"id"`
	URL     string `db:"url"`
	Preview string `db:"preview"`
}

// offer records an image a suggestion offers and gives the ID the editor
// names it by.
func (s store) offer(ctx context.Context, full, preview string) (string, error) {
	h := sha256.Sum256([]byte(full))
	id := hex.EncodeToString(h[:16])
	err := s.db.Write(ctx, func(tx *sqlx.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO library_remote (id, url, preview, created_at) VALUES (?, ?, ?, ?)
			ON CONFLICT (id) DO UPDATE SET preview = excluded.preview, created_at = excluded.created_at`,
			id, full, preview, db.Now())
		return err
	})
	return id, err
}

func (s store) remote(ctx context.Context, id string) (remote, error) {
	var r remote
	err := s.db.R.GetContext(ctx, &r, `SELECT id, url, preview FROM library_remote WHERE id = ?`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return r, errNotFound
	}
	return r, err
}

// fetchImage reads a remote image. Only JPEG and PNG pass, as with an
// upload.
func (m *Module) fetchImage(ctx context.Context, u string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, remoteTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", m.userAgent)
	resp, err := m.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("library: fetch an image: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("library: the image answered %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, remoteLimit+1))
	if err != nil {
		return nil, fmt.Errorf("library: fetch an image: %w", err)
	}
	if len(b) > remoteLimit {
		return nil, fmt.Errorf("%w: the image is over 32 MiB", tagio.ErrRefused)
	}
	return b, nil
}

// remoteImage serves the preview of an offered image through shelf, so
// the page's CSP needs no outside host.
func (m *Module) remoteImage(c *echo.Context) error {
	r, err := m.store.remote(c.Request().Context(), c.Param("id"))
	if errors.Is(err, errNotFound) {
		return echo.NewHTTPError(http.StatusNotFound, "no such image")
	}
	if err != nil {
		return err
	}
	b, err := m.fetchImage(c.Request().Context(), r.Preview)
	if err != nil {
		m.log.Warn("library: fetch a suggested cover", "error", err)
		return echo.NewHTTPError(http.StatusBadGateway, "the image can't be fetched")
	}
	info, err := inspect(b)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadGateway, "the image is not a JPEG or PNG")
	}
	c.Response().Header().Set("Cache-Control", "private, max-age=86400")
	return c.Blob(http.StatusOK, info.MIME, b)
}

// remoteCover fetches an offered image in full and keeps it as an upload,
// which a Save then names as the album's cover.
func (m *Module) remoteCover(c *echo.Context) error {
	ctx := c.Request().Context()
	r, err := m.store.remote(ctx, c.Param("id"))
	if errors.Is(err, errNotFound) {
		return echo.NewHTTPError(http.StatusNotFound, "no such image")
	}
	if err != nil {
		return err
	}
	b, err := m.fetchImage(ctx, r.URL)
	if errors.Is(err, tagio.ErrRefused) {
		return echo.NewHTTPError(http.StatusUnprocessableEntity, "the image is over 32 MiB")
	}
	if err != nil {
		m.log.Warn("library: fetch a suggested cover", "error", err)
		return echo.NewHTTPError(http.StatusBadGateway, "the image can't be fetched")
	}
	info, err := inspect(b)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnprocessableEntity, "the image is not a JPEG or PNG")
	}
	if _, err := m.covers.put(b); err != nil {
		return err
	}
	return c.JSON(http.StatusCreated, info)
}
