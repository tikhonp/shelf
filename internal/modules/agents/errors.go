package agents

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/labstack/echo/v5"
)

// apiError is an answer with a stable code. Handlers return it; h writes it.
// Messages never carry a database error or a host path.
type apiError struct {
	Status int
	Code   string
	Msg    string
	Extra  map[string]any
}

func (e *apiError) Error() string { return e.Code + ": " + e.Msg }

func fail(status int, code, msg string, extra ...any) *apiError {
	e := &apiError{Status: status, Code: code, Msg: msg}
	if len(extra) > 0 {
		e.Extra = map[string]any{}
		for i := 0; i+1 < len(extra); i += 2 {
			e.Extra[extra[i].(string)] = extra[i+1]
		}
	}
	return e
}

func invalid(format string, a ...any) *apiError {
	return fail(http.StatusBadRequest, "invalid", fmt.Sprintf(format, a...))
}
func notFound(what string) *apiError {
	return fail(http.StatusNotFound, "not_found", what+" not found")
}
func conflict(code, msg string, extra ...any) *apiError {
	return fail(http.StatusConflict, code, msg, extra...)
}

// h runs a handler and answers its error as {"error", "code", ...}. An
// error that is not an apiError is logged and answered as a plain 500, so
// SQL and file errors never reach the client.
func (m *Module) h(f echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		c.Response().Header().Set("Cache-Control", "no-store")
		err := f(c)
		if err == nil {
			return nil
		}
		var ae *apiError
		if !errors.As(err, &ae) {
			var he *echo.HTTPError
			// Echo's own errors, such as a path no route matches, carry only a status code.
			var sc echo.HTTPStatusCoder
			switch {
			case errors.As(err, &he) && he.Code < 500:
				ae = fail(he.Code, codeFor(he.Code), he.Message)
			case errors.As(err, &sc) && sc.StatusCode() < 500:
				ae = fail(sc.StatusCode(), codeFor(sc.StatusCode()), http.StatusText(sc.StatusCode()))
			case c.Request().Context().Err() != nil:
				return err
			default:
				m.log.Error("agents: request failed", "path", c.Path(), "error", err)
				ae = fail(http.StatusInternalServerError, "internal", "internal error")
			}
		}
		if ae.Status == http.StatusTooManyRequests {
			if s, ok := ae.Extra["retry_after"].(int); ok {
				c.Response().Header().Set("Retry-After", strconv.Itoa(s))
			}
		}
		body := map[string]any{}
		for k, v := range ae.Extra {
			body[k] = v
		}
		body["error"], body["code"] = ae.Msg, ae.Code
		return c.JSON(ae.Status, body)
	}
}

func codeFor(status int) string {
	switch status {
	case http.StatusNotFound:
		return "not_found"
	case http.StatusUnauthorized:
		return "unauthorized"
	case http.StatusRequestEntityTooLarge:
		return "too_large"
	case http.StatusTooManyRequests:
		return "rate_limited"
	}
	return "invalid"
}

// decode reads a JSON body of at most limit bytes into v. Unknown fields
// are refused, so a misspelt field never silently does nothing.
func decode(c *echo.Context, limit int64, v any) error {
	b, err := io.ReadAll(io.LimitReader(c.Request().Body, limit+1))
	if err != nil {
		return invalid("unreadable body")
	}
	if int64(len(b)) > limit {
		return fail(http.StatusRequestEntityTooLarge, "too_large", fmt.Sprintf("body over %d bytes", limit))
	}
	if len(bytes.TrimSpace(b)) == 0 {
		b = []byte("{}")
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return invalid("bad JSON body: %s", jsonProblem(err))
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return invalid("bad JSON body: trailing data")
	}
	return nil
}

// jsonProblem names what was wrong without echoing the body.
func jsonProblem(err error) string {
	var te *json.UnmarshalTypeError
	var se *json.SyntaxError
	switch {
	case errors.As(err, &te):
		return fmt.Sprintf("field %q has the wrong type", te.Field)
	case errors.As(err, &se):
		return fmt.Sprintf("syntax error at byte %d", se.Offset)
	}
	msg := err.Error()
	if len(msg) > 200 {
		msg = msg[:200]
	}
	return msg
}

func idParam(c *echo.Context, name string) (int64, error) {
	id, err := strconv.ParseInt(c.Param(name), 10, 64)
	if err != nil || id <= 0 {
		return 0, notFound(name)
	}
	return id, nil
}

// intQuery reads an optional integer query parameter.
func intQuery(c *echo.Context, name string, def, lo, hi int64) (int64, error) {
	s := c.QueryParam(name)
	if s == "" {
		return def, nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n < lo || n > hi {
		return 0, invalid("%s must be a number from %d to %d", name, lo, hi)
	}
	return n, nil
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}
