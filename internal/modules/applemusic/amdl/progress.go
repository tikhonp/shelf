package amdl

import (
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// Lines amdl prints, as its rip.go prints them. Each song starts with
// "Track 3 of 12: songs", where 3 counts every item of the album or
// playlist, music videos too.
var (
	trackLine = regexp.MustCompile(`^Track (\d+) of (\d+)(?::|$)`)
	// barLine is a progress bar redraw: a percentage and a bar.
	barLine = regexp.MustCompile(`\d+%\s*\|`)
)

const (
	lineExists          = "Track already exists locally."
	lineConvertedExists = "Converted track already exists locally."
)

// Tracker follows a run's output line by line: which item amdl works on,
// which it found complete, and the last failure it printed for each.
// Line runs in the reader's goroutine; the rest may run anywhere.
type Tracker struct {
	mu       sync.Mutex
	current  int
	first    int
	last     string
	skipped  map[int]bool
	failures map[int]string
}

// NewTracker returns a tracker before the first line.
func NewTracker() *Tracker {
	return &Tracker{skipped: map[int]bool{}, failures: map[int]string{}}
}

// Line reads one line. It returns amdl's item number when the line starts
// an item, and 0 otherwise.
func (t *Tracker) Line(l string) int {
	l = strings.TrimSpace(l)
	t.mu.Lock()
	defer t.mu.Unlock()
	if m := trackLine.FindStringSubmatch(l); m != nil {
		n, _ := strconv.Atoi(m[1])
		t.current = n
		if t.first == 0 {
			t.first = n
		}
		return n
	}
	if t.current == 0 {
		return 0
	}
	switch {
	case l == lineExists || l == lineConvertedExists:
		t.skipped[t.current] = true
	case isFailure(l):
		t.failures[t.current] = l
		t.last = l
	}
	return 0
}

// First is the number of the first item amdl started, or 0.
func (t *Tracker) First() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.first
}

// LastFailure is the last failure amdl printed for any item.
func (t *Tracker) LastFailure() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.last
}

// Skipped reports whether amdl found item n complete on disk.
func (t *Tracker) Skipped(n int) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.skipped[n]
}

// Failure is the last failure amdl printed for item n.
func (t *Tracker) Failure(n int) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.failures[n]
}

// IsBar reports whether a line is a progress bar redraw, which the job log
// keeps only now and then.
func IsBar(l string) bool { return barLine.MatchString(l) }

// isFailure reports whether a line says something failed. amdl's notes
// that it falls back to AAC, and its closing tally, are not failures.
func isFailure(l string) bool {
	l = strings.TrimSpace(l)
	switch {
	case l == "", strings.HasPrefix(l, "======="), strings.HasPrefix(l, "Unavailable, trying"),
		strings.HasPrefix(l, "Error detected"), strings.Contains(l, "will use m3u8 from Web API"):
		return false
	case l == "Unavailable", strings.HasPrefix(l, "⚠"), strings.HasPrefix(l, "panic:"):
		return true
	}
	low := strings.ToLower(l)
	return strings.Contains(low, "failed") || strings.Contains(low, "error") || strings.Contains(low, "requires lite-server")
}
