package navidrome

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"sync"
	"testing"
)

// missingServer is Navidrome's own API as far as RemoveMissing uses it. It
// holds the missing tracks and records what was deleted.
type missingServer struct {
	mu      sync.Mutex
	purge   string
	missing []map[string]string
	deleted []string
	asked   string
}

func (s *missingServer) start(t *testing.T) *url.URL {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		if r.URL.Path != "/auth/login" && r.Header.Get("X-ND-Authorization") != "Bearer tok" {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/auth/login":
			_ = json.NewEncoder(w).Encode(map[string]any{"token": "tok", "isAdmin": true})
		case r.URL.Path == "/api/config/":
			_ = json.NewEncoder(w).Encode(map[string]any{"config": map[string]any{"Scanner": map[string]any{"PurgeMissing": s.purge}}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/missing":
			// Navidrome matches path as a LIKE prefix.
			s.asked = r.URL.Query().Get("path")
			_ = json.NewEncoder(w).Encode(s.missing)
		case r.Method == http.MethodDelete && r.URL.Path == "/api/missing/":
			ids := r.URL.Query()["id"]
			s.deleted = append(s.deleted, ids...)
			_ = json.NewEncoder(w).Encode(map[string]any{"ids": ids})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	return u
}

func TestRemoveMissing(t *testing.T) {
	s := &missingServer{missing: []map[string]string{
		{"id": "a", "path": "qobuz/Album 50%/01.flac"},
		{"id": "b", "path": "qobuz/Album 50%/CD 2/01.flac"},
		// LIKE's % matches this one too, and it is no track of the folder.
		{"id": "c", "path": "qobuz/Album 50% Off/01.flac"},
	}}
	c := NewClient(s.start(t), user, password, http.DefaultClient)
	got, err := c.RemoveMissing(bg, "qobuz/Album 50%")
	if err != nil {
		t.Fatal(err)
	}
	if got.Removed != 2 || got.Purges {
		t.Errorf("got %+v", got)
	}
	if s.asked != "qobuz/Album 50%/" || !slices.Equal(s.deleted, []string{"a", "b"}) {
		t.Errorf("asked %q, deleted %v", s.asked, s.deleted)
	}

	s = &missingServer{}
	c = NewClient(s.start(t), user, password, http.DefaultClient)
	if got, err := c.RemoveMissing(bg, "qobuz/Gone"); err != nil || got.Removed != 0 {
		t.Errorf("nothing missing yet: %+v, %v", got, err)
	}

	s = &missingServer{purge: "always", missing: []map[string]string{{"id": "a", "path": "qobuz/Gone/01.flac"}}}
	c = NewClient(s.start(t), user, password, http.DefaultClient)
	if got, err := c.RemoveMissing(bg, "qobuz/Gone"); err != nil || !got.Purges || len(s.deleted) > 0 {
		t.Errorf("Navidrome purges by itself: %+v, %v, deleted %v", got, err, s.deleted)
	}

	if _, err := New().RemoveMissing(bg, "qobuz/Gone"); err != ErrOff {
		t.Errorf("without Navidrome: %v", err)
	}
}
