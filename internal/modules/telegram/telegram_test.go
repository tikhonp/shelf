package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/go-telegram/bot/models"
	"github.com/labstack/echo/v5"
	"github.com/tikhonp/shelf/internal/modules/downloads"
	"github.com/tikhonp/shelf/internal/platform/web"
)

func TestUnknownUser(t *testing.T) {
	r := newRig(t)
	msg := r.sayAs(stranger, "https://fake.test/album/album-1")
	sent := r.tg.of()
	if len(sent) != 1 || sent[0].method != "sendMessage" || sent[0].text != textPrivate || sent[0].chatID != stranger ||
		sent[0].replyTo != msg.ID {
		t.Fatalf("calls %+v", sent)
	}
	r.sayAs(stranger, "low tide")
	r.sayAs(stranger, "/queue")
	if n := len(r.tg.of()); n != 3 || len(r.downloads()) != 0 {
		t.Fatalf("%d calls, %d downloads", n, len(r.downloads()))
	}
}

func TestLinksQueue(t *testing.T) {
	r := newRig(t)
	msg := r.say("two albums: https://fake.test/album/album-1 and https://fake.test/album/album-2, " +
		"and the first again https://fake.test/album/album-1.")
	photos := r.tg.of("sendPhoto")
	if len(photos) != 2 {
		t.Fatalf("status messages %+v", photos)
	}
	list := r.downloads()
	if len(list) != 2 {
		t.Fatalf("%d downloads", len(list))
	}
	for i, p := range photos {
		d := list[len(list)-1-i] // the list is newest first
		want := downloads.OriginRef{ChatID: owner, RequestMessageID: msg.ID, StatusMessageID: p.msgID, UserID: owner, Username: "user42"}
		if d.Origin != downloads.OriginTelegram || d.OriginRef != want || d.URL == "" {
			t.Fatalf("download %+v", d)
		}
		if p.replyTo != msg.ID || p.photo != "https://fake.test/covers/"+d.Ref.ID+".jpg" || !strings.Contains(p.text, "Fake, Hi-Res 24/96") {
			t.Fatalf("status message %+v", p)
		}
	}
	if !strings.HasPrefix(photos[0].text, "Low Tide Hymns\nNorthern Static\nFake, Hi-Res 24/96\n\nQueued") {
		t.Fatalf("caption %q", photos[0].text)
	}
	r.h.Drain()
	for i, p := range photos {
		d := list[len(list)-1-i]
		final := r.waitEdit(p.msgID, "Done: 3 tracks, FLAC 24-bit 96 kHz, 3.1 KB")
		if final.method != "editMessageCaption" || !slices.Equal(final.buttons(), []string{"Open in Shelf>https://shelf.test/downloads/" + itoa(d.ID)}) {
			t.Fatalf("final edit %+v", final)
		}
		if !r.get(d.ID).Notified {
			t.Fatalf("download %d not notified", d.ID)
		}
	}
}

func TestLinkRefusals(t *testing.T) {
	r := newRig(t)
	r.other.Off = true
	cases := map[string]string{
		"https://example.com/album/1":       "This link is not from Fake or Other.",
		"https://other.test/album/album-1":  "Other is not set up in Shelf.",
		"https://fake.test/video/v-1":       textUnsupported,
		"see https://fake.test/station/s-1": textUnsupported,
	}
	for text, want := range cases {
		msg := r.say(text)
		sent := r.tg.of("sendMessage")
		if last := sent[len(sent)-1]; last.text != want || last.replyTo != msg.ID {
			t.Fatalf("%s: answered %q", text, last.text)
		}
	}
	if len(r.downloads()) != 0 || len(r.tg.of("sendPhoto")) != 0 {
		t.Fatalf("queued %+v", r.downloads())
	}
}

func TestLinksFound(t *testing.T) {
	text := "Слушай: music.apple.com/ru/album/1 и https://fake.test/album/a-2). Also https://fake.test/album/a-2"
	units := len([]rune("Слушай: "))
	msg := &models.Message{Text: text, Entities: []models.MessageEntity{
		{Type: models.MessageEntityTypeURL, Offset: units, Length: len("music.apple.com/ru/album/1")},
		{Type: models.MessageEntityTypeTextLink, Offset: 0, Length: 6, URL: "https://fake.test/album/a-3"},
	}}
	got := links(msg)
	want := []string{"https://music.apple.com/ru/album/1", "https://fake.test/album/a-3", "https://fake.test/album/a-2"}
	if !slices.Equal(got, want) {
		t.Fatalf("links %q", got)
	}
	photo := &models.Message{Caption: "https://fake.test/album/a-4"}
	if got := links(photo); !slices.Equal(got, []string{"https://fake.test/album/a-4"}) {
		t.Fatalf("caption links %q", got)
	}
}

func TestCoverFallback(t *testing.T) {
	r := newRig(t)
	r.tg.setFail(func(_ context.Context, c call) error {
		if c.method == "sendPhoto" {
			return errors.New("bad request, Bad Request: wrong file identifier/HTTP URL specified")
		}
		return nil
	})
	r.say("https://fake.test/album/album-1")
	texts := r.tg.of("sendMessage")
	if len(texts) != 1 || !strings.HasPrefix(texts[0].text, "Low Tide Hymns") {
		t.Fatalf("status message %+v", texts)
	}
	d := r.downloads()[0]
	if !d.OriginRef.StatusIsText || d.OriginRef.StatusMessageID != texts[0].msgID {
		t.Fatalf("origin %+v", d.OriginRef)
	}
	r.h.Drain()
	if final := r.waitEdit(texts[0].msgID, "Done: 3 tracks"); final.method != "editMessageText" {
		t.Fatalf("final edit %+v", final)
	}
}

func TestQueueCommand(t *testing.T) {
	r := newRig(t)
	r.say("/queue")
	if last := r.tg.of("sendMessage"); last[0].text != textQueueEmpty {
		t.Fatalf("answered %q", last[0].text)
	}

	g := newGate()
	r.src.Hook = g.hook
	r.say("https://fake.test/album/album-1")
	g.waitReached(t)
	r.say("https://fake.test/album/album-2")
	r.say("/queue@music_shelf_bot")
	sent := r.tg.of("sendMessage")
	want := "Low Tide Hymns, Northern Static: Downloading, 0 of 3 tracks\nhttps://fake.test/album/album-2: Queued"
	if got := sent[len(sent)-1].text; got != want {
		t.Fatalf("answered %q", got)
	}
	close(g.release)
	r.h.Drain()
}

func TestHelp(t *testing.T) {
	r := newRig(t)
	for _, text := range []string{"/start", "/help", "/unknown"} {
		r.say(text)
	}
	r.m.b.handle(bg, &models.Update{Message: &models.Message{ID: 99, Chat: models.Chat{ID: owner}, From: &models.User{ID: owner},
		Sticker: &models.Sticker{FileID: "s"}}})
	sent := r.tg.of("sendMessage")
	if len(sent) != 4 {
		t.Fatalf("%d answers", len(sent))
	}
	for _, c := range sent {
		if c.text != textHelp {
			t.Fatalf("answered %q", c.text)
		}
	}
}

func TestStatusRoute(t *testing.T) {
	r := newRig(t)
	e := echo.New()
	e.HTTPErrorHandler = web.ErrorHandler
	r.m.Routes(e.Group("/api"))
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/telegram", nil))
	var got StatusJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if want := (StatusJSON{Enabled: true, Connected: true, Username: "music_shelf_bot", Users: 1}); got != want {
		t.Fatalf("%+v", got)
	}
	if strings.Contains(rec.Body.String(), "123:abc") {
		t.Fatal("the answer holds the token")
	}
	r.m.onPollError(errors.New("conflict"))
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/telegram", nil))
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Connected {
		t.Fatalf("%+v after a failed poll", got)
	}
}

// The bot answers nothing before it is connected, and Notify says why.
func TestNotConnected(t *testing.T) {
	r := newRig(t)
	r.m.b.setClient(nil, nil)
	r.say("https://fake.test/album/album-1")
	if len(r.downloads()) != 0 {
		t.Fatal("queued without a connection")
	}
	if err := r.m.Notify(bg, downloads.Download{ID: 1}); !errors.Is(err, errNotConnected) {
		t.Fatalf("Notify: %v", err)
	}
}
