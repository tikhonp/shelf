package telegram

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/tikhonp/shelf/internal/modules/downloads"
	"github.com/tikhonp/shelf/internal/source"
)

func (r *rig) lastSent() call {
	r.t.Helper()
	sent := r.tg.of("sendMessage")
	if len(sent) == 0 {
		r.t.Fatal("nothing was sent")
	}
	return sent[len(sent)-1]
}

func TestTextSearch(t *testing.T) {
	r := newRig(t)
	msg := r.say("  low   tide ")
	ans := r.lastSent()
	want := []string{
		"Low Tide Hymns, Northern Static, 2019, Fake Hi-Res 24/96=q:fake:album:album-1",
		"Low Tide Hymns, Northern Static, 2019, Other Hi-Res 24/96=q:other:album:album-1",
	}
	if ans.text != `Albums for "low tide":` || ans.replyTo != msg.ID || !slices.Equal(ans.buttons(), want) {
		t.Fatalf("answer %q %q", ans.text, ans.buttons())
	}

	r.say("zzz")
	if got := r.lastSent(); got.text != `Nothing found for "zzz".` || len(got.buttons()) != 0 {
		t.Fatalf("answer %+v", got)
	}

	// Up to five albums from each source, and none from one that is off.
	r.other.Off = true
	r.say("e")
	if got := r.lastSent().buttons(); len(got) != searchLimit || !strings.HasSuffix(got[0], "=q:fake:album:album-1") {
		t.Fatalf("buttons %q", got)
	}
	if n := len(r.downloads()); n != 0 {
		t.Fatalf("a search queued %d downloads", n)
	}
}

// searchOnly is a source that only searches: it fails, or answers late.
type searchOnly struct {
	id   string
	late time.Duration
}

func (s searchOnly) ID() string                           { return s.id }
func (s searchOnly) Name() string                         { return strings.ToUpper(s.id[:1]) + s.id[1:] }
func (s searchOnly) Health(context.Context) source.Health { return source.Health{OK: true} }
func (s searchOnly) SearchKinds() []source.Kind           { return []source.Kind{source.KindAlbum} }

func (s searchOnly) Search(context.Context, source.Query) (source.Page, error) {
	if s.late == 0 {
		return source.Page{}, errors.New("source down")
	}
	// It ignores the context, as a stuck client would.
	time.Sleep(s.late)
	return source.Page{Items: []source.Item{{Ref: source.Ref{Source: s.id, Kind: source.KindAlbum, ID: "late"}, Title: "Late Album"}}}, nil
}

func TestTextSearchSourceFails(t *testing.T) {
	r := newRig(t, searchOnly{id: "broken"}, searchOnly{id: "slow", late: 300 * time.Millisecond})
	r.m.b.searchWait = 100 * time.Millisecond
	r.say("low tide")
	got := r.lastSent().buttons()
	if len(got) != 2 || !strings.HasSuffix(got[0], "=q:fake:album:album-1") || !strings.HasSuffix(got[1], "=q:other:album:album-1") {
		t.Fatalf("buttons %q", got)
	}

	// When no source answers in time, the answer says search is down.
	r.src.Off, r.other.Off = true, true
	r.say("low tide")
	if got := r.lastSent(); got.text != textSearchDown {
		t.Fatalf("answered %q", got.text)
	}
}

func TestSearchButtonQueues(t *testing.T) {
	r := newRig(t)
	r.say("glass")
	results := r.lastSent()
	if b := results.buttons(); len(b) != 2 || !strings.HasSuffix(b[0], "=q:fake:album:album-2") {
		t.Fatalf("buttons %q", b)
	}
	r.tap(owner, results, dataQueue+"fake:album:album-2")
	if a := r.lastAnswer(); a != answerQueued {
		t.Fatalf("answered %q", a)
	}
	photos := r.tg.of("sendPhoto")
	if len(photos) != 1 || !strings.HasPrefix(photos[0].text, "Glass Weather\nNorthern Static\nFake, Hi-Res 24/96") ||
		photos[0].replyTo != results.msgID {
		t.Fatalf("status message %+v", photos)
	}
	d := r.downloads()[0]
	if d.Ref != (source.Ref{Source: "fake", Kind: source.KindAlbum, ID: "album-2"}) || d.Origin != downloads.OriginTelegram ||
		d.OriginRef.StatusMessageID != photos[0].msgID {
		t.Fatalf("download %+v", d)
	}
	r.h.Drain()
	r.waitEdit(photos[0].msgID, "Done: 3 tracks")

	// A source switched off since the search answers so.
	r.other.Off = true
	r.tap(owner, results, dataQueue+"other:album:album-2")
	if a := r.lastAnswer(); a != "Other is not set up in Shelf." {
		t.Fatalf("answered %q", a)
	}
}
