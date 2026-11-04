package library

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tikhonp/shelf/internal/modules/library/lrclib"
	"github.com/tikhonp/shelf/internal/modules/library/musicbrainz"
	"github.com/tikhonp/shelf/internal/source"
)

const (
	blueID  = "11111111-1111-4111-8111-111111111111"
	threeID = "55555555-5555-4555-8555-555555555555"
	bachID  = "66666666-6666-4666-8666-666666666666"
)

// services stands in for MusicBrainz, the Cover Art Archive, LRCLIB and
// the image hosts, with the recorded answers in musicbrainz/testdata.
type services struct {
	t   *testing.T
	srv *httptest.Server

	mu       sync.Mutex
	paths    []string
	queries  []string
	mbCalls  atomic.Int32
	down     bool
	lyricsOn bool
}

func (s *services) handle(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.paths = append(s.paths, r.URL.Path+"?inc="+r.URL.Query().Get("inc"))
	if q := r.URL.Query().Get("query"); q != "" {
		s.queries = append(s.queries, q)
	}
	down := s.down
	s.mu.Unlock()
	if down {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	send := func(name string) {
		b, err := os.ReadFile(filepath.Join("musicbrainz", "testdata", name))
		if err != nil {
			s.t.Error(err)
		}
		_, _ = w.Write([]byte(strings.ReplaceAll(string(b), "http://coverartarchive.org", s.srv.URL+"/img")))
	}
	p := r.URL.Path
	if strings.HasPrefix(p, "/ws/2/") {
		s.mbCalls.Add(1)
	}
	switch {
	case p == "/ws/2/release/"+blueID:
		send("release-blue.json")
	case p == "/ws/2/release/"+threeID:
		send("release-three.json")
	case p == "/ws/2/release/"+bachID:
		send("release-classical.json")
	case p == "/ws/2/release":
		switch q := r.URL.Query().Get("query"); {
		case q == `barcode:"075992203823"` || strings.Contains(q, `"Blue`):
			send("search-blue.json")
		case strings.Contains(q, "Goldberg"):
			send("search-classical.json")
		default:
			_, _ = w.Write([]byte(`{"releases": []}`))
		}
	case p == "/ws/2/recording" && r.URL.Query().Get("query") == `isrc:"USRE10000001"`:
		send("isrc.json")
	case p == "/ws/2/recording":
		_, _ = w.Write([]byte(`{"recordings": []}`))
	case p == "/caa/release/"+blueID:
		send("caa.json")
	case strings.HasPrefix(p, "/img/"):
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(jpegOf(s.t, 40, 40))
	case p == "/lrclib/get" && s.lyricsOn && r.URL.Query().Get("track_name") == "All I Want":
		_, _ = w.Write([]byte(`{"plainLyrics": "I am on a lonely road", "syncedLyrics": "[00:01.00] I am on a lonely road", "instrumental": false}`))
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (s *services) asked(prefix string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.ContainsFunc(s.paths, func(p string) bool { return strings.HasPrefix(p, prefix) })
}

func (s *services) lastQuery() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.queries) == 0 {
		return ""
	}
	return s.queries[len(s.queries)-1]
}

// catalog is a source that searches and reads albums.
type catalog struct {
	album    source.Item
	err      error
	searches atomic.Int32
	reads    atomic.Int32
	read     string
}

func (*catalog) ID() string                           { return "cat" }
func (*catalog) Name() string                         { return "Catalog" }
func (*catalog) Health(context.Context) source.Health { return source.Health{OK: true} }
func (*catalog) SearchKinds() []source.Kind           { return []source.Kind{source.KindAlbum} }
func (c *catalog) Search(context.Context, source.Query) (source.Page, error) {
	c.searches.Add(1)
	if c.err != nil {
		return source.Page{}, c.err
	}
	hit := c.album
	hit.Tracks = nil
	return source.Page{Items: []source.Item{hit}, Total: 1}, nil
}

func (c *catalog) Item(_ context.Context, ref source.Ref) (source.Item, error) {
	c.reads.Add(1)
	c.read = ref.ID
	if ref.ID != c.album.Ref.ID {
		return source.Item{}, source.ErrNotFound
	}
	return c.album, nil
}

func blueItem() source.Item {
	return source.Item{Ref: source.Ref{Source: "cat", Kind: source.KindAlbum, ID: "alb-9"}, Title: "Blue", Artist: "Joni Mitchell",
		ReleaseDate: "1971-06-22", Label: "Reprise Records", UPC: "075992203823", Genre: "Pop", TrackCount: 2,
		CoverURL: "http://cover.test/blue.jpg", Tracks: []source.Track{
			{ID: "t1", Position: 1, Title: "All I Want", Artist: "Joni Mitchell", ISRC: "USRE10000001", Duration: 1},
			{ID: "t2", Position: 2, Title: "My Old Man", Version: "Remastered", Artist: "Joni Mitchell", ISRC: "USRE10000002", Duration: 1},
		}}
}

// suggestRig is a rig whose lookups reach the stand-ins.
func suggestRig(t *testing.T) (*rig, *services, *catalog) {
	r := newRig(t)
	sv := &services{t: t}
	sv.srv = httptest.NewServer(http.HandlerFunc(sv.handle))
	t.Cleanup(sv.srv.Close)
	cache := lookups{db: r.m.store.db, now: time.Now}
	r.m.MB = musicbrainz.New("test", cache)
	r.m.MB.Base, r.m.MB.CoverBase, r.m.MB.Limit, r.m.MB.Retry = sv.srv.URL+"/ws/2", sv.srv.URL+"/caa", musicbrainz.NewLimiter(0), nil
	r.m.Lyrics = lrclib.New("test", cache)
	r.m.Lyrics.Base = sv.srv.URL + "/lrclib"
	cat := &catalog{album: blueItem()}
	cat.album.CoverURL = sv.srv.URL + "/img/catalog.jpg"
	reg := source.NewRegistry()
	if err := reg.Add(cat); err != nil {
		t.Fatal(err)
	}
	r.m.sources = reg
	return r, sv, cat
}

// blueAlbum adds the two tracks of Blue with the overrides, walks, and
// gives the album's row ID.
func (r *rig) blueAlbum(over1, over2 map[string][]string) int64 {
	r.t.Helper()
	t1 := blue("1", map[string][]string{"TITLE": {"All I Want"}})
	t2 := blue("2", map[string][]string{"TITLE": {"My Old Man"}})
	for k, v := range over1 {
		t1[k] = v
	}
	for k, v := range over2 {
		t2[k] = v
	}
	for _, t := range []map[string][]string{t1, t2} {
		for k, v := range t {
			if v == nil {
				delete(t, k)
			}
		}
	}
	r.add("folk/Blue/01.flac", "track.flac", t1)
	r.add("folk/Blue/02.flac", "track.flac", t2)
	r.walk()
	return r.albums("").Albums[0].ID
}

func (r *rig) suggest(id int64, query string) suggestionsJSON {
	r.t.Helper()
	var s suggestionsJSON
	r.getJSON("/api/library/albums/"+itoa(id)+"/suggestions"+query, &s)
	return s
}

// find gives a field's suggestions, of the album with track 0.
func find(s suggestionsJSON, track int64, field string) []Suggestion {
	list := s.Album
	if track != 0 {
		list = nil
		for _, t := range s.Tracks {
			if t.ID == track {
				list = t.Suggestions
			}
		}
	}
	var out []Suggestion
	for _, sg := range list {
		if sg.Field == field {
			out = append(out, sg)
		}
	}
	return out
}

func trackIDs(t *testing.T, r *rig, id int64) []int64 {
	t.Helper()
	var out []int64
	for _, tr := range r.open(id).Tracks {
		out = append(out, tr.ID)
	}
	return out
}

func TestSuggestionsFromTheFilesRelease(t *testing.T) {
	r, sv, _ := suggestRig(t)
	sv.lyricsOn = true
	id := r.blueAlbum(map[string][]string{"MUSICBRAINZ_ALBUMID": {blueID}}, map[string][]string{"MUSICBRAINZ_ALBUMID": {blueID}})
	r.write("folk/Blue/02.lrc", []byte("[00:01.00] la"))
	tr := trackIDs(t, r, id)
	s := r.suggest(id, "")

	if a := s.Anchor; a == nil || a.ID != blueID || a.By != byFiles || !a.Matched || a.Tracks != 2 || a.URL != "https://musicbrainz.org/release/"+blueID {
		t.Fatalf("anchor %+v", s.Anchor)
	}
	// MusicBrainz and the catalog agree on the label, so it is surer than
	// either, and shows once with the anchor.
	label := find(s, 0, "recordlabel")
	if len(label) != 1 || label[0].Values[0] != "Reprise Records" || label[0].Anchor != blueID ||
		!slices.Equal(label[0].Sources, []string{srcMB, "Catalog"}) || label[0].Source != srcMB || label[0].Confidence < 96 {
		t.Fatalf("label %+v", label)
	}
	if bc := find(s, 0, "barcode"); len(bc) != 1 || bc[0].Anchor != blueID || len(bc[0].Sources) != 2 {
		t.Fatalf("barcode %+v", bc)
	}
	// What the tracks hold already isn't suggested.
	for _, f := range []string{"album", "albumartist", "releasedate"} {
		if sg := find(s, 0, f); sg != nil {
			t.Errorf("%s suggested though the tracks hold it: %+v", f, sg)
		}
	}
	// The genre isn't a release field, so both sources show.
	if g := find(s, 0, "genre"); len(g) != 2 || g[0].Values[0] != "folk" || g[0].Anchor != "" {
		t.Fatalf("genre %+v", g)
	}
	fill := find(s, 0, "musicbrainz_trackid")
	if len(fill) != 1 || fill[0].Anchor != blueID || fill[0].Values != nil ||
		fill[0].Tracks[tr[0]][0] != "33333333-0000-4000-8000-000000000001" || fill[0].Tracks[tr[1]][0] != "33333333-0000-4000-8000-000000000002" {
		t.Fatalf("release track IDs %+v", fill)
	}
	if a := find(s, tr[1], "artist"); len(a) != 1 || a[0].Values[0] != "Joni Mitchell & James Taylor" {
		t.Fatalf("artist %+v", a)
	}
	if ids := find(s, 0, "musicbrainz_artistid"); len(ids) != 1 || len(ids[0].Tracks[tr[1]]) != 2 {
		t.Fatalf("artist IDs %+v", ids)
	}
	// The catalog's track version joins its title.
	if ti := find(s, tr[1], "title"); len(ti) != 1 || ti[0].Values[0] != "My Old Man (Remastered)" || ti[0].Source != "Catalog" {
		t.Fatalf("title %+v", ti)
	}
	if l := find(s, tr[0], "lyrics"); l != nil {
		t.Fatalf("lyrics come with the album: %+v", l)
	}
	if len(s.Covers) != 2 || s.Covers[0].Source != srcCAA || s.Covers[0].Anchor != blueID || s.Covers[1].Source != "Catalog" {
		t.Fatalf("covers %+v", s.Covers)
	}
	if len(s.Notes) != 1 || s.Notes[0].Track != tr[1] || s.Notes[0].Field != "lyrics" {
		t.Fatalf("notes %+v", s.Notes)
	}
	if len(s.Unavailable) != 0 {
		t.Fatalf("unavailable %+v", s.Unavailable)
	}

	// A second opening is answered from the stored lookups.
	before := sv.mbCalls.Load()
	r.suggest(id, "")
	if sv.mbCalls.Load() != before {
		t.Fatalf("MusicBrainz asked again: %d calls", sv.mbCalls.Load()-before)
	}
}

func TestLyrics(t *testing.T) {
	r, sv, _ := suggestRig(t)
	sv.lyricsOn = true
	id := r.blueAlbum(nil, map[string][]string{"LYRICS": {"[00:01.00] I am on a lonely road"}, "TITLE": {"All I Want"}})
	tr := trackIDs(t, r, id)
	var s trackSuggestionsJSON
	r.getJSON("/api/library/tracks/"+itoa(tr[0])+"/suggestions", &s)
	if len(s.Suggestions) != 1 || s.Suggestions[0].Values[0] != "[00:01.00] I am on a lonely road" || s.Suggestions[0].Source != srcLRCLIB ||
		s.Suggestions[0].Confidence != 70 || len(s.Unavailable) != 0 {
		t.Fatalf("lyrics %+v", s)
	}
	// The second track holds these lyrics already.
	r.getJSON("/api/library/tracks/"+itoa(tr[1])+"/suggestions", &s)
	if len(s.Suggestions) != 0 {
		t.Fatalf("held lyrics suggested: %+v", s)
	}
	sv.down = true
	r.getJSON("/api/library/tracks/"+itoa(tr[0])+"/suggestions", &s)
	if len(s.Suggestions) != 1 {
		t.Fatalf("stored lyrics: %+v", s)
	}
	r.add("folk/Blue/03.flac", "track.flac", blue("3", map[string][]string{"TITLE": {"Little Green"}}))
	r.walk()
	d := r.open(r.albums("").Albums[0].ID)
	r.getJSON("/api/library/tracks/"+itoa(d.Tracks[2].ID)+"/suggestions", &s)
	if len(s.Suggestions) != 0 || len(s.Unavailable) != 1 || s.Unavailable[0].Source != srcLRCLIB {
		t.Fatalf("LRCLIB down: %+v", s)
	}
	if rec := r.do(http.MethodGet, "/api/library/tracks/999/suggestions"); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown track: %d", rec.Code)
	}
}

func TestAnchorWays(t *testing.T) {
	for _, tc := range []struct {
		name      string
		over      map[string][]string
		query     string
		by        string
		id        string
		matched   bool
		reason    string
		search    []string // in the last search query
		notSearch string
		notAsked  string
	}{
		{name: "barcode", over: map[string][]string{"BARCODE": {"075992203823"}}, by: byBarcode, id: blueID, matched: true,
			// The three-track release in the answer costs no lookup.
			notAsked: "/ws/2/release/" + threeID},
		{name: "isrc", over: map[string][]string{"ISRC": {"USRE10000001"}}, by: byISRC, id: blueID, matched: true},
		{name: "search", by: bySearch, id: blueID, matched: true, search: []string{`release:"Blue"`, `artist:"Joni Mitchell"`}},
		{name: "compilation", over: map[string][]string{"ALBUMARTIST": {"Various Artists"}, "ALBUMARTISTS": nil, "COMPILATION": {"1"}},
			by: bySearch, id: blueID, matched: true, notSearch: "artist:"},
		{name: "live", over: map[string][]string{"ALBUM": {"Blue Live"}}, by: bySearch, id: blueID, matched: true, search: []string{"secondarytype:live"}},
		{name: "staged", over: map[string][]string{"MUSICBRAINZ_ALBUMID": {threeID}}, query: "?anchor=" + url.QueryEscape("https://musicbrainz.org/release/"+blueID),
			by: byStaged, id: blueID, matched: true},
		{name: "files fail the threshold", over: map[string][]string{"MUSICBRAINZ_ALBUMID": {threeID}}, by: byFiles, id: threeID,
			reason: "The release has 3 tracks and the album 2."},
		{name: "files name no release", over: map[string][]string{"MUSICBRAINZ_ALBUMID": {"00000000-0000-4000-8000-000000000000"}}, by: byFiles,
			id: "00000000-0000-4000-8000-000000000000", reason: "MusicBrainz has no release with this ID."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, sv, _ := suggestRig(t)
			id := r.blueAlbum(tc.over, tc.over)
			s := r.suggest(id, tc.query)
			if tc.by == "" {
				if s.Anchor != nil {
					t.Fatalf("anchor %+v", s.Anchor)
				}
			} else if a := s.Anchor; a == nil || a.By != tc.by || a.ID != tc.id || a.Matched != tc.matched || a.Reason != tc.reason {
				t.Fatalf("anchor %+v", s.Anchor)
			}
			q := sv.lastQuery()
			for _, w := range tc.search {
				if !strings.Contains(q, w) {
					t.Errorf("query %q lacks %q", q, w)
				}
			}
			if tc.notSearch != "" && strings.Contains(q, tc.notSearch) {
				t.Errorf("query %q has %q", q, tc.notSearch)
			}
			if tc.notAsked != "" && sv.asked(tc.notAsked) {
				t.Errorf("asked for %s", tc.notAsked)
			}
			ids := find(s, 0, "musicbrainz_albumid")
			if tc.matched != (len(ids) == 1) {
				t.Fatalf("album ID suggestions %+v", ids)
			}
			if !tc.matched {
				// The catalog's release fields still show, one value each,
				// with no anchor.
				if bc := find(s, 0, "barcode"); tc.over["BARCODE"] == nil && (len(bc) != 1 || bc[0].Anchor != "" || bc[0].Source != "Catalog") {
					t.Fatalf("barcode %+v", bc)
				}
			}
		})
	}
	t.Run("bad anchor", func(t *testing.T) {
		r, _, _ := suggestRig(t)
		id := r.blueAlbum(nil, nil)
		if rec := r.do(http.MethodGet, "/api/library/albums/"+itoa(id)+"/suggestions?anchor=nope"); rec.Code != http.StatusBadRequest {
			t.Fatalf("%d %s", rec.Code, rec.Body)
		}
		if rec := r.do(http.MethodGet, "/api/library/albums/999/suggestions"); rec.Code != http.StatusNotFound {
			t.Fatalf("%d %s", rec.Code, rec.Body)
		}
	})
}

func TestClassicalSuggestsComposers(t *testing.T) {
	r, sv, _ := suggestRig(t)
	over := map[string][]string{"ALBUM": {"Goldberg Variations"}, "ALBUMARTIST": {"Glenn Gould"}, "ALBUMARTISTS": nil, "GENRE": {"Classical"}}
	id := r.blueAlbum(over, over)
	tr := trackIDs(t, r, id)
	s := r.suggest(id, "")
	if s.Anchor == nil || s.Anchor.ID != bachID || !s.Anchor.Matched {
		t.Fatalf("anchor %+v", s.Anchor)
	}
	if q := sv.lastQuery(); strings.Contains(q, "artist:") {
		t.Fatalf("query %q", q)
	}
	if !sv.asked("/ws/2/release/" + bachID + "?inc=artist-credits+labels+recordings+release-groups+isrcs+genres+recording-level-rels+work-rels+work-level-rels+artist-rels") {
		t.Fatal("the release was read without its works")
	}
	if c := find(s, tr[0], "composer"); len(c) != 1 || c[0].Values[0] != "Johann Sebastian Bach" {
		t.Fatalf("composer %+v", c)
	}
	if w := find(s, tr[1], "work"); len(w) != 1 || w[0].Values[0] != "Goldberg-Variationen, BWV 988: Variatio 1" {
		t.Fatalf("work %+v", w)
	}
	if c := find(s, 0, "catalognumber"); c != nil {
		t.Fatalf("[none] suggested: %+v", c)
	}
}

func TestLocalSuggestions(t *testing.T) {
	r, _, cat := suggestRig(t)
	r.m.MB, r.m.Lyrics = nil, nil
	dir := "folk/Northern Static - Low Tide Hymns (2019) [24B-96kHz]"
	strip := map[string][]string{"TITLE": nil, "TRACKNUMBER": nil, "ALBUM": nil, "ALBUMARTIST": nil, "ALBUMARTISTS": nil}
	with := func(over map[string][]string) map[string][]string {
		t := tagged(strip)
		for k, v := range over {
			t[k] = v
		}
		return t
	}
	r.add(dir+"/01 - Shoreline.flac", "track.flac", with(map[string][]string{"GENRE": {"Folk"}}))
	r.add(dir+"/02 - Joni Mitchell - Undertow.flac", "track.flac", with(map[string][]string{"GENRE": {"Folk"}}))
	r.add(dir+"/1-03 Tide.flac", "track.flac", with(map[string][]string{"GENRE": {"Rock"}}))
	r.write(dir+"/shelf.txt", []byte(`Music Shelf download 7
Source: Catalog
Kind: track
Source ID: t1
Title: Shoreline

Music Shelf download 8
Source: Catalog
Kind: album
Source ID: alb-9
Title: Low Tide Hymns
Artist: Northern Static
UPC: 0000000000017
Tracks:
  1  01 - Shoreline.flac  track 111  ISRC XX0000000001  FLAC 24/96  done
  2  02 - Joni Mitchell - Undertow.flac  track 112  ISRC XX0000000002  FLAC 24/96  done
  3  Tide  track 113  failed
`))
	r.walk()
	id := r.albums("").Albums[0].ID
	d := r.open(id)
	byName := map[string]int64{}
	for _, tr := range d.Tracks {
		byName[filepath.Base(tr.Path)] = tr.ID
	}
	s := r.suggest(id, "")

	one, two, three := byName["01 - Shoreline.flac"], byName["02 - Joni Mitchell - Undertow.flac"], byName["1-03 Tide.flac"]
	want := map[int64]map[string]string{
		one:   {"title": "Shoreline", "track": "1", "isrc": "XX0000000001"},
		two:   {"title": "Undertow", "track": "2", "isrc": "XX0000000002"},
		three: {"title": "Tide", "track": "3", "disc": "1", "artist": ""},
	}
	for tr, fields := range want {
		for f, v := range fields {
			got := find(s, tr, f)
			if v == "" {
				if got != nil {
					t.Errorf("track %d %s: %+v", tr, f, got)
				}
				continue
			}
			if len(got) == 0 || got[0].Values[0] != v {
				t.Errorf("track %d %s: %+v, want %q", tr, f, got, v)
			}
		}
	}
	if a := find(s, one, "title"); a[0].Source != srcFileName || a[0].Confidence != 45 {
		t.Errorf("title %+v", a)
	}
	// The folder and shelf.txt agree on the album.
	if a := find(s, 0, "album"); len(a) != 1 || a[0].Values[0] != "Low Tide Hymns" || !slices.Equal(a[0].Sources, []string{srcShelf, srcFolder}) || a[0].Confidence != 93 {
		t.Fatalf("album %+v", a)
	}
	if a := find(s, 0, "albumartist"); len(a) != 1 || a[0].Values[0] != "Northern Static" {
		t.Fatalf("album artist %+v", a)
	}
	// Two tracks say Folk and one Rock.
	g := find(s, 0, "genre")
	if len(g) != 2 || g[0].Values[0] != "Folk" || g[0].Confidence != 73 || g[1].Values[0] != "Rock" || g[1].Confidence != 57 {
		t.Fatalf("genre %+v", g)
	}
	// shelf.txt names the album in the catalog, so it is read, not searched.
	if cat.searches.Load() != 0 || cat.read != "alb-9" {
		t.Fatalf("catalog: %d searches, read %q", cat.searches.Load(), cat.read)
	}
}

func TestSuggestionsWhenServicesFail(t *testing.T) {
	r, sv, cat := suggestRig(t)
	sv.down = true
	cat.err = &source.Unavailable{Reason: "token expired"}
	id := r.blueAlbum(nil, nil)
	s := r.suggest(id, "")
	got := map[string]string{}
	for _, u := range s.Unavailable {
		got[u.Source] = u.Message
	}
	if got[srcMB] == "" || got["Catalog"] != "Catalog: token expired" || len(got) != 2 {
		t.Fatalf("unavailable %+v", s.Unavailable)
	}
	if s.Anchor != nil || len(s.Covers) != 0 {
		t.Fatalf("anchor %+v, covers %+v", s.Anchor, s.Covers)
	}
	// Nothing failed is stored, so the next opening asks again.
	var n int
	if err := r.m.store.db.R.Get(&n, `SELECT count(*) FROM library_lookups`); err != nil || n != 0 {
		t.Fatalf("%d lookups stored, %v", n, err)
	}
}

func TestRemoteCovers(t *testing.T) {
	r, sv, cat := suggestRig(t)
	id := r.blueAlbum(map[string][]string{"MUSICBRAINZ_ALBUMID": {blueID}}, map[string][]string{"MUSICBRAINZ_ALBUMID": {blueID}})
	cat.album.CoverURL = sv.srv.URL + "/missing.jpg"
	s := r.suggest(id, "")
	if len(s.Covers) != 2 {
		t.Fatalf("covers %+v", s.Covers)
	}
	caa, broken := s.Covers[0].Image, s.Covers[1].Image
	rec := r.do(http.MethodGet, "/api/library/remote/"+caa)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/jpeg" || !sv.asked("/img/release/"+blueID+"/2-500.jpg") {
		t.Fatalf("preview: %d %s", rec.Code, rec.Header())
	}
	rec = r.post("/api/library/remote/"+caa+"/cover", nil)
	info := decode[imageInfo](t, rec)
	if rec.Code != http.StatusCreated || info.Width != 40 || !sv.asked("/img/release/"+blueID+"/2.jpg") {
		t.Fatalf("cover: %d %s", rec.Code, rec.Body)
	}
	if _, err := r.m.covers.get(info.ID); err != nil {
		t.Fatal(err)
	}
	if rec := r.do(http.MethodGet, "/api/library/remote/"+broken); rec.Code != http.StatusBadGateway {
		t.Fatalf("broken: %d", rec.Code)
	}
	if rec := r.post("/api/library/remote/"+strings.Repeat("0", 32)+"/cover", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown: %d", rec.Code)
	}

	// Retention drops lookups and offers past their time.
	if _, err := r.m.store.db.W.Exec(`UPDATE library_lookups SET fetched_at = '2000-01-01T00:00:00.000Z'`); err != nil {
		t.Fatal(err)
	}
	if _, err := r.m.store.db.W.Exec(`UPDATE library_remote SET created_at = '2000-01-01T00:00:00.000Z'`); err != nil {
		t.Fatal(err)
	}
	if n, err := r.m.store.trimLookups(bg, time.Now()); err != nil || n == 0 {
		t.Fatalf("trimmed %d, %v", n, err)
	}
	if rec := r.do(http.MethodGet, "/api/library/remote/"+caa); rec.Code != http.StatusNotFound {
		t.Fatalf("after retention: %d", rec.Code)
	}
}

func TestSaveRefusesTwoReleases(t *testing.T) {
	r := newRig(t)
	id := r.blueAlbum(map[string][]string{"MUSICBRAINZ_ALBUMID": {blueID}}, map[string][]string{"MUSICBRAINZ_ALBUMID": {blueID}})
	d := r.open(id)
	save := func(tracks []trackEdits) (int, refusal) {
		rec := r.post("/api/library/albums/"+itoa(id)+"/save", saveBody{Tracks: tracks})
		return rec.Code, decode[refusal](t, rec)
	}
	code, ref := save(everyTrack(d,
		editJSON{Field: "barcode", Values: []string{"1"}, Anchor: blueID},
		editJSON{Field: "catalognumber", Values: []string{"2"}, Anchor: threeID}))
	if code != http.StatusUnprocessableEntity || !strings.Contains(ref.Error, "two MusicBrainz releases") {
		t.Fatalf("two anchors: %d %+v", code, ref)
	}
	code, ref = save(everyTrack(d, editJSON{Field: "barcode", Values: []string{"1"}, Anchor: "nope"}))
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("bad anchor: %d %+v", code, ref)
	}
	// The files keep another release's album ID.
	code, ref = save(everyTrack(d, editJSON{Field: "barcode", Values: []string{"1"}, Anchor: threeID}))
	if code != http.StatusUnprocessableEntity || ref.Track == 0 || !strings.Contains(ref.Error, "album ID") {
		t.Fatalf("other release: %d %+v", code, ref)
	}
	// Only one track moves to the release, so the other keeps the old ID.
	code, ref = save([]trackEdits{{ID: d.Tracks[0].ID, Revision: d.Tracks[0].Revision, Edits: []editJSON{
		{Field: "barcode", Values: []string{"1"}, Anchor: threeID},
		{Field: "musicbrainz_albumid", Values: []string{threeID}, Anchor: threeID}}}})
	if code != http.StatusUnprocessableEntity || ref.Track != d.Tracks[1].ID {
		t.Fatalf("half the album: %d %+v", code, ref)
	}
	// With the album ID, typed values beside them pass.
	rec := r.post("/api/library/albums/"+itoa(id)+"/save", saveBody{Tracks: everyTrack(d,
		editJSON{Field: "barcode", Values: []string{"1"}, Anchor: threeID},
		editJSON{Field: "musicbrainz_albumid", Values: []string{threeID}, Anchor: threeID},
		editJSON{Field: "comment", Values: []string{"typed"}}), Confirm: []string{ConfirmSplit, ConfirmMerge, ConfirmArtist}})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("one release: %d %s", rec.Code, rec.Body)
	}
}

func TestMerge(t *testing.T) {
	s := &subject{tracks: []*subjectTrack{{ID: 1}, {ID: 2}}}
	a := &anchor{ID: blueID, Matched: true}
	cands := []cand{
		{field: "recordlabel", values: []string{"Reprise"}, source: srcMB, p: 0.9, anchor: blueID},
		{field: "recordlabel", values: []string{"Reprise"}, source: "Catalog", p: 0.8},
		{field: "recordlabel", values: []string{"Reprise"}, source: "Catalog", p: 0.5},
		{field: "recordlabel", values: []string{"Asylum"}, source: srcShelf, p: 0.95},
		{field: "barcode", values: []string{"1"}, source: "Catalog", p: 0.8},
		{field: "genre", values: []string{"a"}, source: "x", p: 0.5},
		{field: "genre", values: []string{"b"}, source: "x", p: 0.6},
		{field: "genre", values: []string{"c"}, source: "x", p: 0.7},
		{field: "genre", values: []string{"d"}, source: "x", p: 0.8},
		{track: 2, field: "title", values: []string{"T"}, source: "x", p: 0.5},
	}
	got := map[string][]Suggestion{}
	for _, m := range merge(s, cands, a) {
		got[m.Field+itoa(m.track)] = append(got[m.Field+itoa(m.track)], m.Suggestion)
	}
	// 1 - 0.1*0.2 = 0.98: a source counts once, with its best p, and the
	// anchor's value is the only one a release field shows.
	if l := got["recordlabel0"]; len(l) != 1 || l[0].Confidence != 98 || l[0].Anchor != blueID {
		t.Fatalf("label %+v", l)
	}
	if got["barcode0"] != nil {
		t.Fatalf("a release field without the anchor's value: %+v", got["barcode0"])
	}
	if g := got["genre0"]; len(g) != 3 || g[0].Values[0] != "d" || g[2].Values[0] != "b" {
		t.Fatalf("genre %+v", g)
	}
	if got["title2"] == nil {
		t.Fatal("track suggestion lost")
	}
	// Without a matched anchor a release field keeps its best value, and
	// the catalog's barcode shows.
	n := 0
	for _, m := range merge(s, cands, &anchor{ID: blueID}) {
		switch m.Field {
		case "recordlabel":
			n++
		case "barcode":
			n += 10
		}
	}
	if n != 11 {
		t.Fatalf("unanchored: %d", n)
	}
	if percent(0.999) != 99 || percent(0) != 1 {
		t.Fatal("percent bounds")
	}
}

func TestParseNames(t *testing.T) {
	for name, want := range map[string][3]string{
		"01 - So What.flac":         {"", "1", "So What"},
		"01. So What.m4a":           {"", "1", "So What"},
		"1-02 Freddie.flac":         {"1", "2", "Freddie"},
		"2-10 - Blue in Green.flac": {"2", "10", "Blue in Green"},
		"03-Flamenco.flac":          {"", "3", "Flamenco"},
		"Untitled.flac":             {"", "", "Untitled"},
	} {
		tr := &subjectTrack{ID: 1, Path: "a/" + name}
		got := [3]string{}
		for _, c := range nameSuggestions(tr) {
			switch c.field {
			case "disc":
				got[0] = c.values[0]
			case "track":
				got[1] = c.values[0]
			case "title":
				got[2] = c.values[0]
			}
		}
		if got != want {
			t.Errorf("%s: %q", name, got)
		}
	}
	for folder, want := range map[string][2]string{
		"qobuz/Miles Davis - Kind of Blue (1959) [24B-192kHz]": {"Miles Davis", "Kind of Blue"},
		"qobuz/[INCOMPLETE] Miles Davis - Kind of Blue (1959)": {"Miles Davis", "Kind of Blue"},
		"apple-music/Miles Davis/Kind of Blue":                 {"Miles Davis", "Kind of Blue"},
		"jazz/[1959] Kind of Blue":                             {"", "Kind of Blue"},
	} {
		got := [2]string{}
		for _, c := range folderSuggestions(&subject{row: albumRow{Folder: folder}, tracks: []*subjectTrack{{}}}) {
			if c.field == "albumartist" {
				got[0] = c.values[0]
			} else {
				got[1] = c.values[0]
			}
		}
		if got != want {
			t.Errorf("%s: %q", folder, got)
		}
	}
}
