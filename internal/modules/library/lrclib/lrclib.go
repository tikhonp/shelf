// Package lrclib reads lyrics from LRCLIB, for the editor's suggestions.
package lrclib

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// DefaultBase is the public service.
const DefaultBase = "https://lrclib.net/api"

const (
	callTimeout = 10 * time.Second
	maxAnswer   = 4 << 20
)

// ErrNotFound is a track LRCLIB has no lyrics for.
var ErrNotFound = errors.New("lrclib: not found")

// Cache keeps answers by request, as the MusicBrainz client's does.
type Cache interface {
	Get(ctx context.Context, key string) (status int, body []byte, ok bool)
	Put(ctx context.Context, key string, status int, body []byte)
}

// Client calls LRCLIB.
type Client struct {
	Base      string
	UserAgent string
	HTTP      *http.Client
	Cache     Cache
}

// New returns a client of the public service.
func New(userAgent string, cache Cache) *Client {
	return &Client{Base: DefaultBase, UserAgent: userAgent, HTTP: &http.Client{}, Cache: cache}
}

// Lyrics are a track's lyrics. Synced is LRC text with timestamps.
type Lyrics struct {
	Plain        string `json:"plainLyrics"`
	Synced       string `json:"syncedLyrics"`
	Instrumental bool   `json:"instrumental"`
}

// Get finds the lyrics of one track. LRCLIB matches the duration within
// two seconds.
func (c *Client) Get(ctx context.Context, artist, title, album string, seconds int) (Lyrics, error) {
	q := url.Values{"artist_name": {artist}, "track_name": {title}, "album_name": {album}, "duration": {strconv.Itoa(seconds)}}
	u := c.Base + "/get?" + q.Encode()
	var out Lyrics
	if c.Cache != nil {
		if status, body, ok := c.Cache.Get(ctx, u); ok {
			if status == http.StatusNotFound {
				return out, ErrNotFound
			}
			if json.Unmarshal(body, &out) == nil {
				return out, nil
			}
		}
	}
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return out, err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return out, fmt.Errorf("lrclib: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxAnswer))
	if err != nil {
		return out, fmt.Errorf("lrclib: %w", err)
	}
	switch resp.StatusCode {
	case http.StatusOK:
		if err := json.Unmarshal(body, &out); err != nil {
			return out, fmt.Errorf("lrclib: the answer: %w", err)
		}
		if c.Cache != nil {
			c.Cache.Put(ctx, u, http.StatusOK, body)
		}
		return out, nil
	case http.StatusNotFound:
		if c.Cache != nil {
			c.Cache.Put(ctx, u, http.StatusNotFound, nil)
		}
		return out, ErrNotFound
	default:
		return out, fmt.Errorf("lrclib: answered %d", resp.StatusCode)
	}
}
