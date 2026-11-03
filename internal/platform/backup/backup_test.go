package backup

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/tikhonp/shelf/internal/platform/db"
	"github.com/tikhonp/shelf/internal/platform/db/dbtest"
	"github.com/tikhonp/shelf/internal/platform/jobs"
	"github.com/tikhonp/shelf/internal/platform/jobs/jobstest"
)

var bg = context.Background()

type rig struct {
	t    *testing.T
	h    *jobstest.Harness
	s    *Service
	dir  string
	stop func() // stops the job system and its scheduler
}

func newRig(t *testing.T, loc *time.Location) *rig {
	t.Helper()
	h := jobstest.New(t)
	if loc != nil {
		h.Location = loc
		h.Sys = h.NewSystem()
	}
	dir := t.TempDir()
	s := New(h.DB, dir, h.Location, dbtest.Discard)
	s.Now = h.Clock.Now
	if err := h.Sys.Register("platform", s.JobType()); err != nil {
		t.Fatal(err)
	}
	if err := h.Sys.RegisterSchedules("platform", s.Schedule()); err != nil {
		t.Fatal(err)
	}
	return &rig{t: t, h: h, s: s, dir: dir, stop: h.Start(h.Sys)}
}

// backUp runs one backup job to the end and returns its id.
func (r *rig) backUp() int64 {
	r.t.Helper()
	req := r.s.Request()
	req.CreatedBy = "cli"
	e, err := r.h.Sys.EnqueueNow(bg, req)
	if err != nil {
		r.t.Fatal(err)
	}
	r.h.Drain()
	return e.ID
}

func (r *rig) names() []string {
	r.t.Helper()
	l, err := r.s.List()
	if err != nil {
		r.t.Fatal(err)
	}
	out := make([]string, len(l))
	for i, b := range l {
		out[i] = b.Name
	}
	return out
}

func TestBackupIsAConsistentCopy(t *testing.T) {
	r := newRig(t, nil)
	err := r.h.DB.Write(bg, func(tx *sqlx.Tx) error {
		if _, err := tx.Exec(`CREATE TABLE marks (v TEXT) STRICT`); err != nil {
			return err
		}
		_, err := tx.Exec(`INSERT INTO marks VALUES ('kept')`)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if id := r.backUp(); r.h.State(id) != jobs.Succeeded {
		t.Fatalf("backup job: %s", r.h.State(id))
	}
	list, err := r.s.List()
	if err != nil || len(list) != 1 || list[0].Name != "shelf-2026-03-10.db" || list[0].Size == 0 {
		t.Fatalf("list: %+v, %v", list, err)
	}
	path := filepath.Join(r.s.Dir(), list[0].Name)
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Fatalf("backup mode %v", fi.Mode().Perm())
	}
	cp, err := db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cp.Close() }()
	var v string
	if err := cp.R.Get(&v, `SELECT v FROM marks`); err != nil || v != "kept" {
		t.Fatalf("the copy holds %q, %v", v, err)
	}
}

func TestBackupKeeps14(t *testing.T) {
	r := newRig(t, nil)
	if err := os.MkdirAll(r.s.Dir(), 0o700); err != nil {
		t.Fatal(err)
	}
	// Fourteen older days are there already; today makes fifteen.
	for i := 1; i <= 14; i++ {
		day := time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC).AddDate(0, 0, -i)
		if err := os.WriteFile(filepath.Join(r.s.Dir(), day.Format(FileLayout)), []byte("old"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	r.backUp()
	names := r.names()
	if len(names) != 14 || names[0] != "shelf-2026-03-10.db" || names[13] != "shelf-2026-02-25.db" {
		t.Fatalf("kept %d: %v", len(names), names)
	}
	if _, err := os.Stat(filepath.Join(r.s.Dir(), "shelf-2026-02-24.db")); !os.IsNotExist(err) {
		t.Fatal("the oldest backup was not removed")
	}
	// The next day removes the next oldest.
	r.h.Clock.Advance(24 * time.Hour)
	r.backUp()
	if names := r.names(); len(names) != 14 || names[0] != "shelf-2026-03-11.db" || names[13] != "shelf-2026-02-26.db" {
		t.Fatalf("next day: %v", names)
	}
}

func TestSameDayBackupReplaces(t *testing.T) {
	r := newRig(t, nil)
	r.backUp()
	err := r.h.DB.Write(bg, func(tx *sqlx.Tx) error {
		_, err := tx.Exec(`CREATE TABLE marker_second (v TEXT) STRICT`)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	r.h.Clock.Advance(2 * time.Hour) // still 2026-03-10
	r.backUp()
	if names := r.names(); len(names) != 1 {
		t.Fatalf("two backups on one day: %v", names)
	}
	data, err := os.ReadFile(filepath.Join(r.s.Dir(), "shelf-2026-03-10.db"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "marker_second") {
		t.Fatal("the file is the first backup, not the second")
	}
}

func TestBackupIsNamedByTheDisplayDay(t *testing.T) {
	// 12:00 UTC on the 10th is 01:00 on the 11th in Auckland.
	loc, err := time.LoadLocation("Pacific/Auckland")
	if err != nil {
		t.Fatal(err)
	}
	r := newRig(t, loc)
	r.backUp()
	if names := r.names(); len(names) != 1 || names[0] != "shelf-2026-03-11.db" {
		t.Fatalf("names: %v", names)
	}
}

func TestHalfWrittenBackupIsInvisible(t *testing.T) {
	r := newRig(t, nil)
	if err := os.MkdirAll(r.s.Dir(), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{".shelf-2026-03-10.db.tmp", "shelf-2026-03-10.db.tmp", "shelf-2026-03-10.db-wal", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(r.s.Dir(), n), []byte("half"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if names := r.names(); len(names) != 0 {
		t.Fatalf("listed: %v", names)
	}
	// The next backup cleans a leftover of a crashed snapshot.
	r.backUp()
	if _, err := os.Stat(filepath.Join(r.s.Dir(), ".shelf-2026-03-10.db.tmp")); !os.IsNotExist(err) {
		t.Fatal("a crashed snapshot's temporary file was kept")
	}
}

func TestUnwritableBackupDirIsNamed(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes anywhere")
	}
	r := newRig(t, nil)
	if err := os.MkdirAll(r.s.Dir(), 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(r.s.Dir(), 0o700) })
	id := r.backUp()
	// Failed after one attempt, not queued for the second: a retry won't
	// change the directory's owner.
	j, err := r.h.Sys.Job(bg, id)
	if err != nil {
		t.Fatal(err)
	}
	if j.State != jobs.Failed {
		t.Fatalf("job state %s", j.State)
	}
	if !strings.Contains(j.Error, r.s.Dir()) || !strings.Contains(j.Error, "not writable") || !strings.Contains(j.Error, "uid") {
		t.Fatalf("error %q doesn't say which directory is not writable by whom", j.Error)
	}
}

func TestBackupScheduleRunsAt0330(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		t.Fatal(err)
	}
	r := newRig(t, loc)
	r.stop() // the test drives the scheduler by hand
	if err := r.h.Sys.RunSchedules(bg); err != nil {
		t.Fatal(err)
	}
	var next string
	if err := r.h.DB.R.Get(&next, `SELECT next_run_at FROM schedules WHERE name = 'platform.backup'`); err != nil {
		t.Fatal(err)
	}
	// 03:30 in Moscow (UTC+3) is 00:30 UTC, and 12:00 UTC is past it today.
	if next != "2026-03-11T00:30:00.000Z" {
		t.Fatalf("next run %s", next)
	}
}
