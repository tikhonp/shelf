package main

import (
	"bufio"
	"bytes"
	"io"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/tikhonp/shelf/internal/wrapperapi"
)

// keepLines is how many lines the ring keeps for GET /v1/log.
const keepLines = 1000

const redacted = "[redacted]"

// ring keeps the last lines of wrapper-lite's output and the supervisor's
// notes. It hides the password of a running sign-in in every line, so no
// reader and no container log ever sees it.
type ring struct {
	mu      sync.Mutex
	max     int
	lines   []wrapperapi.LogLine
	n       int64
	secrets map[int]string
	nextKey int
	now     func() time.Time
	// echo hears every line after redaction: main writes them to stdout.
	echo func(wrapperapi.LogLine)
}

func newRing(max int, echo func(wrapperapi.LogLine)) *ring {
	return &ring{max: max, secrets: map[int]string{}, now: time.Now, echo: echo}
}

// hide redacts s in every line added until the returned func is called.
func (r *ring) hide(s string) (stop func()) {
	if s == "" {
		return func() {}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	k := r.nextKey
	r.nextKey++
	r.secrets[k] = s
	return func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		delete(r.secrets, k)
	}
}

// add keeps one line.
func (r *ring) add(text string) {
	r.mu.Lock()
	for _, s := range r.secrets {
		text = strings.ReplaceAll(text, s, redacted)
	}
	text = tokenPrefix.ReplaceAllString(text, "${1}="+redacted)
	r.n++
	l := wrapperapi.LogLine{N: r.n, Time: r.now().UTC(), Text: text}
	r.lines = append(r.lines, l)
	if len(r.lines) > r.max {
		// Copy, so the backing array doesn't grow without bound.
		r.lines = append([]wrapperapi.LogLine(nil), r.lines[len(r.lines)-r.max:]...)
	}
	echo := r.echo
	r.mu.Unlock()
	if echo != nil {
		echo(l)
	}
}

// after returns the kept lines numbered above n.
func (r *ring) after(n int64) []wrapperapi.LogLine {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []wrapperapi.LogLine{}
	for _, l := range r.lines {
		if l.N > n {
			out = append(out, l)
		}
	}
	return out
}

// last returns the text of the last line that isn't empty.
func (r *ring) last() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := len(r.lines) - 1; i >= 0; i-- {
		if t := strings.TrimSpace(r.lines[i].Text); t != "" {
			return t
		}
	}
	return ""
}

// splitLines ends a line at \n or \r, because progress output redraws a
// line with \r.
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

// readLines calls line for every line of r that isn't empty.
func readLines(r io.Reader, line func(string)) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	sc.Split(splitLines)
	for sc.Scan() {
		if t := strings.TrimRight(sc.Text(), " \t"); strings.TrimSpace(t) != "" {
			line(t)
		}
	}
}

// message is a wrapper-lite line without its time and level:
// "2026-11-04 12:00:00.000 [WARN ] auth error: ..." gives "auth error: ...".
func message(line string) string {
	if m := linePrefix.FindStringIndex(line); m != nil {
		return line[m[1]:]
	}
	return line
}

// tokenPrefix matches the token heads that wrapper-lite prints when it loads
// its token cache and when it asks for a licence, as in
// "token cache loaded (dev=eyJhbGciOiJFUzI1... music=AbCdEfGh12345678...)".
var tokenPrefix = regexp.MustCompile(`\b(dev|music)=[^\s)]+`)

// linePrefix is the time and level that lite/logger.h puts before a message.
// The level is padded to five letters: "[INFO ]".
var linePrefix = regexp.MustCompile(`^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}(\.\d+)? \[[A-Z]+ *\] `)
