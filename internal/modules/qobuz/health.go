package qobuz

import (
	"context"
	"errors"
	"net/http"

	qobuzdl "github.com/tikhonp/qobuz-dl-go"
	"github.com/tikhonp/shelf/internal/source"
)

// Texts of the source's health.
const (
	textOff      = "not set up"
	textStarting = "starting"
	textExpired  = "token expired"
	textNoAnswer = "no answer, trying again every 5 minutes"
	textProxy    = ", through the proxy"
)

type phase int

const (
	phaseStarting phase = iota
	phaseUp
	phaseDown
	phaseExpired
)

// Detail is the health's detail while Qobuz is up.
type Detail struct {
	// Label is the subscription, such as "Studio".
	Label string `json:"label"`
	Proxy bool   `json:"proxy"`
}

// Health says what the last start or call found.
func (s *Source) Health(context.Context) source.Health {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.healthLocked()
}

func (s *Source) healthLocked() source.Health {
	if s.off {
		return source.Health{Message: textOff}
	}
	switch s.phase {
	case phaseUp:
		label := s.api.Label()
		msg := label
		if s.cfg.UseProxy {
			msg += textProxy
		}
		return source.Health{OK: true, Message: msg, Detail: Detail{Label: label, Proxy: s.cfg.UseProxy}}
	case phaseDown:
		return source.Health{Message: textNoAnswer}
	case phaseExpired:
		return source.Health{Message: textExpired}
	}
	return source.Health{Message: textStarting}
}

// SourceEvent is the hub's source event.
type SourceEvent struct {
	ID      string `json:"id"`
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

// startEnded records how a start ended and lets waiting calls go on.
func (s *Source) startEnded(err error) {
	switch {
	case err == nil:
		s.set(phaseUp)
	case isExpired(err):
		s.set(phaseExpired)
	default:
		s.set(phaseDown)
	}
	s.once.Do(func() { close(s.started) })
}

// set moves to p, publishes a source event when the health changed, and
// says whether it did.
func (s *Source) set(p phase) bool {
	s.mu.Lock()
	s.phase = p
	h := s.healthLocked()
	e := SourceEvent{ID: ID, OK: h.OK, Message: h.Message}
	if s.lastSent != nil && *s.lastSent == e {
		s.mu.Unlock()
		return false
	}
	s.lastSent = &e
	s.mu.Unlock()
	s.hub.Publish(SourceTopic, e)
	return true
}

func isExpired(err error) bool {
	var ae *qobuzdl.APIError
	return errors.Is(err, qobuzdl.ErrAuthentication) || (errors.As(err, &ae) && ae.Status == http.StatusUnauthorized)
}

// failed turns a port error into shelf's. A 401 means the token expired,
// which stays until shelf restarts with a new one, and a 404 is an unknown
// item.
func (s *Source) failed(err error) error {
	var ae *qobuzdl.APIError
	switch {
	case err == nil:
		return nil
	case isExpired(err):
		if s.set(phaseExpired) {
			s.log.Error("qobuz: the token expired. Replace SHELF_QOBUZ_TOKEN and restart shelf.")
		}
		return &source.Unavailable{Reason: textExpired}
	case errors.As(err, &ae) && ae.Status == http.StatusNotFound:
		return source.ErrNotFound
	}
	return err
}
