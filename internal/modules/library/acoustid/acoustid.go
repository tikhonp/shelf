// Package acoustid identifies audio: fpcalc makes a fingerprint, and the
// AcoustID service names the MusicBrainz recordings and releases that hold
// it. The API key travels in the body of a POST, so it reaches no URL or
// log.
package acoustid

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/tikhonp/shelf/internal/modules/library/musicbrainz"
)

// DefaultBase is the public service.
const DefaultBase = "https://api.acoustid.org/v2"

const (
	// every is AcoustID's limit of three requests a second.
	every       = time.Second / 3
	callTimeout = 15 * time.Second
	maxAnswer   = 4 << 20
	// printSeconds is how much of a track fpcalc listens to, as AcoustID's
	// own tools do.
	printSeconds = 120
)

// Client calls fpcalc and the service.
type Client struct {
	Base      string
	Key       string
	UserAgent string
	FPCalc    string
	HTTP      *http.Client
	limit     *musicbrainz.Limiter
}

// New returns a client of the public service.
func New(key, userAgent string) *Client {
	return &Client{Base: DefaultBase, Key: key, UserAgent: userAgent, FPCalc: "fpcalc", HTTP: &http.Client{},
		limit: musicbrainz.NewLimiter(every)}
}

// Print is a track's fingerprint.
type Print struct {
	Duration    float64 `json:"duration"`
	Fingerprint string  `json:"fingerprint"`
}

// Fingerprint listens to the start of a file.
func (c *Client) Fingerprint(ctx context.Context, path string) (Print, error) {
	// fpcalc has no end of options, and every path it gets is absolute, so
	// none can read as a flag.
	cmd := exec.CommandContext(ctx, c.FPCalc, "-json", "-length", strconv.Itoa(printSeconds), path)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return Print{}, fmt.Errorf("fpcalc: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	var p Print
	if err := json.Unmarshal(out, &p); err != nil {
		return Print{}, fmt.Errorf("fpcalc: %w", err)
	}
	if p.Fingerprint == "" {
		return Print{}, errors.New("fpcalc: no fingerprint")
	}
	return p, nil
}

// Match is one recording the service found for a fingerprint.
type Match struct {
	Score     float64
	Recording string
	Releases  []string
}

type answer struct {
	Status string `json:"status"`
	Error  struct {
		Message string `json:"message"`
	} `json:"error"`
	Results []struct {
		Score      float64 `json:"score"`
		Recordings []struct {
			ID       string `json:"id"`
			Releases []struct {
				ID string `json:"id"`
			} `json:"releases"`
		} `json:"recordings"`
	} `json:"results"`
}

// Lookup asks which recordings hold a fingerprint.
func (c *Client) Lookup(ctx context.Context, p Print) ([]Match, error) {
	if err := c.limit.Wait(ctx); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	form := url.Values{"client": {c.Key}, "format": {"json"}, "meta": {"recordings releaseids"},
		"duration": {strconv.Itoa(int(p.Duration))}, "fingerprint": {p.Fingerprint}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Base+"/lookup", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", c.UserAgent)
	res, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(res.Body, maxAnswer))
	if err != nil {
		return nil, err
	}
	var a answer
	if err := json.Unmarshal(body, &a); err != nil {
		return nil, fmt.Errorf("acoustid: status %d: %w", res.StatusCode, err)
	}
	if a.Status != "ok" {
		return nil, fmt.Errorf("acoustid: %s", a.Error.Message)
	}
	var out []Match
	for _, r := range a.Results {
		for _, rec := range r.Recordings {
			m := Match{Score: r.Score, Recording: rec.ID}
			for _, rel := range rec.Releases {
				m.Releases = append(m.Releases, rel.ID)
			}
			out = append(out, m)
		}
	}
	return out, nil
}
