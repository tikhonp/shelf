package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tikhonp/shelf/internal/platform/config"
	"github.com/tikhonp/shelf/internal/platform/db"
	"github.com/tikhonp/shelf/internal/platform/module"
	"golang.org/x/crypto/argon2"
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

func TestManageUsersPasswordStdin(t *testing.T) {
	vars := local(t, nil)
	invoke := func(stdin string, args ...string) (int, string) {
		var out, errs bytes.Buffer
		noTerminal := func(string) (string, error) { return "", errors.New("prompted for a terminal") }
		c := cli{lookup: env(vars), stdin: strings.NewReader(stdin), stdout: &out, stderr: &errs, modules: func(*config.Config) []module.Module { return nil }, passwordReader: noTerminal}
		code := c.run(context.Background(), append([]string{"manage"}, args...))
		return code, out.String() + errs.String()
	}
	if code, out := invoke("", "migrate", "up"); code != 0 {
		t.Fatal(code, out)
	}
	d, err := db.Open(filepath.Join(vars["SHELF_DATA_DIR"], "shelf.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	// matches decodes auth's hash format, so the test checks the exact bytes Shelf stored.
	matches := func(name, password string) bool {
		var hash string
		if err := d.R.Get(&hash, `SELECT password_hash FROM accounts WHERE username = ?`, name); err != nil {
			t.Fatal(err)
		}
		parts := strings.Split(hash, "$")
		salt, _ := base64.RawStdEncoding.DecodeString(parts[4])
		want, _ := base64.RawStdEncoding.DecodeString(parts[5])
		return bytes.Equal(argon2.IDKey([]byte(password), salt, 3, 64*1024, 1, 32), want)
	}

	// Shelf drops one trailing line ending and keeps spaces.
	if code, out := invoke(" piped password \n", "users", "add", "Alice", "--password-stdin"); code != 0 || strings.Contains(out, "piped password") {
		t.Fatal(code, out)
	}
	if !matches("Alice", " piped password ") {
		t.Fatal("stored password is not the piped line")
	}
	if code, out := invoke("windows password\r\n", "users", "set-password", "--password-stdin", "Alice"); code != 0 {
		t.Fatal(code, out)
	}
	if !matches("Alice", "windows password") {
		t.Fatal("CRLF was not dropped")
	}
	if code, out := invoke("no line ending", "users", "add", "Bob", "--password-stdin"); code != 0 || !matches("Bob", "no line ending") {
		t.Fatal(code, out)
	}

	for stdin, want := range map[string]string{
		"":                              "no password on standard input",
		"\n":                            "no password on standard input",
		"first password\nsecond line\n": "more than one line",
		"first password\n\n":            "more than one line",
		"short\n":                       "8 to 128",
		strings.Repeat("x", 5000):       "too long",
	} {
		if code, out := invoke(stdin, "users", "add", "Carol", "--password-stdin"); code != 1 || !strings.Contains(out, want) {
			t.Fatalf("%q: %d %s", stdin, code, out)
		}
	}
	if code, out := invoke("", "users", "list"); code != 0 || strings.Contains(out, "Carol") {
		t.Fatal(code, out)
	}
	// A failed set-password keeps the old password.
	if code, _ := invoke("one\ntwo\n", "users", "set-password", "Alice", "--password-stdin"); code != 1 || !matches("Alice", "windows password") {
		t.Fatal("failed set-password changed the password")
	}

	for _, args := range [][]string{
		{"users", "list", "--password-stdin"},
		{"users", "delete", "Bob", "--password-stdin"},
		{"users", "reset-passkeys", "Bob", "--password-stdin"},
		{"users", "add", "--password-stdin"},
		{"users", "add", "Bob", "--password-stdin", "--password-stdin"},
		{"users", "add", "Bob", "--password-stdin", "exposed-password"},
	} {
		if code, _ := invoke("long enough password\n", args...); code != 2 {
			t.Fatalf("bad args accepted %v", args)
		}
	}
}
