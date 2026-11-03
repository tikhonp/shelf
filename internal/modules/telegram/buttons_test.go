package telegram

import (
	"slices"
	"strings"
	"testing"

	"github.com/tikhonp/shelf/internal/modules/downloads"
	"github.com/tikhonp/shelf/internal/source"
)

func (r *rig) lastAnswer() string {
	r.t.Helper()
	a := r.tg.of("answer")
	if len(a) == 0 {
		r.t.Fatal("no button was answered")
	}
	return a[len(a)-1].text
}

func TestButtons(t *testing.T) {
	r := newRig(t)
	g := newGate()
	r.src.Hook = g.hook
	r.say("https://fake.test/album/album-1")
	status := r.tg.of("sendPhoto")[0]
	d := r.downloads()[0]
	g.waitReached(t)
	running := r.waitEdit(status.msgID, "Downloading")
	if !slices.Contains(running.buttons(), "Cancel=c:"+itoa(d.ID)) {
		t.Fatalf("buttons %q", running.buttons())
	}

	// Cancel.
	r.tap(owner, status, dataCancel+itoa(d.ID))
	if a := r.lastAnswer(); a != answerCancel {
		t.Fatalf("answered %q", a)
	}
	r.waitStatus(d.ID, downloads.Canceled)
	r.h.Drain()
	canceled := r.waitEdit(status.msgID, "\n\nCanceled")
	if !slices.Equal(canceled.buttons(), []string{"Retry=r:" + itoa(d.ID)}) {
		t.Fatalf("buttons %q", canceled.buttons())
	}
	r.tap(owner, status, dataCancel+itoa(d.ID))
	if a := r.lastAnswer(); a != answerEnded {
		t.Fatalf("a second cancel answered %q", a)
	}

	// Retry queues the same download again, and its message follows it.
	close(g.release)
	r.tap(owner, status, dataRetry+itoa(d.ID))
	if a := r.lastAnswer(); a != answerRetry {
		t.Fatalf("answered %q", a)
	}
	r.h.Drain()
	r.waitEdit(status.msgID, "Done: 3 tracks")
	r.tap(owner, status, dataRetry+itoa(d.ID))
	if a := r.lastAnswer(); a != answerNotRetry {
		t.Fatalf("a retry of a done download answered %q", a)
	}

	// Download again: the item is in the library, so the link is skipped,
	// and the button downloads it with force into the same message.
	r.say("https://fake.test/album/album-1")
	photos := r.tg.of("sendPhoto")
	again := photos[len(photos)-1]
	skipped := r.downloads()[0]
	if skipped.Status != downloads.Skipped {
		t.Fatalf("download %+v", skipped)
	}
	sk := r.waitEdit(again.msgID, "Already downloaded on 10 Mar 2026")
	if !slices.Equal(sk.buttons(), []string{"Download again=f:" + itoa(skipped.ID), "Open in Shelf>https://shelf.test/downloads/" + itoa(skipped.ID)}) {
		t.Fatalf("buttons %q", sk.buttons())
	}
	r.tap(owner, again, dataAgain+itoa(skipped.ID))
	if a := r.lastAnswer(); a != answerQueued {
		t.Fatalf("answered %q", a)
	}
	r.tap(owner, again, dataAgain+itoa(skipped.ID)) // a double tap
	if n := len(r.downloads()); n != 3 {
		t.Fatalf("%d downloads after a double tap", n)
	}
	forced := r.downloads()[0]
	if forced.ID == skipped.ID || !forced.Force || forced.OriginRef.StatusMessageID != again.msgID || forced.Ref != skipped.Ref {
		t.Fatalf("download again %+v", forced)
	}
	r.h.Drain()
	if done := r.waitEdit(again.msgID, "Done: 3 tracks"); !strings.Contains(done.buttons()[0], "/downloads/"+itoa(forced.ID)) {
		t.Fatalf("buttons %q", done.buttons())
	}
	if n := len(r.tg.of("sendPhoto")); n != 2 {
		t.Fatalf("%d status messages", n)
	}
}

func TestButtonUnknownUser(t *testing.T) {
	r := newRig(t)
	g := newGate()
	r.src.Hook = g.hook
	r.say("https://fake.test/album/album-1")
	status := r.tg.of("sendPhoto")[0]
	d := r.downloads()[0]
	g.waitReached(t)
	for _, data := range []string{dataCancel + itoa(d.ID), dataRetry + itoa(d.ID), dataAgain + itoa(d.ID),
		dataQueue + "fake:album:album-2", dataAll + "fake:artist:artist-1", dataDismiss} {
		r.tap(stranger, status, data)
		if a := r.lastAnswer(); a != textPrivate {
			t.Fatalf("%s answered %q", data, a)
		}
	}
	if list := r.downloads(); len(list) != 1 || list[0].Status != downloads.Running {
		t.Fatalf("downloads %+v", list)
	}
	close(g.release)
	r.h.Drain()
}

func TestArtistConfirmation(t *testing.T) {
	r := newRig(t)
	msg := r.say("https://fake.test/artist/artist-1")
	asked := r.tg.of("sendMessage")
	if len(asked) != 1 {
		t.Fatalf("answers %+v", asked)
	}
	conf := asked[0]
	wantButtons := []string{"Download all=a:fake:artist:artist-1",
		"Open in Shelf>https://shelf.test/search?q=https%3A%2F%2Ffake.test%2Fartist%2Fartist-1", "Cancel=x"}
	if conf.text != "Download 2 albums by Northern Static?" || conf.replyTo != msg.ID || !slices.Equal(conf.buttons(), wantButtons) {
		t.Fatalf("confirmation %+v %q", conf, conf.buttons())
	}
	if n := len(r.downloads()); n != 0 {
		t.Fatalf("%d downloads before Download all", n)
	}

	r.tap(owner, conf, dataAll+"fake:artist:artist-1")
	r.tap(owner, conf, dataAll+"fake:artist:artist-1") // a double tap
	list := r.downloads()
	if len(list) != 1 {
		t.Fatalf("downloads %+v", list)
	}
	d := list[0]
	want := downloads.OriginRef{ChatID: owner, RequestMessageID: msg.ID, StatusMessageID: conf.msgID, StatusIsText: true, UserID: owner}
	if d.Ref != (source.Ref{Source: "fake", Kind: source.KindArtist, ID: "artist-1"}) || d.OriginRef != want {
		t.Fatalf("download %+v", d)
	}
	r.h.Drain()
	final := r.waitEdit(conf.msgID, "Done: 2 albums")
	if final.method != "editMessageText" || final.text != "Northern Static\nFake\n\nDone: 2 albums, 6.1 KB" {
		t.Fatalf("summary %+v", final)
	}

	// Cancel on a confirmation queues nothing.
	r.say("https://fake.test/label/label-9")
	asked = r.tg.of("sendMessage")
	label := asked[len(asked)-1]
	if label.text != "Download 2 albums from Collection label-9?" {
		t.Fatalf("confirmation %q", label.text)
	}
	r.tap(owner, label, dataDismiss)
	if e := r.waitEdit(label.msgID, textDismissed); len(e.buttons()) != 0 {
		t.Fatalf("buttons left %q", e.buttons())
	}
	if n := len(r.downloads()); n != 1 {
		t.Fatalf("%d downloads", n)
	}

	// An artist without albums, and one the source doesn't have.
	for link, want := range map[string]string{
		"https://fake.test/artist/artist-3-empty": "Nobody Yet has no albums to download.",
		"https://fake.test/artist/missing-1":      "Fake has no such artist.",
	} {
		r.say(link)
		asked = r.tg.of("sendMessage")
		if got := asked[len(asked)-1].text; got != want {
			t.Fatalf("%s: answered %q", link, got)
		}
	}
}
