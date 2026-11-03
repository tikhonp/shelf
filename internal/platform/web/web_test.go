package web_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"
	"github.com/tikhonp/shelf/internal/platform/web"
)

func serve(h echo.HandlerFunc, method, path string, header map[string]string) *httptest.ResponseRecorder {
	e := echo.New()
	e.HTTPErrorHandler = web.ErrorHandler
	e.Any("/*", h)
	req := httptest.NewRequest(method, path, nil)
	for k, v := range header {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func TestErrorAnswers(t *testing.T) {
	internal := func(*echo.Context) error { return errors.New("open /data/shelf.db: token abc123") }
	coded := func(*echo.Context) error { return echo.NewHTTPError(http.StatusConflict, "already queued") }
	notFound := func(*echo.Context) error { return echo.ErrNotFound }
	for _, tc := range []struct {
		name         string
		h            echo.HandlerFunc
		method, path string
		code         int
		body         string
		json         bool
	}{
		{"internal error under /api", internal, http.MethodGet, "/api/x", 500, `{"error":"Internal Server Error"}`, true},
		{"internal error elsewhere", internal, http.MethodGet, "/x", 500, "Internal Server Error\n", false},
		{"own message under /api", coded, http.MethodPost, "/api/x", 409, `{"error":"already queued"}`, true},
		{"own message elsewhere", coded, http.MethodGet, "/x", 409, "already queued\n", false},
		{"sentinel under /api", notFound, http.MethodGet, "/api", 404, `{"error":"Not Found"}`, true},
		{"/apiary is not the API", notFound, http.MethodGet, "/apiary", 404, "Not Found\n", false},
		{"HEAD has no body", coded, http.MethodHead, "/api/x", 409, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := serve(tc.h, tc.method, tc.path, nil)
			if rec.Code != tc.code || strings.TrimSpace(rec.Body.String()) != strings.TrimSpace(tc.body) {
				t.Errorf("got %d %q, want %d %q", rec.Code, rec.Body, tc.code, tc.body)
			}
			if ct := rec.Header().Get("Content-Type"); tc.json != strings.HasPrefix(ct, "application/json") {
				t.Errorf("Content-Type = %q", ct)
			}
		})
	}
}

func TestStreamSendsEvents(t *testing.T) {
	var lastID string
	rec := serve(func(c *echo.Context) error {
		lastID = web.LastEventID(c)
		s, err := web.OpenStream(c)
		if err != nil {
			return err
		}
		if err := s.Send("download", "7", map[string]any{"id": 3, "status": "running\nevent: fake"}); err != nil {
			return err
		}
		if err := s.Ping(); err != nil {
			return err
		}
		return s.Send("bad\nname", "8\r\nid: 99", "x")
	}, http.MethodGet, "/api/events", map[string]string{"Last-Event-ID": " 6 "})

	if lastID != "6" {
		t.Errorf("LastEventID = %q", lastID)
	}
	h := rec.Header()
	if h.Get("Content-Type") != "text/event-stream" || h.Get("Cache-Control") != "no-cache" || h.Get("X-Accel-Buffering") != "no" {
		t.Errorf("headers = %v", h)
	}
	if !rec.Flushed {
		t.Error("the stream was never flushed")
	}
	want := "event: download\nid: 7\ndata: {\"id\":3,\"status\":\"running\\nevent: fake\"}\n\n" +
		": keepalive\n\n" +
		"event: badname\nid: 8id: 99\ndata: \"x\"\n\n"
	if rec.Body.String() != want {
		t.Errorf("body:\n%q\nwant:\n%q", rec.Body, want)
	}
}
