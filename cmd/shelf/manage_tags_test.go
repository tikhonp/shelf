package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.senan.xyz/taglib"
)

func TestManageTags(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "internal", "modules", "library", "tagio", "testdata", "track.flac"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "01 Moanin'.flac")
	if err := os.WriteFile(p, src, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := taglib.WriteTags(p, map[string][]string{
		"TITLE": {"Moanin'"}, "ARTISTS": {"Art Blakey", "The Jazz Messengers"}, "MUSICBRAINZ_RELEASETRACKID": {"r-1"},
		"UPC": {"123"}, "LYRICS": {strings.Repeat("la ", 100)},
	}, 0); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(p)

	// No environment at all: the command needs no configuration or
	// database.
	manage := func(args ...string) (int, string, string) {
		var out, errs bytes.Buffer
		c := cli{lookup: env(nil), stdout: &out, stderr: &errs, modules: modules}
		code := c.run(context.Background(), append([]string{"manage"}, args...))
		return code, out.String(), errs.String()
	}
	code, out, e := manage("tags", p)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, e)
	}
	for _, want := range []string{
		"Format    FLAC 16-bit 44.1 kHz, 2 channels, 0:01",
		"Pictures  none",
		"Standard: 4 keys Navidrome reads",
		"TITLE                       title                Moanin'",
		"ARTISTS                     artists              Art Blakey\n",
		"The Jazz Messengers",
		"MUSICBRAINZ_RELEASETRACKID  musicbrainz_trackid  r-1",
		"… (300 characters)",
		"Extended: 1 key Navidrome never reads\n  UPC  123\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("no %q in\n%s", want, out)
		}
	}
	if after, _ := os.ReadFile(p); !bytes.Equal(before, after) {
		t.Error("the view changed the file")
	}

	if code, _, e := manage("tags", filepath.Join(dir, "notes.txt")); code != 1 || !strings.Contains(e, "not an audio file") {
		t.Errorf("a text file: %d %s", code, e)
	}
	if code, _, _ := manage("tags"); code != 2 {
		t.Errorf("no file: exit %d", code)
	}
}
