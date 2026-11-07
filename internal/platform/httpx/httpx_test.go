package httpx_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"
	"github.com/tikhonp/shelf/internal/platform/httpx"
	"github.com/tikhonp/shelf/internal/platform/web"
)

var discard = slog.New(slog.NewTextHandler(io.Discard, nil))

// app is httpx.New with a route under /api that reads its whole body and a
// route that panics.
func app(t *testing.T, o httpx.Options) *echo.Echo {
	t.Helper()
	if o.Log == nil {
		o.Log = discard
	}
	e := httpx.New(o)
	e.POST("/api/echo", func(c *echo.Context) error {
		b, err := io.ReadAll(c.Request().Body)
		if err != nil {
			return err
		}
		return c.String(http.StatusOK, string(b))
	})
	e.GET("/api/panic", func(*echo.Context) error { panic("boom") })
	e.GET("/api/canceled", func(*echo.Context) error { return context.Canceled })
	return e
}

func do(e *echo.Echo, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func TestHealthzAnswersGetAndHead(t *testing.T) {
	var healthErr error
	e := app(t, httpx.Options{Health: func(context.Context) error { return healthErr }})

	rec := do(e, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "ok\n" {
		t.Fatalf("GET: %d %q", rec.Code, rec.Body)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Error("healthz may be cached")
	}
	rec = do(e, httptest.NewRequest(http.MethodHead, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("HEAD: %d", rec.Code)
	}
	rec = do(e, httptest.NewRequest(http.MethodGet, "/healthz/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("trailing slash: %d", rec.Code)
	}

	healthErr = errors.New("sql: database is closed at /data/shelf.db")
	for _, m := range []string{http.MethodGet, http.MethodHead} {
		rec = do(e, httptest.NewRequest(m, "/healthz", nil))
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s unhealthy: %d", m, rec.Code)
		}
		if strings.Contains(rec.Body.String(), "database") || strings.Contains(rec.Body.String(), "/data") {
			t.Errorf("healthz reveals the error: %q", rec.Body)
		}
	}
}

func TestSecurityHeaders(t *testing.T) {
	e := app(t, httpx.Options{})
	crossSite := httptest.NewRequest(http.MethodPost, "/api/echo", nil)
	crossSite.Header.Set("Sec-Fetch-Site", "cross-site")
	big := httptest.NewRequest(http.MethodPost, "/api/echo", bytes.NewReader(make([]byte, httpx.BodyLimit+1)))
	for name, req := range map[string]*http.Request{
		"ok":          httptest.NewRequest(http.MethodGet, "/healthz", nil),
		"not found":   httptest.NewRequest(http.MethodGet, "/nowhere", nil),
		"cross-site":  crossSite,
		"too big":     big,
		"panic":       httptest.NewRequest(http.MethodGet, "/api/panic", nil),
		"same origin": httptest.NewRequest(http.MethodPost, "/api/echo", strings.NewReader("hi")),
	} {
		t.Run(name, func(t *testing.T) {
			h := do(e, req).Header()
			if got := h.Get("Content-Security-Policy"); got != web.CSP {
				t.Errorf("CSP = %q", got)
			}
			if h.Get("X-Content-Type-Options") != "nosniff" || h.Get("Referrer-Policy") != "no-referrer" {
				t.Errorf("headers = %v", h)
			}
		})
	}
	for _, want := range []string{"default-src 'self'", "img-src 'self' https://static.qobuz.com https://*.mzstatic.com data:",
		"frame-ancestors 'none'", "base-uri 'none'", "form-action 'self'"} {
		if !strings.Contains(web.CSP, want) {
			t.Errorf("CSP misses %q", want)
		}
	}
}

func TestCrossOriginPostRefused(t *testing.T) {
	e := app(t, httpx.Options{PublicURL: &url.URL{Scheme: "https", Host: "shelf.example.com"}})
	post := func(header map[string]string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "http://shelf:8080/api/echo", strings.NewReader("hi"))
		for k, v := range header {
			req.Header.Set(k, v)
		}
		return do(e, req)
	}

	for name, h := range map[string]map[string]string{
		"cross-site fetch": {"Sec-Fetch-Site": "cross-site"},
		"same-site fetch":  {"Sec-Fetch-Site": "same-site"},
		"foreign origin":   {"Origin": "https://evil.example.com"},
	} {
		rec := post(h)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s: %d", name, rec.Code)
			continue
		}
		var body web.ErrorBody
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Error != "cross-origin request refused" {
			t.Errorf("%s: body %q", name, rec.Body)
		}
	}
	for name, h := range map[string]map[string]string{
		"same-origin fetch":  {"Sec-Fetch-Site": "same-origin"},
		"the public origin":  {"Origin": "https://shelf.example.com"},
		"no browser headers": nil,
	} {
		if rec := post(h); rec.Code != http.StatusOK {
			t.Errorf("%s: %d %q", name, rec.Code, rec.Body)
		}
	}

	get := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	get.Header.Set("Sec-Fetch-Site", "cross-site")
	if rec := do(e, get); rec.Code != http.StatusOK {
		t.Errorf("cross-site GET: %d", rec.Code)
	}
}

func TestBodyLimit(t *testing.T) {
	e := app(t, httpx.Options{})
	at := do(e, httptest.NewRequest(http.MethodPost, "/api/echo", bytes.NewReader(make([]byte, httpx.BodyLimit))))
	if at.Code != http.StatusOK || at.Body.Len() != httpx.BodyLimit {
		t.Fatalf("1 MiB: %d, %d bytes", at.Code, at.Body.Len())
	}

	over := do(e, httptest.NewRequest(http.MethodPost, "/api/echo", bytes.NewReader(make([]byte, httpx.BodyLimit+1))))
	if over.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("over 1 MiB: %d", over.Code)
	}
	var body web.ErrorBody
	if err := json.Unmarshal(over.Body.Bytes(), &body); err != nil || body.Error == "" {
		t.Errorf("over 1 MiB: body %q", over.Body)
	}

	// Without Content-Length the limit applies while the handler reads.
	req := httptest.NewRequest(http.MethodPost, "/api/echo", io.MultiReader(bytes.NewReader(make([]byte, httpx.BodyLimit)), strings.NewReader("x")))
	req.ContentLength = -1
	if rec := do(e, req); rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("chunked over 1 MiB: %d", rec.Code)
	}
}

func TestRouteBodyLimit(t *testing.T) {
	e := app(t, httpx.Options{BodyLimits: map[string]int64{"/api/big/:id": 3 << 20}})
	e.POST("/api/big/:id", func(c *echo.Context) error {
		b, err := io.ReadAll(c.Request().Body)
		if err != nil {
			return err
		}
		return c.String(http.StatusOK, strconv.Itoa(len(b)))
	})
	if rec := do(e, httptest.NewRequest(http.MethodPost, "/api/big/1", bytes.NewReader(make([]byte, 3<<20)))); rec.Code != http.StatusOK {
		t.Fatalf("3 MiB on its route: %d", rec.Code)
	}
	if rec := do(e, httptest.NewRequest(http.MethodPost, "/api/big/1", bytes.NewReader(make([]byte, 3<<20+1)))); rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("over the route's limit: %d", rec.Code)
	}
	if rec := do(e, httptest.NewRequest(http.MethodPost, "/api/echo", bytes.NewReader(make([]byte, httpx.BodyLimit+1)))); rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("another route: %d", rec.Code)
	}
}

func TestPanicIsA500WithoutDetails(t *testing.T) {
	var logs bytes.Buffer
	e := app(t, httpx.Options{Log: slog.New(slog.NewJSONHandler(&logs, nil))})
	rec := do(e, httptest.NewRequest(http.MethodGet, "/api/panic", nil))
	if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "boom") {
		t.Fatalf("panic: %d %q", rec.Code, rec.Body)
	}
	if !strings.Contains(logs.String(), "boom") {
		t.Errorf("the panic is not logged:\n%s", logs.String())
	}
}

// A client that left is no failure of shelf's, but a canceled context the
// client didn't cause still is.
func TestAClientThatLeftIsNoError(t *testing.T) {
	var logs bytes.Buffer
	e := app(t, httpx.Options{Log: slog.New(slog.NewJSONHandler(&logs, nil))})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	do(e, httptest.NewRequest(http.MethodGet, "/api/canceled", nil).WithContext(ctx))
	if !strings.Contains(logs.String(), `"level":"INFO"`) {
		t.Errorf("an aborted request is not logged as info:\n%s", logs.String())
	}
	logs.Reset()
	do(e, httptest.NewRequest(http.MethodGet, "/api/canceled", nil))
	if !strings.Contains(logs.String(), `"level":"ERROR"`) {
		t.Errorf("a canceled context with the client still there is not an error:\n%s", logs.String())
	}
}

func TestHealthzIsLoggedOnlyWhenItFails(t *testing.T) {
	var logs bytes.Buffer
	var healthErr error
	e := app(t, httpx.Options{Log: slog.New(slog.NewJSONHandler(&logs, nil)), Health: func(context.Context) error { return healthErr }})
	do(e, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if logs.Len() != 0 {
		t.Fatalf("a healthy /healthz is logged:\n%s", logs.String())
	}
	healthErr = errors.New("down")
	do(e, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if !strings.Contains(logs.String(), `"path":"/healthz"`) {
		t.Errorf("a failing /healthz is not logged:\n%s", logs.String())
	}
}

func TestPathGuardRefusesDotSegmentsAndOddEscapes(t *testing.T) {
	e := app(t, httpx.Options{})
	e.GET("/api/agent/v1/runs", func(c *echo.Context) error { return c.String(http.StatusOK, "agent") })
	e.GET("/api/library/albums", func(c *echo.Context) error { return c.String(http.StatusOK, "library") })
	e.GET("/ui/*", func(c *echo.Context) error { return c.String(http.StatusOK, "ui") })
	for _, target := range []string{
		"/api/agent/v1/../../library/albums",
		"/api/library/x/../../agent/v1/runs",
		"/api/agent/v1/./runs",
		"/api/agent//v1/runs",
		"/api//library/albums",
		"/api/agent/v1/runs/.",
		"/api/agent/v1/runs/..",
		"/api/agent/v1/%2e%2e/%2e%2e/library/albums",
		"/api/agent%2Fv1/runs",
		"/api/agent/v1/%72uns",
		"/%61pi/library/albums",
	} {
		rec := do(e, httptest.NewRequest(http.MethodGet, target, nil))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s", target, rec.Code, rec.Body)
		}
	}
	for target, want := range map[string]string{
		"/api/agent/v1/runs":  "agent",
		"/api/agent/v1/runs/": "agent",
		"/api/library/albums": "library",
		"/ui/a/../b":          "ui",
	} {
		rec := do(e, httptest.NewRequest(http.MethodGet, target, nil))
		if rec.Code != http.StatusOK || rec.Body.String() != want {
			t.Errorf("%s: %d %s", target, rec.Code, rec.Body)
		}
	}
	if httpx.CleanPath("api") || !httpx.CleanPath("/") || !httpx.CleanPath("/api/a.b/c..d") {
		t.Error("CleanPath")
	}
}
