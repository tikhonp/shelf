package library

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/tikhonp/shelf/internal/platform/config"
	"github.com/tikhonp/shelf/internal/platform/module"
)

func TestRunSweepsTempFiles(t *testing.T) {
	root := t.TempDir()
	album := filepath.Join(root, "qobuz", "Album")
	if err := os.MkdirAll(album, 0o755); err != nil {
		t.Fatal(err)
	}
	tmp := filepath.Join(album, ".shelf-tmp-0123456789abcdef")
	track := filepath.Join(album, "01 Track.flac")
	for _, p := range []string{tmp, track} {
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m := New()
	if err := m.Init(module.Deps{Cfg: &config.Config{MusicDir: root}, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}); err != nil {
		t.Fatal(err)
	}
	// Init touches nothing: manage runs it too.
	if _, err := os.Stat(tmp); err != nil {
		t.Fatal("Init swept")
	}
	if err := m.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Error("the temp file is still there")
	}
	if _, err := os.Stat(track); err != nil {
		t.Error("the track went")
	}

	// A music folder that isn't there is logged, not fatal.
	m = New()
	_ = m.Init(module.Deps{Cfg: &config.Config{MusicDir: filepath.Join(root, "missing")}, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err := m.Run(context.Background()); err != nil {
		t.Errorf("Run: %v", err)
	}
}
