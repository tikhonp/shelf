package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/tikhonp/shelf/internal/modules/agents"
)

func TestAgentCommandsPersistAndRevokeWhileDisabled(t *testing.T) {
	vars := local(t, nil)
	var out bytes.Buffer
	run := func(args ...string) int {
		t.Helper()
		out.Reset()
		return testCLI(vars, &out, agents.New()).run(context.Background(), args)
	}
	if code := run("manage", "migrate", "up"); code != 0 {
		t.Fatalf("migrate %d: %s", code, &out)
	}
	if code := run("manage", "agents", "add", "Local agent", "--ops", "library.read,memory.write"); code != 0 {
		t.Fatalf("add %d: %s", code, &out)
	}
	if !strings.Contains(out.String(), "SHELF_AGENT_TOKEN=shelf_agent_") {
		t.Fatal("one-time secret missing")
	}
	if code := run("manage", "agents", "list"); code != 0 || !strings.Contains(out.String(), "Local agent") || strings.Contains(out.String(), "shelf_agent_") {
		t.Fatalf("list %d: %s", code, &out)
	}
	if code := run("manage", "agents", "revoke", "1"); code != 0 {
		t.Fatalf("revoke %d: %s", code, &out)
	}
	if code := run("manage", "agents", "add", "Wrong", "--ops", "library.apply"); code != 1 || strings.Contains(out.String(), "shelf_agent_") {
		t.Fatalf("invalid scope %d: %s", code, &out)
	}
	for _, args := range [][]string{{"manage", "agents", "add"}, {"manage", "agents", "revoke", "0"}, {"manage", "agents", "list", "extra"}} {
		if code := run(args...); code != 2 {
			t.Fatalf("invalid command %v returned %d", args, code)
		}
	}
}
