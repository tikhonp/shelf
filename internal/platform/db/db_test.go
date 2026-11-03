package db_test

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/tikhonp/shelf/internal/platform/db"
	"github.com/tikhonp/shelf/internal/platform/db/dbtest"
)

type src struct {
	name string
	fs   fs.FS
}

func (s src) Name() string      { return s.name }
func (s src) Migrations() fs.FS { return s.fs }

func sql(up, down string) *fstest.MapFile {
	return &fstest.MapFile{Data: []byte("-- +goose Up\n" + up + "\n-- +goose Down\n" + down + "\n")}
}

var ctx = context.Background()

// things gives the tests below a table to write to.
var things = src{"things", fstest.MapFS{"00001_things.sql": sql(
	"CREATE TABLE things (key TEXT PRIMARY KEY, value TEXT NOT NULL, updated_at TEXT) STRICT;",
	"DROP TABLE things;")}}

func tables(t *testing.T, d *db.DB) map[string]bool {
	t.Helper()
	var names []string
	if err := d.R.Select(&names, `SELECT name FROM sqlite_schema WHERE type = 'table'`); err != nil {
		t.Fatal(err)
	}
	m := map[string]bool{}
	for _, n := range names {
		m[n] = true
	}
	return m
}

func TestEveryModuleHasItsOwnVersionTable(t *testing.T) {
	d := dbtest.OpenEmpty(t)
	a := src{"alpha", fstest.MapFS{"00001_a.sql": sql("CREATE TABLE alpha_things (id INTEGER PRIMARY KEY) STRICT;", "DROP TABLE alpha_things;")}}
	b := src{"beta", fstest.MapFS{
		"00001_b.sql": sql("CREATE TABLE beta_things (id INTEGER PRIMARY KEY) STRICT;", "DROP TABLE beta_things;"),
		"00002_b.sql": sql("ALTER TABLE beta_things ADD COLUMN name TEXT;", "ALTER TABLE beta_things DROP COLUMN name;"),
	}}
	if err := db.MigrateUp(ctx, d, dbtest.Discard, dbtest.Platform, a, b); err != nil {
		t.Fatalf("MigrateUp: %v", err)
	}
	got := tables(t, d)
	for _, want := range []string{"alpha_goose_db_version", "beta_goose_db_version", "alpha_things", "beta_things"} {
		if !got[want] {
			t.Errorf("table %s missing; have %v", want, got)
		}
	}

	// A second run applies nothing and fails nothing.
	if err := db.MigrateUp(ctx, d, dbtest.Discard, dbtest.Platform, a, b); err != nil {
		t.Fatalf("second MigrateUp: %v", err)
	}

	st, err := db.Status(ctx, d, a, b)
	if err != nil {
		t.Fatal(err)
	}
	if len(st) != 3 {
		t.Fatalf("status = %+v", st)
	}
	for _, s := range st {
		if !s.Applied {
			t.Errorf("%s %d not applied", s.Module, s.Version)
		}
	}

	if err := db.MigrateDown(ctx, d, dbtest.Discard, b); err != nil {
		t.Fatalf("MigrateDown: %v", err)
	}
	st, _ = db.Status(ctx, d, b)
	if !st[0].Applied || st[1].Applied {
		t.Errorf("after down: %+v", st)
	}
	if !tables(t, d)["alpha_things"] {
		t.Error("rolling back beta touched alpha")
	}
}

func TestMigrateModuleWithoutTables(t *testing.T) {
	d := dbtest.OpenEmpty(t)
	for _, empty := range []src{{"nilfs", nil}, {"emptyfs", fstest.MapFS{}}, {"notes", fstest.MapFS{"README.md": {Data: []byte("x")}}}} {
		t.Run(empty.name, func(t *testing.T) {
			if err := db.MigrateUp(ctx, d, dbtest.Discard, empty, things); err != nil {
				t.Fatalf("MigrateUp: %v", err)
			}
			st, err := db.Status(ctx, d, empty, things)
			if err != nil {
				t.Fatalf("Status: %v", err)
			}
			if len(st) != 1 || st[0].Module != "things" || !st[0].Applied {
				t.Errorf("status = %+v", st)
			}
			err = db.MigrateDown(ctx, d, dbtest.Discard, empty)
			if err == nil || !strings.Contains(err.Error(), "has no migrations") {
				t.Errorf("MigrateDown: %v", err)
			}
			if tables(t, d)[db.VersionTable(empty.name)] {
				t.Error("a version table was made for a module without migrations")
			}
		})
	}
}

func TestFailingMigrationKeepsSchema(t *testing.T) {
	d := dbtest.OpenEmpty(t)
	bad := src{"gamma", fstest.MapFS{
		"00001_ok.sql":  sql("CREATE TABLE gamma_ok (id INTEGER PRIMARY KEY) STRICT;", "DROP TABLE gamma_ok;"),
		"00002_bad.sql": sql("CREATE TABLE gamma_half (id INTEGER PRIMARY KEY) STRICT;\nSELECT * FROM no_such_table;", "DROP TABLE gamma_half;"),
	}}
	err := db.MigrateUp(ctx, d, dbtest.Discard, bad, things)
	if err == nil || !strings.Contains(err.Error(), "00002_bad.sql") {
		t.Fatalf("err = %v", err)
	}
	got := tables(t, d)
	if !got["gamma_ok"] {
		t.Error("the migration before the broken one was not kept")
	}
	if got["gamma_half"] {
		t.Error("the broken migration left a table behind")
	}
	if got["things"] {
		t.Error("a module after the failing one was migrated")
	}
	st, _ := db.Status(ctx, d, bad)
	if !st[0].Applied || st[1].Applied {
		t.Errorf("status: %+v", st)
	}
}

func TestWriteRollsBack(t *testing.T) {
	d := dbtest.Open(t, things)
	boom := errors.New("boom")
	err := d.Write(ctx, func(tx *sqlx.Tx) error {
		if _, err := tx.Exec(`INSERT INTO things (key, value) VALUES ('x', '1')`); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	var n int
	if err := d.R.Get(&n, `SELECT count(*) FROM things`); err != nil || n != 0 {
		t.Fatalf("count = %d, %v", n, err)
	}
}

func TestReadPoolIsReadOnly(t *testing.T) {
	d := dbtest.Open(t, things)
	if _, err := d.R.Exec(`INSERT INTO things (key, value) VALUES ('x', '1')`); err == nil {
		t.Fatal("the read pool wrote")
	}
}

func TestWritesAreVisibleToReaders(t *testing.T) {
	d := dbtest.Open(t, things)
	if err := d.Write(ctx, func(tx *sqlx.Tx) error {
		_, err := tx.Exec(`INSERT INTO things (key, value) VALUES ('x', '1')`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var v string
	if err := d.R.Get(&v, `SELECT value FROM things WHERE key = 'x'`); err != nil || v != "1" {
		t.Fatalf("v = %q, %v", v, err)
	}
	var mode string
	if err := d.R.Get(&mode, `PRAGMA journal_mode`); err != nil || mode != "wal" {
		t.Fatalf("journal_mode = %q, %v", mode, err)
	}
}

func TestTime(t *testing.T) {
	d := dbtest.Open(t, things)
	in := db.At(time.Date(2026, 10, 7, 21, 4, 5, 123456789, time.FixedZone("MSK", 3*3600)))
	if err := d.Write(ctx, func(tx *sqlx.Tx) error {
		_, err := tx.Exec(`INSERT INTO things (key, value, updated_at) VALUES ('t', '1', ?)`, in)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var raw string
	var out db.Time
	if err := d.R.QueryRow(`SELECT updated_at, updated_at FROM things`).Scan(&raw, &out); err != nil {
		t.Fatal(err)
	}
	if raw != "2026-10-07T18:04:05.123Z" {
		t.Errorf("stored as %q", raw)
	}
	if !out.Equal(in.Time) {
		t.Errorf("read back %v, want %v", out, in)
	}
	var sqlNow string
	if err := d.R.Get(&sqlNow, `SELECT strftime('%Y-%m-%dT%H:%M:%fZ', 'now')`); err != nil {
		t.Fatal(err)
	}
	if _, err := time.Parse(db.TimeLayout, sqlNow); err != nil {
		t.Errorf("SQL format %q does not match TimeLayout: %v", sqlNow, err)
	}
}

func TestVacuumInto(t *testing.T) {
	d := dbtest.Open(t, things)
	if err := d.Write(ctx, func(tx *sqlx.Tx) error {
		_, err := tx.Exec(`INSERT INTO things (key, value) VALUES ('x', 'kept')`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "copy.db")
	if err := d.VacuumInto(ctx, dest); err != nil {
		t.Fatalf("VacuumInto: %v", err)
	}
	c, err := db.Open(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	var v string
	if err := c.R.Get(&v, `SELECT value FROM things WHERE key = 'x'`); err != nil || v != "kept" {
		t.Fatalf("copy has %q, %v", v, err)
	}
}
