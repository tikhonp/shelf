// Package applemusic is the Apple Music source. Its catalog client searches
// and reads Apple's web catalog API, its amdl runner downloads with the
// upstream amdl binary, and its wrapper client follows and signs in the
// shelf-wrapper container that amdl decrypts through. It serves
// /api/applemusic/wrapper for the Status page. docs/sources/apple-music.md
// has the details.
package applemusic

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/tikhonp/shelf/internal/modules/applemusic/amdl"
	"github.com/tikhonp/shelf/internal/modules/applemusic/wrapper"
	"github.com/tikhonp/shelf/internal/platform/hub"
	"github.com/tikhonp/shelf/internal/platform/module"
	"github.com/tikhonp/shelf/internal/source"
	"github.com/tikhonp/shelf/internal/wrapperapi"
)

// ID is the source's ID, and Name the module's.
const (
	ID   = "applemusic"
	Name = "applemusic"
)

// SourceTopic is the hub topic of source events.
const SourceTopic = "source"

// Timings of the wrapper's state.
const (
	// PollEvery is how often Run reads the wrapper's state.
	PollEvery = 15 * time.Second
	// PollBusy is how often while the wrapper starts or signs in.
	PollBusy = 2 * time.Second
	// wrapperTimeout bounds one call to the wrapper.
	wrapperTimeout = 10 * time.Second
	// killAfter is how long amdl's group has after SIGTERM.
	killAfter = 5 * time.Second
)

// Module is the Apple Music module. It registers its source switched off
// when SHELF_AMDL_CONFIG is empty or the file is unusable.
type Module struct {
	// AMDL is the amdl binary, from PATH by default. HTTP is the catalog's
	// client. PollEvery and PollBusy default to the constants. Tests change
	// them before Init.
	AMDL      string
	HTTP      *http.Client
	PollEvery time.Duration
	PollBusy  time.Duration

	src     *Source
	log     *slog.Logger
	hub     *hub.Hub
	wrapper *wrapper.Client
	kick    chan struct{}
}

// New returns the module.
func New() *Module {
	return &Module{
		AMDL: "amdl", HTTP: &http.Client{}, PollEvery: PollEvery, PollBusy: PollBusy,
		src: &Source{off: true}, kick: make(chan struct{}, 1),
	}
}

func (*Module) Name() string               { return Name }
func (*Module) Migrations() fs.FS          { return nil }
func (m *Module) Sources() []source.Source { return []source.Source{m.src} }

// Source is the module's source, for tests.
func (m *Module) Source() *Source { return m.src }

// Init reads the base amdl config and builds the clients. It reaches no
// network: manage runs Init too.
func (m *Module) Init(d module.Deps) error {
	m.log, m.hub = d.Log, d.Hub
	cfg := d.Cfg.Apple
	if !cfg.Enabled() {
		return nil
	}
	m.wrapper = wrapper.New(cfg.WrapperControlURL, cfg.WrapperURL, cfg.WrapperToken, &http.Client{Timeout: wrapperTimeout})
	dir := filepath.Join(d.Cfg.DataDir, "amdl")
	s := m.src
	s.dir = cfg.Dir
	s.render = &amdl.Renderer{
		BasePath: cfg.AMDLConfig, Path: filepath.Join(dir, "config.yaml"),
		Settings: amdl.Settings{LiteServer: cfg.WrapperURL.String(), Folder: filepath.Join(d.Cfg.MusicDir, cfg.Dir)},
	}
	base, err := s.render.Load()
	if err != nil {
		m.log.Error("applemusic: Apple Music is off", "error", err)
		return nil
	}
	s.base.Store(&base)
	s.runner = &amdl.Runner{Bin: m.AMDL, Config: s.render.Path, WorkDir: dir, Timeout: cfg.AMDLTimeout, KillAfter: killAfter}
	s.store = &storefront{regions: m.wrapper.Regions, fallback: func() string { return s.baseNow().Storefront }, now: time.Now, ttl: storefrontTTL}
	s.catalog = newCatalog(m.HTTP, s.store.get, func() string { return s.baseNow().Language })
	s.off = false
	return nil
}

// Source is the Apple Music source.
type Source struct {
	off bool
	// dir is the folder amdl writes in, relative to the music root.
	dir     string
	catalog *Catalog
	runner  *amdl.Runner
	render  *amdl.Renderer
	store   *storefront
	base    atomic.Pointer[amdl.Base]

	mu       sync.Mutex
	known    bool
	wrapper  wrapperapi.Status
	wrapErr  error
	lastSent *SourceEvent
}

func (s *Source) ID() string    { return ID }
func (s *Source) Name() string  { return "Apple Music" }
func (s *Source) Enabled() bool { return !s.off }

func (s *Source) baseNow() amdl.Base {
	if b := s.base.Load(); b != nil {
		return *b
	}
	return amdl.Base{}
}

// Health is the wrapper's state as Run last read it.
func (s *Source) Health(context.Context) source.Health {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.healthLocked()
}

// Texts of the source's health.
const (
	textWaiting   = "Waiting for the wrapper"
	textNoAnswer  = "The wrapper does not answer"
	textBadToken  = "The wrapper refused Shelf's token. Check SHELF_WRAPPER_TOKEN."
	textSignedOut = "The wrapper is signed out"
	textStarting  = "The wrapper is starting"
	textSigningIn = "Signing in"
	textAwaiting  = "Waiting for the code"
)

func (s *Source) healthLocked() source.Health {
	switch {
	case !s.known:
		return source.Health{Message: textWaiting}
	case s.wrapErr != nil:
		var ae *wrapper.APIError
		if errors.As(s.wrapErr, &ae) && ae.Status == http.StatusUnauthorized {
			return source.Health{Message: textBadToken}
		}
		return source.Health{Message: textNoAnswer}
	}
	st := s.wrapper
	h := source.Health{Detail: st}
	switch st.State {
	case wrapperapi.Running:
		h.OK, h.Message = true, "Signed in, storefront "+strings.ToUpper(st.Storefront)
	case wrapperapi.SignedOut:
		h.Message = textSignedOut
	case wrapperapi.Starting:
		h.Message = textStarting
	case wrapperapi.SigningIn:
		h.Message = textSigningIn
	case wrapperapi.AwaitingCode:
		h.Message = textAwaiting
	default:
		h.Message = st.Error
	}
	return h
}

// SourceEvent is the hub's source event.
type SourceEvent struct {
	ID      string             `json:"id"`
	OK      bool               `json:"ok"`
	Message string             `json:"message"`
	Wrapper *wrapperapi.Status `json:"wrapper"`
}

// seen records a read of the wrapper's state and returns the event to
// publish when the health changed.
func (s *Source) seen(st wrapperapi.Status, err error) *SourceEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.known, s.wrapErr = true, err
	if err == nil {
		s.wrapper = st
	}
	h := s.healthLocked()
	e := &SourceEvent{ID: ID, OK: h.OK, Message: h.Message}
	if err == nil {
		w := st
		e.Wrapper = &w
	}
	if s.lastSent != nil && sameEvent(*s.lastSent, *e) {
		return nil
	}
	s.lastSent = e
	return e
}

func sameEvent(a, b SourceEvent) bool {
	if a.OK != b.OK || a.Message != b.Message || (a.Wrapper == nil) != (b.Wrapper == nil) {
		return false
	}
	if a.Wrapper == nil {
		return true
	}
	x, y := *a.Wrapper, *b.Wrapper
	return x.State == y.State && x.Storefront == y.Storefront && x.Error == y.Error && x.Since.Equal(y.Since) &&
		(x.CodeDeadline == nil) == (y.CodeDeadline == nil)
}

// Run follows the wrapper's state, faster while it starts or signs in, and
// publishes a source event when it changes.
func (m *Module) Run(ctx context.Context) error {
	if m.wrapper == nil {
		return nil
	}
	t := time.NewTimer(0)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
		case <-m.kick:
		}
		st, err := m.readState(ctx)
		if ctx.Err() != nil {
			return nil
		}
		m.record(st, err)
		every := m.PollEvery
		if err == nil && st.State != wrapperapi.Running && st.State != wrapperapi.SignedOut && st.State != wrapperapi.Error {
			every = m.PollBusy
		}
		if !t.Stop() {
			select {
			case <-t.C:
			default:
			}
		}
		t.Reset(every)
	}
}

func (m *Module) readState(ctx context.Context) (wrapperapi.Status, error) {
	ctx, cancel := context.WithTimeout(ctx, wrapperTimeout)
	defer cancel()
	return m.wrapper.State(ctx)
}

func (m *Module) record(st wrapperapi.Status, err error) {
	if e := m.src.seen(st, err); e != nil {
		if err != nil {
			m.log.Warn("applemusic: wrapper state", "error", err)
		}
		m.hub.Publish(SourceTopic, *e)
	}
}

// Routes serves /api/applemusic/wrapper.
func (m *Module) Routes(api *echo.Group) {
	g := api.Group("/applemusic/wrapper")
	g.GET("", m.getState)
	g.POST("/login", m.postLogin)
	g.POST("/code", m.postCode)
	g.POST("/restart", m.postRestart)
	g.GET("/log", m.getLog)
}

const textOff = "Apple Music is not set up in Shelf"

func (m *Module) client() (*wrapper.Client, error) {
	if m.wrapper == nil {
		return nil, echo.NewHTTPError(http.StatusUnprocessableEntity, textOff)
	}
	return m.wrapper, nil
}

// answer records a state the wrapper gave and sends it, or turns the
// wrapper's error into the client's.
func (m *Module) answer(c *echo.Context, status int, st wrapperapi.Status, err error) error {
	if err != nil {
		if errors.Is(err, wrapper.ErrUnreachable) || isAuth(err) {
			m.record(st, err)
		}
		return m.wrapperError(c.Request().Context(), err)
	}
	m.record(st, nil)
	select {
	case m.kick <- struct{}{}:
	default:
	}
	return c.JSON(status, st)
}

func isAuth(err error) bool {
	var ae *wrapper.APIError
	return errors.As(err, &ae) && ae.Status == http.StatusUnauthorized
}

func (m *Module) wrapperError(ctx context.Context, err error) error {
	var ae *wrapper.APIError
	switch {
	case ctx.Err() != nil:
		return ctx.Err()
	case isAuth(err):
		m.log.Error("applemusic: the wrapper refused shelf's token")
		return echo.NewHTTPError(http.StatusBadGateway, textBadToken)
	case errors.As(err, &ae) && ae.Status < 500:
		return echo.NewHTTPError(ae.Status, ae.Message)
	case errors.Is(err, wrapper.ErrUnreachable):
		m.log.Warn("applemusic: wrapper", "error", err)
		return echo.NewHTTPError(http.StatusServiceUnavailable, textNoAnswer)
	}
	m.log.Warn("applemusic: wrapper", "error", err)
	return echo.NewHTTPError(http.StatusBadGateway, "The wrapper failed")
}

func (m *Module) getState(c *echo.Context) error {
	w, err := m.client()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(c.Request().Context(), wrapperTimeout)
	defer cancel()
	st, err := w.State(ctx)
	return m.answer(c, http.StatusOK, st, err)
}

type loginBody struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// postLogin passes the Apple ID and password to the supervisor. Neither is
// logged or kept.
func (m *Module) postLogin(c *echo.Context) error {
	w, err := m.client()
	if err != nil {
		return err
	}
	var b loginBody
	if err := json.NewDecoder(c.Request().Body).Decode(&b); err != nil || strings.TrimSpace(b.Username) == "" || b.Password == "" {
		return echo.NewHTTPError(http.StatusBadRequest, `send {"username", "password"}`)
	}
	ctx, cancel := context.WithTimeout(c.Request().Context(), wrapperTimeout)
	defer cancel()
	st, err := w.Login(ctx, strings.TrimSpace(b.Username), b.Password)
	return m.answer(c, http.StatusAccepted, st, err)
}

type codeBody struct {
	Code string `json:"code"`
}

func (m *Module) postCode(c *echo.Context) error {
	w, err := m.client()
	if err != nil {
		return err
	}
	var b codeBody
	if err := json.NewDecoder(c.Request().Body).Decode(&b); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, `send {"code"}`)
	}
	ctx, cancel := context.WithTimeout(c.Request().Context(), wrapperTimeout)
	defer cancel()
	st, err := w.Code(ctx, strings.TrimSpace(b.Code))
	return m.answer(c, http.StatusOK, st, err)
}

func (m *Module) postRestart(c *echo.Context) error {
	w, err := m.client()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(c.Request().Context(), wrapperTimeout)
	defer cancel()
	st, err := w.Restart(ctx)
	return m.answer(c, http.StatusAccepted, st, err)
}

// getLog answers the wrapper's log lines after ?after=, for the Status
// page's log tail.
func (m *Module) getLog(c *echo.Context) error {
	w, err := m.client()
	if err != nil {
		return err
	}
	var after int64
	if v := c.QueryParam("after"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			return echo.NewHTTPError(http.StatusBadRequest, "after must be a line number")
		}
		after = n
	}
	ctx, cancel := context.WithTimeout(c.Request().Context(), wrapperTimeout)
	defer cancel()
	page, err := w.Log(ctx, after)
	if err != nil {
		return m.wrapperError(c.Request().Context(), err)
	}
	if page.Lines == nil {
		page.Lines = []wrapperapi.LogLine{}
	}
	return c.JSON(http.StatusOK, page)
}
