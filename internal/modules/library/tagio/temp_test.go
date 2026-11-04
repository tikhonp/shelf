package tagio

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestIsTemp(t *testing.T) {
	f, err := createTemp(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	if !IsTemp(filepath.Base(f.Name())) {
		t.Errorf("%s is not a temp name", f.Name())
	}
	for _, name := range []string{
		".shelf-tmp-", ".shelf-tmp-0123456789abcde", ".shelf-tmp-0123456789abcdef0", ".shelf-tmp-0123456789abcdeg",
		"shelf-tmp-0123456789abcdef", "01 Track.flac", ".shelf-tmp-0123456789abcdef.flac",
	} {
		if IsTemp(name) {
			t.Errorf("%q passed as a temp name", name)
		}
	}
}

func TestSweep(t *testing.T) {
	root := t.TempDir()
	mk := func(rel string) string {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	tmp1 := mk("qobuz/Album/.shelf-tmp-0123456789abcdef")
	tmp2 := mk("Rock/Artist/Album/CD 01/.shelf-tmp-fedcba9876543210")
	keep := []string{
		mk("qobuz/Album/01 Track.flac"),
		mk("qobuz/Album/.shelf-tmp-short"),
		// Dot folders are never shelf's.
		mk(".stfolder/.shelf-tmp-0123456789abcdef"),
		mk("qobuz/.hidden/.shelf-tmp-0123456789abcdef"),
	}
	// A folder with a temp name is not a temp file.
	if err := os.MkdirAll(filepath.Join(root, "qobuz", ".shelf-tmp-aaaaaaaaaaaaaaaa"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(tmp1, filepath.Join(root, "qobuz", ".shelf-tmp-bbbbbbbbbbbbbbbb")); err != nil {
		t.Fatal(err)
	}

	// Another user's file with the name stays.
	if removed, err := Sweep(root, os.Getuid()+1); err != nil || len(removed) != 0 {
		t.Fatalf("another uid removed %v, %v", removed, err)
	}

	removed, err := Sweep(root, os.Getuid())
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(removed)
	if want := []string{tmp2, tmp1}; !slices.Equal(removed, want) {
		t.Errorf("removed %v, want %v", removed, want)
	}
	for _, p := range append(keep, filepath.Join(root, "qobuz", ".shelf-tmp-aaaaaaaaaaaaaaaa")) {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s: %v", p, err)
		}
	}
	if _, err := os.Lstat(filepath.Join(root, "qobuz", ".shelf-tmp-bbbbbbbbbbbbbbbb")); err != nil {
		t.Errorf("the symlink went: %v", err)
	}
}

func TestSweepSkipsUnreadableFolders(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root reads every folder")
	}
	root := t.TempDir()
	locked := filepath.Join(root, "Rock")
	open := filepath.Join(root, "qobuz")
	for _, d := range []string{locked, open} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	tmp := filepath.Join(open, ".shelf-tmp-0123456789abcdef")
	if err := os.WriteFile(tmp, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	removed, err := Sweep(root, os.Getuid())
	if err == nil {
		t.Error("an unreadable folder gave no error")
	}
	if !slices.Equal(removed, []string{tmp}) {
		t.Errorf("removed %v", removed)
	}
	if _, err := Sweep(filepath.Join(root, "missing"), os.Getuid()); err == nil {
		t.Error("a missing root gave no error")
	}
}
