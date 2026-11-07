// Package httpx builds shelf's HTTP listener: the Echo app, its middleware
// chain and /healthz. The platform adds /api and the UI on top.
package httpx

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
	"github.com/tikhonp/shelf/internal/platform/identity"
	"github.com/tikhonp/shelf/internal/platform/web"
)

// BodyLimit is the largest request body, except on the routes in
// Options.BodyLimits. The API takes mostly small JSON bodies.
const BodyLimit = 1 << 20

// ShutdownGrace is how long Run waits for open requests after ctx ends.
// http.Server.Shutdown doesn't cancel a running request, so an event stream
// has to end on serve's context itself, or every stop takes this long.
const ShutdownGrace = 15 * time.Second

// Health reports whether the process can serve. It must be quick and reveal
// nothing.
type Health func(ctx context.Context) error

// Options configure New.
type Options struct {
	Log *slog.Logger
	// PublicURL, when set, is an origin that unsafe requests may come from.
	PublicURL *url.URL
	Health    Health
	// BodyLimits raises the body limit of single routes, keyed by the
	// route's path as registered, such as "/api/library/covers".
	BodyLimits map[string]int64
}

// New returns the Echo app with the shared middleware and /healthz.
func New(o Options) *echo.Echo {
	e := echo.New()
	e.Logger = o.Log
	e.HTTPErrorHandler = web.ErrorHandler

	// Before RemoveTrailingSlash, which would hide a path's own spelling.
	e.Pre(pathGuard)
	e.Pre(middleware.RemoveTrailingSlash())
	e.Use(middleware.RequestID())
	e.Use(RequestLogger(o.Log))
	// Inside the logger, so a panic still leaves its request line.
	e.Use(middleware.Recover())
	e.Use(web.SecurityHeaders())
	e.Use(web.CrossOrigin(o.PublicURL))
	e.Use(bodyLimit(o.BodyLimits))

	e.GET("/healthz", healthz(o.Health))
	e.HEAD("/healthz", healthz(o.Health)) // uptime monitors often ask with HEAD
	return e
}

// pathGuard refuses an /api path with dot segments, empty segments or an
// unusual escaping. nginx matches locations on the normalized URI but
// forwards the raw one, so /api/agent/../library/... would pass the
// agent-only location without basic auth and reach an ordinary route here.
func pathGuard(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		u := c.Request().URL
		if !underAPI(u.Path) && !underAPI(u.RawPath) {
			return next(c)
		}
		if !CleanPath(u.Path) || u.RawPath != "" && u.RawPath != (&url.URL{Path: u.Path}).EscapedPath() {
			return echo.NewHTTPError(http.StatusBadRequest, "the path has dot segments, empty segments or unusual escapes")
		}
		return next(c)
	}
}

func underAPI(p string) bool { return p == "/api" || strings.HasPrefix(p, "/api/") }

// CleanPath reports a path without empty, "." or ".." segments. One
// trailing slash is fine; RemoveTrailingSlash drops it.
func CleanPath(p string) bool {
	if !strings.HasPrefix(p, "/") {
		return false
	}
	p = strings.TrimSuffix(p[1:], "/")
	if p == "" {
		return true
	}
	for seg := range strings.SplitSeq(p, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
	}
	return true
}

// bodyLimit applies BodyLimit, or a route's own limit. It runs after the
// router, so the route's path is known.
func bodyLimit(routes map[string]int64) echo.MiddlewareFunc {
	def := middleware.BodyLimit(BodyLimit)
	own := map[string]echo.MiddlewareFunc{}
	for path, n := range routes {
		own[path] = middleware.BodyLimit(n)
	}
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		small := def(next)
		large := map[string]echo.HandlerFunc{}
		for path, mw := range own {
			large[path] = mw(next)
		}
		return func(c *echo.Context) error {
			if h, ok := large[c.Path()]; ok {
				return h(c)
			}
			return small(c)
		}
	}
}

func healthz(h Health) echo.HandlerFunc {
	return func(c *echo.Context) error {
		c.Response().Header().Set("Cache-Control", "no-store")
		if h != nil {
			ctx, cancel := context.WithTimeout(c.Request().Context(), 2*time.Second)
			defer cancel()
			if err := h(ctx); err != nil {
				c.Logger().Warn("health check failed", "error", err)
				return c.String(http.StatusServiceUnavailable, "unavailable\n")
			}
		}
		return c.String(http.StatusOK, "ok\n")
	}
}

// clientGone reports a request that failed because the client left, such as a
// search the browser aborted for a newer one. shelf did nothing wrong.
func clientGone(c *echo.Context, err error) bool {
	return errors.Is(err, context.Canceled) && c.Request().Context().Err() != nil
}

// RequestLogger logs one line per request. It logs /healthz only when it
// fails, or Docker's checks would fill the log.
func RequestLogger(log *slog.Logger) echo.MiddlewareFunc {
	return middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
		LogLatency: true, LogRemoteIP: true, LogMethod: true, LogURIPath: true, LogStatus: true, LogRequestID: true,
		HandleError: true,
		LogValuesFunc: func(c *echo.Context, v middleware.RequestLoggerValues) error {
			attrs := []any{"method", v.Method, "path", v.URIPath, "status", v.Status,
				"latency_ms", v.Latency.Milliseconds(), "ip", v.RemoteIP, "request_id", v.RequestID}
			if a := identity.From(c.Request().Context()); a.ID != "" {
				attrs = append(attrs, "author_id", a.ID, "author", a.Name)
			}
			if v.Error != nil {
				attrs = append(attrs, "error", v.Error.Error())
			}
			switch {
			case clientGone(c, v.Error):
				log.Info("request", attrs...)
			case v.Status >= 500:
				log.Error("request", attrs...)
			case v.Status >= 400:
				log.Warn("request", attrs...)
			case v.URIPath != "/healthz":
				log.Info("request", attrs...)
			}
			return nil
		},
	})
}

// Run serves on addr until ctx ends, then drains open requests for up to
// ShutdownGrace.
func Run(ctx context.Context, e *echo.Echo, addr string, listening func(net.Addr)) error {
	sc := echo.StartConfig{
		Address: addr, HideBanner: true, HidePort: true,
		GracefulTimeout:  ShutdownGrace,
		ListenerAddrFunc: listening,
	}
	if err := sc.Start(ctx, e); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
