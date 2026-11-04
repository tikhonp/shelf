// Package musicbrainz reads releases from MusicBrainz and their covers from
// the Cover Art Archive, for the editor's suggestions. Every MusicBrainz
// call waits for the shared limiter, and answers go through a cache, so an
// album opened again costs no request.
package musicbrainz

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Where the services live.
const (
	DefaultBase      = "https://musicbrainz.org/ws/2"
	DefaultCoverBase = "https://coverartarchive.org"
	// VariousArtists is MusicBrainz's ID of Various Artists.
	VariousArtists = "89ad4ac3-39f7-470e-963a-56509c546377"
)

const (
	callTimeout = 20 * time.Second
	maxAnswer   = 16 << 20
)

// ErrNotFound is an ID or a search the service has nothing for.
var ErrNotFound = errors.New("musicbrainz: not found")

// ErrBusy is a 503, which MusicBrainz sends when it turns requests away.
var ErrBusy = errors.New("musicbrainz: busy")

// Cache keeps answers by request. Only 200 and 404 answers are kept.
type Cache interface {
	Get(ctx context.Context, key string) (status int, body []byte, ok bool)
	Put(ctx context.Context, key string, status int, body []byte)
}

// Client calls MusicBrainz and the Cover Art Archive.
type Client struct {
	Base      string
	CoverBase string
	// UserAgent names shelf, its version and a contact URL, as MusicBrainz
	// asks.
	UserAgent string
	HTTP      *http.Client
	Limit     *Limiter
	Cache     Cache
	// Retry are the pauses before asking again after a 503, which is how
	// MusicBrainz says a client went too fast or it is busy.
	Retry []time.Duration
}

// New returns a client of the public services.
func New(userAgent string, cache Cache) *Client {
	return &Client{Base: DefaultBase, CoverBase: DefaultCoverBase, UserAgent: userAgent, HTTP: &http.Client{},
		Limit: NewLimiter(time.Second), Cache: cache, Retry: []time.Duration{2 * time.Second, 4 * time.Second}}
}

// get reads base+path into out, through the cache. limited calls wait for
// the limiter.
func (c *Client) get(ctx context.Context, base, path string, q url.Values, limited bool, out any) error {
	u := base + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	if c.Cache != nil {
		if status, body, ok := c.Cache.Get(ctx, u); ok {
			if status == http.StatusNotFound {
				return ErrNotFound
			}
			if err := json.Unmarshal(body, out); err == nil {
				return nil
			}
		}
	}
	for attempt := 0; ; attempt++ {
		if limited && c.Limit != nil {
			if err := c.Limit.Wait(ctx); err != nil {
				return err
			}
		}
		status, body, err := c.do(ctx, u)
		if err != nil {
			return err
		}
		switch {
		case status == http.StatusOK:
			if err := json.Unmarshal(body, out); err != nil {
				return fmt.Errorf("musicbrainz: the answer to %s: %w", path, err)
			}
			if c.Cache != nil {
				c.Cache.Put(ctx, u, status, body)
			}
			return nil
		case status == http.StatusNotFound || status == http.StatusBadRequest && strings.Contains(string(body), "Invalid mbid"):
			if c.Cache != nil {
				c.Cache.Put(ctx, u, http.StatusNotFound, nil)
			}
			return ErrNotFound
		case status == http.StatusServiceUnavailable && attempt < len(c.Retry):
			if err := sleepCtx(ctx, c.Retry[attempt]); err != nil {
				return err
			}
		default:
			return fmt.Errorf("musicbrainz: %s answered %d", path, status)
		}
	}
}

func (c *Client) do(ctx context.Context, u string) (int, []byte, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	req.Header.Set("Accept", "application/json")
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
		return 0, nil, fmt.Errorf("musicbrainz: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxAnswer))
	if err != nil {
		return 0, nil, fmt.Errorf("musicbrainz: %w", err)
	}
	return resp.StatusCode, body, nil
}

// Ping looks up Various Artists, an artist that always exists and has a
// small answer, and says how long MusicBrainz took. It skips the cache,
// which would answer even while the service is down. It doesn't retry, so
// a busy service shows as busy. The time leaves out the wait for the
// limiter.
func (c *Client) Ping(ctx context.Context) (time.Duration, error) {
	if c.Limit != nil {
		if err := c.Limit.Wait(ctx); err != nil {
			return 0, err
		}
	}
	start := time.Now()
	status, _, err := c.do(ctx, c.Base+"/artist/"+VariousArtists+"?fmt=json")
	took := time.Since(start)
	switch {
	case err != nil:
		return took, err
	case status == http.StatusOK:
		return took, nil
	case status == http.StatusServiceUnavailable:
		return took, ErrBusy
	}
	return took, &StatusError{Code: status}
}

// StatusError is an answer Ping doesn't expect.
type StatusError struct{ Code int }

func (e *StatusError) Error() string { return fmt.Sprintf("musicbrainz: answered %d", e.Code) }

// releaseInc is what a release lookup includes. Classical releases also
// ask for the works and their composers.
const (
	releaseInc   = "artist-credits+labels+recordings+release-groups+isrcs+genres"
	classicalInc = releaseInc + "+recording-level-rels+work-rels+work-level-rels+artist-rels"
)

// Release reads one release with its tracks. works adds each recording's
// works and their composers.
func (c *Client) Release(ctx context.Context, id string, works bool) (Release, error) {
	if !uuidRx.MatchString(id) {
		return Release{}, ErrNotFound
	}
	inc := releaseInc
	if works {
		inc = classicalInc
	}
	var r Release
	err := c.get(ctx, c.Base, "/release/"+id, url.Values{"inc": {inc}, "fmt": {"json"}}, true, &r)
	return r, err
}

// SearchReleases runs a release search in MusicBrainz's query syntax.
func (c *Client) SearchReleases(ctx context.Context, query string, limit int) ([]Hit, error) {
	var out struct {
		Releases []Hit `json:"releases"`
	}
	err := c.get(ctx, c.Base, "/release", url.Values{"query": {query}, "limit": {fmt.Sprint(limit)}, "fmt": {"json"}}, true, &out)
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	return out.Releases, err
}

// ISRC lists the releases of every recording with the code. The ISRC
// lookup leaves releases out of its JSON, so this is a recording search,
// whose answer lists each recording's releases with their track counts.
func (c *Client) ISRC(ctx context.Context, isrc string) ([]Hit, error) {
	var out struct {
		Recordings []struct {
			Releases []Hit `json:"releases"`
		} `json:"recordings"`
	}
	q := url.Values{"query": {"isrc:" + Phrase(isrc)}, "limit": {"10"}, "fmt": {"json"}}
	err := c.get(ctx, c.Base, "/recording", q, true, &out)
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	var hits []Hit
	for _, r := range out.Recordings {
		hits = append(hits, r.Releases...)
	}
	return hits, err
}

// Cover is a release's front cover in the Cover Art Archive.
type Cover struct {
	// Image is the full image, Preview a 500 pixel thumbnail.
	Image   string
	Preview string
}

// FrontCover finds a release's front cover.
func (c *Client) FrontCover(ctx context.Context, release string) (Cover, error) {
	if !uuidRx.MatchString(release) {
		return Cover{}, ErrNotFound
	}
	var out struct {
		Images []struct {
			Front      bool              `json:"front"`
			Image      string            `json:"image"`
			Thumbnails map[string]string `json:"thumbnails"`
		} `json:"images"`
	}
	if err := c.get(ctx, c.CoverBase, "/release/"+release, nil, false, &out); err != nil {
		return Cover{}, err
	}
	for _, im := range out.Images {
		if im.Front && im.Image != "" {
			p := im.Thumbnails["500"]
			if p == "" {
				p = im.Thumbnails["large"]
			}
			if p == "" {
				p = im.Image
			}
			return Cover{Image: https(im.Image), Preview: https(p)}, nil
		}
	}
	return Cover{}, ErrNotFound
}

// https upgrades the archive's plain links, which redirect anyway.
func https(u string) string {
	if rest, ok := strings.CutPrefix(u, "http://coverartarchive.org/"); ok {
		return "https://coverartarchive.org/" + rest
	}
	return u
}

var uuidRx = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

var releaseLink = regexp.MustCompile(`(?i)^(?:https?://)?(?:[a-z]+\.)?musicbrainz\.org/release/([0-9a-f-]{36})(?:[/?#].*)?$`)

// ParseRelease reads a release ID, bare or in a MusicBrainz link.
func ParseRelease(s string) (string, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	if uuidRx.MatchString(s) {
		return s, true
	}
	if m := releaseLink.FindStringSubmatch(s); m != nil && uuidRx.MatchString(m[1]) {
		return m[1], true
	}
	return "", false
}

// IsID reports whether s is a MusicBrainz ID.
func IsID(s string) bool { return uuidRx.MatchString(s) }

// Phrase quotes s for a search, escaping what the query syntax reads
// inside quotes.
func Phrase(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	return `"` + r.Replace(s) + `"`
}
