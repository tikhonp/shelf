// Package web holds what every HTTP handler shares: the error answer, the
// security headers and server-sent events.
package web

import (
	"errors"
	"net/http"
	"strings"

	"github.com/labstack/echo/v5"
)

// IsAPI reports whether path is under /api, where every answer is JSON.
func IsAPI(path string) bool { return path == "/api" || strings.HasPrefix(path, "/api/") }

// ErrorBody is the JSON answer of a failed /api request.
type ErrorBody struct {
	Error string `json:"error"`
}

// ErrorHandler answers a failed request with {"error": "..."} under /api and
// plain text elsewhere. The client sees only an *echo.HTTPError's own
// message. Any other error may name a path or carry a token, so the client
// gets the status text and the request log gets the error.
func ErrorHandler(c *echo.Context, err error) {
	if r, _ := echo.UnwrapResponse(c.Response()); r != nil && r.Committed {
		return
	}
	code := echo.StatusCode(err)
	if code == 0 {
		code = http.StatusInternalServerError
	}
	msg := http.StatusText(code)
	var he *echo.HTTPError
	if errors.As(err, &he) && he.Message != "" {
		msg = he.Message
	}
	if c.Request().Method == http.MethodHead {
		_ = c.NoContent(code)
		return
	}
	if IsAPI(c.Request().URL.Path) {
		_ = c.JSON(code, ErrorBody{Error: msg})
		return
	}
	_ = c.String(code, msg+"\n")
}
