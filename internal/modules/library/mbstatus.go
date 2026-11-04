package library

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/tikhonp/shelf/internal/modules/library/musicbrainz"
)

// MusicBrainzJSON is the answer of GET /api/library/musicbrainz, which
// tells the Status page whether MusicBrainz answers.
type MusicBrainzJSON struct {
	OK        bool      `json:"ok"`
	Message   string    `json:"message"`
	LatencyMS int64     `json:"latency_ms"`
	CheckedAt time.Time `json:"checked_at"`
}

// Texts of the MusicBrainz answer.
const (
	textMBDown    = "MusicBrainz is not answering"
	textMBBusy    = "MusicBrainz is busy"
	textMBFailing = "MusicBrainz answers with errors"
)

const (
	// mbFresh is how long one probe answers for. Every page load and
	// reconnect checks the Status parts, and MusicBrainz allows one
	// request a second.
	mbFresh = 30 * time.Second
	// mbTimeout covers the wait for the limiter as well as the call.
	mbTimeout = 10 * time.Second
)

// mbProbe keeps the last probe. Callers that come during one wait for it
// instead of asking again.
type mbProbe struct {
	mu   sync.Mutex
	last MusicBrainzJSON
}

// getMusicBrainz asks MusicBrainz now, or gives the last probe while it is
// fresh. A MusicBrainz that doesn't answer is a state to show, not a failed
// request.
func (m *Module) getMusicBrainz(c *echo.Context) error {
	return c.JSON(http.StatusOK, m.pingMB(c.Request().Context()))
}

func (m *Module) pingMB(ctx context.Context) MusicBrainzJSON {
	p := &m.mb
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.last.CheckedAt.IsZero() && time.Since(p.last.CheckedAt) < mbFresh {
		return p.last
	}
	// The probe goes on after its caller leaves, since others may wait on it.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), mbTimeout)
	defer cancel()
	took, err := m.MB.Ping(ctx)
	out := MusicBrainzJSON{OK: err == nil, LatencyMS: took.Milliseconds(), CheckedAt: time.Now().UTC()}
	if err != nil {
		m.log.Warn("musicbrainz: ping", "error", err)
		out.Message = textMBDown
		var code *musicbrainz.StatusError
		switch {
		case errors.Is(err, musicbrainz.ErrBusy):
			out.Message = textMBBusy
		case errors.As(err, &code):
			out.Message = textMBFailing
		}
	}
	p.last = out
	return out
}
