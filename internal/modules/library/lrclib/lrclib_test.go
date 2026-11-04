package lrclib

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
)

type memCache struct {
	mu sync.Mutex
	m  map[string][]byte
	s  map[string]int
}

func (c *memCache) Get(_ context.Context, k string) (int, []byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	s, ok := c.s[k]
	return s, c.m[k], ok
}

func (c *memCache) Put(_ context.Context, k string, status int, body []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m, c.s = map[string][]byte{}, map[string]int{}
	}
	c.m[k], c.s[k] = body, status
}

func TestGet(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		q := r.URL.Query()
		switch {
		case r.Header.Get("User-Agent") != "ua":
			w.WriteHeader(http.StatusForbidden)
		case q.Get("track_name") == "Roads" && q.Get("duration") == "305" && q.Get("album_name") == "Dummy":
			_, _ = w.Write([]byte(`{"id": 1, "trackName": "Roads", "instrumental": false, "plainLyrics": "Oh, can't anybody see", "syncedLyrics": "[00:12.00] Oh, can't anybody see"}`))
		case q.Get("track_name") == "Down":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"code": 404, "name": "TrackNotFound"}`))
		}
	}))
	defer srv.Close()
	c := New("ua", &memCache{})
	c.Base = srv.URL
	ctx := context.Background()
	for range 2 {
		l, err := c.Get(ctx, "Portishead", "Roads", "Dummy", 305)
		if err != nil || l.Synced != "[00:12.00] Oh, can't anybody see" || l.Plain == "" {
			t.Fatalf("%v %+v", err, l)
		}
		if _, err := c.Get(ctx, "Portishead", "Nothing", "Dummy", 1); !errors.Is(err, ErrNotFound) {
			t.Fatalf("missing: %v", err)
		}
	}
	if calls.Load() != 2 {
		t.Fatalf("%d calls; the cache should answer the second time", calls.Load())
	}
	if _, err := c.Get(ctx, "x", "Down", "y", 1); err == nil || errors.Is(err, ErrNotFound) {
		t.Fatalf("down: %v", err)
	}
}
