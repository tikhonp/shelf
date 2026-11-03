package navidrome

import (
	"context"
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/tikhonp/shelf/internal/platform/config"
	"github.com/tikhonp/shelf/internal/source"
)

// Subsonic request parameters.
const (
	apiVersion = "1.16.1"
	clientName = "shelf"
	// maxAnswer bounds an answer; a scan status is a few hundred bytes.
	maxAnswer = 1 << 20
)

// Client calls Navidrome's Subsonic API with token authentication, so the
// password never travels. It is the module's source.Library.
type Client struct {
	base     *url.URL
	user     string
	password config.Secret
	http     *http.Client
	// salt makes each request's salt. Tests replace it.
	salt func() string
}

var _ source.Library = (*Client)(nil)

// NewClient returns a client for the server at base.
func NewClient(base *url.URL, user string, password config.Secret, hc *http.Client) *Client {
	return &Client{base: base, user: user, password: password, http: hc, salt: randomSalt}
}

func randomSalt() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// call sends one request and unwraps the answer. A failed answer becomes a
// *SubsonicError.
func (c *Client) call(ctx context.Context, method string) (*envelope, error) {
	salt := c.salt()
	sum := md5.Sum([]byte(c.password.Reveal() + salt))
	q := url.Values{
		"u": {c.user}, "t": {hex.EncodeToString(sum[:])}, "s": {salt},
		"v": {apiVersion}, "c": {clientName}, "f": {"json"},
	}
	u := c.base.JoinPath("rest", method)
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("navidrome: %s: %w", method, err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		// The URL holds the token, which works again with its salt, so the
		// error leaves it out.
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return nil, fmt.Errorf("navidrome: %s: %w", method, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("navidrome: %s answered %s", method, resp.Status)
	}
	var e envelope
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxAnswer)).Decode(&e); err != nil {
		return nil, fmt.Errorf("navidrome: %s: the answer is not Subsonic JSON: %w", method, err)
	}
	if e.Response.Status != "ok" {
		if e.Response.Error != nil {
			return nil, e.Response.Error
		}
		return nil, fmt.Errorf("navidrome: %s answered status %q", method, e.Response.Status)
	}
	return &e, nil
}

func (c *Client) scanStatus(ctx context.Context, method string) (source.ScanStatus, error) {
	e, err := c.call(ctx, method)
	if err != nil {
		return source.ScanStatus{}, err
	}
	st := e.Response.ScanStatus
	if st == nil {
		return source.ScanStatus{}, fmt.Errorf("navidrome: %s answered without scanStatus", method)
	}
	out := source.ScanStatus{Scanning: st.Scanning, Count: st.Count}
	if st.LastScan != nil {
		out.LastScan = st.LastScan.UTC()
	}
	return out, nil
}

// ScanStatus reads the scan state.
func (c *Client) ScanStatus(ctx context.Context) (source.ScanStatus, error) {
	return c.scanStatus(ctx, "getScanStatus")
}

// StartScan starts a quick scan, which reads only changed folders.
func (c *Client) StartScan(ctx context.Context) error {
	_, err := c.scanStatus(ctx, "startScan")
	return err
}
