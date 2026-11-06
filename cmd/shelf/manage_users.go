package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/tikhonp/shelf/internal/platform"
	"golang.org/x/term"
)

func terminalPassword(prompt string) (string, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", errors.New("password entry needs a terminal; use docker exec -it or run shelf in a terminal")
	}
	_, _ = fmt.Fprint(os.Stderr, prompt)
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	_, _ = fmt.Fprintln(os.Stderr)
	return string(b), err
}
func (c cli) users(ctx context.Context, app *platform.App, args []string) error {
	if err := app.Auth.Ready(ctx); err != nil {
		return err
	}
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
		read := c.passwordReader
		if read == nil {
			read = terminalPassword
		}
		password, err := read("Password: ")
		if err != nil {
			return err
		}
		confirm, err := read("Confirm password: ")
		if err != nil {
			return err
		}
		if password != confirm {
			return errors.New("passwords do not match")
		}
		if args[0] == "add" {
			return app.Auth.Add(ctx, args[1], password)
		}
		return app.Auth.Reset(ctx, args[1], password, false)
	}
	return errors.New("unknown users command")
}
func validUsersArgs(args []string) bool {
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
