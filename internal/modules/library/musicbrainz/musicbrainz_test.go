package musicbrainz

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var bg = context.Background()

// memCache is the cache as a map.
type memCache struct {
	mu sync.Mutex
	m  map[string]cached
}

type cached struct {
	status int
	body   []byte
}

func (c *memCache) Get(_ context.Context, k string) (int, []byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.m[k]
	return v.status, v.body, ok
}

func (c *memCache) Put(_ context.Context, k string, status int, body []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = map[string]cached{}
	}
	c.m[k] = cached{status, body}
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestReleaseAndCache(t *testing.T) {
	var calls atomic.Int32
	var agent, inc string
	busy := 1
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		agent, inc = r.Header.Get("User-Agent"), r.URL.Query().Get("inc")
		switch {
		case busy > 0:
			busy--
			w.WriteHeader(http.StatusServiceUnavailable)
		case strings.HasSuffix(r.URL.Path, "11111111-1111-4111-8111-111111111111"):
			_, _ = w.Write(fixture(t, "release-blue.json"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	cache := &memCache{}
	c := New("MusicShelf/test ( https://example.test )", cache)
	c.Base, c.Limit, c.Retry = srv.URL, NewLimiter(0), []time.Duration{time.Millisecond}

	r, err := c.Release(bg, "11111111-1111-4111-8111-111111111111", false)
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 || agent != "MusicShelf/test ( https://example.test )" || inc != releaseInc {
		t.Fatalf("calls %d, agent %q, inc %q", calls.Load(), agent, inc)
	}
	if r.Title != "Blue" || r.ArtistCredit.String() != "Joni Mitchell" || r.Label() != "Reprise Records" ||
		r.CatalogNumber() != "MS 2038" || r.TrackCount() != 2 || r.Genre() != "folk" || r.ReleaseGroup.FirstReleaseDate != "1971-06-22" {
		t.Fatalf("release %+v", r)
	}
	two := r.Media[0].Tracks[1].ArtistCredit
	if two.String() != "Joni Mitchell & James Taylor" || !slices.Equal(two.Names(), []string{"Joni Mitchell", "James Taylor"}) ||
		two.Sort() != "Mitchell, Joni & Taylor, James" {
		t.Fatalf("credit %q %q %q", two.String(), two.Names(), two.Sort())
	}

	// The cache answers the second time, a miss included.
	if _, err := c.Release(bg, "11111111-1111-4111-8111-111111111111", false); err != nil || calls.Load() != 2 {
		t.Fatalf("cached: %v, %d calls", err, calls.Load())
	}
	for range 2 {
		if _, err := c.Release(bg, "00000000-0000-4000-8000-000000000000", false); !errors.Is(err, ErrNotFound) {
			t.Fatalf("missing: %v", err)
		}
	}
	if calls.Load() != 3 {
		t.Fatalf("a miss asked %d times", calls.Load()-2)
	}
	// Not an ID: no request at all.
	if _, err := c.Release(bg, "../admin", false); !errors.Is(err, ErrNotFound) || calls.Load() != 3 {
		t.Fatalf("bad ID: %v", err)
	}

	// A service that stays busy is an error, and nothing is cached.
	busy = 5
	if _, err := c.SearchReleases(bg, "release:x", 5); err == nil || errors.Is(err, ErrNotFound) {
		t.Fatalf("busy: %v", err)
	}
	if len(cache.m) != 2 {
		t.Fatalf("cache holds %d", len(cache.m))
	}
}

func TestShapes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/release/6666"):
			_, _ = w.Write(fixture(t, "release-classical.json"))
		case r.URL.Path == "/release":
			_, _ = w.Write(fixture(t, "search-blue.json"))
		case r.URL.Path == "/recording" && r.URL.Query().Get("query") == `isrc:"USRE10000001"`:
			_, _ = w.Write(fixture(t, "isrc.json"))
		case r.URL.Path == "/recording":
			_, _ = w.Write([]byte(`{"recordings": []}`))
		case strings.HasPrefix(r.URL.Path, "/release/1111"):
			_, _ = w.Write(fixture(t, "caa.json"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	c := New("t", nil)
	c.Base, c.CoverBase, c.Limit = srv.URL, srv.URL, NewLimiter(0)

	r, err := c.Release(bg, "66666666-6666-4666-8666-666666666666", true)
	if err != nil {
		t.Fatal(err)
	}
	rec := r.Media[0].Tracks[0].Recording
	if !slices.Equal(rec.Composers(), []string{"Johann Sebastian Bach"}) || rec.Works()[0] != "Goldberg-Variationen, BWV 988: Aria" ||
		r.CatalogNumber() != "" || r.Label() != "CBS Masterworks" || r.Genre() != "" || !slices.Equal(r.ReleaseGroup.Types(), []string{"album"}) {
		t.Fatalf("classical %+v", r)
	}
	hits, err := c.SearchReleases(bg, "release:"+Phrase(`Blue "Live"`), 10)
	if err != nil || len(hits) != 3 || hits[1].TrackCount != 2 || hits[0].Score != 100 {
		t.Fatalf("search %v %+v", err, hits)
	}
	hits, err = c.ISRC(bg, "USRE10000001")
	if err != nil || len(hits) != 2 || hits[1].ID != "13131313-1313-4131-8131-131313131313" {
		t.Fatalf("isrc %v %+v", err, hits)
	}
	if hits, err := c.ISRC(bg, "XX0000000000"); err != nil || hits != nil {
		t.Fatalf("unknown isrc %v", hits)
	}
	cv, err := c.FrontCover(bg, "11111111-1111-4111-8111-111111111111")
	if err != nil || cv.Image != "https://coverartarchive.org/release/11111111-1111-4111-8111-111111111111/2.jpg" ||
		cv.Preview != "https://coverartarchive.org/release/11111111-1111-4111-8111-111111111111/2-500.jpg" {
		t.Fatalf("cover %v %+v", err, cv)
	}
	if _, err := c.FrontCover(bg, "66666666-6666-4666-8666-666666666666"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("no cover: %v", err)
	}
}

func TestParseRelease(t *testing.T) {
	id := "11111111-1111-4111-8111-111111111111"
	for in, ok := range map[string]bool{
		id:                                      true,
		" " + strings.ToUpper(id) + " ":         true,
		"https://musicbrainz.org/release/" + id: true,
		"musicbrainz.org/release/" + id + "/discids":            true,
		"https://beta.musicbrainz.org/release/" + id + "?tab=x": true,
		"https://musicbrainz.org/release-group/" + id:           false,
		"https://example.test/release/" + id:                    false,
		"11111111-1111-4111-8111":                               false,
	} {
		got, gotOK := ParseRelease(in)
		if gotOK != ok || ok && got != id {
			t.Errorf("%q: %q %v", in, got, gotOK)
		}
	}
	if Phrase(`a "b" \c`) != `"a \"b\" \\c"` {
		t.Fatal(Phrase(`a "b" \c`))
	}
}

// fakeClock moves only when a caller sleeps.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func TestLimiterSpacesCalls(t *testing.T) {
	clock := &fakeClock{now: time.Unix(0, 0)}
	l := NewLimiter(time.Second)
	l.now = clock.Now
	var slept []time.Duration
	l.sleep = func(_ context.Context, d time.Duration) error {
		slept = append(slept, d)
		return nil
	}
	for range 3 {
		if err := l.Wait(bg); err != nil {
			t.Fatal(err)
		}
	}
	if !slices.Equal(slept, []time.Duration{0, time.Second, 2 * time.Second}) {
		t.Fatalf("slept %v", slept)
	}
	ctx, cancel := context.WithCancel(bg)
	cancel()
	l.sleep = sleepCtx
	if err := l.Wait(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled: %v", err)
	}
}

func TestLimiterLetsTheEditorGoFirst(t *testing.T) {
	clock := &fakeClock{now: time.Unix(0, 0)}
	l := NewLimiter(time.Second)
	l.now = clock.Now
	gate := make(chan struct{})
	waiting := make(chan struct{}, 16)
	var mu sync.Mutex
	slept := map[Priority][]time.Duration{}
	l.sleep = func(ctx context.Context, d time.Duration) error {
		mu.Lock()
		slept[priorityOf(ctx)] = append(slept[priorityOf(ctx)], d)
		mu.Unlock()
		select {
		case waiting <- struct{}{}:
		default:
		}
		<-gate
		return nil
	}
	done := make(chan struct{}, 3)
	wait := func(ctx context.Context) {
		go func() { _ = l.Wait(ctx); done <- struct{}{} }()
		<-waiting
	}
	// An editor call holds the first slot. A bulk call that comes then
	// steps aside, so an editor call that comes after it still gets the
	// next slot, and the bulk call the one after.
	wait(bg)
	wait(WithPriority(bg, Bulk))
	wait(bg)
	close(gate)
	for range 3 {
		<-done
	}
	mu.Lock()
	defer mu.Unlock()
	bulk := slept[Bulk]
	if !slices.Equal(slept[Interactive], []time.Duration{0, time.Second}) || bulk[0] != time.Second || bulk[len(bulk)-1] != 2*time.Second {
		t.Fatalf("slept %v", slept)
	}
}

func TestPing(t *testing.T) {
	var calls atomic.Int32
	var path string
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		path = r.URL.Path
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"id":"` + VariousArtists + `"}`))
	}))
	defer srv.Close()
	cache := &memCache{}
	c := New("test", cache)
	c.Base, c.Limit, c.Retry = srv.URL, NewLimiter(0), []time.Duration{time.Millisecond}

	// Every ping asks, since a cached answer would hide a service that is down.
	for range 2 {
		if _, err := c.Ping(bg); err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 2 || path != "/artist/"+VariousArtists || len(cache.m) != 0 {
		t.Fatalf("%d calls to %s, cache holds %d", calls.Load(), path, len(cache.m))
	}

	// A 503 returns ErrBusy at once, with no retry.
	status = http.StatusServiceUnavailable
	if _, err := c.Ping(bg); !errors.Is(err, ErrBusy) || calls.Load() != 3 {
		t.Fatalf("busy: %v after %d calls", err, calls.Load())
	}
	status = http.StatusBadGateway
	var se *StatusError
	if _, err := c.Ping(bg); !errors.As(err, &se) || se.Code != http.StatusBadGateway {
		t.Fatalf("502: %v", err)
	}
	srv.Close()
	if _, err := c.Ping(bg); err == nil || errors.Is(err, ErrBusy) || errors.As(err, &se) {
		t.Fatalf("down: %v", err)
	}
}
