// Package config reads shelf's SHELF_* variables, listed in
// docs/architecture.md#configuration. Load reports every problem at once, so
// a broken deployment takes one round to fix, and nothing here prints a value.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Config is the parsed environment. It prints as a placeholder with any
// verb, because the proxy URL may carry a password and the tokens are
// secrets.
type Config struct {
	AuthProvider string
	DataDir      string
	MusicDir     string
	HTTPAddr     string
	// PublicURL is nil when unset.
	PublicURL *url.URL
	TZ        *time.Location
	// Proxy is nil when unset. It may hold credentials.
	Proxy *url.URL

	Qobuz     Qobuz
	Apple     Apple
	Navidrome Navidrome
	Telegram  Telegram
	// AcoustIDKey lets the library identify an album by its audio. Empty
	// leaves AcoustID out of the suggestions.
	AcoustIDKey Secret

	DevFakeSource bool
	LogLevel      slog.Level
}

// Qobuz configures the Qobuz source.
type Qobuz struct {
	Token    Secret
	Dir      string
	Quality  int
	Fallback bool
	Workers  int
	Delay    time.Duration
	UseProxy bool
}

// Enabled reports whether a token was given.
func (q Qobuz) Enabled() bool { return q.Token != "" }

// Apple configures the Apple Music source and its wrapper.
type Apple struct {
	AMDLConfig        string
	AMDLTimeout       time.Duration
	Dir               string
	WrapperURL        *url.URL
	WrapperControlURL *url.URL
	WrapperToken      Secret
}

// Enabled reports whether an amdl config was given.
func (a Apple) Enabled() bool { return a.AMDLConfig != "" }

// Navidrome configures library scans.
type Navidrome struct {
	// URL is nil when scans are off.
	URL       *url.URL
	User      string
	Password  Secret
	ScanDelay time.Duration
}

// Enabled reports whether a URL was given.
func (n Navidrome) Enabled() bool { return n.URL != nil }

// Telegram configures the bot.
type Telegram struct {
	Token    Secret
	Users    []int64
	UseProxy bool
}

// Enabled reports whether a token was given.
func (t Telegram) Enabled() bool { return t.Token != "" }

// DatabasePath is the SQLite file inside the data directory.
func (c *Config) DatabasePath() string { return filepath.Join(c.DataDir, "shelf.db") }

// Format keeps every value out of fmt output, whatever the verb.
func (c Config) Format(f fmt.State, _ rune) { _, _ = f.Write([]byte("config.Config{redacted}")) }

// LogValue keeps every value out of slog output.
func (c Config) LogValue() slog.Value { return slog.StringValue("redacted") }

// LoadFromEnv reads the process environment.
func LoadFromEnv() (*Config, error) { return Load(os.LookupEnv) }

// Load parses the variables that lookup returns. An unset variable takes its
// default. A variable set to an empty value is empty. That is how
// SHELF_AMDL_CONFIG and SHELF_NAVIDROME_URL turn a part off despite their
// defaults, and it is an error for a variable that needs a value.
func Load(lookup func(string) (string, bool)) (*Config, error) {
	p := &parser{lookup: lookup}
	c := &Config{
		AuthProvider: p.required("SHELF_AUTH_PROVIDER", "none"),
		DataDir:      p.required("SHELF_DATA_DIR", "/data"),
		MusicDir:     p.required("SHELF_MUSIC_DIR", "/music"),
		HTTPAddr:     p.required("SHELF_HTTP_ADDR", ":8080"),
		PublicURL:    p.url("SHELF_PUBLIC_URL", "", false, "http", "https"),
		TZ:           p.timeZone("SHELF_TIMEZONE", "Europe/Moscow"),
		Proxy:        p.url("SHELF_PROXY_URL", "", true, "http", "https", "socks5", "socks5h"),
		Qobuz: Qobuz{
			Token:    Secret(p.str("SHELF_QOBUZ_TOKEN", "")),
			Dir:      p.folder("SHELF_QOBUZ_DIR", "qobuz"),
			Quality:  p.oneOf("SHELF_QOBUZ_QUALITY", 27, 5, 6, 7, 27),
			Fallback: p.boolean("SHELF_QOBUZ_FALLBACK", true),
			Workers:  p.positive("SHELF_QOBUZ_WORKERS", 3),
			Delay:    p.duration("SHELF_QOBUZ_DELAY", 0, false),
			UseProxy: p.boolean("SHELF_QOBUZ_USE_PROXY", false),
		},
		Apple: Apple{
			AMDLConfig:        p.str("SHELF_AMDL_CONFIG", "/config/amdl.yaml"),
			AMDLTimeout:       p.duration("SHELF_AMDL_TIMEOUT", 2*time.Hour, true),
			Dir:               p.folder("SHELF_APPLE_DIR", "apple-music"),
			WrapperURL:        p.requiredURL("SHELF_WRAPPER_URL", "http://shelf-wrapper:12340"),
			WrapperControlURL: p.requiredURL("SHELF_WRAPPER_CONTROL_URL", "http://shelf-wrapper:12341"),
			WrapperToken:      Secret(p.str("SHELF_WRAPPER_TOKEN", "")),
		},
		Navidrome: Navidrome{
			URL:       p.url("SHELF_NAVIDROME_URL", "http://navidrome:4533", false, "http", "https"),
			User:      p.str("SHELF_NAVIDROME_USER", ""),
			Password:  Secret(p.str("SHELF_NAVIDROME_PASSWORD", "")),
			ScanDelay: p.duration("SHELF_NAVIDROME_SCAN_DELAY", 30*time.Second, false),
		},
		Telegram: Telegram{
			Token:    Secret(p.str("SHELF_TELEGRAM_TOKEN", "")),
			Users:    p.userIDs("SHELF_TELEGRAM_USERS"),
			UseProxy: p.boolean("SHELF_TELEGRAM_USE_PROXY", false),
		},
		AcoustIDKey:   Secret(p.str("SHELF_ACOUSTID_KEY", "")),
		DevFakeSource: p.boolean("SHELF_DEV_FAKE_SOURCE", false),
		LogLevel:      p.level("SHELF_LOG_LEVEL", "info"),
	}
	if c.AuthProvider != "none" && c.AuthProvider != "simple" {
		p.fail("SHELF_AUTH_PROVIDER must be none or simple")
	}
	if c.AuthProvider == "simple" && (c.PublicURL == nil || c.PublicURL.Scheme != "https" || c.PublicURL.User != nil || c.PublicURL.RawQuery != "" || c.PublicURL.Fragment != "" || c.PublicURL.Path != "" && c.PublicURL.Path != "/") {
		p.fail("SHELF_PUBLIC_URL must be an HTTPS origin when authentication is simple")
	}
	if c.Proxy == nil {
		if c.Qobuz.UseProxy {
			p.fail("SHELF_QOBUZ_USE_PROXY is true but SHELF_PROXY_URL is empty")
		}
		if c.Telegram.UseProxy {
			p.fail("SHELF_TELEGRAM_USE_PROXY is true but SHELF_PROXY_URL is empty")
		}
	}
	if len(p.errs) > 0 {
		return nil, errors.Join(p.errs...)
	}
	return c, nil
}

// Warnings lists settings that are valid but leave a part unable to work.
func (c *Config) Warnings() []string {
	var w []string
	if c.Telegram.Enabled() && len(c.Telegram.Users) == 0 {
		w = append(w, "SHELF_TELEGRAM_TOKEN is set but SHELF_TELEGRAM_USERS is empty: the bot answers nobody")
	}
	if c.Navidrome.Enabled() && (c.Navidrome.User == "" || c.Navidrome.Password == "") {
		w = append(w, "SHELF_NAVIDROME_USER or SHELF_NAVIDROME_PASSWORD is empty: Navidrome will refuse scans")
	}
	if c.Apple.Enabled() && c.Apple.WrapperToken == "" {
		w = append(w, "SHELF_WRAPPER_TOKEN is empty: the wrapper's control API will refuse sign-in and restart")
	}
	return w
}

type parser struct {
	lookup func(string) (string, bool)
	errs   []error
}

func (p *parser) fail(format string, a ...any) { p.errs = append(p.errs, fmt.Errorf(format, a...)) }

func (p *parser) str(name, def string) string {
	v, ok := p.lookup(name)
	if !ok {
		return def
	}
	return strings.TrimSpace(v)
}

func (p *parser) required(name, def string) string {
	v := p.str(name, def)
	if v == "" {
		p.fail("%s is empty", name)
	}
	return v
}

func (p *parser) boolean(name string, def bool) bool {
	v := p.str(name, strconv.FormatBool(def))
	b, err := strconv.ParseBool(v)
	if err != nil {
		p.fail("%s: %q is not true or false", name, v)
	}
	return b
}

func (p *parser) positive(name string, def int) int {
	v := p.str(name, strconv.Itoa(def))
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		p.fail("%s: %q is not a whole number of 1 or more", name, v)
	}
	return n
}

func (p *parser) oneOf(name string, def int, allowed ...int) int {
	v := p.str(name, strconv.Itoa(def))
	n, err := strconv.Atoi(v)
	if err != nil || !slices.Contains(allowed, n) {
		p.fail("%s: %q is not one of %s", name, v, strings.Trim(fmt.Sprint(allowed), "[]"))
	}
	return n
}

// duration needs a value above zero when positive is set, and zero or more
// otherwise.
func (p *parser) duration(name string, def time.Duration, positive bool) time.Duration {
	v := p.str(name, def.String())
	d, err := time.ParseDuration(v)
	switch {
	case err != nil:
		p.fail("%s: %q is not a duration such as 30s or 2h", name, v)
	case positive && d <= 0:
		p.fail("%s: %q must be above zero", name, v)
	case d < 0:
		p.fail("%s: %q must not be negative", name, v)
	}
	return d
}

// url returns nil for an empty value. It never echoes a secret value in an
// error, since a proxy URL may hold a password.
func (p *parser) url(name, def string, secret bool, schemes ...string) *url.URL {
	v := p.str(name, def)
	if v == "" {
		return nil
	}
	shown := strconv.Quote(v)
	if secret {
		shown = "the value"
	}
	u, err := url.Parse(v)
	if err != nil || !slices.Contains(schemes, u.Scheme) || u.Host == "" {
		p.fail("%s: %s is not a URL starting with %s://", name, shown, strings.Join(schemes, ":// or "))
		return nil
	}
	return u
}

func (p *parser) requiredURL(name, def string) *url.URL {
	if p.str(name, def) == "" {
		p.fail("%s is empty", name)
		return nil
	}
	return p.url(name, def, false, "http", "https")
}

// folder is a path under the music root, which it must not leave.
func (p *parser) folder(name, def string) string {
	v := p.required(name, def)
	if v != "" && !filepath.IsLocal(v) {
		p.fail("%s: %q must be a relative path inside the music folder", name, v)
	}
	return filepath.Clean(v)
}

func (p *parser) timeZone(name, def string) *time.Location {
	v := p.required(name, def)
	if v == "" {
		return nil
	}
	loc, err := time.LoadLocation(v)
	if err != nil {
		p.fail("%s: unknown time zone %q", name, v)
	}
	return loc
}

func (p *parser) userIDs(name string) []int64 {
	var ids []int64
	for item := range strings.SplitSeq(p.str(name, ""), ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		id, err := strconv.ParseInt(item, 10, 64)
		if err != nil || id <= 0 {
			p.fail("%s: %q is not a Telegram user ID", name, item)
			continue
		}
		ids = append(ids, id)
	}
	return ids
}

func (p *parser) level(name, def string) slog.Level {
	v := p.str(name, def)
	switch strings.ToLower(v) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	}
	p.fail("%s: %q is not one of debug, info, warn, error", name, v)
	return slog.LevelInfo
}
