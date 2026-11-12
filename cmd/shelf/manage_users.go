package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/tikhonp/shelf/internal/platform"
	"golang.org/x/term"
)

func terminalPassword(prompt string) (string, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", errors.New("password entry needs a terminal; use docker exec -it, or pipe it in with --password-stdin")
	}
	_, _ = fmt.Fprint(os.Stderr, prompt)
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	_, _ = fmt.Fprintln(os.Stderr)
	return string(b), err
}

// passwordStdinFlag makes add and set-password read the password from
// standard input. Scripts and docker exec without a terminal can then set one
// without putting it in the arguments.
const passwordStdinFlag = "--password-stdin"

// maxStdinPassword is far above the 512 bytes that 128 code points of UTF-8
// take, so only input that cannot be a password reaches it.
const maxStdinPassword = 4096

// stdinPassword reads one line. The terminal prompt asks twice to catch a
// typo, and a second read of piped input would catch none.
func stdinPassword(r io.Reader) (string, error) {
	if r == nil {
		return "", errors.New("no standard input")
	}
	b, err := io.ReadAll(io.LimitReader(r, maxStdinPassword+1))
	if err != nil {
		return "", err
	}
	if len(b) > maxStdinPassword {
		return "", errors.New("standard input is too long for a password")
	}
	b = bytes.TrimSuffix(b, []byte("\n"))
	b = bytes.TrimSuffix(b, []byte("\r"))
	if len(b) == 0 {
		return "", errors.New("no password on standard input")
	}
	if bytes.ContainsAny(b, "\r\n") {
		return "", errors.New("standard input has more than one line; pipe only the password")
	}
	return string(b), nil
}

// usersArgs drops --password-stdin, which add and set-password accept before
// or after the username.
func usersArgs(args []string) (rest []string, fromStdin bool) {
	for _, a := range args {
		if a == passwordStdinFlag && !fromStdin {
			fromStdin = true
			continue
		}
		rest = append(rest, a)
	}
	return rest, fromStdin
}

func (c cli) users(ctx context.Context, app *platform.App, args []string) error {
	if err := app.Auth.Ready(ctx); err != nil {
		return err
	}
	args, fromStdin := usersArgs(args)
	switch args[0] {
	case "list":
		rows, err := app.Auth.List(ctx)
		if err != nil {
			return err
		}
		for _, a := range rows {
			sayf(c.stdout, "%d\t%s\t%s\n", a.ID, a.Username, a.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"))
		}
		return nil
	case "delete":
		return app.Auth.Delete(ctx, args[1])
	case "reset-passkeys":
		return app.Auth.Reset(ctx, args[1], "", true)
	case "add", "set-password":
		password, err := c.newPassword(fromStdin)
		if err != nil {
			return err
		}
		if args[0] == "add" {
			return app.Auth.Add(ctx, args[1], password)
		}
		return app.Auth.Reset(ctx, args[1], password, false)
	}
	return errors.New("unknown users command")
}
func (c cli) newPassword(fromStdin bool) (string, error) {
	if fromStdin {
		return stdinPassword(c.stdin)
	}
	read := c.passwordReader
	if read == nil {
		read = terminalPassword
	}
	password, err := read("Password: ")
	if err != nil {
		return "", err
	}
	confirm, err := read("Confirm password: ")
	if err != nil {
		return "", err
	}
	if password != confirm {
		return "", errors.New("passwords do not match")
	}
	return password, nil
}
func validUsersArgs(args []string) bool {
	args, fromStdin := usersArgs(args)
	if fromStdin && (len(args) != 2 || (args[0] != "add" && args[0] != "set-password")) {
		return false
	}
	if len(args) == 1 && args[0] == "list" {
		return true
	}
	if len(args) != 2 {
		return false
	}
	switch args[0] {
	case "add", "delete", "set-password", "reset-passkeys":
		return true
	}
	return false
}
