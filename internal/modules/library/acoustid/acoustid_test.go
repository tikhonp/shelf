package acoustid

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tikhonp/shelf/internal/modules/library/musicbrainz"
)

func client(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := New("secret-key", "test")
	c.Base, c.limit = srv.URL, musicbrainz.NewLimiter(0)
	return c
}

func TestLookupKeepsTheKeyOutOfTheAddress(t *testing.T) {
	c := client(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.String(), "secret-key") || r.Method != http.MethodPost || r.URL.Path != "/lookup" {
			t.Errorf("%s %s", r.Method, r.URL)
		}
		b, _ := io.ReadAll(r.Body)
		f, err := url.ParseQuery(string(b))
		if err != nil || f.Get("client") != "secret-key" || f.Get("fingerprint") != "AQAA" || f.Get("duration") != "201" || f.Get("meta") != "recordings releaseids" {
			t.Errorf("%s", b)
		}
		_, _ = w.Write([]byte(`{"status": "ok", "results": [{"score": 0.93, "recordings": [{"id": "rec", "releases": [{"id": "r1"}, {"id": "r2"}]}]}, {"score": 0.5}]}`))
	})
	got, err := c.Lookup(context.Background(), Print{Duration: 201.4, Fingerprint: "AQAA"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Score != 0.93 || got[0].Recording != "rec" || len(got[0].Releases) != 2 {
		t.Fatalf("%+v", got)
	}
}

func TestLookupReportsTheServiceError(t *testing.T) {
	c := client(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"status": "error", "error": {"code": 4, "message": "invalid API key"}}`))
	})
	_, err := c.Lookup(context.Background(), Print{Fingerprint: "AQAA"})
	if err == nil || !strings.Contains(err.Error(), "invalid API key") || strings.Contains(err.Error(), "secret-key") {
		t.Fatal(err)
	}
	c = client(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("<html>")) })
	if _, err := c.Lookup(context.Background(), Print{}); err == nil {
		t.Fatal("no error")
	}
}

func script(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "fpcalc")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestFingerprint(t *testing.T) {
	c := New("k", "test")
	c.FPCalc = script(t, `echo '{"duration": 12.5, "fingerprint": "AQAA"}'`)
	got, err := c.Fingerprint(context.Background(), "/x.flac")
	if err != nil || got.Duration != 12.5 || got.Fingerprint != "AQAA" {
		t.Fatalf("%+v %v", got, err)
	}
	c.FPCalc = script(t, `echo 'bad file' >&2; exit 1`)
	if _, err := c.Fingerprint(context.Background(), "/x.flac"); err == nil || !strings.Contains(err.Error(), "bad file") {
		t.Fatal(err)
	}
	c.FPCalc = script(t, `echo '{}'`)
	if _, err := c.Fingerprint(context.Background(), "/x.flac"); err == nil {
		t.Fatal("no error")
	}
	c.FPCalc = filepath.Join(t.TempDir(), "gone")
	if _, err := c.Fingerprint(context.Background(), "/x.flac"); err == nil {
		t.Fatal("no error")
	}
}
