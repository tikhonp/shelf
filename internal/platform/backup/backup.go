// Package backup writes the daily snapshot of the database: a consistent copy
// made with VACUUM INTO into <data dir>/backups, keeping the newest 14.
// backup.yaml copies only that folder to the HDD, so rsync never reads the
// live file.
package backup

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/tikhonp/shelf/internal/platform/db"
	"github.com/tikhonp/shelf/internal/platform/jobs"
)

// JobType is the backup job's type name, and its coalescing key.
const JobType = "platform.backup"

// At is when the schedule runs, in SHELF_TIMEZONE.
const At = "03:30"

// Keep is how many backups stay.
const Keep = 14

// FileLayout names a backup by its day in the display time zone.
const FileLayout = "shelf-2006-01-02.db"

var (
	namePattern = regexp.MustCompile(`^shelf-\d{4}-\d{2}-\d{2}\.db$`)
	// Half-written files carry a leading dot and a .tmp suffix; the list
	// never shows them.
	tmpPattern = regexp.MustCompile(`^\.shelf-\d{4}-\d{2}-\d{2}\.db\.tmp$`)
)

// Backup is one file in the backup directory.
type Backup struct {
	Name    string // "shelf-2026-10-07.db"
	Size    int64
	ModTime time.Time
}

// Service makes and lists backups.
type Service struct {
	// Now is the clock; tests replace it.
	Now func() time.Time

	d   *db.DB
	dir string
	loc *time.Location
	log *slog.Logger
}

// New builds the service. The backups go to <dataDir>/backups and are named
// by their day in loc.
func New(d *db.DB, dataDir string, loc *time.Location, log *slog.Logger) *Service {
	if loc == nil {
		loc = time.UTC
	}
	return &Service{Now: time.Now, d: d, dir: filepath.Join(dataDir, "backups"), loc: loc, log: log}
}

// Dir is where backups are written.
func (s *Service) Dir() string { return s.dir }

// JobType is the job: snapshot, then prune.
func (s *Service) JobType() jobs.Type {
	return jobs.Type{
		Name: JobType, Queue: jobs.QueueSystem, MaxAttempts: 2, Backoff: []time.Duration{15 * time.Minute},
		Steps: []jobs.Step{
			{Name: "snapshot", Run: s.snapshot},
			{Name: "prune", Run: s.prune},
		},
	}
}

// Schedule runs the backup daily at 03:30 in the jobs system's time zone.
func (s *Service) Schedule() jobs.Schedule {
	return jobs.Schedule{
		Name: JobType, At: At,
		Request: func(context.Context) (jobs.Request, error) { return s.Request(), nil },
	}
}

// Request is the job to enqueue. A second request while one waits is merged
// into it.
func (s *Service) Request() jobs.Request {
	return jobs.Request{Type: JobType, CoalescingKey: JobType}
}

func (s *Service) snapshot(ctx context.Context, r *jobs.Run) error {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	name := s.Now().In(s.loc).Format(FileLayout)
	tmp := filepath.Join(s.dir, "."+name+".tmp")
	_ = os.Remove(tmp) // VACUUM INTO refuses a file with content: a leftover of a crash
	// VACUUM INTO accepts an empty file. Creating it here keeps it private
	// from the start (SQLite would create it world-readable) and turns an
	// unwritable directory into an error that names it, not SQLite's
	// "unable to open database file (14)".
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrPermission) {
		return jobs.Permanent(notWritable(s.dir))
	}
	if err != nil {
		return err
	}
	_ = f.Close()
	if err := s.d.VacuumInto(ctx, tmp); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("snapshot: %w", err)
	}
	if err := syncFile(tmp); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	// The rename keeps a half-written file out of the list. A second backup
	// on one day replaces that day's file.
	if err := os.Rename(tmp, filepath.Join(s.dir, name)); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	fi, err := os.Stat(filepath.Join(s.dir, name))
	if err != nil {
		return err
	}
	r.Log().Info("Wrote %s (%d bytes)", name, fi.Size())
	return nil
}

// notWritable says who owns dir. Docker creates a bind mount's missing host
// directory as root, and a retry won't change that.
func notWritable(dir string) error {
	owner := ""
	if fi, err := os.Stat(dir); err == nil {
		if st, ok := fi.Sys().(*syscall.Stat_t); ok {
			owner = fmt.Sprintf(", owned by uid %d with mode %v", st.Uid, fi.Mode().Perm())
		}
	}
	return fmt.Errorf("the backup directory %s is not writable by uid %d%s: chown it to %d", dir, os.Geteuid(), owner, os.Geteuid())
}

func syncFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	return f.Sync()
}

func (s *Service) prune(_ context.Context, r *jobs.Run) error {
	list, err := s.List()
	if err != nil {
		return err
	}
	for _, b := range list[min(Keep, len(list)):] {
		if err := os.Remove(filepath.Join(s.dir, b.Name)); err != nil && !os.IsNotExist(err) {
			return err
		}
		r.Log().Info("Removed %s", b.Name)
	}
	// Leftovers of a snapshot that crashed before its rename.
	entries, _ := os.ReadDir(s.dir)
	for _, e := range entries {
		if tmpPattern.MatchString(e.Name()) {
			_ = os.Remove(filepath.Join(s.dir, e.Name()))
		}
	}
	return nil
}

// List returns the backups, newest first. Half-written files are not listed.
func (s *Service) List() ([]Backup, error) {
	entries, err := os.ReadDir(s.dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Backup
	for _, e := range entries {
		if e.IsDir() || !namePattern.MatchString(e.Name()) {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue // removed since the listing
		}
		out = append(out, Backup{Name: e.Name(), Size: fi.Size(), ModTime: fi.ModTime()})
	}
	// The name is the date, so the newest sorts last.
	slices.SortFunc(out, func(a, b Backup) int { return -strings.Compare(a.Name, b.Name) })
	return out, nil
}
