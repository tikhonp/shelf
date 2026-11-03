// Package telegram is the bot: the phone entry point to Music Shelf. A link
// queues a download and gets one status message that the bot edits while
// the download moves; plain text searches albums; /queue lists what runs.
// The module is the downloads module's notifier, which gives each status
// message its final text. docs/telegram.md describes the flow.
package telegram

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/labstack/echo/v5"
	"github.com/tikhonp/shelf/internal/modules/downloads"
	"github.com/tikhonp/shelf/internal/platform/config"
	"github.com/tikhonp/shelf/internal/platform/hub"
	"github.com/tikhonp/shelf/internal/platform/module"
	"github.com/tikhonp/shelf/internal/source"
)

// Name is the module's name.
const Name = "telegram"

// Downloads is the port to the downloads module. *downloads.Service
// implements it.
type Downloads interface {
	Queue(ctx context.Context, r downloads.QueueRequest) (downloads.Download, error)
	Get(ctx context.Context, id int64) (downloads.Detail, error)
	List(ctx context.Context, f downloads.Filter) ([]downloads.Download, error)
	Cancel(ctx context.Context, id int64, by string) error
	Retry(ctx context.Context, id int64, by string) (downloads.Download, error)
	Ahead(ctx context.Context, id int64) (int, error)
	DownloadedAt(ctx context.Context, ref source.Ref) (time.Time, error)
}

var _ Downloads = (*downloads.Service)(nil)

// Timings of the bot.
const (
	// EditEvery is the least time between two progress edits of one status
	// message. A state change edits at once.
	EditEvery = 5 * time.Second
	// lookupWait bounds the item lookup before a status message is sent.
	lookupWait = 10 * time.Second
	// SearchWait bounds a text search. A source that answers later is left
	// out.
	SearchWait = 10 * time.Second
	// notifyWait bounds the last edit, with its waits after a 429.
	notifyWait = time.Minute
	// searchLimit is how many albums each source adds to a search answer.
	searchLimit = 5
	// connectWait is the first pause after Telegram could not be reached. It
	// doubles up to connectWaitMax.
	connectWait    = 5 * time.Second
	connectWaitMax = 5 * time.Minute
)

// Module is the bot. With SHELF_TELEGRAM_TOKEN empty it is off.
type Module struct {
	// Limits, EditEvery and SearchWait are the defaults unless a test
	// changes them before Init.
	Limits     Limits
	EditEvery  time.Duration
	SearchWait time.Duration

	dl  Downloads
	cfg *config.Config
	hub *hub.Hub
	log *slog.Logger
	b   *bot

	handlers sync.WaitGroup
	// pollFailed is when long polling last failed, in Unix nanoseconds.
	pollFailed atomic.Int64
}

// New returns the module. dl is the downloads module's service.
func New(dl Downloads) *Module {
	return &Module{Limits: DefaultLimits(), EditEvery: EditEvery, SearchWait: SearchWait, dl: dl}
}

func (*Module) Name() string      { return Name }
func (*Module) Migrations() fs.FS { return nil }

// Init builds the bot. It reaches no network: manage runs Init too.
func (m *Module) Init(d module.Deps) error {
	m.cfg, m.hub, m.log = d.Cfg, d.Hub, d.Log
	users := map[int64]bool{}
	for _, id := range d.Cfg.Telegram.Users {
		users[id] = true
	}
	m.b = &bot{
		dl: m.dl, sources: d.Sources, queue: NewQueue(m.Limits, d.Log), users: users,
		public: d.Cfg.PublicURL, tz: d.Cfg.TZ, log: d.Log, lookupWait: lookupWait, searchWait: m.SearchWait,
		confirms: map[msgKey]confirmation{}, claimed: map[msgKey]bool{},
	}
	if m.b.tz == nil {
		m.b.tz = time.UTC
	}
	m.b.ed = newEditor(m.b, m.EditEvery)
	return nil
}

// Enabled reports whether a token was given.
func (m *Module) Enabled() bool { return m.cfg.Telegram.Enabled() }

// Run connects to Telegram and long-polls until ctx ends. Telegram that
// can't be reached is tried again with a growing pause; a token Telegram
// refuses stops the bot.
func (m *Module) Run(ctx context.Context) error {
	if !m.Enabled() {
		return nil
	}
	client, me, err := m.connect(ctx)
	if err != nil {
		// The platform ignores the error of a stop.
		return err
	}
	m.log.Info("telegram: connected", "bot", me.Username, "users", len(m.cfg.Telegram.Users))
	_, err = client.SetMyCommands(ctx, &tgbot.SetMyCommandsParams{Commands: []models.BotCommand{
		{Command: "queue", Description: commandQueue},
		{Command: "help", Description: commandHelp},
	}})
	logError(m.log, "set commands", err)
	wait := m.serve(ctx, client, me)
	client.Start(ctx)
	wait()
	return nil
}

func (m *Module) connect(ctx context.Context) (*tgbot.Bot, *models.User, error) {
	pause := connectWait
	for {
		client, err := newClient(m.cfg, m.dispatch, m.onPollError)
		if err != nil {
			return nil, nil, err
		}
		me, err := client.GetMe(ctx)
		if err == nil {
			return client, me, nil
		}
		if errors.Is(err, tgbot.ErrorUnauthorized) || errors.Is(err, tgbot.ErrorNotFound) {
			return nil, nil, errors.New("telegram: Telegram refuses SHELF_TELEGRAM_TOKEN; the bot is off")
		}
		logError(m.log, "can't reach Telegram", err, "retry_in", pause.String())
		select {
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		case <-time.After(pause):
		}
		pause = min(2*pause, connectWaitMax)
	}
}

func (m *Module) onPollError(err error) {
	m.pollFailed.Store(time.Now().UnixNano())
	logError(m.log, "polling", err)
}

// serve starts the bot on tg: the client for its calls and the editor on
// the hub's download events. The returned wait blocks until both have
// stopped after ctx ends.
func (m *Module) serve(ctx context.Context, tg TG, me *models.User) (wait func()) {
	m.b.setClient(tg, me)
	events, stop := m.hub.Subscribe(downloads.Topic)
	done := make(chan struct{})
	go func() {
		defer close(done)
		m.b.ed.run(ctx, events)
	}()
	return func() {
		<-ctx.Done()
		stop()
		<-done
		m.handlers.Wait()
	}
}

// dispatch is the client's handler. Each update gets its own goroutine, so
// a slow search or lookup doesn't delay other updates.
func (m *Module) dispatch(ctx context.Context, _ *tgbot.Bot, u *models.Update) {
	m.handlers.Go(func() { m.b.handle(ctx, u) })
}

// Notify is the downloads module's notifier: it gives a download's status
// message its final text.
func (m *Module) Notify(ctx context.Context, d downloads.Download) error { return m.b.notify(ctx, d) }

// Routes serves /api/telegram for the Status page.
func (m *Module) Routes(api *echo.Group) { api.GET("/telegram", m.status) }

// StatusJSON is the answer of GET /api/telegram.
type StatusJSON struct {
	Enabled bool `json:"enabled"`
	// Connected is false until Telegram took the token, and for pollGrace
	// after long polling failed.
	Connected bool   `json:"connected"`
	Username  string `json:"username"`
	Users     int    `json:"users"`
}

// pollGrace is how long a failed long poll keeps the bot shown as not
// connected.
const pollGrace = 2 * time.Minute

func (m *Module) status(c *echo.Context) error {
	out := StatusJSON{Enabled: m.Enabled(), Users: len(m.cfg.Telegram.Users)}
	if me := m.b.identity(); me != nil {
		out.Username = me.Username
		failed := m.pollFailed.Load()
		out.Connected = failed == 0 || time.Since(time.Unix(0, failed)) > pollGrace
	}
	return c.JSON(http.StatusOK, out)
}

// bot is the bot's logic, apart from the connection.
type bot struct {
	dl         Downloads
	sources    *source.Registry
	queue      *Queue
	ed         *editor
	users      map[int64]bool
	public     *url.URL
	tz         *time.Location
	log        *slog.Logger
	lookupWait time.Duration
	searchWait time.Duration

	mu       sync.Mutex
	tg       TG
	me       *models.User
	confirms map[msgKey]confirmation
	// claimed holds the messages that became a status message after a tap,
	// so a second tap on the same button queues nothing.
	claimed map[msgKey]bool
}

// errNotConnected is Notify's answer while the bot is off or not yet
// connected.
var errNotConnected = errors.New("telegram: the bot is not connected")

func (b *bot) setClient(tg TG, me *models.User) {
	b.mu.Lock()
	b.tg, b.me = tg, me
	b.mu.Unlock()
}

func (b *bot) client() TG {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.tg
}

func (b *bot) identity() *models.User {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.me
}

func (b *bot) allowed(userID int64) bool { return b.users[userID] }

// actor is the "by" of a cancel or retry from Telegram.
func actor(u models.User) string { return downloads.OriginTelegram + ":" + itoa(u.ID) }

// handle answers one update.
func (b *bot) handle(ctx context.Context, u *models.Update) {
	if b.client() == nil {
		return
	}
	switch {
	case u.Message != nil:
		b.handleMessage(ctx, u.Message)
	case u.CallbackQuery != nil:
		b.handleCallback(ctx, u.CallbackQuery)
	}
}

func (b *bot) handleMessage(ctx context.Context, msg *models.Message) {
	if msg.From == nil {
		return
	}
	if !b.allowed(msg.From.ID) {
		b.reply(ctx, msg, textPrivate, nil)
		return
	}
	text := strings.TrimSpace(msg.Text)
	if text == "" {
		text = strings.TrimSpace(msg.Caption)
	}
	if cmd, ok := command(text); ok {
		switch cmd {
		case "queue":
			b.listQueue(ctx, msg)
		default:
			b.reply(ctx, msg, textHelp, nil)
		}
		return
	}
	if ls := links(msg); len(ls) > 0 {
		b.handleLinks(ctx, msg, ls)
		return
	}
	if text == "" {
		b.reply(ctx, msg, textHelp, nil)
		return
	}
	b.search(ctx, msg, text)
}

// command reads "/queue" or "/queue@shelf_bot args" as "queue".
func command(text string) (string, bool) {
	if !strings.HasPrefix(text, "/") {
		return "", false
	}
	name, _, _ := strings.Cut(strings.Fields(text)[0][1:], "@")
	return strings.ToLower(name), true
}

// send sends a text message into chat, after the calls queued before it.
func (b *bot) send(ctx context.Context, chatID int64, replyTo int, text string, kb *models.InlineKeyboardMarkup) (*models.Message, error) {
	p := &tgbot.SendMessageParams{ChatID: chatID, Text: cut(text, maxText), LinkPreviewOptions: noPreview()}
	if kb != nil {
		p.ReplyMarkup = kb
	}
	if replyTo != 0 {
		p.ReplyParameters = &models.ReplyParameters{MessageID: replyTo, AllowSendingWithoutReply: true}
	}
	var sent *models.Message
	err := b.queue.Do(ctx, chatID, func(ctx context.Context) (err error) {
		sent, err = b.client().SendMessage(ctx, p)
		return err
	})
	return sent, err
}

// reply answers msg with text, and logs a failure.
func (b *bot) reply(ctx context.Context, msg *models.Message, text string, kb *models.InlineKeyboardMarkup) {
	_, err := b.send(ctx, msg.Chat.ID, msg.ID, text, kb)
	logError(b.log, "reply", err, "chat_id", msg.Chat.ID)
}

// answer answers a button tap. Telegram shows text briefly, or only stops
// the button's spinner when text is empty.
func (b *bot) answer(ctx context.Context, q *models.CallbackQuery, text string) {
	_, err := b.client().AnswerCallbackQuery(ctx, &tgbot.AnswerCallbackQueryParams{CallbackQueryID: q.ID, Text: text})
	logError(b.log, "answer a button", err)
}

// listQueue answers /queue: what runs, then what waits, one line each.
func (b *bot) listQueue(ctx context.Context, msg *models.Message) {
	list, err := b.dl.List(ctx, downloads.Filter{Statuses: []downloads.Status{downloads.Running, downloads.Queued}, Limit: 50})
	if err != nil {
		b.log.Error("telegram: list the queue", "error", err)
		b.reply(ctx, msg, answerBroken, nil)
		return
	}
	if len(list) == 0 {
		b.reply(ctx, msg, textQueueEmpty, nil)
		return
	}
	var running, queued []string
	for i := len(list) - 1; i >= 0; i-- { // oldest first
		d := list[i]
		line := b.name(d) + ": "
		if d.Status == downloads.Running {
			line += progressLine(d.TracksDone, d.TracksTotal, "track")
			running = append(running, line)
			continue
		}
		queued = append(queued, line+textQueued)
	}
	b.reply(ctx, msg, strings.Join(append(running, queued...), "\n"), nil)
}

// name is a download's title and artist, or its link before the title is
// known.
func (b *bot) name(d downloads.Download) string {
	switch {
	case d.Title != "" && d.Artist != "" && d.Artist != d.Title:
		return d.Title + ", " + d.Artist
	case d.Title != "":
		return d.Title
	case d.URL != "":
		return d.URL
	}
	return b.sourceName(d.Ref.Source) + " " + string(d.Ref.Kind) + " " + d.Ref.ID
}

func (b *bot) sourceName(id string) string {
	if s, ok := b.sources.Get(id); ok {
		return s.Name()
	}
	return id
}

// Telegram's limits on text and captions, in characters.
const (
	maxText    = 4096
	maxCaption = 1024
)

// cut shortens s to n characters, ending with an ellipsis when it cuts.
// Telegram counts UTF-16 units; runes are close enough below the limit.
func cut(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

func noPreview() *models.LinkPreviewOptions {
	off := true
	return &models.LinkPreviewOptions{IsDisabled: &off}
}
