package telegram

import (
	"context"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/tikhonp/shelf/internal/modules/downloads"
	"github.com/tikhonp/shelf/internal/modules/fakesource"
	"github.com/tikhonp/shelf/internal/platform/config"
	"github.com/tikhonp/shelf/internal/platform/db/dbtest"
	"github.com/tikhonp/shelf/internal/platform/hub"
	"github.com/tikhonp/shelf/internal/platform/jobs"
	"github.com/tikhonp/shelf/internal/platform/jobs/jobstest"
	"github.com/tikhonp/shelf/internal/platform/module"
	"github.com/tikhonp/shelf/internal/source"
	"golang.org/x/time/rate"
)

var bg = context.Background()

// The user the bot answers, and one it doesn't.
const (
	owner    int64 = 42
	stranger int64 = 7
)

func unlimited() Limits {
	return Limits{PerChat: rate.Inf, PerChatBurst: 1, Global: rate.Inf, GlobalBurst: 1, MaxAttempts: 5}
}

// call is one Bot API call the fake heard.
type call struct {
	method  string
	chatID  int64
	msgID   int // the message sent, or the one edited
	text    string
	photo   string
	kb      *models.InlineKeyboardMarkup
	replyTo int
	at      time.Time
}

// buttons lists the call's buttons as "text=data" or "text>url".
func (c call) buttons() []string {
	var out []string
	if c.kb == nil {
		return out
	}
	for _, row := range c.kb.InlineKeyboard {
		for _, b := range row {
			if b.URL != "" {
				out = append(out, b.Text+">"+b.URL)
			} else {
				out = append(out, b.Text+"="+b.CallbackData)
			}
		}
	}
	return out
}

// fakeTG records the bot's calls. Messages it sends get IDs from 1000.
type fakeTG struct {
	mu     sync.Mutex
	calls  []call
	nextID int
	// fail, when set, may fail a call before it is recorded.
	fail func(ctx context.Context, c call) error
}

func newFakeTG() *fakeTG { return &fakeTG{nextID: 999} }

func (f *fakeTG) record(ctx context.Context, c call) (int, error) {
	f.mu.Lock()
	fail := f.fail
	f.mu.Unlock()
	if fail != nil {
		if err := fail(ctx, c); err != nil {
			return 0, err
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if c.method == "sendMessage" || c.method == "sendPhoto" {
		f.nextID++
		c.msgID = f.nextID
	}
	c.at = time.Now()
	f.calls = append(f.calls, c)
	return c.msgID, nil
}

func (f *fakeTG) setFail(fn func(ctx context.Context, c call) error) {
	f.mu.Lock()
	f.fail = fn
	f.mu.Unlock()
}

// of lists the calls of one method, or every call without one.
func (f *fakeTG) of(methods ...string) []call {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []call
	for _, c := range f.calls {
		for _, m := range methods {
			if c.method == m {
				out = append(out, c)
			}
		}
		if len(methods) == 0 {
			out = append(out, c)
		}
	}
	return out
}

// editsOf lists the edits of one message.
func (f *fakeTG) editsOf(msgID int) []call {
	var out []call
	for _, c := range f.of("editMessageText", "editMessageCaption") {
		if c.msgID == msgID {
			out = append(out, c)
		}
	}
	return out
}

func markup(m models.ReplyMarkup) *models.InlineKeyboardMarkup {
	kb, _ := m.(*models.InlineKeyboardMarkup)
	return kb
}

func replyID(p *models.ReplyParameters) int {
	if p == nil {
		return 0
	}
	return p.MessageID
}

func (f *fakeTG) SendMessage(ctx context.Context, p *tgbot.SendMessageParams) (*models.Message, error) {
	chat := p.ChatID.(int64)
	id, err := f.record(ctx, call{method: "sendMessage", chatID: chat, text: p.Text, kb: markup(p.ReplyMarkup), replyTo: replyID(p.ReplyParameters)})
	if err != nil {
		return nil, err
	}
	return &models.Message{ID: id, Chat: models.Chat{ID: chat}, Text: p.Text}, nil
}

func (f *fakeTG) SendPhoto(ctx context.Context, p *tgbot.SendPhotoParams) (*models.Message, error) {
	chat := p.ChatID.(int64)
	photo := p.Photo.(*models.InputFileString).Data
	id, err := f.record(ctx, call{method: "sendPhoto", chatID: chat, photo: photo, text: p.Caption, kb: markup(p.ReplyMarkup), replyTo: replyID(p.ReplyParameters)})
	if err != nil {
		return nil, err
	}
	return &models.Message{ID: id, Chat: models.Chat{ID: chat}, Caption: p.Caption, Photo: []models.PhotoSize{{FileID: "f"}}}, nil
}

func (f *fakeTG) EditMessageText(ctx context.Context, p *tgbot.EditMessageTextParams) (*models.Message, error) {
	_, err := f.record(ctx, call{method: "editMessageText", chatID: p.ChatID.(int64), msgID: p.MessageID, text: p.Text, kb: markup(p.ReplyMarkup)})
	return &models.Message{}, err
}

func (f *fakeTG) EditMessageCaption(ctx context.Context, p *tgbot.EditMessageCaptionParams) (*models.Message, error) {
	_, err := f.record(ctx, call{method: "editMessageCaption", chatID: p.ChatID.(int64), msgID: p.MessageID, text: p.Caption, kb: markup(p.ReplyMarkup)})
	return &models.Message{}, err
}

func (f *fakeTG) AnswerCallbackQuery(ctx context.Context, p *tgbot.AnswerCallbackQueryParams) (bool, error) {
	_, err := f.record(ctx, call{method: "answer", text: p.Text})
	return err == nil, err
}

func (f *fakeTG) SetMyCommands(ctx context.Context, _ *tgbot.SetMyCommandsParams) (bool, error) {
	_, err := f.record(ctx, call{method: "setMyCommands"})
	return err == nil, err
}

// rig is the bot on the real downloads module, with the fake sources
// "fake" and "other", a fake Telegram client and running jobs.
type rig struct {
	t     *testing.T
	h     *jobstest.Harness
	dl    *downloads.Module
	m     *Module
	tg    *fakeTG
	src   *fakesource.Source
	other *fakesource.Source
	reg   *source.Registry
	hub   *hub.Hub
	cfg   *config.Config
	stop  func()
	jobs  func()
	msgID int
}

func newRig(t *testing.T, extra ...source.Source) *rig {
	t.Helper()
	moscow, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		t.Fatal(err)
	}
	public, _ := url.Parse("https://shelf.test")
	r := &rig{t: t, h: jobstest.New(t, downloads.New()), tg: newFakeTG(), reg: source.NewRegistry(), hub: hub.New(),
		src: fakesource.NewSource("fake", "Fake"), other: fakesource.NewSource("other", "Other"),
		cfg: &config.Config{MusicDir: t.TempDir(), TZ: moscow, PublicURL: public,
			Telegram: config.Telegram{Token: "123:abc", Users: []int64{owner}}}}
	for _, s := range append([]source.Source{r.src, r.other}, extra...) {
		if err := r.reg.Add(s); err != nil {
			t.Fatal(err)
		}
	}
	r.build(r.h.Sys)
	r.jobs = r.h.Start(r.h.Sys)
	return r
}

// build makes the downloads module and the bot on sys and starts the bot,
// as a start of the process does.
func (r *rig) build(sys *jobs.System) {
	r.t.Helper()
	r.dl = downloads.New()
	r.m = New(r.dl.Service())
	r.m.Limits = unlimited()
	r.m.EditEvery = 0
	r.m.SearchWait = 2 * time.Second
	r.dl.Service().SetNotifier(r.m)
	d := module.Deps{Cfg: r.cfg, Log: dbtest.Discard, DB: r.h.DB, Jobs: sys, Hub: r.hub, Sources: r.reg, Closing: make(chan struct{})}
	for _, m := range []module.Initializer{r.dl, r.m} {
		if err := m.Init(d); err != nil {
			r.t.Fatal(err)
		}
	}
	r.dl.Service().Now = r.h.Clock.Now
	if err := sys.Register(downloads.Name, r.dl.JobTypes()...); err != nil {
		r.t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(bg)
	wait := r.m.serve(ctx, r.tg, &models.User{ID: 1, Username: "music_shelf_bot", IsBot: true})
	var once sync.Once
	r.stop = func() { once.Do(func() { cancel(); wait() }) }
	r.t.Cleanup(r.stop)
}

// restart stops the bot and the jobs system and starts new ones on the same
// database and fake Telegram, as a restart of the process does.
func (r *rig) restart() {
	r.t.Helper()
	r.stop()
	r.jobs()
	sys := r.h.NewSystem()
	r.h.Sys = sys
	r.build(sys)
	r.jobs = r.h.Start(sys)
}

func (r *rig) svc() *downloads.Service { return r.dl.Service() }

func (r *rig) message(from int64, text string) *models.Message {
	r.msgID++
	return &models.Message{ID: r.msgID, Chat: models.Chat{ID: from}, From: &models.User{ID: from, Username: "user" + itoa(from)}, Text: text}
}

// say sends text from the owner and waits for the bot's answer.
func (r *rig) say(text string) *models.Message { return r.sayAs(owner, text) }

func (r *rig) sayAs(from int64, text string) *models.Message {
	msg := r.message(from, text)
	r.m.b.handle(bg, &models.Update{Message: msg})
	return msg
}

// tap taps a button on a message the bot sent, as user from.
func (r *rig) tap(from int64, on call, data string) {
	msg := &models.Message{ID: on.msgID, Chat: models.Chat{ID: on.chatID}}
	if on.photo != "" {
		msg.Photo = []models.PhotoSize{{FileID: "f"}}
	}
	if on.replyTo != 0 {
		msg.ReplyToMessage = &models.Message{ID: on.replyTo}
	}
	r.m.b.handle(bg, &models.Update{CallbackQuery: &models.CallbackQuery{
		ID: "q", From: models.User{ID: from}, Data: data, Message: models.MaybeInaccessibleMessage{Message: msg},
	}})
}

func (r *rig) downloads() []downloads.Download {
	r.t.Helper()
	list, err := r.svc().List(bg, downloads.Filter{Limit: 200})
	if err != nil {
		r.t.Fatal(err)
	}
	return list
}

func (r *rig) get(id int64) downloads.Detail {
	r.t.Helper()
	d, err := r.svc().Get(bg, id)
	if err != nil {
		r.t.Fatal(err)
	}
	return d
}

func (r *rig) waitStatus(id int64, want downloads.Status) {
	r.t.Helper()
	r.h.WaitFor("download "+string(want), func() bool { return r.get(id).Status == want })
}

// waitEdit waits for an edit of msgID whose text contains want.
func (r *rig) waitEdit(msgID int, want string) call {
	r.t.Helper()
	var got call
	r.h.WaitFor("an edit with "+want, func() bool {
		for _, c := range r.tg.editsOf(msgID) {
			if strings.Contains(c.text, want) {
				got = c
				return true
			}
		}
		return false
	})
	return got
}

// gate holds a fake download at a track until released or its context ends.
type gate struct {
	reached chan struct{}
	release chan struct{}
}

func newGate() *gate { return &gate{reached: make(chan struct{}, 16), release: make(chan struct{})} }

func (g *gate) hook(ctx context.Context, _ source.Ref, _ int) error {
	g.reached <- struct{}{}
	select {
	case <-g.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (g *gate) waitReached(t *testing.T) {
	t.Helper()
	select {
	case <-g.reached:
	case <-time.After(30 * time.Second):
		t.Fatal("the download never reached the gate")
	}
}
