package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"
)

func env(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) {
		v, ok := m[k]
		return v, ok
	}
}

func TestConfigDefaults(t *testing.T) {
	c, err := Load(env(nil))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.DataDir != "/data" || c.MusicDir != "/music" || c.HTTPAddr != ":8080" {
		t.Errorf("paths and address: %q %q %q", c.DataDir, c.MusicDir, c.HTTPAddr)
	}
	if c.DatabasePath() != "/data/shelf.db" {
		t.Errorf("DatabasePath = %s", c.DatabasePath())
	}
	if c.PublicURL != nil || c.Proxy != nil {
		t.Errorf("PublicURL %v, Proxy %v", c.PublicURL, c.Proxy)
	}
	if c.TZ.String() != "Europe/Moscow" {
		t.Errorf("TZ = %s", c.TZ)
	}
	q := c.Qobuz
	if q.Enabled() || q.Dir != "qobuz" || q.Quality != 27 || !q.Fallback || q.Workers != 3 || q.Delay != 0 || q.UseProxy {
		t.Errorf("Qobuz = %+v", q)
	}
	a := c.Apple
	if !a.Enabled() || a.AMDLConfig != "/config/amdl.yaml" || a.AMDLTimeout != 2*time.Hour || a.Dir != "apple-music" ||
		a.WrapperURL.String() != "http://shelf-wrapper:12340" ||
		a.WrapperControlURL.String() != "http://shelf-wrapper:12341" || a.WrapperToken != "" {
		t.Errorf("Apple = %+v", a)
	}
	n := c.Navidrome
	if !n.Enabled() || n.URL.String() != "http://navidrome:4533" || n.User != "" || n.Password != "" || n.ScanDelay != 30*time.Second {
		t.Errorf("Navidrome = %+v", n)
	}
	tg := c.Telegram
	if tg.Enabled() || len(tg.Users) != 0 || tg.UseProxy {
		t.Errorf("Telegram = %+v", tg)
	}
	if c.DevFakeSource || c.LogLevel != slog.LevelInfo {
		t.Errorf("DevFakeSource %v, LogLevel %v", c.DevFakeSource, c.LogLevel)
	}
}

func TestConfigReportsEveryProblem(t *testing.T) {
	_, err := Load(env(map[string]string{
		"SHELF_TIMEZONE":       "Mars/Olympus",
		"SHELF_LOG_LEVEL":      "loud",
		"SHELF_QOBUZ_WORKERS":  "0",
		"SHELF_TELEGRAM_USERS": "1, bob",
		"SHELF_HTTP_ADDR":      "",
	}))
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{"SHELF_TIMEZONE", "SHELF_LOG_LEVEL", "SHELF_QOBUZ_WORKERS", `"bob"`, "SHELF_HTTP_ADDR is empty"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %s:\n%v", want, err)
		}
	}
}

func TestConfigRejectsBadValues(t *testing.T) {
	for _, tc := range []struct{ name, value string }{
		{"SHELF_QOBUZ_DELAY", "soon"},
		{"SHELF_QOBUZ_DELAY", "-1s"},
		{"SHELF_AMDL_TIMEOUT", "0s"},
		{"SHELF_NAVIDROME_SCAN_DELAY", "30"},
		{"SHELF_QOBUZ_FALLBACK", "maybe"},
		{"SHELF_DEV_FAKE_SOURCE", "yes"},
		{"SHELF_QOBUZ_WORKERS", "three"},
		{"SHELF_QOBUZ_QUALITY", "28"},
		{"SHELF_QOBUZ_QUALITY", "27.0"},
		{"SHELF_TELEGRAM_USERS", "-5"},
		{"SHELF_PUBLIC_URL", "shelf.example.com"},
		{"SHELF_PUBLIC_URL", "ftp://shelf.example.com"},
		{"SHELF_WRAPPER_URL", "http://"},
		{"SHELF_WRAPPER_CONTROL_URL", ""},
		{"SHELF_NAVIDROME_URL", "navidrome:4533"},
		{"SHELF_PROXY_URL", "ftp://proxy:21"},
		{"SHELF_TIMEZONE", "Europe/Atlantis"},
		{"SHELF_TIMEZONE", ""},
		{"SHELF_QOBUZ_DIR", "../elsewhere"},
		{"SHELF_APPLE_DIR", "/music/apple"},
		{"SHELF_DATA_DIR", " "},
	} {
		t.Run(tc.name+"="+tc.value, func(t *testing.T) {
			_, err := Load(env(map[string]string{tc.name: tc.value}))
			if err == nil || !strings.Contains(err.Error(), tc.name) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestConfigEmptyTurnsPartsOff(t *testing.T) {
	c, err := Load(env(map[string]string{"SHELF_AMDL_CONFIG": "", "SHELF_NAVIDROME_URL": " "}))
	if err != nil {
		t.Fatal(err)
	}
	if c.Apple.Enabled() || c.Navidrome.Enabled() {
		t.Errorf("Apple %v, Navidrome %v", c.Apple.Enabled(), c.Navidrome.Enabled())
	}
}

func TestConfigParsesValues(t *testing.T) {
	c, err := Load(env(map[string]string{
		"SHELF_PUBLIC_URL":         "https://shelf.example.com",
		"SHELF_PROXY_URL":          "socks5h://proxy:1080",
		"SHELF_QOBUZ_TOKEN":        "tok",
		"SHELF_QOBUZ_DIR":          "music/qobuz/",
		"SHELF_QOBUZ_QUALITY":      "6",
		"SHELF_QOBUZ_FALLBACK":     "false",
		"SHELF_QOBUZ_DELAY":        "1s",
		"SHELF_QOBUZ_USE_PROXY":    "true",
		"SHELF_TELEGRAM_USERS":     " 101, 202,,303 ",
		"SHELF_TELEGRAM_USE_PROXY": "1",
		"SHELF_TIMEZONE":           "UTC",
		"SHELF_LOG_LEVEL":          "DEBUG",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if c.PublicURL.Host != "shelf.example.com" || c.Proxy.Scheme != "socks5h" {
		t.Errorf("PublicURL %v, Proxy %v", c.PublicURL, c.Proxy)
	}
	if !c.Qobuz.Enabled() || c.Qobuz.Token.Reveal() != "tok" || c.Qobuz.Dir != "music/qobuz" || c.Qobuz.Quality != 6 ||
		c.Qobuz.Fallback || c.Qobuz.Delay != time.Second || !c.Qobuz.UseProxy {
		t.Errorf("Qobuz = %+v", c.Qobuz)
	}
	if !slices.Equal(c.Telegram.Users, []int64{101, 202, 303}) || !c.Telegram.UseProxy {
		t.Errorf("Telegram = %+v", c.Telegram)
	}
	if c.TZ != time.UTC || c.LogLevel != slog.LevelDebug {
		t.Errorf("TZ %v, LogLevel %v", c.TZ, c.LogLevel)
	}
}

func TestConfigUseProxyNeedsAProxy(t *testing.T) {
	_, err := Load(env(map[string]string{"SHELF_QOBUZ_USE_PROXY": "true", "SHELF_TELEGRAM_USE_PROXY": "true"}))
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{"SHELF_QOBUZ_USE_PROXY", "SHELF_TELEGRAM_USE_PROXY"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %s:\n%v", want, err)
		}
	}
}

func TestConfigWarnsTelegramWithoutUsers(t *testing.T) {
	c, err := Load(env(map[string]string{"SHELF_TELEGRAM_TOKEN": "123:abc"}))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(c.Warnings(), func(w string) bool { return strings.Contains(w, "SHELF_TELEGRAM_USERS") }) {
		t.Errorf("warnings = %q", c.Warnings())
	}

	c, err = Load(env(map[string]string{"SHELF_TELEGRAM_TOKEN": "123:abc", "SHELF_TELEGRAM_USERS": "42"}))
	if err != nil {
		t.Fatal(err)
	}
	if slices.ContainsFunc(c.Warnings(), func(w string) bool { return strings.Contains(w, "SHELF_TELEGRAM_USERS") }) {
		t.Errorf("warns with a user: %q", c.Warnings())
	}
}

func TestConfigWarnsMissingCredentials(t *testing.T) {
	c, err := Load(env(nil))
	if err != nil {
		t.Fatal(err)
	}
	w := strings.Join(c.Warnings(), "\n")
	for _, want := range []string{"SHELF_NAVIDROME_PASSWORD", "SHELF_WRAPPER_TOKEN"} {
		if !strings.Contains(w, want) {
			t.Errorf("warnings miss %s:\n%s", want, w)
		}
	}

	c, err = Load(env(map[string]string{"SHELF_AMDL_CONFIG": "", "SHELF_NAVIDROME_URL": ""}))
	if err != nil {
		t.Fatal(err)
	}
	if w := c.Warnings(); len(w) != 0 {
		t.Errorf("warnings for parts that are off: %q", w)
	}
}

func TestConfigHidesSecrets(t *testing.T) {
	secrets := map[string]string{
		"SHELF_QOBUZ_TOKEN":        "qobuz-s3cret",
		"SHELF_TELEGRAM_TOKEN":     "telegram-s3cret",
		"SHELF_NAVIDROME_PASSWORD": "navidrome-s3cret",
		"SHELF_WRAPPER_TOKEN":      "wrapper-s3cret",
		"SHELF_PROXY_URL":          "http://user:proxy-s3cret@proxy:3128",
	}
	c, err := Load(env(secrets))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q"} {
		fmt.Fprintf(&out, verb+"\n", c)
		fmt.Fprintf(&out, verb+"\n", *c)
		fmt.Fprintf(&out, verb+"\n", struct{ Cfg *Config }{c})
		fmt.Fprintf(&out, verb+"\n", c.Qobuz)
		fmt.Fprintf(&out, verb+"\n", c.Telegram.Token)
	}
	fmt.Fprintln(&out, c.Qobuz.Token, c.Apple, c.Navidrome)
	slog.New(slog.NewJSONHandler(&out, nil)).Info("start", "cfg", c, "qobuz", c.Qobuz, "token", c.Qobuz.Token)
	slog.New(slog.NewTextHandler(&out, nil)).Info("start", "cfg", *c, "telegram", c.Telegram)
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	out.Write(b)

	for name, v := range secrets {
		secret := v
		if name == "SHELF_PROXY_URL" {
			secret = "proxy-s3cret"
		}
		if strings.Contains(out.String(), secret) {
			t.Errorf("%s leaked:\n%s", name, out.String())
		}
	}
	if c.Qobuz.Token.Reveal() != "qobuz-s3cret" {
		t.Error("Reveal lost the value")
	}
}

func TestConfigErrorsHideTheProxyURL(t *testing.T) {
	_, err := Load(env(map[string]string{"SHELF_PROXY_URL": "ftp://user:proxy-s3cret@proxy:21"}))
	if err == nil || strings.Contains(err.Error(), "proxy-s3cret") {
		t.Fatalf("err = %v", err)
	}
}

func TestAuthConfiguration(t *testing.T) {
	c, err := Load(env(nil))
	if err != nil || c.AuthProvider != "none" {
		t.Fatal(c, err)
	}
	for _, vars := range []map[string]string{
		{"SHELF_AUTH_PROVIDER": ""}, {"SHELF_AUTH_PROVIDER": "other"},
		{"SHELF_AUTH_PROVIDER": "simple"},
		{"SHELF_AUTH_PROVIDER": "simple", "SHELF_PUBLIC_URL": "http://localhost:8080"},
		{"SHELF_AUTH_PROVIDER": "simple", "SHELF_PUBLIC_URL": "https://user:pass@shelf.example.com"},
		{"SHELF_AUTH_PROVIDER": "simple", "SHELF_PUBLIC_URL": "https://shelf.example.com/path"},
		{"SHELF_AUTH_PROVIDER": "simple", "SHELF_PUBLIC_URL": "https://shelf.example.com?extra=1"},
	} {
		if _, err := Load(env(vars)); err == nil {
			t.Fatalf("accepted invalid auth config %v", vars)
		}
	}
	c, err = Load(env(map[string]string{"SHELF_AUTH_PROVIDER": "simple", "SHELF_PUBLIC_URL": "https://shelf.example.com"}))
	if err != nil || c.AuthProvider != "simple" {
		t.Fatal(c, err)
	}
}
