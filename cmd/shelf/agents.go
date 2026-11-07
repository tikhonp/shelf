package main

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/tikhonp/shelf/internal/modules/agents"
	"github.com/tikhonp/shelf/internal/platform"
)

type agentsCommand struct {
	action, name string
	id           int64
	ops          []string
}

func parseAgents(args []string) (agentsCommand, error) {
	bad := func() (agentsCommand, error) {
		return agentsCommand{}, errors.New("use manage agents add <name> [--ops op1,op2], list, or revoke <id>")
	}
	if len(args) == 0 {
		return bad()
	}
	c := agentsCommand{action: args[0]}
	switch c.action {
	case "list":
		if len(args) != 1 {
			return bad()
		}
	case "revoke":
		if len(args) != 2 {
			return bad()
		}
		var e error
		c.id, e = strconv.ParseInt(args[1], 10, 64)
		if e != nil || c.id <= 0 {
			return bad()
		}
	case "add":
		if len(args) != 2 && len(args) != 4 {
			return bad()
		}
		c.name = args[1]
		if len(args) == 4 {
			if args[2] != "--ops" || args[3] == "" {
				return bad()
			}
			c.ops = strings.Split(args[3], ",")
		}
	default:
		return bad()
	}
	return c, nil
}
func (c cli) agents(ctx context.Context, app *platform.App, cmd agentsCommand) error {
	mod, ok := app.Module(agents.Name)
	if !ok {
		return errors.New("agents module is not registered")
	}
	m, ok := mod.(*agents.Module)
	if !ok {
		return errors.New("invalid agents module")
	}
	switch cmd.action {
	case "add":
		conn, token, e := m.AddConnection(ctx, cmd.name, cmd.ops)
		if e != nil {
			return e
		}
		sayf(c.stdout, "Connection %d: %s\nSHELF_AGENT_TOKEN=%s\nStore this secret now. Shelf will not show it again. Give it separately from the setup prompt.\n", conn.ID, conn.Name, token)
	case "revoke":
		if e := m.RevokeConnection(ctx, cmd.id); e != nil {
			return e
		}
		sayf(c.stdout, "Revoked connection %d\n", cmd.id)
	case "list":
		cs, e := m.ListConnections(ctx)
		if e != nil {
			return e
		}
		w := tabwriter.NewWriter(c.stdout, 0, 0, 2, ' ', 0)
		_, _ = fmt.Fprintln(w, "ID\tNAME\tOPERATIONS\tCREATED\tLAST USED\tREVOKED")
		for _, conn := range cs {
			last, revoked := "", ""
			if conn.LastUsedAt != nil {
				last = *conn.LastUsedAt
			}
			if conn.RevokedAt != nil {
				revoked = *conn.RevokedAt
			}
			_, _ = fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\n", conn.ID, conn.Name, strings.Join(conn.Operations, ","), conn.CreatedAt, last, revoked)
		}
		return w.Flush()
	}
	return nil
}
