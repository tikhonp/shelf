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
	// barLine is a progress bar redraw. amdl draws two bars, both without
	// the bar itself: "Decrypting...  42%  (12/27 MB, 4.6 MB/s)" while it
	// fetches and decrypts a song, and "Downloading..." for AAC.
	barLine = regexp.MustCompile(`^(Decrypting|Downloading)\.\.\.\s`)
	// barCount is a bar's byte count: "(12/27 MB," and "(1.0 kB/27 MB,",
	// or "(3.1 MB," from a bar that doesn't know the size.
	barCount = regexp.MustCompile(`\((\d+(?:\.\d+)?)\s*([kMGT]?B)?(?:/(\d+(?:\.\d+)?)\s*([kMGT]?B))?,`)
	// barPrefix is a whole redraw, with the time a finished bar shows:
	// "Decrypting... 100%  (27/27 MB, 24 MB/s) [1s] ".
	barPrefix = regexp.MustCompile(`^(?:Decrypting|Downloading)\.\.\.[^(]*\([^)]*\)\s*(?:\[[^\]]*\]\s*)?`)
)

// siUnits are progressbar's byte units, which are decimal.
var siUnits = map[string]float64{"B": 1, "kB": 1e3, "MB": 1e6, "GB": 1e9, "TB": 1e12}

const (
	lineExists          = "Track already exists locally."
	lineConvertedExists = "Converted track already exists locally."
	// lineErrorExit ends a run in which a song failed, with exit-on-error.
	lineErrorExit = "Error detected, exiting..."
)

// Tracker follows a download's output line by line: which item amdl works
// on, which it found complete, and the last failure it printed for each.
// Line runs in the reader's goroutine; the rest may run anywhere.
type Tracker struct {
	mu       sync.Mutex
	current  int
	first    int
	last     string
	started  map[int]bool
	earlier  map[int]bool
	skipped  map[int]bool
	failures map[int]string
}

// NewTracker returns a tracker before the first line.
func NewTracker() *Tracker {
	return &Tracker{started: map[int]bool{}, earlier: map[int]bool{}, skipped: map[int]bool{}, failures: map[int]string{}}
}

// Again starts the next run of the same link. amdl finds the songs the
// last run wrote complete, and those are not skips. Only this run's
// failures count.
func (t *Tracker) Again() {
	t.mu.Lock()
	defer t.mu.Unlock()
	for n := range t.started {
		t.earlier[n] = true
	}
	t.current, t.last = 0, ""
	t.failures = map[int]string{}
}

// Line reads one line. It returns amdl's item number when the line starts
// an item, and 0 otherwise.
func (t *Tracker) Line(l string) int {
	l = Text(l)
	t.mu.Lock()
	defer t.mu.Unlock()
	if m := trackLine.FindStringSubmatch(l); m != nil {
		n, _ := strconv.Atoi(m[1])
		t.current = n
		t.started[n] = true
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
		if !t.earlier[t.current] {
			t.skipped[t.current] = true
		}
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
func IsBar(l string) bool { return barLine.MatchString(strings.TrimSpace(l)) }

// SplitBar splits a bar's redraw from what amdl printed after it on the
// same line: "Decrypted" when the bar ends, or a failure that cut it
// short. For a line that isn't a bar, rest is the whole line.
func SplitBar(l string) (bar, rest string) {
	l = strings.TrimSpace(l)
	if !IsBar(l) {
		return "", l
	}
	m := barPrefix.FindStringIndex(l)
	if m == nil {
		return l, ""
	}
	return strings.TrimSpace(l[:m[1]]), strings.TrimSpace(l[m[1]:])
}

// Text is a line without a bar before it.
func Text(l string) string {
	_, rest := SplitBar(l)
	return rest
}

// FailedPass reports whether a run ended with amdl's "Error detected,
// exiting...", which amdl prints when it went through every song and
// some failed.
func FailedPass(tail []string) bool {
	for _, l := range tail {
		if strings.TrimSpace(l) == lineErrorExit {
			return true
		}
	}
	return false
}

// Bar reads how many bytes a progress bar line shows done. The bar rounds
// the count to two or three digits, so it is an estimate. It counts the
// stream amdl fetches, not the file amdl converts it into.
func Bar(l string) (int64, bool) {
	if !IsBar(l) {
		return 0, false
	}
	m := barCount.FindStringSubmatch(l)
	if m == nil {
		return 0, false
	}
	// "12/27 MB" gives the unit once, after the size.
	unit := m[2]
	if unit == "" {
		unit = m[4]
	}
	v, err := strconv.ParseFloat(m[1], 64)
	if err != nil || unit == "" {
		return 0, false
	}
	return int64(v * siUnits[unit]), true
}

// isFailure reports whether a line says something failed. amdl's notes
// that it falls back to AAC, and its closing tally, are not failures.
func isFailure(l string) bool {
	l = Text(l)
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
