package downloads

import (
	"testing"

	"github.com/tikhonp/shelf/internal/platform/identity"
)

func TestDownloadAuthorSurvivesChildrenRetryAndAccountRemoval(t *testing.T) {
	r := newRig(t)
	alice := identity.Account(17, "Alice")
	ctx := identity.With(bg, alice)
	d, err := r.svc.Queue(ctx, QueueRequest{Origin: OriginWeb, Ref: album("album-1")})
	if err != nil {
		t.Fatal(err)
	}
	if d.AuthorID == nil || *d.AuthorID != alice.ID || d.Author != "Alice" || r.job(d.JobID).CreatedBy != alice.ID {
		t.Fatalf("author %+v", d)
	}
	// Module history has no account foreign key or lookup.
	if _, err := r.h.DB.W.Exec(`INSERT INTO accounts(id,username,password_hash,user_handle,created_at) VALUES(17,'Alice','unused',x'17','2026-01-01T00:00:00.000Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := r.h.DB.W.Exec(`DELETE FROM accounts WHERE id=17`); err != nil {
		t.Fatal(err)
	}
	loaded, err := r.svc.Get(bg, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Author != "Alice" {
		t.Fatal(loaded)
	}
	group, err := r.svc.Queue(ctx, QueueRequest{Origin: OriginWeb, Ref: artist("artist-1")})
	if err != nil {
		t.Fatal(err)
	}
	r.start()
	r.waitStatus(group.ID, Done)
	detail, err := r.svc.Get(bg, group.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, child := range detail.Children {
		if child.AuthorID == nil || *child.AuthorID != alice.ID || child.Author != "Alice" {
			t.Fatalf("child %+v", child)
		}
	}
	r.waitStatus(d.ID, Done)
	if _, err := r.h.DB.W.Exec(`UPDATE downloads SET status='failed' WHERE id=?`, d.ID); err != nil {
		t.Fatal(err)
	}
	bob := identity.Account(18, "Bob")
	retried, err := r.svc.Retry(identity.With(bg, bob), d.ID, "web")
	if err != nil {
		t.Fatal(err)
	}
	if retried.Author != "Alice" || r.job(retried.JobID).CreatedBy != bob.ID {
		t.Fatalf("retry replaced creator: %+v", retried)
	}
	telegram, err := r.svc.Queue(bg, QueueRequest{Origin: OriginTelegram, OriginRef: OriginRef{UserID: 123, Username: "tg_user"}, Ref: album("album-2")})
	if err != nil {
		t.Fatal(err)
	}
	if telegram.Author != "tg_user" {
		t.Fatal(telegram)
	}
}

func TestLegacyTelegramAuthorMatchesInResponsesAndEvents(t *testing.T) {
	for _, d := range []Download{
		{OriginRef: OriginRef{UserID: 123, Username: "alice_tg"}},
		{OriginRef: OriginRef{UserID: 123}},
		{Author: "Alice", OriginRef: OriginRef{UserID: 123, Username: "old_name"}},
		{},
	} {
		response, event := toJSON(d), d.event("")
		if response.Author != event.Author {
			t.Fatalf("response author %q, event author %q", response.Author, event.Author)
		}
		if d.OriginRef.UserID != 0 && event.Author == "Unknown" {
			t.Fatal("lost Telegram author")
		}
	}
}
