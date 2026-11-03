package amdl

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// Runner runs amdl once per download.
type Runner struct {
	// Bin is the amdl binary; Config the rendered config it reads.
	Bin    string
	Config string
	// WorkDir is the working folder, with the cache and temp folders in it.
	WorkDir string
	// Timeout bounds one run: SHELF_AMDL_TIMEOUT.
	Timeout time.Duration
	// KillAfter is how long the group has after SIGTERM before SIGKILL.
	KillAfter time.Duration
	// Env is added to the environment, for tests.
	Env []string
}

// tailLines is how many of the last lines a run keeps for its error.
const tailLines = 50

// Track is one entry of the JSON array amdl prints at the end with --json.
type Track struct {
	Path     string `json:"path"`
	Artist   string `json:"artist"`
	ArtistID string `json:"artist_id"`
	Album    string `json:"album"`
	Song     string `json:"song"`
}

// Result is what a run that exited 0 printed.
type Result struct {
	// Tracks are the songs amdl wrote or found complete, in its order.
	Tracks []Track
	// Listed reports whether amdl printed its JSON array. It doesn't when
	// it stops early, such as without a token.
	Listed bool
	// Tail are the last lines, for an error message.
	Tail []string
}

// ExitError is a run that exited with a code other than 0.
type ExitError struct {
	Code int
	// Lines are the last lines that look like errors.
	Lines []string
}

func (e *ExitError) Error() string {
	msg := fmt.Sprintf("amdl exited with code %d", e.Code)
	if len(e.Lines) > 0 {
		msg += ": " + strings.Join(e.Lines, " | ")
	}
	return msg
}

// ErrTimeout is a run that took longer than Timeout.
var ErrTimeout = errors.New("amdl took too long")

// Run runs amdl for one link with extra flags, such as --atmos. line hears
// every line of its output; both \n and \r end a line, since amdl draws
// progress bars with \r. Cancel and the timeout send SIGTERM to amdl's
// process group, so ffmpeg stops too, and SIGKILL after KillAfter.
func (r *Runner) Run(ctx context.Context, link string, flags []string, line func(string)) (Result, error) {
	cache, tmp := filepath.Join(r.WorkDir, ".cache"), filepath.Join(r.WorkDir, "tmp")
	for _, d := range []string{cache, tmp} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return Result{}, err
		}
	}
	args := append([]string{"--config", r.Config, "--json"}, flags...)
	cmd := exec.Command(r.Bin, append(args, link)...)
	cmd.Dir = r.WorkDir
	cmd.Env = append(append(os.Environ(), "XDG_CACHE_HOME="+cache, "TMPDIR="+tmp), r.Env...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	pr, pw, err := os.Pipe()
	if err != nil {
		return Result{}, err
	}
	cmd.Stdout, cmd.Stderr = pw, pw
	if err := cmd.Start(); err != nil {
		_ = pr.Close()
		_ = pw.Close()
		return Result{}, fmt.Errorf("start amdl: %w", err)
	}
	_ = pw.Close()
	pid := cmd.Process.Pid

	tctx, cancel := context.WithTimeout(ctx, r.Timeout)
	defer cancel()
	ended := make(chan struct{})
	killed := make(chan struct{})
	go func() {
		defer close(killed)
		select {
		case <-ended:
			return
		case <-tctx.Done():
		}
		_ = syscall.Kill(-pid, syscall.SIGTERM)
		t := time.NewTimer(r.KillAfter)
		defer t.Stop()
		select {
		case <-ended:
		case <-t.C:
			_ = syscall.Kill(-pid, syscall.SIGKILL)
		}
	}()

	var tail []string
	var listed []string
	readLines(pr, func(l string) {
		tail = append(tail, l)
		if len(tail) > tailLines {
			tail = tail[1:]
		}
		if t := strings.TrimSpace(l); strings.HasPrefix(t, "[") || t == "null" {
			listed = append(listed, t)
			if len(listed) > 5 {
				listed = listed[1:]
			}
		}
		if line != nil {
			line(l)
		}
	})
	_ = pr.Close()
	werr := cmd.Wait()
	// Nothing of the run may outlive it, such as an ffmpeg amdl left.
	_ = syscall.Kill(-pid, syscall.SIGKILL)
	close(ended)
	<-killed

	switch {
	case ctx.Err() != nil:
		return Result{Tail: tail}, ctx.Err()
	case tctx.Err() != nil:
		return Result{Tail: tail}, fmt.Errorf("%w: it ran longer than %s", ErrTimeout, r.Timeout)
	}
	if werr != nil {
		var ee *exec.ExitError
		if errors.As(werr, &ee) {
			return Result{Tail: tail}, &ExitError{Code: ee.ExitCode(), Lines: ErrorLines(tail)}
		}
		return Result{Tail: tail}, werr
	}
	res := Result{Tail: tail}
	// The array is the last line that parses; a song or folder name that
	// starts with [ prints a line that doesn't.
	for i := len(listed) - 1; i >= 0; i-- {
		var tracks []Track
		if json.Unmarshal([]byte(listed[i]), &tracks) == nil {
			res.Tracks, res.Listed = tracks, true
			break
		}
	}
	return res, nil
}

// ErrorLines picks the last three lines of tail that look like errors, or
// else the last line.
func ErrorLines(tail []string) []string {
	var out []string
	for _, l := range tail {
		if isFailure(l) {
			out = append(out, strings.TrimSpace(l))
		}
	}
	if len(out) > 3 {
		out = out[len(out)-3:]
	}
	if len(out) == 0 {
		for i := len(tail) - 1; i >= 0; i-- {
			if t := strings.TrimSpace(tail[i]); t != "" {
				return []string{t}
			}
		}
	}
	return out
}

// splitLines ends a line at \n or \r.
func splitLines(data []byte, atEOF bool) (int, []byte, error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	if i := bytes.IndexAny(data, "\r\n"); i >= 0 {
		return i + 1, data[:i], nil
	}
	if atEOF {
		return len(data), data, nil
	}
	return 0, nil, nil
}

// maxLine bounds one line. The JSON array of a long playlist is one line.
const maxLine = 16 << 20

func readLines(r io.Reader, line func(string)) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), maxLine)
	sc.Split(splitLines)
	for sc.Scan() {
		if t := strings.TrimRight(sc.Text(), " \t"); strings.TrimSpace(t) != "" {
			line(t)
		}
	}
	// A line longer than maxLine stops the scan; drain the rest so amdl
	// never blocks on a full pipe.
	_, _ = io.Copy(io.Discard, r)
}
