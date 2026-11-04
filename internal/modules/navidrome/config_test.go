package navidrome

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
)

// configServer is Navidrome's own API as far as Config uses it.
func configServer(t *testing.T, admin bool) *url.URL {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/auth/login":
			var b map[string]string
			_ = json.NewDecoder(r.Body).Decode(&b)
			if b["username"] != user || b["password"] != password {
				http.Error(w, `{"error":"Invalid username or password"}`, http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"token": "tok", "isAdmin": admin})
		case r.URL.Path == "/api/config/":
			if r.Header.Get("X-ND-Authorization") != "Bearer tok" {
				http.Error(w, "no", http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(`{"id":"config","configFile":"/etc/navidrome/navidrome.toml","config":{
				"Tags":{"genre":{"Split":[";"]},"mood":{"Ignore":true}},
				"Scanner":{"ArtistJoiner":" / ","ArtistSplitExceptions":["AC/DC"],"GenreSeparators":""},
				"PID":{"Album":"musicbrainz_albumid|albumartistid,album","Track":"x"},
				"CoverArtPriority":"embedded, cover.*",
				"Spotify":{"Secret":"must not be kept"}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	return u
}

func TestConfigReadsTheTagSettings(t *testing.T) {
	c := NewClient(configServer(t, true), user, password, http.DefaultClient)
	got, err := c.Config(bg)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got.Tags["genre"].Split, []string{";"}) || !got.Tags["mood"].Ignore {
		t.Errorf("tags %+v", got.Tags)
	}
	if got.ArtistJoiner != " / " || !slices.Equal(got.ArtistSplitExceptions, []string{"AC/DC"}) {
		t.Errorf("scanner %+v", got)
	}
	if got.AlbumPID != "musicbrainz_albumid|albumartistid,album" || got.CoverArtPriority != "embedded, cover.*" {
		t.Errorf("pid %q, covers %q", got.AlbumPID, got.CoverArtPriority)
	}
}

func TestConfigRefusals(t *testing.T) {
	c := NewClient(configServer(t, false), user, password, http.DefaultClient)
	if _, err := c.Config(bg); err == nil || !strings.Contains(err.Error(), "not an admin") {
		t.Errorf("a user who isn't an admin: %v", err)
	}
	c = NewClient(configServer(t, true), user, "wrong-pass", http.DefaultClient)
	_, err := c.Config(bg)
	if err == nil || strings.Contains(err.Error(), "wrong-pass") {
		t.Errorf("a wrong password: %v", err)
	}
	m := New()
	if _, err := m.Config(bg); err != ErrOff {
		t.Errorf("without Navidrome: %v", err)
	}
}
