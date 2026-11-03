package applemusic

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

// The web player's script and the token in it, as amdl finds them.
var (
	scriptPattern = regexp.MustCompile(`/assets/index~[^/"'\s]+\.js`)
	tokenPattern  = regexp.MustCompile(`eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+`)
)

// Token lifetimes.
const (
	// tokenMargin is how long before exp the token is scraped again.
	tokenMargin = 10 * time.Minute
	// tokenNoExp is how long a token without exp is kept.
	tokenNoExp = time.Hour
	// maxPage bounds the page and the script; the script is about 3 MB.
	maxPage = 32 << 20
)

// tokens holds the web player's bearer token until shortly before it
// expires. One scrape runs at a time; the others wait for it.
type tokens struct {
	fetch func(ctx context.Context) (string, error)
	now   func() time.Time

	mu  sync.Mutex
	tok string
	exp time.Time
}

func (t *tokens) get(ctx context.Context) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.tok != "" && t.now().Before(t.exp.Add(-tokenMargin)) {
		return t.tok, nil
	}
	tok, err := t.fetch(ctx)
	if err != nil {
		return "", err
	}
	t.tok = tok
	t.exp = expiry(tok)
	if t.exp.IsZero() {
		t.exp = t.now().Add(tokenNoExp + tokenMargin)
	}
	return tok, nil
}

// drop forgets tok after Apple refused it, unless another call already
// replaced it.
func (t *tokens) drop(tok string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.tok == tok {
		t.tok = ""
	}
}

// expiry reads a JWT's exp claim without checking the signature.
func expiry(tok string) time.Time {
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		return time.Time{}
	}
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return time.Time{}
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if json.Unmarshal(b, &claims) != nil || claims.Exp == 0 {
		return time.Time{}
	}
	return time.Unix(claims.Exp, 0)
}

// scrape fetches the web player's page, finds its index script, and takes
// the first JWT in it.
func (c *Catalog) scrape(ctx context.Context) (string, error) {
	page, err := c.fetchText(ctx, c.web+"/")
	if err != nil {
		return "", fmt.Errorf("apple music: the web player: %w", err)
	}
	script := scriptPattern.FindString(page)
	if script == "" {
		return "", errors.New("apple music: no index script on the web player's page")
	}
	js, err := c.fetchText(ctx, c.web+script)
	if err != nil {
		return "", fmt.Errorf("apple music: the web player's script: %w", err)
	}
	tok := tokenPattern.FindString(js)
	if tok == "" {
		return "", errors.New("apple music: no token in the web player's script")
	}
	return tok, nil
}

func (c *Catalog) fetchText(ctx context.Context, u string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("answered %s", resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxPage))
	return string(b), err
}
