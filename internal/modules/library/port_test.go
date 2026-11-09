package library

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"hash/crc32"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/tikhonp/shelf/internal/libraryport"
	"github.com/tikhonp/shelf/internal/modules/library/acoustid"
	"github.com/tikhonp/shelf/internal/platform/db"
	"github.com/tikhonp/shelf/internal/platform/identity"
	"go.senan.xyz/taglib"
)

// alice is a reviewer's request context.
var alice = identity.With(bg, identity.Account(5, "Alice"))

// accept enqueues a plan in its own transaction, as the agents module
// does, and runs after once it commits.
func (r *rig) accept(ctx context.Context, p libraryport.Plan, origin string) (int64, error) {
	r.t.Helper()
	var id int64
	var after func()
	err := r.m.store.db.Write(ctx, func(tx *sqlx.Tx) error {
		var err error
		id, after, err = r.m.Port().Enqueue(ctx, tx, p, origin)
		return err
	})
	if err == nil {
		after()
	}
	return id, err
}

func (r *rig) plan(req libraryport.PlanRequest) libraryport.Plan {
	r.t.Helper()
	p, err := r.m.Port().Plan(bg, req)
	if err != nil {
		r.t.Fatal(err)
	}
	return p
}

func portTrackIDs(t *testing.T, r *rig, album int64) []int64 {
	t.Helper()
	a, err := r.m.Port().Album(bg, album)
	if err != nil {
		t.Fatal(err)
	}
	var out []int64
	for _, tr := range a.Tracks {
		out = append(out, tr.ID)
	}
	return out
}

func genre(id int64, values ...string) libraryport.PlanFile {
	return libraryport.PlanFile{TrackID: id, Edits: []libraryport.Edit{{Field: "genre", Values: values}}}
}

func TestPortPlanAndEnqueue(t *testing.T) {
	r := newRig(t)
	p1 := r.add("folk/Blue/01.flac", "track.flac", blue("1", map[string][]string{"GENRE": {"Folk"}}))
	r.add("folk/Blue/02.flac", "track.flac", blue("2", map[string][]string{"GENRE": {"Folk"}}))
	r.walk()
	r.startJobs()
	album := r.albums("").Albums[0].ID
	ids := portTrackIDs(t, r, album)
	before := tagsOf(t, p1)

	p := r.plan(libraryport.PlanRequest{Files: []libraryport.PlanFile{genre(ids[0], "Singer-Songwriter")}})
	if p.Refusal != nil || p.AlbumID != album || len(p.Missing) != 0 {
		t.Fatalf("plan %+v", p)
	}
	pv := p.Preview
	if len(pv.Files) != 1 || pv.Files[0].Path != "folk/Blue/01.flac" || pv.Files[0].TrackID != ids[0] ||
		len(pv.Files[0].Keys) != 1 || pv.Files[0].Keys[0].Key != "GENRE" ||
		!slices.Equal(pv.Files[0].Keys[0].Before, []string{"Folk"}) || !slices.Equal(pv.Files[0].Keys[0].After, []string{"Singer-Songwriter"}) ||
		pv.Album.ID != album || len(pv.Digest) != 64 || len(pv.Warnings) != 0 || len(pv.Folder) != 0 {
		t.Fatalf("preview %+v", pv)
	}
	if !slices.Equal(tagsOf(t, p1)["GENRE"], before["GENRE"]) {
		t.Fatal("Plan wrote a tag")
	}

	// A rolled-back transaction leaves no change and no job.
	boom := errors.New("boom")
	err := r.m.store.db.Write(alice, func(tx *sqlx.Tx) error {
		if _, _, err := r.m.Port().Enqueue(alice, tx, p, "proposal:1/acceptance:1"); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatal(err)
	}
	var n int
	if err := r.m.store.db.R.Get(&n, `SELECT (SELECT count(*) FROM library_changes) + (SELECT count(*) FROM jobs WHERE type = ?)`, WriteJob); err != nil || n != 0 {
		t.Fatalf("a rolled back enqueue left %d rows: %v", n, err)
	}

	id, err := r.accept(alice, p, "proposal:1/acceptance:1")
	if err != nil {
		t.Fatal(err)
	}
	r.drain()
	c := r.change(id)
	if c.State != StateDone || c.Author != "Alice" || c.AuthorID == nil || *c.AuthorID != "user:5" ||
		c.Origin == nil || *c.Origin != "proposal:1/acceptance:1" {
		t.Fatalf("change %+v", c)
	}
	if got := tagsOf(t, p1)["GENRE"]; !slices.Equal(got, []string{"Singer-Songwriter"}) {
		t.Fatalf("GENRE %v", got)
	}
	// The same origin never makes a second change.
	if _, err := r.accept(alice, p, "proposal:1/acceptance:1"); !errors.Is(err, libraryport.ErrOriginTaken) {
		t.Fatalf("second enqueue: %v", err)
	}
	st, err := r.m.Port().Changes(bg, []int64{id, 999})
	if err != nil || len(st) != 1 || st[id].State != StateDone || st[id].Written != 1 || st[id].Files != 1 || st[id].FinishedAt == nil {
		t.Fatalf("changes %+v %v", st, err)
	}
	// A refused or unconfirmed plan never enqueues.
	if _, err := r.accept(alice, libraryport.Plan{Refusal: &libraryport.Refusal{Error: "no"}}, ""); !errors.Is(err, libraryport.ErrRefused) {
		t.Fatal(err)
	}
	if _, err := r.accept(alice, libraryport.Plan{Missing: []string{ConfirmSplit}, Stored: p.Stored}, ""); !errors.Is(err, libraryport.ErrMissingConfirm) {
		t.Fatal(err)
	}
}

// An unrelated write between preview and acceptance leaves the digest
// alone, and the change goes out against the new revisions. A write to a
// reviewed key changes it.
func TestPortDigestFollowsOnlyTheReviewedEffects(t *testing.T) {
	r := newRig(t)
	p1 := r.add("folk/Blue/01.flac", "track.flac", blue("1", map[string][]string{"GENRE": {"Folk"}}))
	r.add("folk/Blue/02.flac", "track.flac", blue("2", map[string][]string{"GENRE": {"Folk"}}))
	r.walk()
	r.startJobs()
	album := r.albums("").Albums[0].ID
	ids := portTrackIDs(t, r, album)
	req := libraryport.PlanRequest{Files: []libraryport.PlanFile{genre(ids[0], "Singer-Songwriter")}}
	first := r.plan(req)

	tags := tagsOf(t, p1)
	tags["MOOD"] = []string{"calm"}
	if err := taglib.WriteTags(p1, tags, taglib.Clear); err != nil {
		t.Fatal(err)
	}
	second := r.plan(req)
	if second.Preview.Digest != first.Preview.Digest {
		t.Fatalf("an unrelated key changed the digest: %+v", second.Preview)
	}
	if bytes.Equal(second.Stored, first.Stored) {
		t.Fatal("the plan kept the old revision")
	}
	id, err := r.accept(alice, second, "proposal:1/acceptance:1")
	if err != nil {
		t.Fatal(err)
	}
	r.drain()
	if c := r.change(id); c.State != StateDone {
		t.Fatalf("change %+v", c)
	}
	if got := tagsOf(t, p1); !slices.Equal(got["GENRE"], []string{"Singer-Songwriter"}) || !slices.Equal(got["MOOD"], []string{"calm"}) {
		t.Fatalf("tags %v", got)
	}

	// The reviewed key's own value moved: the effect differs.
	tags = tagsOf(t, p1)
	tags["GENRE"] = []string{"Rock"}
	if err := taglib.WriteTags(p1, tags, taglib.Clear); err != nil {
		t.Fatal(err)
	}
	third := r.plan(req)
	if third.Preview.Digest == first.Preview.Digest {
		t.Fatal("a changed before value kept the digest")
	}
	// An old plan's revision catches a write after it, at the job.
	stale := r.plan(libraryport.PlanRequest{Files: []libraryport.PlanFile{genre(ids[0], "Jazz")}})
	tags["GENRE"] = []string{"Blues"}
	if err := taglib.WriteTags(p1, tags, taglib.Clear); err != nil {
		t.Fatal(err)
	}
	id, err = r.accept(alice, stale, "proposal:1/acceptance:2")
	if err != nil {
		t.Fatal(err)
	}
	r.drain()
	if c := r.change(id); c.State != StateFailed || c.Reason != ReasonConflict {
		t.Fatalf("stale change %+v", c)
	}
}

func TestPortPlanRefusalsAndWarnings(t *testing.T) {
	r := newRig(t)
	r.add("folk/Blue/01.flac", "track.flac", blue("1", nil))
	r.add("folk/Blue/02.flac", "track.flac", blue("2", nil))
	r.add("folk/Court/01.flac", "track.flac", blue("1", map[string][]string{"ALBUM": {"Court and Spark"}}))
	r.add("pop/M4A/01.m4a", "track.m4a", map[string][]string{"TITLE": {"T"}, "ALBUM": {"M"}, "ARTIST": {"A"}, "ALBUMARTIST": {"A"}, "TRACKNUMBER": {"1"}, "DATE": {"2000"}})
	r.walk()
	var blueID, courtID, m4aID int64
	for _, a := range r.albums("").Albums {
		switch a.Title {
		case "Blue":
			blueID = a.ID
		case "Court and Spark":
			courtID = a.ID
		case "M":
			m4aID = a.ID
		}
	}
	blueTracks, courtTracks, m4aTracks := portTrackIDs(t, r, blueID), portTrackIDs(t, r, courtID), portTrackIDs(t, r, m4aID)

	refusal := func(name string, req libraryport.PlanRequest, reason string) {
		t.Helper()
		p := r.plan(req)
		if p.Refusal == nil || p.Refusal.Reason != reason {
			t.Errorf("%s: %+v", name, p)
		}
	}
	refusal("no tracks", libraryport.PlanRequest{}, ReasonRefused)
	refusal("two albums", libraryport.PlanRequest{Files: []libraryport.PlanFile{genre(blueTracks[0], "X"), genre(courtTracks[0], "X")}}, "regrouped")
	refusal("unknown track", libraryport.PlanRequest{Files: []libraryport.PlanFile{genre(9999, "X")}}, "regrouped")
	refusal("nothing changes", libraryport.PlanRequest{Files: []libraryport.PlanFile{{TrackID: blueTracks[0], Edits: []libraryport.Edit{{Field: "album", Values: []string{"Blue"}}}}}}, ReasonRefused)
	refusal("bad anchor", libraryport.PlanRequest{Files: []libraryport.PlanFile{{TrackID: blueTracks[0],
		Edits: []libraryport.Edit{{Field: "genre", Values: []string{"X"}, Anchor: "nope"}}}}}, ReasonRefused)
	// M4A keeps release and recording dates in one DATE, so two values
	// for them in one plan are refused, as a Save is.
	refusal("m4a dates", libraryport.PlanRequest{Files: []libraryport.PlanFile{
		{TrackID: m4aTracks[0], Edits: []libraryport.Edit{{Field: "releasedate", Values: []string{"2001"}}}},
		{TrackID: m4aTracks[0], Edits: []libraryport.Edit{{Field: "recordingdate", Values: []string{"2002"}}}},
	}}, ReasonRefused)
	refusal("unknown cover", libraryport.PlanRequest{Files: []libraryport.PlanFile{{TrackID: blueTracks[0]}},
		Cover: &libraryport.PlanCover{Image: strings.Repeat("a", 64)}}, ReasonRefused)

	// Moving one track to another album asks for confirmations.
	move := libraryport.PlanRequest{Files: []libraryport.PlanFile{{TrackID: blueTracks[0],
		Edits: []libraryport.Edit{{Field: "album", Values: []string{"Court and Spark"}}}}}}
	p := r.plan(move)
	if p.Refusal != nil || !slices.Equal(p.Missing, []string{ConfirmMerge, ConfirmSplit}) || len(p.Preview.Warnings) != 2 ||
		p.Preview.Warnings[0].Kind != ConfirmMerge || p.Preview.Warnings[1].Kind != ConfirmSplit {
		t.Fatalf("warnings %+v", p)
	}
	if _, err := r.accept(alice, p, "proposal:2/acceptance:1"); !errors.Is(err, libraryport.ErrMissingConfirm) {
		t.Fatal(err)
	}
	move.Confirm = []string{ConfirmSplit, ConfirmMerge}
	if p := r.plan(move); len(p.Missing) != 0 || p.Preview.Digest == "" {
		t.Fatalf("confirmed %+v", p)
	}
	// Two operations on one track go out as one file.
	two := r.plan(libraryport.PlanRequest{Files: []libraryport.PlanFile{genre(blueTracks[0], "X"),
		{TrackID: blueTracks[0], Edits: []libraryport.Edit{{Key: "MOOD", Values: []string{"calm"}}}}}})
	if two.Refusal != nil || len(two.Preview.Files) != 1 || len(two.Preview.Files[0].Keys) != 2 {
		t.Fatalf("two ops %+v", two)
	}
}

func TestPortCoversPinsAndRetention(t *testing.T) {
	r := newRig(t)
	p1 := r.add("folk/Blue/01.flac", "track.flac", blue("1", nil))
	r.add("folk/Blue/02.flac", "track.flac", blue("2", nil))
	old := jpegOf(t, 50, 50)
	embedCover(t, p1, old)
	r.walk()
	r.startJobs()
	port := r.m.Port()
	album := r.albums("").Albums[0].ID

	a, err := port.Album(bg, album)
	if err != nil || len(a.Tracks) != 2 || !a.Tracks[0].HasCover || a.Tracks[1].HasCover || a.Tracks[0].Revision == "" {
		t.Fatalf("album %+v %v", a, err)
	}
	for _, is := range a.Issues {
		if is.Kind == string(BadCover) {
			t.Fatal("Album ran ffmpeg")
		}
	}
	if b, mime, err := port.AlbumCover(bg, album); err != nil || mime != "image/jpeg" || !bytes.Equal(b, old) {
		t.Fatalf("album cover %s %v", mime, err)
	}

	if _, err := port.PutCover(bg, []byte("not an image")); !errors.Is(err, libraryport.ErrRefused) {
		t.Fatal(err)
	}
	up := jpegOf(t, 300, 300)
	img, err := port.PutCover(bg, up)
	if err != nil || img.MIME != "image/jpeg" || img.Width != 300 || img.ID != hashOf(up) {
		t.Fatalf("put %+v %v", img, err)
	}
	if _, err := port.CoverInfo(bg, strings.Repeat("b", 64)); !errors.Is(err, libraryport.ErrNotFound) {
		t.Fatal(err)
	}
	if b, err := port.CoverBytes(bg, img.ID); err != nil || !bytes.Equal(b, up) {
		t.Fatal(err)
	}

	tracks := portTrackIDs(t, r, album)
	req := libraryport.PlanRequest{Files: []libraryport.PlanFile{{TrackID: tracks[0]}}, Cover: &libraryport.PlanCover{Image: img.ID}}
	p := r.plan(req)
	if p.Refusal != nil {
		t.Fatalf("cover plan %+v", p.Refusal)
	}
	pv := p.Preview
	if len(pv.Files) != 1 || pv.Files[0].CoverBefore != hashOf(old) || pv.Files[0].CoverAfter == "" ||
		len(pv.Folder) != 1 || pv.Folder[0].Path != "folk/Blue/cover.jpg" || pv.Folder[0].Before != "" || pv.Folder[0].After != img.ID {
		t.Fatalf("cover preview %+v", pv)
	}

	if b, err := port.CoverBytes(bg, pv.Files[0].CoverBefore); err != nil || !bytes.Equal(b, old) {
		t.Fatal("before artwork must be available during review, before acceptance", err)
	}

	// Pin and Unpin run in the caller's transaction and read no file.
	other := jpegOf(t, 20, 20)
	if _, err := r.m.covers.put(other); err != nil {
		t.Fatal(err)
	}
	pin := func(holder string, ids ...string) error {
		return r.m.store.db.Write(bg, func(tx *sqlx.Tx) error { return port.Pin(bg, tx, holder, ids) })
	}
	if err := pin("agents:proposal:1", hashOf(other)); !errors.Is(err, libraryport.ErrNotFound) {
		t.Fatalf("an unchecked cover was pinned: %v", err)
	}
	if err := pin("agents:proposal:1", "../../etc"); !errors.Is(err, libraryport.ErrNotFound) {
		t.Fatal(err)
	}
	if err := pin("agents:preview:1", pv.Files[0].CoverBefore); err != nil {
		t.Fatal("pinning the recorded before artwork", err)
	}

	if err := pin("agents:proposal:1", img.ID); err != nil {
		t.Fatal(err)
	}
	if err := pin("agents:proposal:1", img.ID); err != nil {
		t.Fatal("pinning twice", err)
	}
	// A rolled back pin leaves nothing.
	_ = r.m.store.db.Write(bg, func(tx *sqlx.Tx) error {
		if err := port.Pin(bg, tx, "agents:proposal:2", []string{img.ID}); err != nil {
			t.Fatal(err)
		}
		return errors.New("roll back")
	})
	var n int
	if err := r.m.store.db.R.Get(&n, `SELECT count(*) FROM library_cover_pins WHERE holder = 'agents:proposal:2'`); err != nil || n != 0 {
		t.Fatalf("%d pins after a rollback: %v", n, err)
	}

	// Retention keeps a pinned upload past its day, and sweeps it once
	// the holder lets go.
	age := func(id string) {
		if err := os.Chtimes(filepath.Join(r.m.covers.dir, id+".jpg"), time.Now(), time.Now().Add(-48*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	age(img.ID)
	r.retain()
	if _, err := r.m.covers.get(img.ID); err != nil {
		t.Fatal("retention swept a pinned cover", err)
	}
	// Restart: the pin itself lets a later revision pin it again.
	r.m.mu.Lock()
	r.m.checkedCovers = nil
	r.m.mu.Unlock()
	if err := pin("agents:proposal:3", img.ID); err != nil {
		t.Fatal("re-pinning a pinned cover", err)
	}
	for _, h := range []string{"agents:proposal:1", "agents:proposal:3"} {
		if err := r.m.store.db.Write(bg, func(tx *sqlx.Tx) error { return port.Unpin(bg, tx, h) }); err != nil {
			t.Fatal(err)
		}
	}
	age(img.ID)
	r.retain()
	if _, err := r.m.covers.get(img.ID); !errors.Is(err, errNotFound) {
		t.Fatal("an unpinned old upload stayed", err)
	}

	// The accepted cover goes out through the ordinary write.
	img, err = port.PutCover(bg, up)
	if err != nil {
		t.Fatal(err)
	}
	p = r.plan(req)
	id, err := r.accept(alice, p, "proposal:4/acceptance:1")
	if err != nil {
		t.Fatal(err)
	}
	r.drain()
	if c := r.change(id); c.State != StateDone || !c.Cover {
		t.Fatalf("cover change %+v", c)
	}
	if b, err := os.ReadFile(filepath.Join(r.root, "folk/Blue/cover.jpg")); err != nil || !bytes.Equal(b, up) {
		t.Fatal("cover.jpg", err)
	}
}

// retain runs the retention step.
func (r *rig) retain() {
	r.t.Helper()
	if _, err := r.m.store.trim(bg, HistoryLimit, time.Now()); err != nil {
		r.t.Fatal(err)
	}
	used, err := r.m.store.coverIDs(bg)
	if err != nil {
		r.t.Fatal(err)
	}
	if _, err := r.m.covers.sweep(used, time.Now()); err != nil {
		r.t.Fatal(err)
	}
}

func TestPortHistoryAfterTrim(t *testing.T) {
	r := newRig(t)
	r.add("folk/Blue/01.flac", "track.flac", blue("1", nil))
	r.walk()
	r.startJobs()
	port := r.m.Port()
	album := r.albums("").Albums[0].ID
	tracks := portTrackIDs(t, r, album)
	var ids []int64
	for i, g := range []string{"A", "B", "C"} {
		req := libraryport.PlanRequest{Files: []libraryport.PlanFile{genre(tracks[0], g)}}
		if i == 2 {
			req.Files[0].Edits = []libraryport.Edit{{Key: "MOOD", Values: []string{g}}}
		}
		p := r.plan(req)
		origin := ""
		if i == 2 {
			origin = "proposal:9/acceptance:3"
		}
		id, err := r.accept(alice, p, origin)
		if err != nil {
			t.Fatal(err)
		}
		r.drain()
		ids = append(ids, id)
	}
	undo := r.reversed(KindUndo, ids[0+1])
	page, err := port.History(bg, 0, 2)
	if err != nil || len(page.Changes) != 2 || page.Next != ids[1] || page.TrimmedThrough != 0 {
		t.Fatalf("page %+v %v", page, err)
	}
	if c := page.Changes[0]; c.Kind != KindSave || c.Author != "Alice" || c.AuthorID != "user:5" || len(c.Files) != 1 ||
		c.Files[0].Keys[0].Key != "GENRE" || c.Files[0].TrackID != tracks[0] || c.Origin != "" {
		t.Fatalf("first %+v", c)
	}
	page, err = port.History(bg, page.Next, 50)
	if err != nil || len(page.Changes) != 2 || page.Changes[0].Origin != "proposal:9/acceptance:3" ||
		page.Changes[1].ID != undo.ID || page.Changes[1].Kind != KindUndo || *page.Changes[1].Target != ids[1] {
		t.Fatalf("second page %+v %v", page, err)
	}
	done, err := port.History(bg, page.Next, 50)
	if err != nil || len(done.Changes) != 0 || done.Next != page.Next {
		t.Fatalf("end %+v %v", done, err)
	}

	// Keeping two saves removes the first, and keeping one removes the
	// second with its undo, so the marker moves past the undo.
	if _, err := r.m.store.trim(bg, 2, time.Now()); err != nil {
		t.Fatal(err)
	}
	if page, _ := port.History(bg, 0, 50); page.TrimmedThrough != ids[0] || page.Changes[0].ID != ids[1] {
		t.Fatalf("after trim %+v", page)
	}
	if _, err := r.m.store.trim(bg, 1, time.Now()); err != nil {
		t.Fatal(err)
	}
	page, _ = port.History(bg, 0, 50)
	if page.TrimmedThrough != undo.ID || len(page.Changes) != 1 || page.Changes[0].ID != ids[2] {
		t.Fatalf("after second trim %+v", page)
	}
	if st, _ := port.Changes(bg, []int64{ids[0]}); len(st) != 0 {
		t.Fatal("a trimmed change still reports")
	}
	// A Clear right after a proposal's change ends keeps it, so the agents
	// module can still read its outcome.
	if _, err := r.m.store.trim(bg, 0, time.Now()); err != nil {
		t.Fatal(err)
	}
	if st, _ := port.Changes(bg, []int64{ids[2]}); st[ids[2]].State != StateDone {
		t.Fatalf("a Clear in the grace removed the proposal's change: %+v", st)
	}
	// Once the grace has passed, Clear removes the rest and the marker never moves back.
	if _, err := r.m.store.trim(bg, 0, time.Now().Add(originGrace+time.Minute)); err != nil {
		t.Fatal(err)
	}
	if page, _ := port.History(bg, 0, 50); page.TrimmedThrough != undo.ID || len(page.Changes) != 0 {
		t.Fatalf("after clear %+v", page)
	}
	if _, err := r.m.store.trim(bg, 0, time.Now()); err != nil {
		t.Fatal(err)
	}
	if got, _ := r.m.store.trimmedThrough(bg, r.m.store.db.R); got != undo.ID {
		t.Fatalf("marker %d", got)
	}
}

func TestPortReads(t *testing.T) {
	r, _, _ := suggestRig(t)
	r.add("folk/Blue/01.flac", "track.flac", blue("1", nil))
	r.add("folk/Blue/02.flac", "track.flac", blue("2", nil))
	r.walk()
	port := r.m.Port()
	page, err := port.Albums(bg, libraryport.AlbumQuery{Search: "blue"})
	if err != nil || page.Total != 1 || len(page.Albums) != 1 || page.Albums[0].Title != "Blue" || page.Albums[0].Issues == nil {
		t.Fatalf("albums %+v %v", page, err)
	}
	for _, q := range []libraryport.AlbumQuery{{Limit: 101}, {Filter: "nope"}, {Sort: "size"}, {Source: "nope"}, {Offset: -1}} {
		if _, err := port.Albums(bg, q); !errors.Is(err, libraryport.ErrRefused) {
			t.Errorf("%+v: %v", q, err)
		}
	}
	if _, err := port.Album(bg, 999); !errors.Is(err, libraryport.ErrNotFound) {
		t.Fatal(err)
	}
	of, err := port.AlbumsOf(bg, []string{"folk/Blue/01.flac", "gone.flac"})
	if err != nil || of["folk/Blue/01.flac"] != page.Albums[0].ID || of["gone.flac"] != 0 {
		t.Fatalf("albums of %v %v", of, err)
	}
	a, _ := port.Album(bg, page.Albums[0].ID)
	var std, ext bool
	for _, tg := range a.Tracks[0].Tags {
		if tg.Key == "GENRE" || tg.Key == "ALBUM" {
			std = std || !tg.Extended && len(tg.Fields) > 0
		}
		if tg.Key == "REPLAYGAIN_TRACK_GAIN" || tg.Extended {
			ext = true
		}
	}
	if !std || !ext {
		t.Fatalf("tags %+v", a.Tracks[0].Tags)
	}

	// Suggestions never ask for a fingerprint, local or not.
	r.m.Prints = acoustid.New("key", "test")
	r.m.Prints.Base = "http://127.0.0.1:1"
	for _, external := range []bool{false, true} {
		raw, err := port.Suggestions(bg, page.Albums[0].ID, external)
		if err != nil {
			t.Fatal(err)
		}
		var s suggestionsJSON
		if err := json.Unmarshal(raw, &s); err != nil || s.Fingerprinting {
			t.Fatalf("suggestions %s %v", raw, err)
		}
	}
	var n int
	if err := r.m.store.db.R.Get(&n, `SELECT count(*) FROM jobs WHERE type = ?`, FingerprintJob); err != nil || n != 0 {
		t.Fatalf("%d fingerprint jobs: %v", n, err)
	}
	if _, err := port.Suggestions(bg, 999, false); !errors.Is(err, libraryport.ErrNotFound) {
		t.Fatal(err)
	}
	if _, _, err := port.AlbumCover(bg, page.Albums[0].ID); !errors.Is(err, libraryport.ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := port.MaterializeOffer(bg, "nope"); !errors.Is(err, libraryport.ErrNotFound) {
		t.Fatal(err)
	}
}

func TestPortRejectsTruncatedArtwork(t *testing.T) {
	r := newRig(t)
	b := pngOf(t, 32, 32)
	// The PNG header remains readable after truncation, but the complete
	// image is invalid. An external upload must not pass on its header alone.
	if _, err := r.m.Port().PutCover(bg, b[:len(b)-10]); !errors.Is(err, libraryport.ErrRefused) {
		t.Fatalf("truncated artwork accepted: %v", err)
	}
}

func TestPortRejectsOversizedArtworkHeader(t *testing.T) {
	r := newRig(t)
	b := pngOf(t, 1, 1)
	binary.BigEndian.PutUint32(b[16:20], 20000)
	binary.BigEndian.PutUint32(b[29:33], crc32.ChecksumIEEE(b[12:29]))
	if _, err := inspect(b); err != nil {
		t.Fatalf("test needs a readable header: %v", err)
	}
	if _, err := r.m.Port().PutCover(bg, b); !errors.Is(err, libraryport.ErrRefused) || !strings.Contains(err.Error(), "40 million") {
		t.Fatalf("oversized image header was not bounded: %v", err)
	}
}

// An undo of a proposal's change restarts its grace, so a Clear right after
// the undo can't hide that the change was undone.
func TestPortTrimKeepsProposalChangeAfterRecentUndo(t *testing.T) {
	r := newRig(t)
	r.add("folk/Blue/01.flac", "track.flac", blue("1", nil))
	r.walk()
	r.startJobs()
	tracks := portTrackIDs(t, r, r.albums("").Albums[0].ID)
	id, err := r.accept(bg, r.plan(libraryport.PlanRequest{Files: []libraryport.PlanFile{genre(tracks[0], "A")}}), "proposal:1/acceptance:1")
	if err != nil {
		t.Fatal(err)
	}
	r.drain()
	old := db.At(time.Now().Add(-originGrace - time.Minute))
	if _, err := r.m.store.db.W.Exec("UPDATE library_changes SET finished_at=? WHERE id=?", old, id); err != nil {
		t.Fatal(err)
	}
	r.reversed(KindUndo, id)
	if _, err := r.m.store.trim(bg, 0, time.Now()); err != nil {
		t.Fatal(err)
	}
	st, err := r.m.Port().Changes(bg, []int64{id})
	if err != nil || !st[id].Undone {
		t.Fatalf("the undone change left in its grace: %+v %v", st, err)
	}
	if _, err := r.m.store.trim(bg, 0, time.Now().Add(originGrace+time.Minute)); err != nil {
		t.Fatal(err)
	}
	if st, _ := r.m.Port().Changes(bg, []int64{id}); len(st) != 0 {
		t.Fatalf("the change outlived its grace: %+v", st)
	}
}

func TestPortHistoryTrimStopsBeforeUnfinishedChange(t *testing.T) {
	for _, state := range []string{StateQueued, StateWriting} {
		t.Run(state, func(t *testing.T) {
			r := newRig(t)
			first := r.writing()
			second := r.writing()
			if _, err := r.m.store.db.W.Exec("UPDATE library_changes SET state=? WHERE id=?", state, first); err != nil {
				t.Fatal(err)
			}
			if _, err := r.m.store.db.W.Exec("UPDATE library_changes SET state='done' WHERE id=?", second); err != nil {
				t.Fatal(err)
			}
			if _, err := r.m.store.trim(bg, 0, time.Now()); err != nil {
				t.Fatal(err)
			}
			page, err := r.m.Port().History(bg, 0, 50)
			if err != nil || len(page.Changes) != 0 || page.TrimmedThrough != first-1 {
				t.Fatalf("blocked page %+v: %v", page, err)
			}
			if _, err := r.m.store.db.W.Exec("UPDATE library_changes SET state='done' WHERE id=?", first); err != nil {
				t.Fatal(err)
			}
			page, err = r.m.Port().History(bg, page.TrimmedThrough, 50)
			if err != nil || len(page.Changes) != 1 || page.Changes[0].ID != first || page.TrimmedThrough != second {
				t.Fatalf("completed page %+v: %v", page, err)
			}
		})
	}
}

func TestPortHistoryFilesReportWrittenForPartialSave(t *testing.T) {
	r := newRig(t)
	id := r.writing(changeFile{Path: "folk/Blue/01.flac", Before: map[string][]string{"GENRE": {"Rock"}}, After: map[string][]string{"GENRE": {"Jazz"}}}, changeFile{Path: "folk/Blue/02.flac", Before: map[string][]string{"GENRE": {"Rock"}}, After: map[string][]string{"GENRE": {"Jazz"}}})
	if _, err := r.m.store.db.W.Exec("UPDATE library_changes SET state='partial' WHERE id=?", id); err != nil {
		t.Fatal(err)
	}
	if _, err := r.m.store.db.W.Exec("UPDATE library_change_files SET written=1 WHERE change_id=? AND path=?", id, "folk/Blue/01.flac"); err != nil {
		t.Fatal(err)
	}
	page, err := r.m.Port().History(bg, 0, 50)
	if err != nil || len(page.Changes) != 1 {
		t.Fatalf("history %+v: %v", page, err)
	}
	files := page.Changes[0].Files
	if len(files) != 2 || !files[0].Written || files[1].Written {
		t.Fatalf("files %+v", files)
	}
	raw, err := json.Marshal(files)
	if err != nil || !bytes.Contains(raw, []byte(`"written":false`)) || !bytes.Contains(raw, []byte(`"written":true`)) {
		t.Fatalf("history JSON %s: %v", raw, err)
	}
	raw, err = json.Marshal(files[0].PreviewFile)
	if err != nil || bytes.Contains(raw, []byte("written")) {
		t.Fatalf("preview JSON %s: %v", raw, err)
	}
}
