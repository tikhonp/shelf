// Package wrapper calls the shelf-wrapper container: the supervisor's
// control API, for the wrapper's state and sign-in, and wrapper-lite's own
// /status, for the account's storefront. The protocol is in
// internal/wrapperapi.
package wrapper

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"github.com/tikhonp/shelf/internal/platform/config"
	"github.com/tikhonp/shelf/internal/wrapperapi"
)

// Client calls one shelf-wrapper container.
type Client struct {
	control *url.URL
	lite    *url.URL
	token   config.Secret
	http    *http.Client
}

// New returns a client. hc should have a timeout: every call here is short.
func New(control, lite *url.URL, token config.Secret, hc *http.Client) *Client {
	return &Client{control: control, lite: lite, token: token, http: hc}
}

// ErrUnreachable is a call that got no answer.
var ErrUnreachable = errors.New("the wrapper does not answer")

// APIError is an answer of the control API that isn't 2xx.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("wrapper answered %d: %s", e.Status, e.Message)
}

// maxAnswer bounds an answer; the log is the largest, at 1,000 lines.
const maxAnswer = 4 << 20

func (c *Client) do(ctx context.Context, method, path string, q url.Values, body, out any) error {
	u := c.control.JoinPath(path)
	u.RawQuery = q.Encode()
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token.Reveal())
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxAnswer))
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	if resp.StatusCode/100 != 2 {
		var eb wrapperapi.ErrorBody
		if json.Unmarshal(data, &eb) != nil || eb.Error == "" {
			eb.Error = http.StatusText(resp.StatusCode)
		}
		return &APIError{Status: resp.StatusCode, Message: eb.Error}
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("the wrapper's answer to %s: %w", path, err)
	}
	return nil
}

// State reads the wrapper's state.
func (c *Client) State(ctx context.Context) (wrapperapi.Status, error) {
	var st wrapperapi.Status
	err := c.do(ctx, http.MethodGet, wrapperapi.PathState, nil, nil, &st)
	return st, err
}

// Login starts a sign-in. The password goes to the supervisor and nowhere
// else.
func (c *Client) Login(ctx context.Context, username, password string) (wrapperapi.Status, error) {
	var st wrapperapi.Status
	err := c.do(ctx, http.MethodPost, wrapperapi.PathLogin, nil, wrapperapi.LoginRequest{Username: username, Password: password}, &st)
	return st, err
}

// Code sends the 2FA code.
func (c *Client) Code(ctx context.Context, code string) (wrapperapi.Status, error) {
	var st wrapperapi.Status
	err := c.do(ctx, http.MethodPost, wrapperapi.PathCode, nil, wrapperapi.CodeRequest{Code: code}, &st)
	return st, err
}

// Restart starts service mode again.
func (c *Client) Restart(ctx context.Context) (wrapperapi.Status, error) {
	var st wrapperapi.Status
	err := c.do(ctx, http.MethodPost, wrapperapi.PathRestart, nil, nil, &st)
	return st, err
}

// Log reads the kept log lines after line n.
func (c *Client) Log(ctx context.Context, after int64) (wrapperapi.LogPage, error) {
	var p wrapperapi.LogPage
	err := c.do(ctx, http.MethodGet, wrapperapi.PathLog, url.Values{"after": {strconv.FormatInt(after, 10)}}, nil, &p)
	return p, err
}

// Regions reads wrapper-lite's GET /status: the account's regions, empty
// when nobody is signed in.
func (c *Client) Regions(ctx context.Context) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.lite.JoinPath("status").String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("wrapper-lite /status answered %s", resp.Status)
	}
	var st struct {
		Data struct {
			Regions []string `json:"regions"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&st); err != nil {
		return nil, fmt.Errorf("wrapper-lite /status: %w", err)
	}
	return st.Data.Regions, nil
}
