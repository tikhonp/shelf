// Package navidrome is the library module. It asks Navidrome to scan after
// downloads end, follows the scan, and serves /api/library/scan. It is the
// downloads module's Scanner port.
package navidrome

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/tikhonp/shelf/internal/platform/config"
	"github.com/tikhonp/shelf/internal/platform/hub"
	"github.com/tikhonp/shelf/internal/platform/jobs"
	"github.com/tikhonp/shelf/internal/platform/module"
	"github.com/tikhonp/shelf/internal/source"
)

// Name is the module's name.
const Name = "navidrome"

// Topic is the hub topic of scan events.
const Topic = "scan"

// Defaults of the module's timings.
const (
	// DeferFor is how long a scan job waits when Navidrome is already
	// scanning.
	DeferFor = 30 * time.Second
	// PollEvery is how often a running scan's state is read.
	PollEvery = 10 * time.Second
	// requestTimeout bounds one Subsonic request.
	requestTimeout = 10 * time.Second
	// statusTimeout bounds the live read behind GET /api/library/scan.
	statusTimeout = 5 * time.Second
	// maxPollFailures stops following a scan when Navidrome stops answering,
	// after about five minutes at the default PollEvery.
	maxPollFailures = 30
)

// Module is the library module. With SHELF_NAVIDROME_URL empty it is off:
// downloads start no scan and POST /api/library/scan answers 422.
type Module struct {
	// PollEvery, DeferFor and RetryAfter default to the constants. Tests
	// change them.
	PollEvery  time.Duration
	DeferFor   time.Duration
	RetryAfter time.Duration

	cfg  config.Navidrome
	lib  source.Library
	jobs *jobs.System
	hub  *hub.Hub
	log  *slog.Logger
	// wake starts following a scan. It holds one signal, so a burst of
	// wakes follows once.
	wake chan struct{}
}

// New returns the module.
func New() *Module {
	return &Module{PollEvery: PollEvery, DeferFor: DeferFor, RetryAfter: RetryAfter, wake: make(chan struct{}, 1)}
}

func (*Module) Name() string      { return Name }
func (*Module) Migrations() fs.FS { return nil }

// Init builds the Subsonic client when Navidrome is set up. It reaches no
// network: manage runs Init too.
func (m *Module) Init(d module.Deps) error {
	m.cfg, m.jobs, m.hub, m.log = d.Cfg.Navidrome, d.Jobs, d.Hub, d.Log
	if m.cfg.Enabled() {
		m.lib = NewClient(m.cfg.URL, m.cfg.User, m.cfg.Password, &http.Client{Timeout: requestTimeout})
	}
	return nil
}

// Enabled reports whether Navidrome is set up.
func (m *Module) Enabled() bool { return m.lib != nil }

// Routes serves /api/library/scan.
func (m *Module) Routes(api *echo.Group) {
	api.GET("/library/scan", m.getScan)
	api.POST("/library/scan", m.postScan)
}

// Event is the hub's scan event.
type Event struct {
	Scanning bool       `json:"scanning"`
	Count    int        `json:"count"`
	LastScan *time.Time `json:"last_scan"`
}

func event(st source.ScanStatus) Event {
	e := Event{Scanning: st.Scanning, Count: st.Count}
	if !st.LastScan.IsZero() {
		t := st.LastScan
		e.LastScan = &t
	}
	return e
}

func (m *Module) publish(st source.ScanStatus) { m.hub.Publish(Topic, event(st)) }

// ScanJSON is the answer of GET /api/library/scan: the scan state, and
// whether Navidrome answers, for the Status page.
type ScanJSON struct {
	Event
	Enabled bool   `json:"enabled"`
	OK      bool   `json:"ok"`
	Message string `json:"message"`
	// Address is Navidrome's host and port.
	Address   string `json:"address"`
	LatencyMS int64  `json:"latency_ms"`
}

// Texts of the scan answer.
const (
	textOff       = "Navidrome is not set up in Shelf"
	textNoAnswer  = "Navidrome does not answer"
	textRefusedAs = "Navidrome refused: "
)

// getScan reads the scan state from Navidrome now. A Navidrome that doesn't
// answer is a state to show, not a failed request.
func (m *Module) getScan(c *echo.Context) error {
	if !m.Enabled() {
		return c.JSON(http.StatusOK, ScanJSON{Message: textOff})
	}
	ctx, cancel := context.WithTimeout(c.Request().Context(), statusTimeout)
	defer cancel()
	start := time.Now()
	st, err := m.lib.ScanStatus(ctx)
	out := ScanJSON{Enabled: true, Address: m.cfg.URL.Host, LatencyMS: time.Since(start).Milliseconds()}
	if err != nil {
		if c.Request().Context().Err() != nil {
			return nil
		}
		m.log.Warn("navidrome: scan status", "error", err)
		out.Message = textNoAnswer
		var se *SubsonicError
		if errors.As(err, &se) {
			out.Message = textRefusedAs + se.Message
		}
		return c.JSON(http.StatusOK, out)
	}
	if st.Scanning {
		m.follow()
	}
	out.Event, out.OK = event(st), true
	return c.JSON(http.StatusOK, out)
}

// ScanQueuedJSON is the answer of POST /api/library/scan.
type ScanQueuedJSON struct {
	JobID int64 `json:"job_id"`
}

func (m *Module) postScan(c *echo.Context) error {
	if !m.Enabled() {
		return echo.NewHTTPError(http.StatusUnprocessableEntity, textOff)
	}
	id, err := m.ScanNow(c.Request().Context(), "web")
	if err != nil {
		return err
	}
	return c.JSON(http.StatusAccepted, ScanQueuedJSON{JobID: id})
}
