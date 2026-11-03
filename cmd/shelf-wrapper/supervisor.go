package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/tikhonp/shelf/internal/wrapperapi"
)

// Config is where wrapper-lite lives and how the supervisor times it.
type Config struct {
	// Launcher is wrapper-lite's host launcher, run from Dir.
	Launcher string
	Dir      string
	// DataDir is the folder that BaseDir names inside wrapper-lite's
	// chroot. The 2FA code file goes there.
	DataDir string
	BaseDir string
	Host    string
	Port    int
	// Extra are more wrapper-lite flags, from SHELF_WRAPPER_LITE_ARGS.
	Extra []string
	// StatusURL is service mode's GET /status.
	StatusURL string
	// Env is the launcher's environment; nil is the supervisor's.
	Env []string

	// BackoffMin and BackoffMax bound the wait before service mode starts
	// again after it exited by itself. A run that lasted StableAfter starts
	// the next wait from BackoffMin.
	BackoffMin  time.Duration
	BackoffMax  time.Duration
	StableAfter time.Duration
	// StopGrace is how long wrapper-lite has after SIGINT before SIGKILL.
	StopGrace time.Duration
	// CodeWindow is how long wrapper-lite waits for the code file.
	CodeWindow time.Duration
	// PollEvery and PollStarting space the reads of /status, the second
	// until service mode first answers.
	PollEvery    time.Duration
	PollStarting time.Duration
}

// DefaultConfig is the layout of the wrapper-lite image.
func DefaultConfig() Config {
	return Config{
		Launcher: "/app/wrapper-lite-rootless", Dir: "/app", DataDir: "/app/rootfs/data", BaseDir: "/data",
		Host: "0.0.0.0", Port: 12340, StatusURL: "http://127.0.0.1:12340/status",
		BackoffMin: time.Second, BackoffMax: time.Minute, StableAfter: time.Minute,
		StopGrace: 10 * time.Second, CodeWindow: 60 * time.Second,
		PollEvery: 15 * time.Second, PollStarting: time.Second,
	}
}

// codeFile is the file wrapper-lite polls for during a sign-in.
const codeFile = "2fa.txt"

// Errors the control API turns into 409 and 400.
var (
	errBusy        = errors.New("a sign-in is running")
	errNotAwaiting = errors.New("the wrapper is not waiting for a code")
	errBadCode     = errors.New("the code must be six digits")
	errQueued      = errors.New("the wrapper is busy with the last request; try again")
)

type phase int

const (
	phaseIdle phase = iota
	phaseService
	phaseLogin
)

type cmdKind int

const (
	cmdRestart cmdKind = iota
	cmdLogin
)

type command struct {
	kind  cmdKind
	login wrapperapi.LoginRequest
}

// Supervisor runs wrapper-lite: service mode for amdl, restarted when it
// exits, and a sign-in run when the control API asks for one. One
// goroutine, Run, starts and stops the processes; the control API reaches
// it through cmds and reads the state under mu.
type Supervisor struct {
	cfg  Config
	ring *ring
	http *http.Client
	cmds chan command
	// wake tells the poller that service mode started again.
	wake chan struct{}
	now  func() time.Time

	mu    sync.Mutex
	st    wrapperapi.Status
	phase phase
	// gen counts service-mode starts, so a poll of an old run changes
	// nothing.
	gen int
	// loginErr marks an error from a sign-in. It stays until a region
	// shows up, unlike an error from a crash, which the next answer clears.
	loginErr bool
	// pid is the running launcher's, for tests.
	pid int

	// onStart, when set, hears every launch with its mode. Tests use it.
	onStart func(mode string)
}

// NewSupervisor returns a supervisor that hasn't started anything.
func NewSupervisor(cfg Config, r *ring) *Supervisor {
	s := &Supervisor{
		cfg: cfg, ring: r, now: time.Now,
		http: &http.Client{Timeout: 3 * time.Second},
		cmds: make(chan command, 1),
		wake: make(chan struct{}, 1),
	}
	s.st = wrapperapi.Status{State: wrapperapi.Starting, Since: s.now().UTC()}
	return s
}

// Status is the current state.
func (s *Supervisor) Status() wrapperapi.Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.st
}

// setLocked changes the state. Since moves only when the state does.
func (s *Supervisor) setLocked(st wrapperapi.State, storefront, msg string) {
	if s.st.State != st {
		s.st.Since = s.now().UTC()
	}
	s.st.State, s.st.Storefront, s.st.Error, s.st.CodeDeadline = st, storefront, msg, nil
}

func (s *Supervisor) set(st wrapperapi.State, msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.setLocked(st, "", msg)
	s.loginErr = false
}

func (s *Supervisor) note(format string, a ...any) {
	s.ring.add("shelf-wrapper: " + fmt.Sprintf(format, a...))
}

// Login asks Run to sign in. The state is signing_in from here, so a second
// request answers 409 at once.
func (s *Supervisor) Login(req wrapperapi.LoginRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.st.Busy() {
		return errBusy
	}
	select {
	case s.cmds <- command{kind: cmdLogin, login: req}:
	default:
		return errQueued
	}
	s.setLocked(wrapperapi.SigningIn, "", "")
	s.loginErr = false
	return nil
}

// Restart asks Run to start service mode again. During a sign-in it stops
// the sign-in.
func (s *Supervisor) Restart() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case s.cmds <- command{kind: cmdRestart}:
	default:
		return errQueued
	}
	s.setLocked(wrapperapi.Starting, "", "")
	s.loginErr = false
	return nil
}

// SubmitCode writes the 2FA code where wrapper-lite looks for it. The file
// is written under a temporary name and renamed, so wrapper-lite never
// reads half of it.
func (s *Supervisor) SubmitCode(code string) error {
	if !wrapperapi.CodePattern.MatchString(code) {
		return errBadCode
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.st.State != wrapperapi.AwaitingCode {
		return errNotAwaiting
	}
	if err := writeCode(s.cfg.DataDir, code); err != nil {
		return err
	}
	s.setLocked(wrapperapi.SigningIn, "", "")
	return nil
}

func writeCode(dir, code string) error {
	f, err := os.CreateTemp(dir, ".2fa-*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, err = f.WriteString(code + "\n")
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(tmp, 0o600)
	}
	if err == nil {
		err = os.Rename(tmp, filepath.Join(dir, codeFile))
	}
	if err != nil {
		_ = os.Remove(tmp)
	}
	return err
}

// proc is one run of the launcher.
type proc struct {
	cmd     *exec.Cmd
	started time.Time
	// done closes once the process has exited and its output is read.
	done chan struct{}
	err  error
}

func (p *proc) exitCode() int {
	var ee *exec.ExitError
	if errors.As(p.err, &ee) {
		return ee.ExitCode()
	}
	if p.err != nil {
		return -1
	}
	return 0
}

// start runs the launcher in its own process group, with its output in the
// ring and line called for each line before done closes.
func (s *Supervisor) start(mode string, args []string, line func(string)) (*proc, error) {
	cmd := exec.Command(s.cfg.Launcher, args...)
	cmd.Dir = s.cfg.Dir
	cmd.Env = s.cfg.Env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	pr, pw, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	cmd.Stdout, cmd.Stderr = pw, pw
	if err := cmd.Start(); err != nil {
		_ = pr.Close()
		_ = pw.Close()
		return nil, err
	}
	_ = pw.Close()
	p := &proc{cmd: cmd, started: s.now(), done: make(chan struct{})}
	s.mu.Lock()
	s.pid = cmd.Process.Pid
	s.mu.Unlock()
	if s.onStart != nil {
		s.onStart(mode)
	}
	read := make(chan struct{})
	go func() {
		defer close(read)
		readLines(pr, func(l string) {
			// wrapper-lite logs every request; the polls of /status would
			// fill the log and say nothing.
			if message(l) != lineStatusRequest {
				s.ring.add(l)
			}
			if line != nil {
				line(l)
			}
		})
	}()
	go func() {
		p.err = cmd.Wait()
		// A child that outlived the launcher may hold the pipe open; its
		// output after that isn't worth waiting for.
		select {
		case <-read:
		case <-time.After(2 * time.Second):
			_ = pr.Close()
			<-read
		}
		_ = pr.Close()
		close(p.done)
	}()
	return p, nil
}

// stop sends SIGINT, the one signal the launcher handles, and SIGKILL to
// the whole group after StopGrace.
func (s *Supervisor) stop(p *proc) {
	if p == nil {
		return
	}
	pid := p.cmd.Process.Pid
	select {
	case <-p.done:
	default:
		_ = p.cmd.Process.Signal(syscall.SIGINT)
		t := time.NewTimer(s.cfg.StopGrace)
		select {
		case <-p.done:
			t.Stop()
		case <-t.C:
			s.note("wrapper-lite did not stop within %s of SIGINT; killing it", s.cfg.StopGrace)
			_ = syscall.Kill(-pid, syscall.SIGKILL)
			<-p.done
		}
	}
	// wrapper-lite's own processes are in the group too. None may outlive
	// the launcher, or the next run finds its port taken.
	_ = syscall.Kill(-pid, syscall.SIGKILL)
}

func (s *Supervisor) serviceArgs() []string {
	args := []string{"--base-dir", s.cfg.BaseDir, "--host", s.cfg.Host, "--port", strconv.Itoa(s.cfg.Port)}
	return append(args, s.cfg.Extra...)
}

func (s *Supervisor) startService() (*proc, error) {
	s.mu.Lock()
	s.gen++
	s.phase = phaseService
	s.mu.Unlock()
	s.note("starting wrapper-lite in service mode")
	p, err := s.start("service", s.serviceArgs(), nil)
	select {
	case s.wake <- struct{}{}:
	default:
	}
	if err != nil {
		s.mu.Lock()
		s.phase = phaseIdle
		s.mu.Unlock()
	}
	return p, err
}

// Run keeps service mode running until ctx ends, and runs sign-ins in
// between. It returns once wrapper-lite has stopped.
func (s *Supervisor) Run(ctx context.Context) error {
	go s.poll(ctx)
	backoff := s.cfg.BackoffMin
	for {
		p, err := s.startService()
		var exited <-chan struct{}
		if err != nil {
			s.note("can't start wrapper-lite: %v", err)
			s.set(wrapperapi.Error, "can't start wrapper-lite: "+err.Error())
		} else {
			exited = p.done
		}
		var c command
		if exited != nil {
			select {
			case <-ctx.Done():
				s.stop(p)
				return nil
			case c = <-s.cmds:
				s.stop(p)
				backoff = s.cfg.BackoffMin
				if c.kind == cmdLogin {
					s.signIn(ctx, c.login)
				} else {
					s.note("restarting service mode")
					s.set(wrapperapi.Starting, "")
				}
				continue
			case <-exited:
			}
			s.stop(p)
			if s.now().Sub(p.started) >= s.cfg.StableAfter {
				backoff = s.cfg.BackoffMin
			}
			why := fmt.Sprintf("wrapper-lite exited with code %d", p.exitCode())
			if l := message(s.ring.last()); l != "" {
				why += ": " + l
			}
			s.mu.Lock()
			s.phase = phaseIdle
			// A sign-in asked for in the meantime keeps its state; the
			// wait below picks it up.
			if !s.st.Busy() {
				s.setLocked(wrapperapi.Error, "", why)
				s.loginErr = false
			}
			s.mu.Unlock()
		}
		s.note("service mode stopped; starting it again in %s", backoff)
		t := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			t.Stop()
			return nil
		case c = <-s.cmds:
			t.Stop()
			backoff = s.cfg.BackoffMin
			if c.kind == cmdLogin {
				s.signIn(ctx, c.login)
			} else {
				s.set(wrapperapi.Starting, "")
			}
			continue
		case <-t.C:
		}
		backoff = min(2*backoff, s.cfg.BackoffMax)
	}
}

// Messages of wrapper-lite's sign-in, from lite/auth.cpp and lite_main.cpp,
// and the line it logs for each GET /status.
const (
	lineStatusRequest = "request: GET /status"
	lineAskCode       = "Enter your 2FA code"
	lineGotCode       = "Code file detected"
	lineComplete      = "login complete, exiting"
)

// failurePrefixes start the lines that say why a sign-in failed.
var failurePrefixes = []string{"auth error", "server message", "2FA code timeout", "auth failed"}

// signIn runs wrapper-lite's login mode with service mode stopped, then
// leaves the state for the next service run.
func (s *Supervisor) signIn(ctx context.Context, req wrapperapi.LoginRequest) {
	stopHiding := s.ring.hide(req.Password)
	defer stopHiding()
	s.mu.Lock()
	s.phase = phaseLogin
	s.mu.Unlock()
	// A code left from an earlier sign-in would be sent at once.
	_ = os.Remove(filepath.Join(s.cfg.DataDir, codeFile))
	args := append([]string{"--login", req.Username + ":" + req.Password, "--code-from-file", "--base-dir", s.cfg.BaseDir}, s.cfg.Extra...)
	s.note("signing in as %s: %s --login %s:%s --code-from-file --base-dir %s", req.Username, s.cfg.Launcher, req.Username, redacted, s.cfg.BaseDir)

	var failure string
	complete := false
	p, err := s.start("login", args, func(l string) {
		msg := message(l)
		switch {
		case strings.Contains(msg, lineAskCode):
			s.mu.Lock()
			if s.st.State == wrapperapi.SigningIn {
				s.setLocked(wrapperapi.AwaitingCode, "", "")
				d := s.now().UTC().Add(s.cfg.CodeWindow)
				s.st.CodeDeadline = &d
			}
			s.mu.Unlock()
		case strings.HasPrefix(msg, lineGotCode):
			s.mu.Lock()
			if s.st.State == wrapperapi.AwaitingCode {
				s.setLocked(wrapperapi.SigningIn, "", "")
			}
			s.mu.Unlock()
		case strings.Contains(msg, lineComplete):
			complete = true
		}
		for _, pre := range failurePrefixes {
			if strings.HasPrefix(msg, pre) {
				failure = msg
			}
		}
	})
	if err != nil {
		s.loginFailed("can't start wrapper-lite: " + err.Error())
		return
	}
	select {
	case <-p.done:
	case <-ctx.Done():
		s.stop(p)
		return
	case <-s.cmds:
		// Login refuses while a sign-in runs, so this is a restart.
		s.stop(p)
		s.note("sign-in stopped by a restart")
		s.set(wrapperapi.Starting, "")
		return
	}
	s.stop(p)
	_ = os.Remove(filepath.Join(s.cfg.DataDir, codeFile))
	if complete && p.exitCode() == 0 {
		s.note("signed in as %s", req.Username)
		s.set(wrapperapi.Starting, "")
		return
	}
	if failure == "" {
		failure = fmt.Sprintf("the sign-in did not finish (exit code %d)", p.exitCode())
	}
	s.loginFailed(failure)
}

func (s *Supervisor) loginFailed(msg string) {
	s.note("sign-in failed: %s", msg)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.phase = phaseIdle
	s.setLocked(wrapperapi.Error, "", msg)
	s.loginErr = true
}

// poll reads service mode's /status and moves the state between starting,
// running and signed_out.
func (s *Supervisor) poll(ctx context.Context) {
	t := time.NewTimer(s.cfg.PollStarting)
	defer t.Stop()
	// answered is the last service run that answered. Until the current
	// one does, polls come every PollStarting.
	answered := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.wake:
			t.Reset(s.cfg.PollStarting)
			continue
		case <-t.C:
		}
		s.mu.Lock()
		gen, ph := s.gen, s.phase
		s.mu.Unlock()
		if ph == phaseService {
			regions, err := s.regions(ctx)
			s.observe(gen, regions, err)
			if err == nil {
				answered = gen
			}
		}
		if ph == phaseService && answered == gen {
			t.Reset(s.cfg.PollEvery)
		} else {
			t.Reset(s.cfg.PollStarting)
		}
	}
}

func (s *Supervisor) observe(gen int, regions []string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// A failed poll changes nothing: wrapper-lite serves one key at a time,
	// so a busy service can be slow to answer.
	if err != nil || gen != s.gen || s.phase != phaseService || s.st.Busy() {
		return
	}
	switch {
	case len(regions) > 0:
		s.setLocked(wrapperapi.Running, strings.ToLower(regions[0]), "")
		s.loginErr = false
	case s.st.State == wrapperapi.Error && s.loginErr:
		// The sign-in's error stays on show while nobody is signed in.
	default:
		s.setLocked(wrapperapi.SignedOut, "", "")
	}
}

// liteStatus is wrapper-lite's answer to GET /status.
type liteStatus struct {
	Code int `json:"code"`
	Data struct {
		Regions []string `json:"regions"`
	} `json:"data"`
}

func (s *Supervisor) regions(ctx context.Context) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.cfg.StatusURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status answered %s", resp.Status)
	}
	var ls liteStatus
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&ls); err != nil {
		return nil, err
	}
	return ls.Data.Regions, nil
}
