package applemusic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tikhonp/shelf/internal/source"
)

// Apple's endpoints and the browser the requests look like.
const (
	apiBase   = "https://amp-api.music.apple.com"
	webBase   = "https://music.apple.com"
	userAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36"
	// callTimeout bounds one request.
	callTimeout = 15 * time.Second
	// maxAnswer bounds one catalog answer.
	maxAnswer = 32 << 20
	// maxPages bounds how many next links one lookup follows.
	maxPages = 100
)

// backoff429 are the waits after a 429 without Retry-After. After the last
// one the call fails.
var backoff429 = []time.Duration{2 * time.Second, 4 * time.Second, 8 * time.Second}

// maxRetryAfter caps a Retry-After that asks for longer.
const maxRetryAfter = time.Minute

// ErrRateLimited is a call Apple kept answering with 429.
var ErrRateLimited = errors.New("apple music: too many requests; try again in a minute")

// Catalog calls Apple's web catalog API, the requests amdl makes, written
// from scratch.
type Catalog struct {
	api, web string
	http     *http.Client
	tokens   *tokens
	// storefront and language are read for every call: the storefront
	// follows the wrapper's account, the language the base config.
	storefront func(ctx context.Context) string
	language   func() string
	// sleep waits between retries; tests replace it.
	sleep func(ctx context.Context, d time.Duration) error
}

func newCatalog(hc *http.Client, storefront func(context.Context) string, language func() string) *Catalog {
	c := &Catalog{api: apiBase, web: webBase, http: hc, storefront: storefront, language: language, sleep: sleepCtx}
	c.tokens = &tokens{fetch: c.scrape, now: time.Now}
	return c
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// get reads one answer into out. path is under /v1/catalog/{storefront}/,
// or a next link that starts with /v1/. A 401 scrapes the token again and
// retries once; a 429 waits and retries up to three times; a 404 is
// source.ErrNotFound.
func (c *Catalog) get(ctx context.Context, path string, q url.Values, out any) error {
	u, err := c.url(ctx, path, q)
	if err != nil {
		return err
	}
	name := u.Path
	refreshed := false
	waits := 0
	for {
		tok, err := c.tokens.get(ctx)
		if err != nil {
			return err
		}
		status, retryAfter, err := c.do(ctx, u.String(), tok, out)
		if err != nil {
			return err
		}
		switch {
		case status == http.StatusOK:
			return nil
		case status == http.StatusUnauthorized && !refreshed:
			c.tokens.drop(tok)
			refreshed = true
		case status == http.StatusTooManyRequests && waits < len(backoff429):
			d := backoff429[waits]
			if retryAfter > 0 {
				d = min(retryAfter, maxRetryAfter)
			}
			waits++
			if err := c.sleep(ctx, d); err != nil {
				return err
			}
		case status == http.StatusTooManyRequests:
			return ErrRateLimited
		case status == http.StatusNotFound:
			return source.ErrNotFound
		default:
			return &statusError{path: name, status: status}
		}
	}
}

// statusError is an answer get doesn't retry. The path has no token.
type statusError struct {
	path   string
	status int
}

func (e *statusError) Error() string {
	return fmt.Sprintf("apple music: %s answered %d %s", e.path, e.status, http.StatusText(e.status))
}

func (c *Catalog) url(ctx context.Context, path string, q url.Values) (*url.URL, error) {
	if !strings.HasPrefix(path, "/v1/") {
		path = "/v1/catalog/" + url.PathEscape(c.storefront(ctx)) + "/" + path
	}
	u, err := url.Parse(c.api + path)
	if err != nil {
		return nil, err
	}
	v := u.Query()
	for k, vals := range q {
		v[k] = vals
	}
	if l := c.language(); l != "" && v.Get("l") == "" {
		v.Set("l", l)
	}
	u.RawQuery = v.Encode()
	return u, nil
}

// do sends one request. Only a 200 answer is decoded into out.
func (c *Catalog) do(ctx context.Context, u, tok string, out any) (int, time.Duration, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return 0, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Origin", c.web)
	req.Header.Set("User-Agent", userAgent)
	resp, err := c.http.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return 0, 0, fmt.Errorf("apple music: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
		return resp.StatusCode, retryAfter(resp.Header.Get("Retry-After")), nil
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxAnswer)).Decode(out); err != nil {
		return 0, 0, fmt.Errorf("apple music: the answer to %s: %w", req.URL.Path, err)
	}
	return resp.StatusCode, 0, nil
}

func retryAfter(v string) time.Duration {
	if v == "" {
		return 0
	}
	if n, err := strconv.Atoi(v); err == nil && n > 0 {
		return time.Duration(n) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		return max(time.Until(t), time.Second)
	}
	return 0
}

// The answers' shapes: a document of resources, each with attributes,
// relationships and, for an artist, views.
type document struct {
	Data    []resource          `json:"data"`
	Next    string              `json:"next"`
	Results map[string]relation `json:"results"`
}

type resource struct {
	ID            string              `json:"id"`
	Type          string              `json:"type"`
	Attributes    attributes          `json:"attributes"`
	Relationships map[string]relation `json:"relationships"`
	Views         map[string]relation `json:"views"`
}

type relation struct {
	Next string     `json:"next"`
	Data []resource `json:"data"`
}

type attributes struct {
	Name             string   `json:"name"`
	ArtistName       string   `json:"artistName"`
	AlbumName        string   `json:"albumName"`
	CuratorName      string   `json:"curatorName"`
	URL              string   `json:"url"`
	ReleaseDate      string   `json:"releaseDate"`
	RecordLabel      string   `json:"recordLabel"`
	UPC              string   `json:"upc"`
	ISRC             string   `json:"isrc"`
	ContentRating    string   `json:"contentRating"`
	GenreNames       []string `json:"genreNames"`
	TrackNumber      int      `json:"trackNumber"`
	DiscNumber       int      `json:"discNumber"`
	TrackCount       int      `json:"trackCount"`
	DurationInMillis int64    `json:"durationInMillis"`
	AudioTraits      []string `json:"audioTraits"`
	Artwork          *artwork `json:"artwork"`
	IsSingle         bool     `json:"isSingle"`
	IsCompilation    bool     `json:"isCompilation"`
}

type artwork struct {
	URL string `json:"url"`
}

// coverSize is the size an artwork template is filled with. The UI may ask
// for another by changing the last part of the URL.
const coverSize = "600"

func (a *artwork) cover() string {
	if a == nil || a.URL == "" {
		return ""
	}
	return strings.NewReplacer("{w}", coverSize, "{h}", coverSize, "{c}", "bb", "{f}", "jpg").Replace(a.URL)
}

// pages follows a relationship's next links and returns every resource.
func (c *Catalog) pages(ctx context.Context, first relation) ([]resource, error) {
	all := first.Data
	next := first.Next
	for i := 0; next != "" && i < maxPages; i++ {
		var doc document
		if err := c.get(ctx, next, nil, &doc); err != nil {
			return nil, err
		}
		all = append(all, doc.Data...)
		next = doc.Next
	}
	return all, nil
}

// storefront caches the account's region from wrapper-lite's /status.
type storefront struct {
	regions  func(ctx context.Context) ([]string, error)
	fallback func() string
	now      func() time.Time
	ttl      time.Duration

	mu    sync.Mutex
	value string
	at    time.Time
	// failedAt keeps a wrapper that doesn't answer from being asked on
	// every call.
	failedAt time.Time
}

// storefrontTTL is how long the account's region is kept.
const storefrontTTL = 10 * time.Minute

// storefrontRetry is how soon a wrapper that didn't answer is asked again.
const storefrontRetry = time.Minute

// regionWait bounds the read of /status.
const regionWait = 3 * time.Second

func (s *storefront) get(ctx context.Context) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	if s.value != "" && now.Sub(s.at) < s.ttl {
		return s.value
	}
	if s.failedAt.IsZero() || now.Sub(s.failedAt) >= storefrontRetry {
		ctx, cancel := context.WithTimeout(ctx, regionWait)
		regions, err := s.regions(ctx)
		cancel()
		if err == nil && len(regions) > 0 && regions[0] != "" {
			s.value, s.at, s.failedAt = strings.ToLower(regions[0]), now, time.Time{}
			return s.value
		}
		s.failedAt = now
	}
	if s.value != "" {
		return s.value
	}
	return strings.ToLower(s.fallback())
}
