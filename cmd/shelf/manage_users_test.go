package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/tikhonp/shelf/internal/platform/config"
	"github.com/tikhonp/shelf/internal/platform/module"
)

func TestManageUsers(t *testing.T) {
	vars := local(t, nil)
	password := "a sufficiently long password"
	invoke := func(read func(string) (string, error), args ...string) (int, string) {
		var out, errs bytes.Buffer
		c := cli{lookup: env(vars), stdout: &out, stderr: &errs, modules: func(*config.Config) []module.Module { return nil }, passwordReader: read}
		code := c.run(context.Background(), append([]string{"manage"}, args...))
		return code, out.String() + errs.String()
	}
	reader := func(string) (string, error) { return password, nil }
	if code, out := invoke(reader, "users", "add", "Alice"); code != 1 || !strings.Contains(out, "migrate up") {
		t.Fatalf("before migrations %d %s", code, out)
	}
	if code, out := invoke(reader, "migrate", "up"); code != 0 {
		t.Fatal(code, out)
	}
	for _, name := range []string{"Alice", "alice"} {
		if code, out := invoke(reader, "users", "add", name); code != 0 {
			t.Fatal(code, out)
		}
	}
	if code, out := invoke(reader, "users", "add", "Alice"); code != 1 || !strings.Contains(out, "exists") {
		t.Fatal(code, out)
	}
	if code, out := invoke(reader, "users", "list"); code != 0 || !strings.Contains(out, "Alice") || !strings.Contains(out, "alice") || strings.Contains(out, password) || strings.Contains(out, "argon2") {
		t.Fatal(code, out)
	}
	if code, out := invoke(reader, "users", "set-password", "Alice"); code != 0 {
		t.Fatal(code, out)
	}
	if code, out := invoke(reader, "users", "reset-passkeys", "Alice"); code != 0 {
		t.Fatal(code, out)
	}
	if code, out := invoke(reader, "users", "delete", "Alice"); code != 0 {
		t.Fatal(code, out)
	}
	if code, out := invoke(reader, "users", "set-password", "ALICE"); code != 1 || !strings.Contains(out, "not found") {
		t.Fatal(code, out)
	}
	if code, out := invoke(func(string) (string, error) { return "", errors.New("no terminal") }, "users", "add", "Bob"); code != 1 || !strings.Contains(out, "no terminal") {
		t.Fatal(code, out)
	}
	n := 0
	if code, out := invoke(func(string) (string, error) {
		n++
		if n == 1 {
			return password, nil
		}
		return "different password", nil
	}, "users", "add", "Bob"); code != 1 || !strings.Contains(out, "do not match") {
		t.Fatal(code, out)
	}
	for _, args := range [][]string{{"users"}, {"users", "add"}, {"users", "list", "extra"}, {"users", "add", "Bob", "exposed-password"}} {
		if code, _ := invoke(reader, args...); code != 2 {
			t.Fatalf("bad args accepted %v", args)
		}
	}
}
