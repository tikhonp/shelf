//go:build contract

package library

// The Navidrome contract test: shelf's writes against a real Navidrome, in
// Docker, judged by what Navidrome shows. Production runs
// deluan/navidrome:latest, so its rules can change under shelf, and this
// run shows when they do. `make contract` runs it in Linux containers that
// share a volume. Docker Desktop's file sharing on macOS can show Navidrome
// stale mtimes, and its quick scan then misses writes.
//
// SHELF_CONTRACT_NAVIDROME and SHELF_CONTRACT_MUSIC name a fresh Navidrome
// and the music folder it reads. Without them the test starts Navidrome
// itself, which is right on a Linux host.

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// navidromeImage is the image the run uses. SHELF_CONTRACT_IMAGE picks
// another, such as a pinned release.
func navidromeImage() string {
	if v := os.Getenv("SHELF_CONTRACT_IMAGE"); v != "" {
		return v
	}
	return "deluan/navidrome:latest"
}

// navidrome is a scratch Navidrome reading a music folder.
type navidrome struct {
	t    *testing.T
	base string
}

const ndUser, ndPassword = "admin", "contract-test"

func startNavidrome(t *testing.T, music string) *navidrome {
	t.Helper()
	if base := os.Getenv("SHELF_CONTRACT_NAVIDROME"); base != "" {
		return newNavidrome(t, base)
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("no docker")
	}
	data := t.TempDir()
	out, err := exec.Command("docker", "run", "-d", "--rm", "-p", "127.0.0.1::4533",
		"-v", music+":/music:ro", "-v", data+":/data",
		"-e", "ND_SCANNER_SCHEDULE=0", "-e", "ND_SCANNER_WATCHERWAIT=0", "-e", "ND_LOGLEVEL=error",
		navidromeImage()).CombinedOutput()
	if err != nil {
		t.Fatalf("docker run: %v: %s", err, out)
	}
	id := strings.TrimSpace(string(out))
	t.Cleanup(func() {
		if t.Failed() {
			logs, _ := exec.Command("docker", "logs", "--tail", "50", id).CombinedOutput()
			t.Logf("navidrome log:\n%s", logs)
		}
		_ = exec.Command("docker", "rm", "-f", id).Run()
	})
	port, err := exec.Command("docker", "port", id, "4533/tcp").Output()
	if err != nil {
		t.Fatal(err)
	}
	return newNavidrome(t, "http://"+strings.TrimSpace(strings.Split(string(port), "\n")[0]))
}

// newNavidrome waits for a fresh Navidrome and creates its admin.
func newNavidrome(t *testing.T, base string) *navidrome {
	t.Helper()
	nd := &navidrome{t: t, base: base}
	deadline := time.Now().Add(60 * time.Second)
	for {
		if res, err := http.Get(nd.base + "/ping"); err == nil {
			_ = res.Body.Close()
			if res.StatusCode == http.StatusOK {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("navidrome didn't start")
		}
		time.Sleep(200 * time.Millisecond)
	}
	body, _ := json.Marshal(map[string]string{"username": ndUser, "password": ndPassword})
	res, err := http.Post(nd.base+"/auth/createAdmin", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("createAdmin: %d", res.StatusCode)
	}
	return nd
}

// call asks the Subsonic API and decodes the answer's body into out.
func (nd *navidrome) call(method string, params url.Values, out any) {
	nd.t.Helper()
	b := nd.raw(method, params)
	var env struct {
		Response json.RawMessage `json:"subsonic-response"`
	}
	if err := json.Unmarshal(b, &env); err != nil {
		nd.t.Fatalf("%s: %v: %s", method, err, b)
	}
	var st struct {
		Status string `json:"status"`
		Error  struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.Unmarshal(env.Response, &st)
	if st.Status != "ok" {
		nd.t.Fatalf("%s: %s", method, st.Error.Message)
	}
	if out != nil {
		if err := json.Unmarshal(env.Response, out); err != nil {
			nd.t.Fatalf("%s: %v", method, err)
		}
	}
}

func (nd *navidrome) raw(method string, params url.Values) []byte {
	nd.t.Helper()
	if params == nil {
		params = url.Values{}
	}
	salt := fmt.Sprint(time.Now().UnixNano())
	sum := md5.Sum([]byte(ndPassword + salt))
	params.Set("u", ndUser)
	params.Set("t", hex.EncodeToString(sum[:]))
	params.Set("s", salt)
	params.Set("v", "1.16.1")
	params.Set("c", "shelf-contract")
	params.Set("f", "json")
	res, err := http.Get(nd.base + "/rest/" + method + "?" + params.Encode())
	if err != nil {
		nd.t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		nd.t.Fatal(err)
	}
	return b
}

// scan runs a quick scan, as shelf asks for in production, and waits for
// it to end.
func (nd *navidrome) scan() {
	nd.t.Helper()
	var st struct {
		ScanStatus struct {
			Scanning bool   `json:"scanning"`
			LastScan string `json:"lastScan"`
		} `json:"scanStatus"`
	}
	nd.call("getScanStatus", nil, &st)
	before := st.ScanStatus.LastScan
	nd.call("startScan", nil, nil)
	deadline := time.Now().Add(60 * time.Second)
	for {
		nd.call("getScanStatus", nil, &st)
		if !st.ScanStatus.Scanning && st.ScanStatus.LastScan != before {
			return
		}
		if time.Now().After(deadline) {
			nd.t.Fatal("the scan didn't end")
		}
		time.Sleep(200 * time.Millisecond)
	}
}

type ndAlbum struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Year      int    `json:"year"`
	SongCount int    `json:"songCount"`
	Starred   string `json:"starred"`
	Song      []struct {
		ID         string `json:"id"`
		Title      string `json:"title"`
		Starred    string `json:"starred"`
		ReplayGain struct {
			TrackGain float64 `json:"trackGain"`
			AlbumGain float64 `json:"albumGain"`
			AlbumPeak float64 `json:"albumPeak"`
		} `json:"replayGain"`
	} `json:"song"`
}

func (nd *navidrome) albums() map[string]ndAlbum {
	nd.t.Helper()
	var l struct {
		AlbumList2 struct {
			Album []ndAlbum `json:"album"`
		} `json:"albumList2"`
	}
	nd.call("getAlbumList2", url.Values{"type": {"alphabeticalByName"}, "size": {"500"}}, &l)
	out := map[string]ndAlbum{}
	for _, a := range l.AlbumList2.Album {
		out[a.Name] = a
	}
	return out
}

// cover waits until Navidrome serves an album's cover at a width, and answers
// the width it last served. Its artwork catches up seconds after a scan ends,
// with the placeholder until then.
func (nd *navidrome) cover(id string, want int) int {
	nd.t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		w := 0
		if cfg, _, err := image.DecodeConfig(bytes.NewReader(nd.raw("getCoverArt", url.Values{"id": {id}}))); err == nil {
			w = cfg.Width
		}
		if w == want || time.Now().After(deadline) {
			return w
		}
		time.Sleep(250 * time.Millisecond)
	}
}

func (nd *navidrome) album(id string) ndAlbum {
	nd.t.Helper()
	var a struct {
		Album ndAlbum `json:"album"`
	}
	nd.call("getAlbum", url.Values{"id": {id}}, &a)
	return a.Album
}

func TestNavidromeContract(t *testing.T) {
	r := newRig(t)
	if dir := os.Getenv("SHELF_CONTRACT_MUSIC"); dir != "" {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
				t.Fatal(err)
			}
		}
		r.root = dir
		r.m = r.module(nil)
	}
	p1 := r.add("folk/Blue/01.flac", "track.flac", blue("1", nil))
	r.add("folk/Blue/02.flac", "track.flac", blue("2", nil))
	// Navidrome's legacy date rule reads DATE as the release date while
	// ORIGINALDATE is set and RELEASEDATE isn't.
	legacy := func(track string) map[string][]string {
		return tagged(map[string][]string{"ALBUM": {"Moanin'"}, "ARTIST": {"Art Blakey"}, "ALBUMARTIST": {"Art Blakey"},
			"ALBUMARTISTS": {"Art Blakey"}, "TITLE": {"Moanin' " + track}, "TRACKNUMBER": {track},
			"DATE": {"2006"}, "ORIGINALDATE": {"1958"}, "RELEASEDATE": nil})
	}
	r.add("jazz/Moanin/01.flac", "track.flac", legacy("1"))
	r.add("jazz/Moanin/02.flac", "track.flac", legacy("2"))
	for _, n := range []string{"1", "2"} {
		tags := legacy(n)
		tags["ALBUM"] = []string{"Somethin' Else"}
		r.add("jazz/Else/0"+n+".flac", "track.flac", tags)
	}
	r.write("folk/Blue/.shelf-tmp-0123456789abcdef", []byte("half a write"))
	// An album whose discs each have a cover, and two albums in one folder.
	disc := func(n string) map[string][]string {
		return blue("1", map[string][]string{"ALBUM": {"Discs"}, "DISCNUMBER": {n}})
	}
	r.add("folk/Discs/CD1/01.flac", "track.flac", disc("1"))
	r.add("folk/Discs/CD2/01.flac", "track.flac", disc("2"))
	r.write("folk/Discs/CD1/cover.jpg", jpegOf(t, 100, 100))
	r.write("folk/Discs/CD2/cover.jpg", jpegOf(t, 110, 110))
	r.add("singles/a.flac", "track.flac", blue("1", map[string][]string{"ALBUM": {"One"}}))
	r.add("singles/b.flac", "track.flac", blue("1", map[string][]string{"ALBUM": {"Two"}}))
	r.write("singles/cover.jpg", jpegOf(t, 120, 120))
	music, err := filepath.EvalSymlinks(r.root)
	if err != nil {
		t.Fatal(err)
	}
	nd := startNavidrome(t, music)
	nd.scan()
	r.walk()
	r.startJobs()

	// shelf computes the album IDs Navidrome does, and Navidrome skips
	// shelf's temp files.
	ours := map[string]albumJSON{}
	for _, a := range r.albums("").Albums {
		ours[a.Title] = a
	}
	check := func(when string) map[string]ndAlbum {
		t.Helper()
		theirs := nd.albums()
		if len(theirs) != len(ours) {
			t.Fatalf("%s: Navidrome has %d albums, shelf %d", when, len(theirs), len(ours))
		}
		for name, a := range theirs {
			// An album that lost a track in a quick scan keeps its old
			// songCount, so the songs are what count.
			songs := len(nd.album(a.ID).Song)
			if o, ok := ours[name]; !ok || o.NavidromeID != a.ID || o.Year != a.Year || o.Tracks != songs {
				t.Errorf("%s: %s is %+v with %d songs in Navidrome, %+v in shelf", when, name, a, songs, o)
			}
		}
		return theirs
	}
	reindex := func() {
		t.Helper()
		nd.scan()
		r.walk()
		ours = map[string]albumJSON{}
		for _, a := range r.albums("").Albums {
			ours[a.Title] = a
		}
	}
	nds := check("at start")
	blueND := nds["Blue"]
	nd.call("star", url.Values{"albumId": {blueND.ID}}, nil)
	song := nd.album(blueND.ID).Song[0]
	nd.call("star", url.Values{"id": {song.ID}}, nil)

	// A retag in place keeps the track's ID and star.
	blue := ours["Blue"]
	s := r.saved(blue.ID, saveBody{Tracks: everyTrack(r.open(blue.ID), editJSON{Field: "title", Values: []string{"All I Want"}})})
	if s.State != StateDone {
		t.Fatalf("retag %+v", s)
	}
	reindex()
	got := nd.album(blueND.ID)
	if got.Song[0].ID != song.ID || got.Song[0].Title != "All I Want" || got.Song[0].Starred == "" {
		t.Errorf("retagged track %+v", got.Song[0])
	}

	// A whole-album rename gives a new album ID, and the star follows it.
	s = r.saved(ours["Blue"].ID, saveBody{Tracks: everyTrack(r.open(ours["Blue"].ID), editJSON{Field: "album", Values: []string{"Blue (Remastered)"}})})
	reindex()
	nds = check("after a rename")
	if a := nds["Blue (Remastered)"]; a.ID == blueND.ID || a.Starred == "" {
		t.Errorf("renamed album %+v", a)
	}

	// Undo brings back the old ID, with the star.
	if u := r.reversed(KindUndo, s.ID); u.State != StateDone {
		t.Fatalf("undo %+v", u)
	}
	reindex()
	nds = check("after the undo")
	if a := nds["Blue"]; a.ID != blueND.ID || a.Starred == "" {
		t.Errorf("album after the undo %+v", a)
	}

	// Moving one track takes the star along, as the Save's warning says.
	d := r.open(ours["Blue"].ID)
	r.saved(ours["Blue"].ID, saveBody{Tracks: []trackEdits{{ID: d.Tracks[1].ID, Revision: d.Tracks[1].Revision,
		Edits: []editJSON{{Field: "album", Values: []string{"Blue Too"}}}}}, Confirm: []string{ConfirmSplit}})
	reindex()
	nds = check("after a split")

	if nds["Blue Too"].Starred == "" || nds["Blue"].Starred != "" {
		t.Errorf("after a split: Blue %q, Blue Too %q", nds["Blue"].Starred, nds["Blue Too"].Starred)
	}

	// The RELEASEDATE fix's pair keeps the year Navidrome shows.
	if y := nds["Moanin'"].Year; y != 1958 {
		t.Fatalf("Moanin' shows %d", y)
	}
	m := ours["Moanin'"]
	r.saved(m.ID, saveBody{Tracks: everyTrack(r.open(m.ID),
		editJSON{Field: "releasedate", Values: []string{"2006"}}, editJSON{Field: "recordingdate", Values: []string{"1958"}})})
	reindex()
	if y := check("after the date fix")["Moanin'"].Year; y != 1958 {
		t.Errorf("Moanin' shows %d after the date fix", y)
	}

	// A new cover is what Navidrome serves for the album. The split left
	// Blue Too in the folder, so the Save asks first.
	up := r.upload(jpegOf(t, 64, 64))
	b := ours["Blue"]
	r.saved(b.ID, saveBody{Tracks: everyTrack(r.open(b.ID)), Confirm: []string{ConfirmSharedCover}, Cover: &coverEdit{Upload: up.ID}})
	reindex()
	if w := nd.cover("al-"+ours["Blue"].NavidromeID, 64); w != 64 {
		t.Errorf("Navidrome serves a %d wide cover", w)
	}

	// Navidrome and shelf agree on the cover of an album with disc folders.
	shows := func(name string) []byte {
		t.Helper()
		return nd.raw("getCoverArt", url.Values{"id": {"al-" + ours[name].NavidromeID}})
	}
	placeholder := shows("Moanin'")
	if w := nd.cover("al-"+ours["Discs"].NavidromeID, 100); w != 100 {
		t.Errorf("Navidrome shows a %d wide cover for Discs", w)
	}
	if !bytes.Equal(shows("Discs"), r.shown(ours["Discs"].ID)) {
		t.Errorf("shelf and Navidrome show different covers for Discs")
	}
	// A new cover in the release folder beats the discs' own.
	up = r.upload(jpegOf(t, 72, 72))
	discs := ours["Discs"]
	r.saved(discs.ID, saveBody{Tracks: everyTrack(r.open(discs.ID)), Cover: &coverEdit{Upload: up.ID}})
	reindex()
	if w := nd.cover("al-"+ours["Discs"].NavidromeID, 72); w != 72 {
		t.Errorf("Navidrome shows a %d wide cover after a replace", w)
	}
	// Each disc keeps its own art through the replace.
	if w := nd.cover("dc-"+ours["Discs"].NavidromeID+":2", 110); w != 110 {
		t.Errorf("Navidrome shows a %d wide cover for disc 2", w)
	}
	// Delete leaves Navidrome nothing to fall back to.
	discs = ours["Discs"]
	r.saved(discs.ID, saveBody{Tracks: everyTrack(r.open(discs.ID)), Cover: &coverEdit{Delete: true}})
	reindex()
	// The placeholder is WebP, which has no width here.
	if w := nd.cover("al-"+ours["Discs"].NavidromeID, 0); w != 0 {
		t.Errorf("Navidrome still shows a %d wide cover for Discs after a Delete", w)
	}
	if !bytes.Equal(shows("Discs"), placeholder) {
		t.Error("Navidrome still shows a cover for Discs after a Delete")
	}

	// A folder's cover reaches every album in the folder.
	one := ours["One"]
	r.saved(one.ID, saveBody{Tracks: everyTrack(r.open(one.ID)), Confirm: []string{ConfirmSharedCover}, Cover: &coverEdit{Upload: up.ID}})
	reindex()
	if w := nd.cover("al-"+ours["Two"].NavidromeID, 72); w != 72 {
		t.Errorf("Navidrome shows a %d wide cover for the album sharing the folder", w)
	}

	// A run's date fix keeps the year and the star, and Navidrome plays by the
	// run's ReplayGain.
	name := "Somethin' Else"
	before := ours[name]
	nd.call("star", url.Values{"albumId": {before.NavidromeID}}, nil)
	run := r.startRun(runBody{Selection: selection{Albums: []int64{before.ID}}, Fixes: []string{string(NoReleaseDate), FixReplayGain}})
	r.drain()
	if got := r.run(run.ID); got.State != StateDone || got.Files != 2 {
		t.Fatalf("run %+v", got)
	}
	reindex()
	a := check("after a run")[name]
	if a.Year != 1958 || a.Starred == "" {
		t.Errorf("after a run %+v", a)
	}
	tags := tagsOf(t, filepath.Join(r.root, "jazz", "Else", "01.flac"))
	want, err := strconv.ParseFloat(strings.Fields(tags["REPLAYGAIN_ALBUM_GAIN"][0])[0], 64)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range nd.album(a.ID).Song {
		g := s.ReplayGain
		// The fixture's -6 dB track gain is gone, and the album's is the run's.
		if g.AlbumGain != want || g.AlbumPeak == 0 || g.TrackGain == -6 || g.TrackGain == 0 {
			t.Errorf("after a run Navidrome has %+v, the file %v", g, tags)
		}
	}

	// Its undo is one step, and the old gain comes back.
	if rec := r.post("/api/library/changes/"+itoa(run.ID)+"/undo", nil); rec.Code != http.StatusAccepted {
		t.Fatalf("undo: %d %s", rec.Code, rec.Body)
	}
	r.drain()
	reindex()
	a = check("after a run's undo")[name]
	if a.ID != before.NavidromeID || a.Year != 1958 || a.Starred == "" {
		t.Errorf("after a run's undo %+v", a)
	}
	for _, s := range nd.album(a.ID).Song {
		if g := s.ReplayGain; g.TrackGain != -6 || g.AlbumGain != 0 {
			t.Errorf("after a run's undo Navidrome has %+v", g)
		}
	}

	if _, err := os.Stat(p1); err != nil {
		t.Fatal(err)
	}
}
