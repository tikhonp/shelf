package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/tikhonp/shelf/internal/platform/testwait"
	"github.com/tikhonp/shelf/internal/wrapperapi"
)

// rig runs a supervisor over testdata/fake-launcher.sh, with a fake
// /status that lists the region the fake sign-in writes.
type rig struct {
	t    *testing.T
	s    *Supervisor
	data string
	api  *httptest.Server

	mu     sync.Mutex
	starts []start

	cancel context.CancelFunc
	done   chan error
}

type start struct {
	mode string
	at   time.Time
}

func newRig(t *testing.T, env ...string) *rig {
	t.Helper()
	launcher, err := filepath.Abs("testdata/fake-launcher.sh")
	if err != nil {
		t.Fatal(err)
	}
	data := t.TempDir()
	status := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, err := os.Stat(filepath.Join(data, "listening")); err != nil {
			http.Error(w, "not listening", http.StatusServiceUnavailable)
			return
		}
		regions := []string{}
		if b, err := os.ReadFile(filepath.Join(data, "STOREFRONT_ID")); err == nil {
			regions = append(regions, strings.TrimSpace(string(b)))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "msg": "SUCCESS", "data": map[string]any{"regions": regions}})
	}))
	t.Cleanup(status.Close)
	cfg := Config{
		Launcher: launcher, Dir: t.TempDir(), DataDir: data, BaseDir: "/data", Host: "0.0.0.0", Port: 12340,
		StatusURL: status.URL + "/status", Env: append(append(os.Environ(), "FAKE_DATA="+data), env...),
		BackoffMin: 20 * time.Millisecond, BackoffMax: 160 * time.Millisecond, StableAfter: time.Hour,
		StopGrace: 2 * time.Second, CodeWindow: 60 * time.Second,
		PollEvery: 30 * time.Millisecond, PollStarting: 20 * time.Millisecond,
	}
	r := &rig{t: t, data: data}
	r.s = NewSupervisor(cfg, newRing(keepLines, nil))
	r.s.onStart = func(mode string) {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.starts = append(r.starts, start{mode, time.Now()})
	}
	r.api = httptest.NewServer(control(r.s, "secret-token", slog.New(slog.NewTextHandler(io.Discard, nil))))
	t.Cleanup(r.api.Close)
	return r
}

// run starts the supervisor; cleanup stops it.
func (r *rig) run() *rig {
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel, r.done = cancel, make(chan error, 1)
	go func() { r.done <- r.s.Run(ctx) }()
	r.t.Cleanup(func() {
		cancel()
		select {
		case err := <-r.done:
			if err != nil {
				r.t.Error(err)
			}
		case <-time.After(testwait.For(15 * time.Second)):
			r.t.Error("the supervisor did not stop")
		}
	})
	return r
}

func (r *rig) startsOf(mode string) []start {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []start
	for _, s := range r.starts {
		if s.mode == mode {
			out = append(out, s)
		}
	}
	return out
}

func (r *rig) waitFor(what string, ok func(wrapperapi.Status) bool) wrapperapi.Status {
	r.t.Helper()
	deadline := testwait.Deadline(10 * time.Second)
	for {
		st := r.s.Status()
		if ok(st) {
			return st
		}
		if time.Now().After(deadline) {
			r.t.Fatalf("waiting for %s; state %+v; log:\n%s", what, st, r.logText())
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func (r *rig) waitState(want wrapperapi.State) wrapperapi.Status {
	r.t.Helper()
	return r.waitFor(string(want), func(st wrapperapi.Status) bool { return st.State == want })
}

func (r *rig) logText() string {
	var b strings.Builder
	for _, l := range r.s.ring.after(0) {
		b.WriteString(l.Text + "\n")
	}
	return b.String()
}

// call sends one control request with the right token.
func (r *rig) call(method, path string, body any) (int, []byte) {
	r.t.Helper()
	return callAPI(r.t, r.api.URL, "Bearer secret-token", method, path, body)
}

func callAPI(t *testing.T, base, auth, method, path string, body any) (int, []byte) {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, base+path, rd)
	if err != nil {
		t.Fatal(err)
	}
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, out
}

func (r *rig) signedIn() {
	if err := os.WriteFile(filepath.Join(r.data, "STOREFRONT_ID"), []byte("RU\n"), 0o600); err != nil {
		r.t.Fatal(err)
	}
}

func TestStateRunning(t *testing.T) {
	r := newRig(t)
	r.signedIn()
	r.run()
	st := r.waitState(wrapperapi.Running)
	if st.Storefront != "ru" || st.Since.IsZero() || st.Error != "" {
		t.Fatalf("%+v", st)
	}
	code, body := r.call(http.MethodGet, wrapperapi.PathState, nil)
	var got wrapperapi.Status
	if code != http.StatusOK || json.Unmarshal(body, &got) != nil || got.State != wrapperapi.Running {
		t.Fatalf("%d %s", code, body)
	}
}

func TestStateSignedOut(t *testing.T) {
	r := newRig(t).run()
	st := r.waitState(wrapperapi.SignedOut)
	if st.Storefront != "" {
		t.Fatalf("%+v", st)
	}
	// Requests for /status stay out of the log.
	if text := r.logText(); strings.Contains(text, "GET /status") || !strings.Contains(text, "listening on") {
		t.Fatalf("log:\n%s", text)
	}
}

func TestServiceRestarts(t *testing.T) {
	r := newRig(t, "FAKE_SERVICE=exit").run()
	deadline := testwait.Deadline(10 * time.Second)
	for len(r.startsOf("service")) < 6 {
		if time.Now().After(deadline) {
			t.Fatalf("%d starts", len(r.startsOf("service")))
		}
		time.Sleep(5 * time.Millisecond)
	}
	starts := r.startsOf("service")
	// Gaps of 20, 40, 80, 160, then 160 ms: they double up to the maximum.
	want := []time.Duration{20, 40, 80, 160, 160}
	for i, w := range want {
		gap := starts[i+1].at.Sub(starts[i].at)
		if gap < w*time.Millisecond {
			t.Errorf("gap %d is %s, want at least %dms", i, gap, w)
		}
	}
	st := r.s.Status()
	if st.State != wrapperapi.Error || !strings.Contains(st.Error, "exited with code 3") || !strings.Contains(st.Error, "fake crash") {
		t.Fatalf("%+v", st)
	}
}

func TestStopSequence(t *testing.T) {
	t.Run("SIGINT", func(t *testing.T) {
		r := newRig(t).run()
		r.waitState(wrapperapi.SignedOut)
		begin := time.Now()
		r.cancel()
		if err := <-r.done; err != nil {
			t.Fatal(err)
		}
		r.done <- nil
		if time.Since(begin) > time.Second {
			t.Fatalf("stop took %s", time.Since(begin))
		}
		if !strings.Contains(r.logText(), "received signal 2") {
			t.Fatalf("wrapper-lite never saw SIGINT:\n%s", r.logText())
		}
	})
	t.Run("SIGKILL", func(t *testing.T) {
		r := newRig(t, "FAKE_SERVICE=stubborn")
		r.s.cfg.StopGrace = 300 * time.Millisecond
		r.run()
		r.waitState(wrapperapi.SignedOut)
		r.s.mu.Lock()
		pid := r.s.pid
		r.s.mu.Unlock()
		begin := time.Now()
		r.cancel()
		if err := <-r.done; err != nil {
			t.Fatal(err)
		}
		r.done <- nil
		if took := time.Since(begin); took < 300*time.Millisecond {
			t.Fatalf("killed after %s, before the grace ended", took)
		}
		if err := syscall.Kill(-pid, 0); !errors.Is(err, syscall.ESRCH) {
			t.Fatalf("the process group lives on: %v", err)
		}
		if !strings.Contains(r.logText(), "did not stop within") {
			t.Fatalf("no note of the kill:\n%s", r.logText())
		}
	})
}

func (r *rig) login(user, pass string) {
	r.t.Helper()
	code, body := r.call(http.MethodPost, wrapperapi.PathLogin, wrapperapi.LoginRequest{Username: user, Password: pass})
	if code != http.StatusAccepted {
		r.t.Fatalf("login: %d %s", code, body)
	}
}

func TestLoginAwaitsCode(t *testing.T) {
	r := newRig(t).run()
	r.waitState(wrapperapi.SignedOut)
	r.login("me@example.com", "hunter22")
	before := time.Now()
	st := r.waitState(wrapperapi.AwaitingCode)
	if st.CodeDeadline == nil {
		t.Fatal("no deadline")
	}
	left := time.Until(*st.CodeDeadline)
	if left > 60*time.Second || left < 50*time.Second || st.CodeDeadline.Before(before) {
		t.Fatalf("deadline in %s", left)
	}
	if got := r.startsOf("login"); len(got) != 1 {
		t.Fatalf("%d sign-in runs", len(got))
	}
}

func TestCodeOutsideAwaiting(t *testing.T) {
	r := newRig(t).run()
	r.waitState(wrapperapi.SignedOut)
	if code, body := r.call(http.MethodPost, wrapperapi.PathCode, wrapperapi.CodeRequest{Code: "123456"}); code != http.StatusConflict {
		t.Fatalf("%d %s", code, body)
	}
	if _, err := os.Stat(filepath.Join(r.data, codeFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a code file was written: %v", err)
	}
	for _, bad := range []string{"12345", "1234567", "12a456", ""} {
		if code, _ := r.call(http.MethodPost, wrapperapi.PathCode, wrapperapi.CodeRequest{Code: bad}); code != http.StatusBadRequest {
			t.Errorf("%q answered %d", bad, code)
		}
	}
}

func TestCodeFileWritten(t *testing.T) {
	r := newRig(t)
	r.s.mu.Lock()
	r.s.setLocked(wrapperapi.AwaitingCode, "", "")
	r.s.mu.Unlock()
	if err := r.s.SubmitCode("654321"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(r.data, codeFile)
	b, err := os.ReadFile(path)
	if err != nil || string(b) != "654321\n" {
		t.Fatalf("%q %v", b, err)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", fi.Mode().Perm())
	}
	if tmps, _ := filepath.Glob(filepath.Join(r.data, ".2fa-*")); len(tmps) != 0 {
		t.Fatalf("temp files left: %v", tmps)
	}
	if st := r.s.Status(); st.State != wrapperapi.SigningIn || st.CodeDeadline != nil {
		t.Fatalf("%+v", st)
	}
	// The code is sent once: a second one has no sign-in waiting for it.
	if err := r.s.SubmitCode("654321"); !errors.Is(err, errNotAwaiting) {
		t.Fatal(err)
	}
}

func TestLoginSuccess(t *testing.T) {
	r := newRig(t).run()
	r.waitState(wrapperapi.SignedOut)
	r.login("me@example.com", "hunter22")
	r.waitState(wrapperapi.AwaitingCode)
	if code, body := r.call(http.MethodPost, wrapperapi.PathCode, wrapperapi.CodeRequest{Code: "123456"}); code != http.StatusOK {
		t.Fatalf("%d %s", code, body)
	}
	st := r.waitState(wrapperapi.Running)
	if st.Storefront != "ru" {
		t.Fatalf("%+v", st)
	}
	if n := len(r.startsOf("service")); n != 2 {
		t.Fatalf("service mode started %d times, want 2", n)
	}
	if !strings.Contains(r.logText(), "signed in as me@example.com") {
		t.Fatal(r.logText())
	}
}

func TestLoginWithoutCode(t *testing.T) {
	r := newRig(t, "FAKE_LOGIN=direct").run()
	r.waitState(wrapperapi.SignedOut)
	r.login("me@example.com", "hunter22")
	r.waitState(wrapperapi.Running)
}

func TestLoginFailure(t *testing.T) {
	r := newRig(t, "FAKE_LOGIN=fail").run()
	r.waitState(wrapperapi.SignedOut)
	r.login("me@example.com", "wrong")
	st := r.waitState(wrapperapi.Error)
	if st.Error != "auth error: code=-20101, message=Your Apple ID or password was entered incorrectly." {
		t.Fatalf("%q", st.Error)
	}
	deadline := testwait.Deadline(10 * time.Second)
	for len(r.startsOf("service")) < 2 {
		if time.Now().After(deadline) {
			t.Fatal("service mode did not start again")
		}
		time.Sleep(5 * time.Millisecond)
	}
	// Service mode answers with no region, and the error stays on show.
	time.Sleep(200 * time.Millisecond)
	if st := r.s.Status(); st.State != wrapperapi.Error {
		t.Fatalf("%+v", st)
	}
	// A wrong code fails the same way.
	r2 := newRig(t).run()
	r2.waitState(wrapperapi.SignedOut)
	r2.login("me@example.com", "hunter22")
	r2.waitState(wrapperapi.AwaitingCode)
	r2.call(http.MethodPost, wrapperapi.PathCode, wrapperapi.CodeRequest{Code: "000000"})
	if st := r2.waitState(wrapperapi.Error); !strings.HasPrefix(st.Error, "auth error: code=-22421") {
		t.Fatalf("%q", st.Error)
	}
}

func TestLoginCodeTimeout(t *testing.T) {
	r := newRig(t, "FAKE_WAIT=3").run()
	r.waitState(wrapperapi.SignedOut)
	r.login("me@example.com", "hunter22")
	st := r.waitState(wrapperapi.Error)
	if st.Error != "2FA code timeout (60s), aborting login" {
		t.Fatalf("%q", st.Error)
	}
}

func TestLoginWhileRunning(t *testing.T) {
	r := newRig(t).run()
	r.waitState(wrapperapi.SignedOut)
	r.login("me@example.com", "hunter22")
	r.waitState(wrapperapi.AwaitingCode)
	code, body := r.call(http.MethodPost, wrapperapi.PathLogin, wrapperapi.LoginRequest{Username: "me@example.com", Password: "x"})
	if code != http.StatusConflict || !strings.Contains(string(body), "sign-in is running") {
		t.Fatalf("%d %s", code, body)
	}
	if n := len(r.startsOf("login")); n != 1 {
		t.Fatalf("%d sign-in runs", n)
	}
	for _, bad := range []wrapperapi.LoginRequest{{Username: "", Password: "x"}, {Username: "a", Password: ""}} {
		if code, _ := r.call(http.MethodPost, wrapperapi.PathLogin, bad); code != http.StatusBadRequest {
			t.Errorf("%+v answered %d", bad, code)
		}
	}
}

func TestRestartWakesPoll(t *testing.T) {
	r := newRig(t)
	r.s.cfg.PollEvery = time.Hour
	r.run()
	r.waitState(wrapperapi.SignedOut)
	code, body := r.call(http.MethodPost, wrapperapi.PathRestart, nil)
	var st wrapperapi.Status
	if code != http.StatusAccepted || json.Unmarshal(body, &st) != nil || st.State != wrapperapi.Starting {
		t.Fatalf("%d %s", code, body)
	}
	// The new run answers long before PollEvery.
	r.waitState(wrapperapi.SignedOut)
	if n := len(r.startsOf("service")); n != 2 {
		t.Fatalf("%d starts", n)
	}
}

func TestRestartStopsSignIn(t *testing.T) {
	r := newRig(t).run()
	r.waitState(wrapperapi.SignedOut)
	r.login("me@example.com", "hunter22")
	r.waitState(wrapperapi.AwaitingCode)
	if code, body := r.call(http.MethodPost, wrapperapi.PathRestart, nil); code != http.StatusAccepted {
		t.Fatalf("%d %s", code, body)
	}
	r.waitState(wrapperapi.SignedOut)
	if !strings.Contains(r.logText(), "sign-in stopped by a restart") {
		t.Fatal(r.logText())
	}
	if n := len(r.startsOf("service")); n != 2 {
		t.Fatalf("service mode started %d times", n)
	}
}

func TestControlNeedsToken(t *testing.T) {
	r := newRig(t)
	for _, auth := range []string{"", "Bearer wrong", "secret-token", "Bearer secret-token2"} {
		if code, _ := callAPI(t, r.api.URL, auth, http.MethodGet, wrapperapi.PathState, nil); code != http.StatusUnauthorized {
			t.Errorf("%q answered %d", auth, code)
		}
	}
	if code, _ := r.call(http.MethodGet, wrapperapi.PathState, nil); code != http.StatusOK {
		t.Fatalf("the right token answered %d", code)
	}
	// No configured token refuses everyone, even an empty bearer.
	open := httptest.NewServer(control(r.s, "", slog.New(slog.NewTextHandler(io.Discard, nil))))
	defer open.Close()
	for _, auth := range []string{"", "Bearer ", "Bearer secret-token"} {
		if code, _ := callAPI(t, open.URL, auth, http.MethodGet, wrapperapi.PathState, nil); code != http.StatusUnauthorized {
			t.Errorf("no token: %q answered %d", auth, code)
		}
	}
	if code, _ := r.call(http.MethodGet, "/v1/nothing", nil); code != http.StatusNotFound {
		t.Errorf("unknown route answered %d", code)
	}
}

func TestLogRingRedacts(t *testing.T) {
	ring := newRing(keepLines, nil)
	for i := 1; i <= 1500; i++ {
		ring.add(fmt.Sprintf("line %d", i))
	}
	lines := ring.after(0)
	if len(lines) != 1000 || lines[0].N != 501 || lines[999].Text != "line 1500" {
		t.Fatalf("%d lines from %d", len(lines), lines[0].N)
	}
	if got := ring.after(1490); len(got) != 10 || got[0].N != 1491 {
		t.Fatalf("%d after 1490", len(got))
	}

	// A sign-in whose launcher echoes its arguments shows no password.
	var echoed []string
	var mu sync.Mutex
	r := newRig(t, "FAKE_LOGIN=fail", "FAKE_ECHO=1")
	r.s.ring.echo = func(l wrapperapi.LogLine) {
		mu.Lock()
		defer mu.Unlock()
		echoed = append(echoed, l.Text)
	}
	r.run()
	r.waitState(wrapperapi.SignedOut)
	r.login("me@example.com", "p4ss:w0rd!")
	r.waitState(wrapperapi.Error)
	text := r.logText()
	if strings.Contains(text, "p4ss:w0rd!") || !strings.Contains(text, "--login me@example.com:[redacted]") {
		t.Fatalf("log:\n%s", text)
	}
	code, body := r.call(http.MethodGet, wrapperapi.PathLog+"?after=0", nil)
	if code != http.StatusOK || bytes.Contains(body, []byte("p4ss")) {
		t.Fatalf("%d %s", code, body)
	}
	var page wrapperapi.LogPage
	if err := json.Unmarshal(body, &page); err != nil || len(page.Lines) == 0 {
		t.Fatalf("%v %s", err, body)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, l := range echoed {
		if strings.Contains(l, "p4ss") {
			t.Fatalf("stdout got %q", l)
		}
	}
}

func TestSplitArgs(t *testing.T) {
	got, err := splitArgs(` --device-info "Music/5.0.2/Android/10/Pixel 8/7663314/en-US/en-US/0123" --log-level 'debug'  -x "" `)
	want := []string{"--device-info", "Music/5.0.2/Android/10/Pixel 8/7663314/en-US/en-US/0123", "--log-level", "debug", "-x", ""}
	if err != nil || !slices.Equal(got, want) {
		t.Fatalf("%q %v", got, err)
	}
	if got, err := splitArgs(""); err != nil || len(got) != 0 {
		t.Fatalf("%q %v", got, err)
	}
	if _, err := splitArgs(`--proxy "http://x`); err == nil {
		t.Fatal("an open quote was accepted")
	}
}

func TestMessage(t *testing.T) {
	for in, want := range map[string]string{
		"2026-11-04 12:00:00.123 [WARN ] auth error: code=1":   "auth error: code=1",
		"2026-11-04 12:00:00.123 [ERROR] login failed":         "login failed",
		"2026-11-04 12:00:00.123 [INFO ] login [a] b":          "login [a] b",
		"no prefix [x] here":                                   "no prefix [x] here",
		"shelf-wrapper: starting wrapper-lite in service mode": "shelf-wrapper: starting wrapper-lite in service mode",
	} {
		if got := message(in); got != want {
			t.Errorf("message(%q) = %q, want %q", in, got, want)
		}
	}
}
