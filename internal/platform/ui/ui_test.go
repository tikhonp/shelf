package ui_test

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/labstack/echo/v5"
	"github.com/tikhonp/shelf/internal/platform/ui"
	"github.com/tikhonp/shelf/internal/platform/web"
)

const index = `<!doctype html><title>Music Shelf</title><script type="module" src="/assets/index-abc123.js"></script>`

var build = fstest.MapFS{
	".keep":                   {},
	"index.html":              {Data: []byte(index)},
	"favicon.svg":             {Data: []byte("<svg/>")},
	"assets/index-abc123.js":  {Data: []byte("console.log(1)")},
	"assets/index-abc123.css": {Data: []byte("body{}")},
}

func get(fsys fs.FS, path string) *httptest.ResponseRecorder {
	e := echo.New()
	e.HTTPErrorHandler = web.ErrorHandler
	e.GET("/*", ui.Handler(fsys))
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestSPAFallback(t *testing.T) {
	for _, p := range []string{"/", "/index.html", "/downloads", "/downloads/42", "/items/qobuz/album/abc",
		"/missing.png", "/assets", "/../../etc/passwd", "/%2e%2e/secret"} {
		rec := get(build, p)
		if rec.Code != http.StatusOK || rec.Body.String() != index {
			t.Errorf("%s: %d %q", p, rec.Code, rec.Body)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
			t.Errorf("%s: Content-Type %q", p, ct)
		}
	}

	rec := get(build, "/favicon.svg")
	if rec.Code != http.StatusOK || rec.Body.String() != "<svg/>" || rec.Header().Get("Content-Type") != "image/svg+xml" {
		t.Errorf("favicon: %d %q %q", rec.Code, rec.Body, rec.Header().Get("Content-Type"))
	}

	for _, p := range []string{"/assets/missing.js", "/assets/index-abc123.js.map", "/api/version", "/api"} {
		if rec := get(build, p); rec.Code != http.StatusNotFound {
			t.Errorf("%s: %d %q", p, rec.Code, rec.Body)
		}
	}
}

func TestCacheHeaders(t *testing.T) {
	for p, want := range map[string]string{
		"/assets/index-abc123.js":  "public, max-age=31536000, immutable",
		"/assets/index-abc123.css": "public, max-age=31536000, immutable",
		"/":                        "no-cache",
		"/downloads":               "no-cache",
		"/favicon.svg":             "no-cache",
	} {
		if got := get(build, p).Header().Get("Cache-Control"); got != want {
			t.Errorf("%s: Cache-Control %q, want %q", p, got, want)
		}
	}
	if ct := get(build, "/assets/index-abc123.js").Header().Get("Content-Type"); ct != "text/javascript; charset=utf-8" {
		t.Errorf("JS Content-Type %q", ct)
	}
	if ct := get(build, "/assets/index-abc123.css").Header().Get("Content-Type"); ct != "text/css; charset=utf-8" {
		t.Errorf("CSS Content-Type %q", ct)
	}
}

func TestMissingUIBuildShowsPlaceholder(t *testing.T) {
	for _, p := range []string{"/", "/downloads"} {
		rec := get(fstest.MapFS{".keep": {}}, p)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "UI not built") {
			t.Errorf("%s: %d %q", p, rec.Code, rec.Body)
		}
		if strings.Contains(rec.Body.String(), "style") {
			t.Error("the placeholder has inline style, which the CSP blocks")
		}
		if rec.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s: the placeholder may be cached", p)
		}
	}
}

func TestDistIsEmbedded(t *testing.T) {
	if _, err := fs.Stat(ui.Dist, ".keep"); err != nil {
		t.Fatalf("dist/.keep is not embedded: %v", err)
	}
}
