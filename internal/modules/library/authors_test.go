package library

import (
	"context"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/tikhonp/shelf/internal/platform/db"
	"github.com/tikhonp/shelf/internal/platform/identity"
)

func TestHistoryAuthorsPreserveCreatorsAndRunChildren(t *testing.T) {
	r := newRig(t)
	alice := identity.Account(1, "Alice")
	bob := identity.Account(2, "Bob")
	save := changeRow{Kind: KindSave, State: StateQueued, Request: "{}", CreatedAt: db.Now()}
	insert := func(ctx context.Context, c *changeRow) {
		t.Helper()
		if err := r.m.store.db.Write(ctx, func(tx *sqlx.Tx) error { return r.m.store.insertChange(ctx, tx, c) }); err != nil {
			t.Fatal(err)
		}
	}
	insert(identity.With(bg, alice), &save)
	undo := changeRow{Kind: KindUndo, Target: &save.ID, State: StateQueued, Request: "{}", CreatedAt: db.Now()}
	insert(identity.With(bg, bob), &undo)
	child := changeRow{Kind: KindUndo, Parent: &undo.ID, State: StateQueued, Request: "{}", CreatedAt: db.Now()}
	insert(bg, &child)
	for _, want := range []struct {
		id int64
		a  identity.Author
	}{{save.ID, alice}, {undo.ID, bob}, {child.ID, bob}} {
		c, err := r.m.store.change(bg, r.m.store.db.R, want.id)
		if err != nil {
			t.Fatal(err)
		}
		if c.AuthorID == nil || *c.AuthorID != want.a.ID || c.Author != want.a.Name {
			t.Fatalf("author %+v", c)
		}
		j := r.m.changeJSON(bg, c)
		if j.Author != want.a.Name {
			t.Fatal(j)
		}
	}
	unknown := changeRow{Kind: KindSave, State: StateQueued, Request: "{}", CreatedAt: db.Now()}
	insert(bg, &unknown)
	if j := r.m.changeJSON(bg, unknown); j.Author != "Unknown" {
		t.Fatal(j)
	}
}
