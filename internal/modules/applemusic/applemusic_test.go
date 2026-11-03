package applemusic

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/tikhonp/shelf/internal/platform/testwait"
	"github.com/tikhonp/shelf/internal/platform/web"
	"github.com/tikhonp/shelf/internal/source"
	"github.com/tikhonp/shelf/internal/wrapperapi"
)

func (r *rig) do(method, path, body string) *httptest.ResponseRecorder {
	e := echo.New()
	e.HTTPErrorHandler = web.ErrorHandler
	r.m.Routes(e.Group("/api"))
	rec := httptest.NewRecorder()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rd)
	req.Header.Set("Content-Type", "application/json")
	e.ServeHTTP(rec, req)
	return rec
}

func TestModuleEnabled(t *testing.T) {
	r := newRig(t)
	if !r.s.Enabled() || r.s.ID() != "applemusic" || r.s.Name() != "Apple Music" || len(r.m.Sources()) != 1 {
		t.Fatal("not enabled")
	}
	f, ok := source.DefaultFormat(r.s.Formats())
	if !ok || f.ID != "applemusic:alac" {
		t.Fatalf("%+v", f)
	}
	// Init writes nothing: the config is rendered before a run.
	if _, err := os.Stat(filepath.Join(r.data, "amdl")); !os.IsNotExist(err) {
		t.Fatalf("Init wrote %v", err)
	}
}

func TestMissingBaseConfig(t *testing.T) {
	// A file that isn't there turns the source off; the wrapper routes stay,
	// so the Status page can still sign in.
	r := newRig(t, "SHELF_AMDL_CONFIG=/nowhere/amdl.yaml")
	if r.s.Enabled() {
		t.Fatal("enabled without a config")
	}
	if rec := r.do(http.MethodGet, "/api/applemusic/wrapper", ""); rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	// So does a broken one.
	broken := filepath.Join(t.TempDir(), "broken.yaml")
	if err := os.WriteFile(broken, []byte("general: [\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if r := newRig(t, "SHELF_AMDL_CONFIG="+broken); r.s.Enabled() {
		t.Fatal("enabled with a broken config")
	}
	// An empty SHELF_AMDL_CONFIG turns Apple Music off, routes too.
	r = newRig(t, "SHELF_AMDL_CONFIG=")
	if r.s.Enabled() {
		t.Fatal("enabled")
	}
	if rec := r.do(http.MethodGet, "/api/applemusic/wrapper", ""); rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "not set up") {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if err := r.m.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	// The source still knows its links, so they are refused by name.
	if _, ok := r.s.ParseLink("https://music.apple.com/ru/album/1"); !ok {
		t.Fatal("a switched-off source forgot its links")
	}
}

func decodeStatus(t *testing.T, rec *httptest.ResponseRecorder) wrapperapi.Status {
	t.Helper()
	var st wrapperapi.Status
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatalf("%v: %s", err, rec.Body)
	}
	return st
}

func TestWrapperRoutes(t *testing.T) {
	r := newRig(t)
	rec := r.do(http.MethodGet, "/api/applemusic/wrapper", "")
	if rec.Code != http.StatusOK || decodeStatus(t, rec).Storefront != "ru" {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}

	rec = r.do(http.MethodPost, "/api/applemusic/wrapper/login", `{"username":" me@example.com ","password":"p4ss w0rd"}`)
	if rec.Code != http.StatusAccepted || decodeStatus(t, rec).State != wrapperapi.SigningIn {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if len(r.wrap.logins) != 1 || r.wrap.logins[0] != (wrapperapi.LoginRequest{Username: "me@example.com", Password: "p4ss w0rd"}) {
		t.Fatalf("%+v", r.wrap.logins)
	}
	// The supervisor's 409 reaches the client with its message.
	rec = r.do(http.MethodPost, "/api/applemusic/wrapper/login", `{"username":"me@example.com","password":"x"}`)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "a sign-in is running") {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	for _, bad := range []string{`{"username":"","password":"x"}`, `{"username":"a"}`, `not json`} {
		if rec := r.do(http.MethodPost, "/api/applemusic/wrapper/login", bad); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d", bad, rec.Code)
		}
	}

	r.wrap.set(func(f *fakeWrapper) { f.state = wrapperapi.Status{State: wrapperapi.AwaitingCode} })
	rec = r.do(http.MethodPost, "/api/applemusic/wrapper/code", `{"code":" 123456 "}`)
	if rec.Code != http.StatusOK || len(r.wrap.codes) != 1 || r.wrap.codes[0] != "123456" {
		t.Fatalf("%d %s %v", rec.Code, rec.Body, r.wrap.codes)
	}
	if rec := r.do(http.MethodPost, "/api/applemusic/wrapper/code", `{"code":"123456"}`); rec.Code != http.StatusConflict {
		t.Fatalf("%d", rec.Code)
	}

	rec = r.do(http.MethodPost, "/api/applemusic/wrapper/restart", "")
	if rec.Code != http.StatusAccepted || r.wrap.restarts != 1 || decodeStatus(t, rec).State != wrapperapi.Starting {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}

	rec = r.do(http.MethodGet, "/api/applemusic/wrapper/log?after=3", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "wrapper-lite listening") {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if rec := r.do(http.MethodGet, "/api/applemusic/wrapper/log?after=x", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("%d", rec.Code)
	}

	// A wrong token is shelf's problem to report, not the client's.
	r2 := newRig(t, "SHELF_WRAPPER_TOKEN=wrong")
	rec = r2.do(http.MethodGet, "/api/applemusic/wrapper", "")
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "SHELF_WRAPPER_TOKEN") {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if h := r2.s.Health(bg); h.OK || !strings.Contains(h.Message, "refused") {
		t.Fatalf("%+v", h)
	}
	// A wrapper that doesn't answer is 503.
	r.wrap.set(func(f *fakeWrapper) { f.down = true })
	rec = r.do(http.MethodGet, "/api/applemusic/wrapper", "")
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "does not answer") {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
}

// TestPasswordNeverLogged sends a sign-in through shelf with a log that
// keeps everything, and looks for the password.
func TestPasswordNeverLogged(t *testing.T) {
	r := newRig(t)
	var buf bytes.Buffer
	r.m.log = slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	r.wrap.set(func(f *fakeWrapper) { f.state = wrapperapi.Status{State: wrapperapi.SigningIn} })
	r.do(http.MethodPost, "/api/applemusic/wrapper/login", `{"username":"me@example.com","password":"hunter2-secret"}`)
	r.wrap.set(func(f *fakeWrapper) { f.down = true })
	r.do(http.MethodPost, "/api/applemusic/wrapper/login", `{"username":"me@example.com","password":"hunter2-secret"}`)
	if strings.Contains(buf.String(), "hunter2") {
		t.Fatalf("log:\n%s", buf.String())
	}
}

func TestHealthFollowsWrapper(t *testing.T) {
	r := newRig(t)
	if h := r.s.Health(bg); h.OK || h.Message != "Waiting for the wrapper" {
		t.Fatalf("%+v", h)
	}
	events, stop := r.hub.Subscribe(SourceTopic)
	defer stop()
	ctx, cancel := context.WithCancel(bg)
	done := make(chan error, 1)
	go func() { done <- r.m.Run(ctx) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()

	next := func() SourceEvent {
		t.Helper()
		select {
		case e := <-events:
			return e.Data.(SourceEvent)
		case <-time.After(testwait.For(5 * time.Second)):
			t.Fatal("no source event")
		}
		return SourceEvent{}
	}
	e := next()
	if !e.OK || e.Message != "Signed in, storefront RU" || e.Wrapper == nil || e.Wrapper.State != wrapperapi.Running || e.ID != ID {
		t.Fatalf("%+v", e)
	}
	h := r.s.Health(bg)
	if !h.OK || h.Detail.(wrapperapi.Status).Storefront != "ru" {
		t.Fatalf("%+v", h)
	}

	r.wrap.set(func(f *fakeWrapper) { f.state = wrapperapi.Status{State: wrapperapi.SignedOut} })
	if e := next(); e.OK || e.Message != "The wrapper is signed out" {
		t.Fatalf("%+v", e)
	}
	r.wrap.set(func(f *fakeWrapper) {
		f.state = wrapperapi.Status{State: wrapperapi.Error, Error: "auth error: code=-20101, message=Wrong password."}
	})
	if e := next(); e.OK || e.Message != "auth error: code=-20101, message=Wrong password." {
		t.Fatalf("%+v", e)
	}
	r.wrap.set(func(f *fakeWrapper) { f.down = true })
	if e := next(); e.OK || e.Message != "The wrapper does not answer" || e.Wrapper != nil {
		t.Fatalf("%+v", e)
	}
	// Nothing changes, nothing is sent.
	select {
	case e := <-events:
		t.Fatalf("repeat %+v", e)
	case <-time.After(100 * time.Millisecond):
	}
}
