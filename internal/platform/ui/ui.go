// Package ui serves the Vue app that `npm run build` in web/ writes into
// dist. The repo holds only dist/.keep, so Go builds without a UI build, and
// shelf then serves a placeholder.
package ui

import (
	"embed"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"

	"github.com/labstack/echo/v5"
	"github.com/tikhonp/shelf/internal/platform/web"
)

//go:embed all:dist
var embedded embed.FS

// Dist is the embedded build, rooted at its contents.
var Dist fs.FS

func init() {
	sub, err := fs.Sub(embedded, "dist")
	if err != nil {
		panic(err)
	}
	Dist = sub
}

const (
	// Vite puts a content hash in every name under assets/, so a deploy
	// that changes a file changes its URL.
	immutable = "public, max-age=31536000, immutable"
	// index.html and the files copied from web/public keep their names, so
	// the browser asks again each time.
	revalidate = "no-cache"
)

var contentTypes = map[string]string{
	".js": "text/javascript; charset=utf-8", ".css": "text/css; charset=utf-8", ".html": "text/html; charset=utf-8",
	".svg": "image/svg+xml", ".png": "image/png", ".ico": "image/x-icon", ".woff2": "font/woff2",
	".webmanifest": "application/manifest+json", ".txt": "text/plain; charset=utf-8",
}

// Handler serves fsys as a single-page app. A path under assets/ gets its
// file or a 404. Any other path gets its file from fsys when one exists and
// index.html otherwise, so the Vue router can take it. Handler never serves
// a path under /api.
func Handler(fsys fs.FS) echo.HandlerFunc {
	return func(c *echo.Context) error {
		p := c.Request().URL.Path
		if web.IsAPI(p) {
			return echo.ErrNotFound
		}
		name := strings.TrimPrefix(path.Clean("/"+p), "/")
		if strings.HasPrefix(name, "assets/") {
			if !isFile(fsys, name) {
				return echo.ErrNotFound
			}
			return serve(c, fsys, name, immutable)
		}
		if name != "" && isFile(fsys, name) {
			return serve(c, fsys, name, revalidate)
		}
		if !isFile(fsys, "index.html") {
			c.Response().Header().Set("Cache-Control", "no-store")
			return c.HTML(http.StatusOK, placeholder)
		}
		return serve(c, fsys, "index.html", revalidate)
	}
}

func isFile(fsys fs.FS, name string) bool {
	if !fs.ValidPath(name) {
		return false
	}
	st, err := fs.Stat(fsys, name)
	return err == nil && st.Mode().IsRegular()
}

func serve(c *echo.Context, fsys fs.FS, name, cacheControl string) error {
	b, err := fs.ReadFile(fsys, name)
	if err != nil {
		return err
	}
	ct, ok := contentTypes[path.Ext(name)]
	if !ok {
		if ct = mime.TypeByExtension(path.Ext(name)); ct == "" {
			ct = "application/octet-stream"
		}
	}
	c.Response().Header().Set("Cache-Control", cacheControl)
	return c.Blob(http.StatusOK, ct, b)
}

// placeholder has no inline style, which the CSP would block.
const placeholder = `<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><title>Music Shelf</title></head>
<body>
<h1>Music Shelf</h1>
<p>UI not built. Run <code>npm --prefix web run build</code>, then build shelf again.</p>
</body>
</html>
`
