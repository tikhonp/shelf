package applemusic

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tikhonp/shelf/internal/platform/config"
	"github.com/tikhonp/shelf/internal/platform/hub"
	"github.com/tikhonp/shelf/internal/platform/module"
	"github.com/tikhonp/shelf/internal/source"
	"github.com/tikhonp/shelf/internal/wrapperapi"
	"go.yaml.in/yaml/v3"
)

// TestMain lets the test binary stand in for amdl: the module runs it with
// SHELF_FAKE_AMDL set.
func TestMain(m *testing.M) {
	if mode := os.Getenv("SHELF_FAKE_AMDL"); mode != "" {
		os.Exit(fakeAMDL(mode, os.Args[1:]))
	}
	os.Exit(m.Run())
}

// makeToken returns a JWT shaped like the web player's, expiring at exp.
func makeToken(exp time.Time, salt string) string {
	enc := base64.RawURLEncoding
	head := enc.EncodeToString([]byte(`{"alg":"ES256","kid":"WebPlayKid","typ":"JWT"}`))
	body := enc.EncodeToString(fmt.Appendf(nil, `{"iss":"AMPWebPlay","iat":%d,"exp":%d}`, exp.Add(-time.Hour).Unix(), exp.Unix()))
	return "eyJ" + strings.TrimPrefix(head, "eyJ") + "." + body + "." + enc.EncodeToString([]byte("sig-"+salt))
}

// fakeApple serves the web player's page and script and the catalog
// fixtures for storefront ru.
type fakeApple struct {
	t   *testing.T
	srv *httptest.Server

	mu       sync.Mutex
	token    string
	tokens   int
	scrapes  int
	requests []string
	// fail401 answers the next calls 401 and rotates the token.
	fail401 int
	// fail429 answers the next calls 429, with retryAfter when set.
	fail429     int
	retryAfter  string
	refuseViews bool
}

func newFakeApple(t *testing.T) *fakeApple {
	f := &fakeApple{t: t}
	f.rotate()
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeApple) rotate() {
	f.tokens++
	f.token = makeToken(time.Now().Add(48*time.Hour), fmt.Sprint(f.tokens))
}

func (f *fakeApple) currentToken() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.token
}

func (f *fakeApple) calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.requests...)
}

func (f *fakeApple) fixture(w http.ResponseWriter, name string) {
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		f.t.Errorf("fixture %s: %v", name, err)
		http.Error(w, "no fixture", http.StatusInternalServerError)
		return
	}
	_, _ = w.Write(b)
}

func (f *fakeApple) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch r.URL.Path {
	case "/":
		f.fixture(w, "page.html")
		return
	case "/assets/index~4f2a9c1b7e.js":
		f.scrapes++
		_, _ = fmt.Fprintf(w, `var e=1;const k="%s";var other="eyJbm90IHRoaXM.eyJub3Q.c2ln";`, f.token)
		return
	}
	if r.Header.Get("Authorization") != "Bearer "+f.token || r.Header.Get("Origin") != f.srv.URL {
		http.Error(w, "{}", http.StatusUnauthorized)
		return
	}
	f.requests = append(f.requests, r.URL.Path+"?"+r.URL.RawQuery)
	if f.fail401 > 0 {
		f.fail401--
		f.rotate()
		http.Error(w, "{}", http.StatusUnauthorized)
		return
	}
	if f.fail429 > 0 {
		f.fail429--
		if f.retryAfter != "" {
			w.Header().Set("Retry-After", f.retryAfter)
		}
		http.Error(w, "{}", http.StatusTooManyRequests)
		return
	}
	q := r.URL.Query()
	p := strings.TrimPrefix(r.URL.Path, "/v1/catalog/ru/")
	switch {
	case p == "search":
		f.fixture(w, "search-"+q.Get("types")+".json")
	case p == "albums/1440000000" && q.Get("include") == "tracks,artists":
		f.fixture(w, "album.json")
	case p == "albums/1440000000/tracks" && q.Get("offset") == "2":
		f.fixture(w, "album-tracks-2.json")
	case p == "albums/1109714933" && q.Get("include") == "tracks,artists":
		f.fixture(w, "live/album-1109714933.json")
	case p == "songs/1440000002":
		f.fixture(w, "song.json")
	case p == "playlists/pl.0123456789abcdef":
		f.fixture(w, "playlist.json")
	case p == "artists/657515" && q.Get("views") != "":
		f.fixture(w, "live/artist-657515-views.json")
	case p == "artists/657515/view/full-albums" && q.Get("offset") == "10":
		f.fixture(w, "live/artist-657515-view-full-albums-10.json")
	case p == "artists/657515/view/singles" && (q.Get("offset") == "10" || q.Get("offset") == "20"):
		f.fixture(w, "live/artist-657515-view-singles-"+q.Get("offset")+".json")
	case p == "playlists/pl.videos":
		_, _ = w.Write([]byte(`{"data":[{"id":"pl.videos","type":"playlists","attributes":{"name":"Videos"},"relationships":{"tracks":{"data":[{"id":"1500000001","type":"music-videos","attributes":{"name":"Shoreline (Video)"}}]}}}]}`))
	case p == "artists/111" && q.Get("views") != "":
		f.fixture(w, "artist-views.json")
	case p == "artists/111/view/full-albums" && q.Get("offset") == "1":
		f.fixture(w, "artist-view-2.json")
	case p == "artists/222" && q.Get("views") != "" && f.refuseViews:
		http.Error(w, `{"errors":[{"status":"400"}]}`, http.StatusBadRequest)
	case p == "artists/222" && q.Get("views") != "":
		_, _ = w.Write([]byte(`{"data":[{"id":"222","type":"artists","attributes":{"name":"Harbor Lights"}}]}`))
	case p == "artists/222":
		f.fixture(w, "artist.json")
	case p == "artists/222/albums" && q.Get("offset") == "0":
		f.fixture(w, "artist-albums.json")
	case p == "artists/222/albums" && q.Get("offset") == "100":
		f.fixture(w, "artist-albums-2.json")
	default:
		http.Error(w, `{"errors":[{"status":"404"}]}`, http.StatusNotFound)
	}
}

// fakeWrapper serves wrapper-lite's /status and the supervisor's control
// API on one address.
type fakeWrapper struct {
	srv *httptest.Server

	mu       sync.Mutex
	regions  []string
	state    wrapperapi.Status
	down     bool
	statuses int
	logins   []wrapperapi.LoginRequest
	codes    []string
	restarts int
}

const wrapperToken = "wrapper-secret"

func newFakeWrapper(t *testing.T) *fakeWrapper {
	f := &fakeWrapper{regions: []string{"ru"}, state: wrapperapi.Status{State: wrapperapi.Running, Storefront: "ru", Since: time.Now().UTC()}}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeWrapper) set(fn func(f *fakeWrapper)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(f)
}

func (f *fakeWrapper) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.down {
		hj, ok := w.(http.Hijacker)
		if ok {
			if c, _, err := hj.Hijack(); err == nil {
				_ = c.Close()
				return
			}
		}
		http.Error(w, "down", http.StatusBadGateway)
		return
	}
	if r.URL.Path == "/status" {
		f.statuses++
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "msg": "SUCCESS", "data": map[string]any{"regions": f.regions}})
		return
	}
	if r.Header.Get("Authorization") != "Bearer "+wrapperToken {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(wrapperapi.ErrorBody{Error: "wrong or missing token"})
		return
	}
	answer := func(code int) {
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(f.state)
	}
	switch r.Method + " " + r.URL.Path {
	case "GET " + wrapperapi.PathState:
		answer(http.StatusOK)
	case "POST " + wrapperapi.PathLogin:
		var req wrapperapi.LoginRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if f.state.Busy() {
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(wrapperapi.ErrorBody{Error: "a sign-in is running"})
			return
		}
		f.logins = append(f.logins, req)
		f.state = wrapperapi.Status{State: wrapperapi.SigningIn, Since: time.Now().UTC()}
		answer(http.StatusAccepted)
	case "POST " + wrapperapi.PathCode:
		var req wrapperapi.CodeRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if f.state.State != wrapperapi.AwaitingCode {
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(wrapperapi.ErrorBody{Error: "the wrapper is not waiting for a code"})
			return
		}
		f.codes = append(f.codes, req.Code)
		f.state = wrapperapi.Status{State: wrapperapi.SigningIn, Since: time.Now().UTC()}
		answer(http.StatusOK)
	case "POST " + wrapperapi.PathRestart:
		f.restarts++
		f.state = wrapperapi.Status{State: wrapperapi.Starting, Since: time.Now().UTC()}
		answer(http.StatusAccepted)
	case "GET " + wrapperapi.PathLog:
		_ = json.NewEncoder(w).Encode(wrapperapi.LogPage{Lines: []wrapperapi.LogLine{{N: 7, Text: "wrapper-lite listening on 0.0.0.0:12340"}}})
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

const baseConfig = `general:
  lite-server: "http://127.0.0.1:12340"
  language: "en-GB"
  storefront: "us"
  exit-on-error: false
paths:
  alac: "/downloads"
  album-folder: "{ArtistName} - {AlbumName} ({ReleaseYear})[{Quality}]"
convert:
  after-download: true
  format: "flac"
`

// rig is the module after Init, with its catalog on fakeApple and its
// wrapper on fakeWrapper.
type rig struct {
	t       *testing.T
	m       *Module
	s       *Source
	apple   *fakeApple
	wrap    *fakeWrapper
	hub     *hub.Hub
	data    string
	music   string
	cfgPath string
}

func newRig(t *testing.T, env ...string) *rig {
	t.Helper()
	r := &rig{t: t, apple: newFakeApple(t), wrap: newFakeWrapper(t), hub: hub.New(), data: t.TempDir(), music: t.TempDir()}
	r.cfgPath = filepath.Join(t.TempDir(), "amdl.yaml")
	if err := os.WriteFile(r.cfgPath, []byte(baseConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	vars := map[string]string{
		"SHELF_DATA_DIR": r.data, "SHELF_MUSIC_DIR": r.music, "SHELF_AMDL_CONFIG": r.cfgPath,
		"SHELF_WRAPPER_URL": r.wrap.srv.URL, "SHELF_WRAPPER_CONTROL_URL": r.wrap.srv.URL,
		"SHELF_WRAPPER_TOKEN": wrapperToken, "SHELF_AMDL_TIMEOUT": "1m",
	}
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		vars[k] = v
	}
	cfg, err := config.Load(func(k string) (string, bool) { v, ok := vars[k]; return v, ok })
	if err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	r.m = New()
	r.m.AMDL = exe
	r.m.PollEvery, r.m.PollBusy = 20*time.Millisecond, 10*time.Millisecond
	if err := r.m.Init(module.Deps{Cfg: cfg, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Hub: r.hub}); err != nil {
		t.Fatal(err)
	}
	r.s = r.m.Source()
	if r.s.catalog != nil {
		r.s.catalog.api, r.s.catalog.web = r.apple.srv.URL, r.apple.srv.URL
		r.s.catalog.sleep = func(context.Context, time.Duration) error { return nil }
	}
	return r
}

var bg = context.Background()

func (r *rig) item(kind source.Kind, id string) source.Item {
	r.t.Helper()
	it, err := r.s.Item(bg, source.Ref{Source: ID, Kind: kind, ID: id})
	if err != nil {
		r.t.Fatal(err)
	}
	return it
}

// fakeAMDL acts as amdl for one run. It reads the rendered config for the
// folder, prints amdl's lines, writes FLAC files with a real STREAMINFO
// block, and prints the JSON array.
func fakeAMDL(mode string, args []string) int {
	if f := os.Getenv("FAKE_ARGS"); f != "" {
		_ = os.WriteFile(f, []byte(strings.Join(args, " ")), 0o644)
	}
	var cfg struct {
		General struct {
			LiteServer  string `yaml:"lite-server"`
			ExitOnError bool   `yaml:"exit-on-error"`
		} `yaml:"general"`
		Paths struct {
			ALAC string `yaml:"alac"`
		} `yaml:"paths"`
	}
	b, err := os.ReadFile(args[1])
	if err != nil || yaml.Unmarshal(b, &cfg) != nil || !cfg.General.ExitOnError {
		fmt.Println("load Config failed")
		return 0
	}
	dir := filepath.Join(cfg.Paths.ALAC, "Northern Static - Low Tide Hymns (2019)[24B-96kHz]")
	type added struct {
		Path     string `json:"path"`
		Artist   string `json:"artist"`
		ArtistID string `json:"artist_id"`
		Album    string `json:"album"`
		Song     string `json:"song"`
	}
	var list []added
	write := func(n int, song string) {
		p := filepath.Join(dir, fmt.Sprintf("%02d. %s.flac", n, song))
		_ = os.MkdirAll(dir, 0o755)
		_ = os.WriteFile(p, flacHeader(24, 96000), 0o644)
		list = append(list, added{Path: p, Artist: "Northern Static", ArtistID: "111", Album: "Low Tide Hymns", Song: song})
	}
	if mode == "replay" {
		return replayAMDL(cfg.Paths.ALAC)
	}
	fmt.Println("lite-server regions: [ru]")
	switch mode {
	case "early":
		fmt.Println("Failed to get token.")
		return 0
	case "album":
		fmt.Println("Queue 1 of 1: Album")
		fmt.Println(filepath.Base(dir))
		fmt.Println("Track 1 of 3: songs")
		fmt.Println("01. Shoreline")
		fmt.Print("Decrypting...  50%  (1.0/2.0 MB, 4.0 MB/s) \rDecrypting... 100%  (2.0/2.0 MB, 4.0 MB/s) [0s] Decrypted\n")
		write(1, "Shoreline")
		fmt.Println("Track 2 of 3: songs")
		fmt.Println("Unavailable")
		fmt.Println("Track 3 of 3: songs")
		fmt.Println("Converted track already exists locally.")
		write(3, "Breakwater")
	case "song":
		fmt.Println("Song->Album")
		fmt.Println("Track 2 of 3: songs")
		write(2, "Undertow")
	case "none":
		fmt.Println("Track 1 of 3: songs")
		fmt.Println("Unavailable")
		fmt.Println("Track 2 of 3: songs")
		fmt.Println("Unavailable")
		fmt.Println("Track 3 of 3: songs")
		fmt.Println("Unavailable")
	case "flaky":
		// Song 2 fails in the first FAKE_FAILS runs, with the failure on its
		// bar's line, as when Apple's key server refused a key in a live run.
		runs := 1
		if b, err := os.ReadFile(os.Getenv("FAKE_STATE")); err == nil {
			_, _ = fmt.Sscan(string(b), &runs)
			runs++
		}
		_ = os.WriteFile(os.Getenv("FAKE_STATE"), []byte(fmt.Sprint(runs)), 0o644)
		fails := 0
		_, _ = fmt.Sscan(os.Getenv("FAKE_FAILS"), &fails)
		fmt.Println("Queue 1 of 1: Album")
		fmt.Println(filepath.Base(dir))
		for i, song := range []string{"Shoreline", "Undertow", "Breakwater"} {
			n := i + 1
			fmt.Printf("Track %d of 3: songs\n%02d. %s\n", n, n, song)
			if _, err := os.Stat(filepath.Join(dir, fmt.Sprintf("%02d. %s.flac", n, song))); err == nil {
				fmt.Println("Converted track already exists locally.")
				list = append(list, added{Path: filepath.Join(dir, fmt.Sprintf("%02d. %s.flac", n, song)), Artist: "Northern Static", ArtistID: "111", Album: "Low Tide Hymns", Song: song})
				continue
			}
			if n == 2 && runs <= fails {
				fmt.Print("Decrypting...  10%  (1.0/9.9 MB, 5.0 MB/s) \rDecrypting...  20%  (2.0/9.9 MB, 5.0 MB/s) Failed to run v4: lite-server /key returned code=500 msg=internal error\n")
				continue
			}
			fmt.Print("Decrypting... 100%  (9.9/9.9 MB, 5.0 MB/s) [1s] Decrypted\n")
			write(n, song)
		}
		if runs <= fails {
			fmt.Println("=======  [✔ ] Completed: 2/3  |  [⚠ ] Warnings: 0  |  [✖ ] Errors: 1  =======")
			fmt.Println("Error detected, exiting...")
			return 1
		}
	case "fail":
		fmt.Println("Track 1 of 3: songs")
		fmt.Println("Failed to run v4: key retrieval failed")
		fmt.Println("Error detected, exiting...")
		return 1
	}
	fmt.Println("=======  [✔ ] Completed")
	out, _ := json.Marshal(list)
	fmt.Println(string(out))
	return 0
}

// replayAMDL prints the recorded run in FAKE_REPLAY with its folder moved
// to alac, and writes the FLAC files it lists, as amdl did.
func replayAMDL(alac string) int {
	b, err := os.ReadFile(os.Getenv("FAKE_REPLAY"))
	if err != nil {
		fmt.Println(err)
		return 1
	}
	out := strings.ReplaceAll(string(b), "/music/apple-music", alac)
	lines := strings.Split(strings.TrimSpace(out), "\n")
	var list []struct {
		Path string `json:"path"`
	}
	_ = json.Unmarshal([]byte(lines[len(lines)-1]), &list)
	for _, a := range list {
		if _, err := os.Stat(a.Path); err == nil {
			continue
		}
		_ = os.MkdirAll(filepath.Dir(a.Path), 0o755)
		_ = os.WriteFile(a.Path, flacHeader(16, 44100), 0o644)
	}
	_, _ = os.Stdout.WriteString(out)
	return 0
}

// flacHeader is a FLAC file's magic and STREAMINFO block.
func flacHeader(bits, rate int) []byte {
	b := []byte("fLaC")
	b = append(b, 0x80, 0, 0, 34) // last block, STREAMINFO, 34 bytes
	si := make([]byte, 34)
	binary.BigEndian.PutUint16(si[0:], 4096)
	binary.BigEndian.PutUint16(si[2:], 4096)
	// 20 bits of rate, 3 of channels minus one, 5 of bits minus one.
	v := uint32(rate)<<12 | uint32(1)<<9 | uint32(bits-1)<<4
	si[10], si[11], si[12], si[13] = byte(v>>24), byte(v>>16), byte(v>>8), byte(v)
	return append(b, si...)
}
