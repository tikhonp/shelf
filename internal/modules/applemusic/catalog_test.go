package applemusic

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/tikhonp/shelf/internal/source"
)

func TestTokenScrape(t *testing.T) {
	r := newRig(t)
	tok, err := r.s.catalog.scrape(bg)
	if err != nil || tok != r.apple.currentToken() {
		t.Fatalf("%q %v", tok, err)
	}
	exp := expiry(tok)
	if time.Until(exp) < 47*time.Hour || time.Until(exp) > 49*time.Hour {
		t.Fatalf("exp %v", exp)
	}
	// A page without the script, and a script without a token, are errors
	// that say which.
	for page, want := range map[string]string{
		"<html>no script</html>":                     "no index script",
		`<script src="/assets/index~1.js"></script>`: "no token",
	} {
		bare := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/" {
				_, _ = w.Write([]byte(page))
				return
			}
			_, _ = w.Write([]byte("const nothing = 1;"))
		}))
		c := newCatalog(&http.Client{}, nil, nil)
		c.web = bare.URL
		if _, err := c.scrape(bg); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v", want, err)
		}
		bare.Close()
	}
}

func TestTokenCached(t *testing.T) {
	now := time.Now()
	fetches := 0
	exp := now.Add(time.Hour)
	tk := &tokens{
		fetch: func(context.Context) (string, error) { fetches++; return makeToken(exp, "x"), nil },
		now:   func() time.Time { return now },
	}
	for range 3 {
		if _, err := tk.get(bg); err != nil {
			t.Fatal(err)
		}
	}
	if fetches != 1 {
		t.Fatalf("%d fetches", fetches)
	}
	// Ten minutes before exp it is scraped again.
	now = exp.Add(-11 * time.Minute)
	_, _ = tk.get(bg)
	if fetches != 1 {
		t.Fatalf("scraped %d times eleven minutes before exp", fetches)
	}
	now = exp.Add(-9 * time.Minute)
	_, _ = tk.get(bg)
	if fetches != 2 {
		t.Fatalf("not scraped nine minutes before exp: %d", fetches)
	}
	// A token without exp is kept for an hour.
	tk = &tokens{fetch: func(context.Context) (string, error) { fetches++; return "eyJa.eyJb.c", nil }, now: func() time.Time { return now }}
	fetches = 0
	_, _ = tk.get(bg)
	now = now.Add(59 * time.Minute)
	_, _ = tk.get(bg)
	if fetches != 1 {
		t.Fatalf("%d fetches within the hour", fetches)
	}

	// Through the catalog: two calls, one scrape.
	r := newRig(t)
	r.item(source.KindTrack, "1440000002")
	r.item(source.KindTrack, "1440000002")
	if r.apple.scrapes != 1 {
		t.Fatalf("%d scrapes", r.apple.scrapes)
	}
}

func TestTokenRefreshOn401(t *testing.T) {
	r := newRig(t)
	r.item(source.KindTrack, "1440000002")
	r.apple.mu.Lock()
	r.apple.fail401 = 1
	r.apple.mu.Unlock()
	it := r.item(source.KindTrack, "1440000002")
	if it.Title != "Undertow" || r.apple.scrapes != 2 {
		t.Fatalf("%q after %d scrapes", it.Title, r.apple.scrapes)
	}
	// A second 401 in a row is an error, not a loop.
	r.apple.mu.Lock()
	r.apple.fail401 = 2
	r.apple.mu.Unlock()
	_, err := r.s.Item(bg, source.Ref{Source: ID, Kind: source.KindTrack, ID: "1440000002"})
	var se *statusError
	if !errors.As(err, &se) || se.status != 401 || strings.Contains(err.Error(), "eyJ") {
		t.Fatalf("%v", err)
	}
}

func TestBackoffOn429(t *testing.T) {
	r := newRig(t)
	var waits []time.Duration
	r.s.catalog.sleep = func(_ context.Context, d time.Duration) error { waits = append(waits, d); return nil }
	r.apple.mu.Lock()
	r.apple.fail429 = 3
	r.apple.mu.Unlock()
	if it := r.item(source.KindTrack, "1440000002"); it.Title != "Undertow" {
		t.Fatal(it.Title)
	}
	if !slices.Equal(waits, []time.Duration{2 * time.Second, 4 * time.Second, 8 * time.Second}) {
		t.Fatalf("waits %v", waits)
	}
	// After three waits the call fails.
	waits = nil
	r.apple.mu.Lock()
	r.apple.fail429 = 4
	r.apple.mu.Unlock()
	if _, err := r.s.Item(bg, source.Ref{Source: ID, Kind: source.KindTrack, ID: "1440000002"}); !errors.Is(err, ErrRateLimited) || len(waits) != 3 {
		t.Fatalf("%v after %v", err, waits)
	}
	// Retry-After wins, capped at a minute.
	waits = nil
	r.apple.mu.Lock()
	r.apple.fail429, r.apple.retryAfter = 2, "600"
	r.apple.mu.Unlock()
	r.item(source.KindTrack, "1440000002")
	if !slices.Equal(waits, []time.Duration{time.Minute, time.Minute}) {
		t.Fatalf("waits %v", waits)
	}
	// A context that ends stops the wait.
	r.s.catalog.sleep = sleepCtx
	r.apple.mu.Lock()
	r.apple.fail429, r.apple.retryAfter = 1, ""
	r.apple.mu.Unlock()
	ctx, cancel := context.WithTimeout(bg, 50*time.Millisecond)
	defer cancel()
	if _, err := r.s.Item(ctx, source.Ref{Source: ID, Kind: source.KindTrack, ID: "1440000002"}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}

func TestSearchMapsTypes(t *testing.T) {
	r := newRig(t)
	p, err := r.s.Search(bg, source.Query{Kind: source.KindAlbum, Text: "northern", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Items) != 2 || p.Total != 3 || p.Limit != 2 {
		t.Fatalf("%+v", p)
	}
	a := p.Items[0]
	want := source.Item{
		Ref: source.Ref{Source: ID, Kind: source.KindAlbum, ID: "1440000000"}, Title: "Low Tide Hymns", Artist: "Northern Static",
		Year: 2019, ReleaseDate: "2019-03-01", Label: "Example Label", UPC: "0000000000017", Genre: "Electronic", Explicit: true,
		TrackCount: 3, CoverURL: "https://is1-ssl.mzstatic.com/image/thumb/Music/v4/aa/bb/cc/source/600x600bb.jpg",
		URL:     "https://music.apple.com/ru/album/low-tide-hymns/1440000000",
		Quality: source.Quality{Codec: "alac", HiRes: true, Atmos: true, Badge: "Hi-Res Lossless"},
	}
	if !reflect.DeepEqual(a, want) {
		t.Fatalf("got  %+v\nwant %+v", a, want)
	}
	if s := p.Items[1]; s.Year != 2021 || s.Quality.Badge != "AAC" || !strings.HasSuffix(s.CoverURL, "/600x600bb.jpg") {
		t.Fatalf("%+v", s)
	}
	calls := r.apple.calls()
	if !strings.Contains(calls[0], "types=albums") || !strings.Contains(calls[0], "l=en-GB") || !strings.Contains(calls[0], "limit=2") {
		t.Fatalf("%q", calls)
	}

	p, _ = r.s.Search(bg, source.Query{Kind: source.KindTrack, Text: "shoreline"})
	if s := p.Items[0]; s.Ref.Kind != source.KindTrack || s.Album != "Low Tide Hymns" || s.Duration != 202 || s.ISRC != "XX0000000001" || s.TrackCount != 1 || p.Total != 1 {
		t.Fatalf("%+v %d", s, p.Total)
	}
	p, _ = r.s.Search(bg, source.Query{Kind: source.KindArtist, Text: "northern"})
	if len(p.Items) != 2 || p.Items[0].Ref != (source.Ref{Source: ID, Kind: source.KindArtist, ID: "111"}) || p.Items[1].CoverURL != "" {
		t.Fatalf("%+v", p.Items)
	}
	p, _ = r.s.Search(bg, source.Query{Kind: source.KindPlaylist, Text: "ambient"})
	if pl := p.Items[0]; pl.Ref.ID != "pl.0123456789abcdef" || pl.Artist != "Apple Music Ambient" || !strings.HasSuffix(pl.CoverURL, "600x600cc.jpg") {
		t.Fatalf("%+v", pl)
	}
	// An empty text asks nothing; Apple caps a page at 25.
	before := len(r.apple.calls())
	if p, err := r.s.Search(bg, source.Query{Kind: source.KindAlbum, Text: "  "}); err != nil || len(p.Items) != 0 || len(r.apple.calls()) != before {
		t.Fatalf("%+v %v", p, err)
	}
	_, _ = r.s.Search(bg, source.Query{Kind: source.KindAlbum, Text: "x", Limit: 100, Offset: 50})
	if last := r.apple.calls()[len(r.apple.calls())-1]; !strings.Contains(last, "limit=25") || !strings.Contains(last, "offset=50") {
		t.Fatal(last)
	}
	if _, err := r.s.Search(bg, source.Query{Kind: source.KindLabel, Text: "x"}); err == nil {
		t.Fatal("label search accepted")
	}
}

func TestQualityFromTraits(t *testing.T) {
	for _, tc := range []struct {
		traits []string
		want   source.Quality
	}{
		{[]string{"atmos", "hi-res-lossless", "lossless", "lossy-stereo", "spatial"}, source.Quality{Codec: "alac", HiRes: true, Atmos: true, Badge: "Hi-Res Lossless"}},
		{[]string{"lossless", "lossy-stereo"}, source.Quality{Codec: "alac", Badge: "Lossless"}},
		{[]string{"atmos", "lossy-stereo", "spatial"}, source.Quality{Codec: "aac", Atmos: true, Badge: "AAC"}},
		{[]string{"lossy-stereo"}, source.Quality{Codec: "aac", Badge: "AAC"}},
		{nil, source.Quality{}},
	} {
		if got := qualityFromTraits(tc.traits); got != tc.want {
			t.Errorf("%v: %+v, want %+v", tc.traits, got, tc.want)
		}
	}
}

func TestAlbumPaging(t *testing.T) {
	r := newRig(t)
	it := r.item(source.KindAlbum, "1440000000")
	if len(it.Tracks) != 3 || it.TrackCount != 3 || it.Duration != 202+180+240 {
		t.Fatalf("%d tracks, %d s", len(it.Tracks), it.Duration)
	}
	want := source.Track{ID: "1440000003", Position: 3, Disc: 2, Title: "Breakwater", Artist: "Northern Static", ISRC: "XX0000000003", Duration: 240, Quality: source.Quality{Codec: "alac", Badge: "Lossless"}}
	if it.Tracks[2] != want || !it.Tracks[0].Explicit || it.Tracks[1].Artist != "Northern Static feat. Harbor Lights" {
		t.Fatalf("%+v", it.Tracks)
	}
	if it.Label != "Example Label" || it.Year != 2019 || it.Quality.Badge != "Hi-Res Lossless" {
		t.Fatalf("%+v", it)
	}

	// A playlist lists its songs, not its music videos, and amdl's item
	// numbers skip the video.
	rel, err := r.s.release(bg, source.Ref{Source: ID, Kind: source.KindPlaylist, ID: "pl.0123456789abcdef"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rel.item.Tracks) != 2 || rel.item.Tracks[1].Title != "Salt" || rel.position(1) != 1 || rel.position(2) != 0 || rel.position(3) != 2 {
		t.Fatalf("%+v %v", rel.item.Tracks, rel.positions)
	}
	// A song comes with its album's name and label.
	s := r.item(source.KindTrack, "1440000002")
	if s.Album != "Low Tide Hymns" || s.Label != "Example Label" || len(s.Tracks) != 1 || s.Tracks[0].Position != 1 {
		t.Fatalf("%+v", s)
	}
	if _, err := r.s.Item(bg, source.Ref{Source: ID, Kind: source.KindAlbum, ID: "999"}); !errors.Is(err, source.ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := r.s.Item(bg, source.Ref{Source: ID, Kind: source.KindLabel, ID: "1"}); !errors.Is(err, source.ErrNotFound) {
		t.Fatal(err)
	}
}

func sectionIDs(it source.Item) map[string][]string {
	out := map[string][]string{}
	for _, s := range it.Sections {
		out[s.ID] = []string{}
		for _, a := range s.Items {
			out[s.ID] = append(out[s.ID], a.Ref.ID)
		}
	}
	return out
}

func TestArtistViews(t *testing.T) {
	r := newRig(t)
	it := r.item(source.KindArtist, "111")
	want := map[string][]string{
		"albums":       {"1440000000", "1440000400"},
		"eps":          {"1440000100"},
		"compilations": {},
		"appears_on":   {"1440000300"},
	}
	if got := sectionIDs(it); !reflect.DeepEqual(got, want) {
		t.Fatalf("%v", got)
	}
	if it.Title != "Northern Static" || it.TrackCount != 4 || it.Sections[1].Title != "EPs and singles" {
		t.Fatalf("%+v", it)
	}
	if !strings.Contains(r.apple.calls()[0], "views=full-albums%2Csingles%2Ccompilation-albums%2Cappears-on-albums") {
		t.Fatal(r.apple.calls()[0])
	}
	if a := it.Sections[0].Items[0]; a.Ref.Source != ID || a.Ref.Kind != source.KindAlbum || len(a.Tracks) != 0 {
		t.Fatalf("%+v", a)
	}
}

func TestArtistFallback(t *testing.T) {
	r := newRig(t)
	want := map[string][]string{
		"albums":       {"1440000500"},
		"eps":          {"1440000501"},
		"compilations": {"1440000502"},
	}
	// An answer without views, and a refused views request, both page
	// through /albums.
	for _, refuse := range []bool{false, true} {
		r.apple.mu.Lock()
		r.apple.refuseViews = refuse
		r.apple.mu.Unlock()
		it := r.item(source.KindArtist, "222")
		if got := sectionIDs(it); !reflect.DeepEqual(got, want) || it.Title != "Harbor Lights" || it.TrackCount != 3 {
			t.Fatalf("refused %v: %v %+v", refuse, got, it)
		}
	}
}

func TestStorefront(t *testing.T) {
	r := newRig(t)
	if got := r.s.store.get(bg); got != "ru" {
		t.Fatal(got)
	}
	// Cached for ten minutes.
	r.wrap.set(func(f *fakeWrapper) { f.regions = []string{"jp"} })
	if got := r.s.store.get(bg); got != "ru" || r.wrap.statuses != 1 {
		t.Fatalf("%s after %d reads", got, r.wrap.statuses)
	}
	now := time.Now().Add(11 * time.Minute)
	r.s.store.now = func() time.Time { return now }
	if got := r.s.store.get(bg); got != "jp" {
		t.Fatal(got)
	}

	// A wrapper that doesn't answer leaves the config's storefront.
	r2 := newRig(t)
	r2.wrap.set(func(f *fakeWrapper) { f.down = true })
	if got := r2.s.store.get(bg); got != "us" {
		t.Fatal(got)
	}
	// ...and isn't asked again for a minute.
	r2.wrap.set(func(f *fakeWrapper) { f.down = false })
	if got := r2.s.store.get(bg); got != "us" || r2.wrap.statuses != 0 {
		t.Fatalf("%s after %d reads", got, r2.wrap.statuses)
	}
	// A signed-out wrapper lists no region: the config's storefront again.
	r3 := newRig(t)
	r3.wrap.set(func(f *fakeWrapper) { f.regions = []string{} })
	if got := r3.s.store.get(bg); got != "us" {
		t.Fatal(got)
	}
	// The catalog asks in the account's storefront, in lower case.
	r.s.store.now = func() time.Time { return now.Add(20 * time.Minute) }
	r.wrap.set(func(f *fakeWrapper) { f.regions = []string{"RU"} })
	r.item(source.KindTrack, "1440000002")
	for _, c := range r.apple.calls() {
		if !strings.HasPrefix(c, "/v1/catalog/ru/") {
			t.Fatalf("%q", c)
		}
	}
}
