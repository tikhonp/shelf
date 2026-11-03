package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tikhonp/shelf/internal/platform/db"
)

func TestManageDownload(t *testing.T) {
	vars := local(t, map[string]string{"SHELF_DEV_FAKE_SOURCE": "true", "SHELF_MUSIC_DIR": t.TempDir()})
	manage := func(args ...string) (int, string, string) {
		var out, errs bytes.Buffer
		c := cli{lookup: env(vars), stdout: &out, stderr: &errs, modules: modules}
		code := c.run(context.Background(), append([]string{"manage"}, args...))
		return code, out.String(), errs.String()
	}
	if code, _, e := manage("migrate", "up"); code != 0 {
		t.Fatalf("migrate: %d %s", code, e)
	}
	code, out, e := manage("download", "--format", "mp3", "https://fake.test/album/album-1")
	if code != 0 || out != "Download 1 queued: fake:album:album-1 in format fake:mp3\n" {
		t.Fatalf("%d %q %s", code, out, e)
	}
	code, out, _ = manage("download", "https://fake.test/album/album-1", "--force", "--format=fake:flac")
	if code != 0 || !strings.HasPrefix(out, "Download 2 queued") {
		t.Fatalf("%d %q", code, out)
	}

	d, err := db.Open(filepath.Join(vars["SHELF_DATA_DIR"], "shelf.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	var rows []struct {
		Origin    string `db:"origin"`
		Format    string `db:"format"`
		Force     bool   `db:"force"`
		CreatedBy string `db:"created_by"`
		Queue     string `db:"queue"`
	}
	if err := d.R.Select(&rows, `
		SELECT d.origin, d.format, d.force, j.created_by, j.queue FROM downloads d JOIN jobs j ON j.id = d.job_id ORDER BY d.id`); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Origin != "cli" || rows[0].CreatedBy != "cli" || rows[0].Format != "fake:mp3" || rows[0].Force ||
		rows[1].Format != "fake:flac" || !rows[1].Force || rows[1].Queue != "system" {
		t.Fatalf("%+v", rows)
	}

	for _, args := range [][]string{{"download"}, {"download", "a", "b"}, {"download", "--format"}, {"download", "x", "--loud"}} {
		if code, _, _ := manage(args...); code != 2 {
			t.Errorf("%v: exit %d", args, code)
		}
	}
	if code, _, e := manage("download", "https://example.com/album/1"); code != 1 || !strings.Contains(e, "link not from a known source") {
		t.Errorf("unknown link: %d %s", code, e)
	}
	if code, _, e := manage("download", "https://fake.test/album/album-1", "--format", "ogg"); code != 1 || !strings.Contains(e, "no such format") {
		t.Errorf("unknown format: %d %s", code, e)
	}
}
