package navidrome

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// Config holds the Navidrome settings that change how it reads tags: the
// owner's tag mappings, the artist split and the album ID. The library
// module reads files the same way.
type Config struct {
	Tags                  map[string]TagConfig
	ArtistSplitExceptions []string
	ArtistJoiner          string
	GenreSeparators       string
	AlbumPID              string
	CoverArtPriority      string
}

// TagConfig is one entry of Navidrome's Tags setting.
type TagConfig struct {
	Ignore    bool
	Aliases   []string
	Type      string
	MaxLength int
	Split     []string
	Album     bool
}

// configAnswer is the part of GET /api/config/ shelf reads. The rest may
// hold other services' keys, so it is never decoded or kept.
type configAnswer struct {
	Config struct {
		Tags    map[string]TagConfig
		Scanner struct {
			ArtistJoiner          string
			ArtistSplitExceptions []string
			GenreSeparators       string
			// PurgeMissing is never, always or full: when Navidrome's own scan
			// forgets files that are gone.
			PurgeMissing string
		}
		PID struct {
			Album string
		}
		CoverArtPriority string
	} `json:"config"`
}

// Config reads Navidrome's settings through its own API, which needs an
// admin's login. The path needs its trailing slash, or Navidrome
// answers 404.
func (c *Client) Config(ctx context.Context) (Config, error) {
	token, err := c.login(ctx)
	if err != nil {
		return Config{}, err
	}
	a, err := c.config(ctx, token)
	if err != nil {
		return Config{}, err
	}
	s := a.Config
	return Config{
		Tags: s.Tags, ArtistSplitExceptions: s.Scanner.ArtistSplitExceptions, ArtistJoiner: s.Scanner.ArtistJoiner,
		GenreSeparators: s.Scanner.GenreSeparators, AlbumPID: s.PID.Album, CoverArtPriority: s.CoverArtPriority,
	}, nil
}

func (c *Client) config(ctx context.Context, token string) (configAnswer, error) {
	var a configAnswer
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base.JoinPath("api", "config").String()+"/", nil)
	if err != nil {
		return a, fmt.Errorf("navidrome: config: %w", err)
	}
	req.Header.Set("X-ND-Authorization", "Bearer "+token)
	if err := c.native(req, &a); err != nil {
		return a, fmt.Errorf("navidrome: config: %w", err)
	}
	return a, nil
}

func (c *Client) login(ctx context.Context) (string, error) {
	body, err := json.Marshal(map[string]string{"username": c.user, "password": c.password.Reveal()})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base.JoinPath("auth", "login").String(), bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("navidrome: login: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	var a struct {
		Token   string `json:"token"`
		IsAdmin bool   `json:"isAdmin"`
	}
	if err := c.native(req, &a); err != nil {
		return "", fmt.Errorf("navidrome: login: %w", err)
	}
	if a.Token == "" {
		return "", errors.New("navidrome: login answered without a token")
	}
	if !a.IsAdmin {
		return "", errors.New("navidrome: the user is not an admin")
	}
	return a.Token, nil
}

// native sends a request to Navidrome's own API and decodes the answer. A
// refusal says only its status, since the body may echo the request.
func (c *Client) native(req *http.Request, into any) error { return c.nativeUpTo(req, into, maxAnswer) }

// nativeUpTo is native for an answer of up to limit bytes.
func (c *Client) nativeUpTo(req *http.Request, into any, limit int64) error {
	resp, err := c.http.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("answered %s", resp.Status)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, limit)).Decode(into); err != nil {
		return fmt.Errorf("the answer is not JSON: %w", err)
	}
	return nil
}

// ErrOff is the answer of Config when Navidrome isn't set up.
var ErrOff = errors.New("navidrome: not set up")

// Config reads Navidrome's tag settings. It is the library module's port.
func (m *Module) Config(ctx context.Context) (Config, error) {
	if m.client == nil {
		return Config{}, ErrOff
	}
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	return m.client.Config(ctx)
}
