package library

import (
	"testing"

	"github.com/tikhonp/shelf/internal/platform/db"
	"github.com/tikhonp/shelf/internal/platform/db/dbtest"
)

// The rebuild that lets a delete into the history keeps every change with
// its files, runs and undos, keeps foreign keys working, and never hands
// out the ID of a cleared change again. Its down migration drops the
// deletes and keeps the rest.
func TestDeleteMigrationKeepsTheHistory(t *testing.T) {
	d := dbtest.Open(t, New())
	dbtest.DownTo(t, d, New(), 6)
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := d.W.Exec(q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	count := func(q string) int {
		t.Helper()
		var n int
		if err := d.W.Get(&n, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		return n
	}
	const at = "2026-11-05T10:00:00.000Z"
	ins := `INSERT INTO library_changes (id, kind, target, parent, album_id, folder, title, artist, state, created_at)
		VALUES (?, ?, ?, ?, 1, 'qobuz/A', 'A', 'B', 'done', ?)`
	exec(ins, 1, "save", nil, nil, at)
	exec(ins, 2, "undo", 1, nil, at)
	exec(ins, 3, "save", nil, nil, at)
	exec(ins, 4, "save", nil, 3, at)
	exec(ins, 5, "save", nil, nil, at)
	exec(`DELETE FROM library_changes WHERE id = 5`)
	exec(`INSERT INTO library_change_files (change_id, path, before, after) VALUES (1, 'qobuz/A/01.flac', '{}', '{}')`)
	exec(`INSERT INTO library_runs (change_id, fixes) VALUES (3, '[]')`)
	exec(`INSERT INTO library_run_albums (run, position, album_id, change_id, title, artist, state) VALUES (3, 0, 1, 4, 'A', 'B', 'done')`)

	if err := db.MigrateUp(bg, d, dbtest.Discard, New()); err != nil {
		t.Fatal(err)
	}
	if n := count(`SELECT count(*) FROM library_changes`); n != 4 {
		t.Fatalf("%d changes after the rebuild", n)
	}
	if count(`SELECT count(*) FROM library_change_files`) != 1 || count(`SELECT count(*) FROM library_run_albums`) != 1 {
		t.Fatal("the rebuild lost files or a run's albums")
	}
	if count(`SELECT count(*) FROM pragma_foreign_key_check`) != 0 || count(`PRAGMA foreign_keys`) != 1 {
		t.Fatal("foreign keys are broken or off")
	}
	var id int64
	if err := d.W.Get(&id, `INSERT INTO library_changes (kind, album_id, folder, title, artist, state, created_at)
		VALUES ('delete', 1, 'qobuz/A', 'A', 'B', 'done', ?) RETURNING id`, at); err != nil {
		t.Fatal(err)
	}
	if id != 6 {
		t.Errorf("the delete got ID %d, which a cleared change had or never was", id)
	}
	// The foreign keys still cascade: a save takes its files and undos.
	exec(`DELETE FROM library_changes WHERE id = 1`)
	if count(`SELECT count(*) FROM library_change_files`) != 0 || count(`SELECT count(*) FROM library_changes WHERE id = 2`) != 0 {
		t.Error("deleting a save left its files or its undo")
	}
	exec(`DELETE FROM library_changes WHERE id = 3`)
	if count(`SELECT count(*) FROM library_run_albums`) != 0 || count(`SELECT count(*) FROM library_changes WHERE id = 4`) != 0 {
		t.Error("deleting a run left its albums")
	}

	exec(ins, 7, "save", nil, nil, at)
	dbtest.DownTo(t, d, New(), 6)
	if n := count(`SELECT count(*) FROM library_changes`); n != 1 {
		t.Errorf("%d changes after the down migration, want the save alone", n)
	}
	if count(`PRAGMA foreign_keys`) != 1 {
		t.Error("foreign keys stay off after the down migration")
	}
}
