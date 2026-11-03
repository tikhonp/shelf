package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v5"
)

// KeepAlive is how often a stream sends a comment while it has nothing to
// say, so the gateway and the browser keep an idle connection open.
const KeepAlive = 15 * time.Second

// Stream writes server-sent events to one client.
type Stream struct {
	w  http.ResponseWriter
	rc *http.ResponseController
}

// OpenStream answers c with an event stream. X-Accel-Buffering keeps the
// gateway's nginx from holding events back.
func OpenStream(c *echo.Context) (*Stream, error) {
	w := c.Response()
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	s := &Stream{w: w, rc: http.NewResponseController(w)}
	return s, s.rc.Flush()
}

// LastEventID is the ID of the last event a reconnecting client saw, or "".
func LastEventID(c *echo.Context) string {
	return strings.TrimSpace(c.Request().Header.Get("Last-Event-ID"))
}

// Send writes one event with v as JSON data. An empty id leaves the client's
// last ID as it was.
func (s *Stream) Send(event, id string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	var b strings.Builder
	if event != "" {
		fmt.Fprintf(&b, "event: %s\n", oneLine(event))
	}
	if id != "" {
		fmt.Fprintf(&b, "id: %s\n", oneLine(id))
	}
	fmt.Fprintf(&b, "data: %s\n\n", data)
	return s.write(b.String())
}

// Ping writes a comment, which clients ignore.
func (s *Stream) Ping() error { return s.write(": keepalive\n\n") }

func (s *Stream) write(text string) error {
	if _, err := s.w.Write([]byte(text)); err != nil {
		return err
	}
	return s.rc.Flush()
}

// oneLine removes line breaks. A newline would end the field, and the rest
// of the text would start a field of its own.
func oneLine(s string) string { return strings.NewReplacer("\r", "", "\n", "").Replace(s) }
