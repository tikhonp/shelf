//go:build unix

package main

import (
	"bytes"
	"context"
	"syscall"
	"testing"
)

func TestServeSetsUmask(t *testing.T) {
	old := syscall.Umask(0o077)
	defer syscall.Umask(old)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for name, vars := range map[string]map[string]string{
		"valid config": local(t, nil),
		// before the config is read, so nothing written later escapes it
		"bad config": local(t, map[string]string{"SHELF_TIMEZONE": "Mars/Olympus"}),
	} {
		syscall.Umask(0o077)
		var out bytes.Buffer
		testCLI(vars, &out).run(ctx, []string{"serve"})
		if got := syscall.Umask(0o077); got != 0o022 {
			t.Errorf("%s: umask %#o after serve, want 022", name, got)
		}
	}
}
