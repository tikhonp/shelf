package library

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeNavidrome is Navidrome as a delete sees it. Its scan marks the tracks
// missing after markAfter looks.
type fakeNavidrome struct {
	mu        sync.Mutex
	scans     int
	looks     int
	markAfter int
	tracks    int
	purges    bool
	err       error
	folders   []string
}

func (f *fakeNavidrome) port() Navidrome {
	return Navidrome{
		ScanNow: func(context.Context) error {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.scans++
			return nil
		},
		RemoveMissing: func(_ context.Context, folder string) (int, bool, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.looks++
			f.folders = append(f.folders, folder)
			if f.err != nil || f.purges {
				return 0, f.purges, f.err
			}
			if f.looks < f.markAfter {
				return 0, false, nil
			}
			return f.tracks, false, nil
		},
	}
}

// forgetter records the folders a delete asks the downloads to forget.
type forgetter struct {
	mu      sync.Mutex
	folders []string
}

func (f *forgetter) forget(_ context.Context, folder string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.folders = append(f.folders, folder)
	return 1, nil
}

func (r *rig) deletes(nav *fakeNavidrome) *forgetter {
	r.m.DeleteWait = time.Millisecond
	if nav != nil {
		r.m.SetNavidrome(nav.port())
	}
	fg := &forgetter{}
	r.m.SetForget(fg.forget)
	return fg
}

func (r *rig) deleteAlbum(id int64, confirm ...string) changeJSON {
	r.t.Helper()
	rec := r.post("/api/library/albums/"+itoa(id)+"/delete", map[string]any{"confirm": confirm})
	if rec.Code != http.StatusAccepted {
		r.t.Fatalf("delete: %d %s", rec.Code, rec.Body)
	}
	return decode[changeJSON](r.t, rec)
}

func onDisk(p string) bool {
	_, err := os.Lstat(p)
	return !errors.Is(err, os.ErrNotExist)
}

// A delete asks first, removes the whole release folder with its disc
// folders and other files, has the downloads forget it, waits for
// Navidrome's scan to mark the tracks missing and has it forget them, and
// leaves a row in the history that nothing undoes.
func TestDeleteRemovesTheFolder(t *testing.T) {
	r := newRig(t)
	r.album("Blue", nil, nil)
	r.add("folk/Blue/CD 2/1.flac", "track.flac", map[string][]string{"ALBUM": {"Blue"}, "ALBUMARTIST": {"Joni Mitchell"},
		"ARTIST": {"Joni Mitchell"}, "TITLE": {"Track"}, "TRACKNUMBER": {"1"}, "DISCNUMBER": {"2"}})
	r.write("folk/Blue/cover.jpg", []byte("jpeg"))
	r.write("folk/Blue/shelf.txt", []byte("provenance"))
	r.album("Hejira", nil)
	r.walk()
	nav := &fakeNavidrome{markAfter: 2, tracks: 3}
	fg := r.deletes(nav)
	id := r.albumID("Blue")

	rec := r.post("/api/library/albums/"+itoa(id)+"/delete", map[string]any{})
	got := decode[refusal](t, rec)
	if rec.Code != http.StatusConflict || got.Reason != "confirm" || len(got.Confirm) != 1 || got.Confirm[0].Kind != ConfirmDelete {
		t.Fatalf("no confirm: %d %+v", rec.Code, got)
	}
	if !strings.Contains(got.Confirm[0].Message, "5 files") {
		t.Errorf("the warning: %q", got.Confirm[0].Message)
	}
	if !onDisk(filepath.Join(r.root, "folk/Blue")) {
		t.Fatal("asking deleted the folder")
	}

	c := r.deleteAlbum(id, ConfirmDelete)
	if c.Kind != KindDelete || c.State != StateQueued || c.Deleted == nil || c.Deleted.Files != 5 || c.Deleted.Navidrome != NavidromeWaiting {
		t.Fatalf("queued %+v %+v", c, c.Deleted)
	}
	r.startJobs()
	r.drain()
	if onDisk(filepath.Join(r.root, "folk/Blue")) {
		t.Fatal("the folder is still there")
	}
	if !onDisk(filepath.Join(r.root, "folk/Hejira/1.flac")) {
		t.Fatal("the delete took another album's folder")
	}
	c = r.change(c.ID)
	if c.State != StateDone || c.Deleted.Navidrome != NavidromeRemoved || c.Deleted.NavidromeTracks != 3 || c.Deleted.Forgot != 1 {
		t.Errorf("done %+v %+v", c, c.Deleted)
	}
	if nav.scans != 1 || nav.looks != 2 || nav.folders[0] != "folk/Blue" || len(fg.folders) != 1 || fg.folders[0] != "folk/Blue" {
		t.Errorf("scans %d, looks %d, Navidrome %v, downloads %v", nav.scans, nav.looks, nav.folders, fg.folders)
	}
	r.walk()
	if n := len(r.albums("").Albums); n != 1 {
		t.Errorf("%d albums after the walk", n)
	}
	h := r.historyRows()
	if len(h) != 1 || h[0].ID != c.ID || h[0].Title != "Blue" || h[0].Deleted == nil {
		t.Errorf("history %+v", h)
	}
	for _, kind := range []string{"undo", "redo"} {
		if rec := r.post("/api/library/changes/"+itoa(c.ID)+"/"+kind, map[string]any{}); rec.Code != http.StatusNotFound {
			t.Errorf("%s of a delete: %d %s", kind, rec.Code, rec.Body)
		}
	}
	if rec := r.post("/api/library/albums/"+itoa(id)+"/delete", map[string]any{"confirm": []string{ConfirmDelete}}); rec.Code != http.StatusNotFound {
		t.Errorf("a deleted album again: %d %s", rec.Code, rec.Body)
	}
}

// A folder that holds other albums, beside the album or in folders under
// it, names them and asks for them too.
func TestDeleteAsksForTheAlbumsItTakes(t *testing.T) {
	r := newRig(t)
	tags := func(album string) map[string][]string {
		return map[string][]string{"ALBUM": {album}, "ALBUMARTIST": {"Joni Mitchell"}, "ARTIST": {"Joni Mitchell"}, "TITLE": {"T"}, "TRACKNUMBER": {"1"}}
	}
	r.add("folk/Joni/1.flac", "track.flac", tags("Singles"))
	r.add("folk/Joni/2.flac", "track.flac", tags("Other Singles"))
	r.add("folk/Joni/Blue/1.flac", "track.flac", tags("Blue"))
	r.add("folk/Joni Two/1.flac", "track.flac", tags("Beside"))
	r.walk()
	r.deletes(nil)
	id := r.albumID("Singles")

	rec := r.post("/api/library/albums/"+itoa(id)+"/delete", map[string]any{"confirm": []string{ConfirmDelete}})
	got := decode[refusal](t, rec)
	if rec.Code != http.StatusConflict || len(got.Confirm) != 1 || got.Confirm[0].Kind != ConfirmSharedFolder {
		t.Fatalf("%d %+v", rec.Code, got)
	}
	msg := got.Confirm[0].Message
	if !strings.Contains(msg, "2 other albums") || !strings.Contains(msg, "Other Singles by Joni Mitchell") || !strings.Contains(msg, "Blue") || strings.Contains(msg, "Beside") {
		t.Errorf("the warning: %q", msg)
	}
	c := r.deleteAlbum(id, ConfirmDelete, ConfirmSharedFolder)
	if len(c.Deleted.Albums) != 2 || c.Deleted.Navidrome != NavidromeOff {
		t.Errorf("queued %+v", c.Deleted)
	}
	r.startJobs()
	r.drain()
	if onDisk(filepath.Join(r.root, "folk/Joni")) || !onDisk(filepath.Join(r.root, "folk/Joni Two/1.flac")) {
		t.Error("the delete took the wrong folders")
	}
	if c = r.change(c.ID); c.State != StateDone || c.Deleted.Navidrome != NavidromeOff {
		t.Errorf("without Navidrome: %+v %+v", c, c.Deleted)
	}
}

func TestDeleteRefuses(t *testing.T) {
	r := newRig(t)
	r.album("Blue", nil)
	r.album("Hejira", nil)
	r.add("Loose/1.flac", "track.flac", map[string][]string{"ALBUM": {"Loose"}, "ARTIST": {"A"}, "TITLE": {"T"}, "TRACKNUMBER": {"1"}})
	r.walk()
	r.deletes(nil)
	try := func(id int64, confirm ...string) (int, refusal) {
		rec := r.post("/api/library/albums/"+itoa(id)+"/delete", map[string]any{"confirm": confirm})
		return rec.Code, decode[refusal](t, rec)
	}

	if code, _ := try(9999, ConfirmDelete); code != http.StatusNotFound {
		t.Errorf("an unknown album: %d", code)
	}
	if code, got := try(r.albumID("Loose"), ConfirmDelete); code != http.StatusUnprocessableEntity || got.Reason != ReasonRefused {
		t.Errorf("a top folder: %d %+v", code, got)
	}
	r.m.SetBusy(func(dir string) bool { return strings.HasSuffix(dir, "Blue") })
	if code, got := try(r.albumID("Blue"), ConfirmDelete); code != http.StatusConflict || got.Reason != ReasonBusy {
		t.Errorf("busy: %d %+v", code, got)
	}
	r.m.SetBusy(nil)
	if os.Getuid() != 0 {
		dir := filepath.Join(r.root, "folk/Blue")
		if err := os.Chmod(dir, 0o555); err != nil {
			t.Fatal(err)
		}
		if code, got := try(r.albumID("Blue"), ConfirmDelete); code != http.StatusConflict || got.Reason != ReasonLocked {
			t.Errorf("a folder shelf can't write: %d %+v", code, got)
		}
		_ = os.Chmod(dir, 0o755)
		if err := os.Chmod(filepath.Join(r.root, "folk"), 0o555); err != nil {
			t.Fatal(err)
		}
		if code, got := try(r.albumID("Blue"), ConfirmDelete); code != http.StatusConflict || got.Reason != ReasonLocked {
			t.Errorf("a folder above shelf can't write: %d %+v", code, got)
		}
		_ = os.Chmod(filepath.Join(r.root, "folk"), 0o755)
	}

	// A second delete waits behind the first, which would leave it nothing.
	c := r.deleteAlbum(r.albumID("Blue"), ConfirmDelete)
	if code, _ := try(r.albumID("Blue"), ConfirmDelete); code != http.StatusConflict {
		t.Errorf("a second delete: %d", code)
	}
	// A folder that changes while the delete waits fails it, and Navidrome
	// and the downloads hear nothing.
	nav := &fakeNavidrome{tracks: 1}
	fg := r.deletes(nav)
	if err := os.RemoveAll(filepath.Join(r.root, "folk/Blue")); err != nil {
		t.Fatal(err)
	}
	r.startJobs()
	r.drain()
	if c = r.change(c.ID); c.State != StateFailed || c.Reason != ReasonConflict || c.Deleted.Navidrome != "" {
		t.Errorf("a folder gone: %+v %+v", c, c.Deleted)
	}
	if nav.scans+nav.looks != 0 || len(fg.folders) != 0 {
		t.Errorf("Navidrome %d %d, downloads %v", nav.scans, nav.looks, fg.folders)
	}
}

// Navidrome that purges by itself, never marks the tracks missing, or
// fails is recorded as such, and the files stay deleted.
func TestDeleteNavidromeOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name string
		nav  *fakeNavidrome
		want string
	}{
		{"purges", &fakeNavidrome{purges: true}, NavidromePurges},
		{"never marks them", &fakeNavidrome{markAfter: 1000}, NavidromeNone},
		{"fails", &fakeNavidrome{err: errors.New("no answer")}, NavidromeFailed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t)
			r.album("Blue", nil)
			r.walk()
			r.deletes(tc.nav)
			c := r.deleteAlbum(r.albumID("Blue"), ConfirmDelete)
			r.startJobs()
			r.drain()
			c = r.change(c.ID)
			if c.State != StateDone || c.Deleted.Navidrome != tc.want {
				t.Errorf("%+v %+v", c, c.Deleted)
			}
			if tc.want != NavidromePurges && tc.nav.looks != deleteTries {
				t.Errorf("%d looks", tc.nav.looks)
			}
		})
	}
}

// A delete a stop cut off stays writing through the start's settling, and
// its job deletes what is left.
func TestDeleteCutOffGoesOn(t *testing.T) {
	r := newRig(t)
	r.album("Blue", nil, nil)
	r.walk()
	r.deletes(nil)
	c := r.deleteAlbum(r.albumID("Blue"), ConfirmDelete)
	if _, err := r.m.store.db.W.Exec(`UPDATE library_changes SET state = ? WHERE id = ?`, StateWriting, c.ID); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(r.root, "folk/Blue/1.flac")); err != nil {
		t.Fatal(err)
	}
	if err := r.m.reconcileAll(bg); err != nil {
		t.Fatal(err)
	}
	if got := r.change(c.ID); got.State != StateWriting {
		t.Fatalf("the start settled the delete: %+v", got)
	}
	r.startJobs()
	r.drain()
	if got := r.change(c.ID); got.State != StateDone || onDisk(filepath.Join(r.root, "folk/Blue")) {
		t.Errorf("%+v", got)
	}
}
