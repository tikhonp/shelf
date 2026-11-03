package web

import (
	"net/http"
	"net/url"

	"github.com/labstack/echo/v5"
)

// CSP allows only shelf's own origin, plus the covers from the sources'
// image hosts, which are the only outside resources the UI shows.
const CSP = "default-src 'self'; img-src 'self' https://static.qobuz.com https://*.mzstatic.com data:; " +
	"style-src 'self'; script-src 'self'; connect-src 'self'; font-src 'self'; frame-ancestors 'none'; " +
	"base-uri 'none'; form-action 'self'"

// SecurityHeaders sets the CSP and the other security headers on every
// answer, errors included.
func SecurityHeaders() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			h := c.Response().Header()
			h.Set("Content-Security-Policy", CSP)
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("Referrer-Policy", "no-referrer")
			return next(c)
		}
	}
}

// CrossOrigin refuses cross-site requests with unsafe methods, using
// Sec-Fetch-Site and Origin. It also trusts the public URL's origin,
// because the gateway may rewrite Host.
func CrossOrigin(publicURL *url.URL) echo.MiddlewareFunc {
	cop := http.NewCrossOriginProtection()
	if publicURL != nil {
		_ = cop.AddTrustedOrigin(publicURL.Scheme + "://" + publicURL.Host)
	}
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			if err := cop.Check(c.Request()); err != nil {
				return echo.NewHTTPError(http.StatusForbidden, "cross-origin request refused")
			}
			return next(c)
		}
	}
}
