package telegram

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/go-telegram/bot/models"
	"github.com/tikhonp/shelf/internal/modules/downloads"
	"github.com/tikhonp/shelf/internal/source"
)

// Callback data, under Telegram's limit of 64 bytes.
const (
	dataCancel  = "c:" // c:<download id>
	dataRetry   = "r:" // r:<download id>
	dataMissing = "f:" // f:<download id>, fetch what is missing, with force
	dataQueue   = "q:" // q:<source>:<kind>:<id>, an item from search results
	dataAll     = "a:" // a:<source>:<kind>:<id>, a confirmed artist or label
	dataDismiss = "x"  // dismiss a confirmation
	maxData     = 64
)

func callbackButton(text, data string) models.InlineKeyboardButton {
	return models.InlineKeyboardButton{Text: text, CallbackData: data}
}

// keyboard holds rows of buttons. With none, an edit removes the message's
// buttons.
func keyboard(rows ...[]models.InlineKeyboardButton) *models.InlineKeyboardMarkup {
	kb := &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{}}
	for _, r := range rows {
		if len(r) > 0 {
			kb.InlineKeyboard = append(kb.InlineKeyboard, r)
		}
	}
	return kb
}

// handleCallback acts on a button tap. It checks the user again, because a
// button stays in a chat that someone else may see.
func (b *bot) handleCallback(ctx context.Context, q *models.CallbackQuery) {
	if !b.allowed(q.From.ID) {
		b.answer(ctx, q, textPrivate)
		return
	}
	kind, rest, _ := strings.Cut(q.Data, ":")
	switch kind + ":" {
	case dataCancel:
		b.cancel(ctx, q, rest)
	case dataRetry:
		b.retry(ctx, q, rest)
	case dataMissing:
		b.again(ctx, q, rest)
	case dataQueue:
		b.queueFromSearch(ctx, q, rest)
	case dataAll:
		b.queueAll(ctx, q, rest)
	case dataDismiss + ":":
		b.dismiss(ctx, q)
	default:
		b.answer(ctx, q, "")
	}
}

// tapped returns the message with the tapped button, and its chat. The
// message is nil when Telegram no longer shows it to the bot.
func tapped(q *models.CallbackQuery) (*models.Message, int64) {
	if m := q.Message.Message; m != nil {
		return m, m.Chat.ID
	}
	if m := q.Message.InaccessibleMessage; m != nil {
		return nil, m.Chat.ID
	}
	return nil, q.From.ID
}

func downloadID(s string) (int64, bool) {
	id, err := strconv.ParseInt(s, 10, 64)
	return id, err == nil && id > 0
}

func (b *bot) cancel(ctx context.Context, q *models.CallbackQuery, arg string) {
	id, ok := downloadID(arg)
	if !ok {
		b.answer(ctx, q, answerBroken)
		return
	}
	err := b.dl.Cancel(ctx, id, actor(q.From))
	switch {
	case err == nil:
		b.answer(ctx, q, answerCancel)
	case errors.Is(err, downloads.ErrNotActive):
		b.answer(ctx, q, answerEnded)
	case errors.Is(err, downloads.ErrNotFound):
		b.answer(ctx, q, answerGone)
	default:
		b.log.Error("telegram: cancel", "download", id, "error", err)
		b.answer(ctx, q, answerBroken)
	}
}

func (b *bot) retry(ctx context.Context, q *models.CallbackQuery, arg string) {
	id, ok := downloadID(arg)
	if !ok {
		b.answer(ctx, q, answerBroken)
		return
	}
	d, err := b.dl.Retry(ctx, id, actor(q.From))
	switch {
	case err == nil:
	case errors.Is(err, downloads.ErrNotRetryable):
		b.answer(ctx, q, answerNotRetry)
		return
	case errors.Is(err, downloads.ErrNotFound):
		b.answer(ctx, q, answerGone)
		return
	default:
		b.log.Error("telegram: retry", "download", id, "error", err)
		b.answer(ctx, q, answerBroken)
		return
	}
	b.answer(ctx, q, answerRetry)
	ref := d.OriginRef
	if ref.ChatID == 0 || ref.StatusMessageID == 0 {
		return
	}
	// The message showed the end; it follows the download again.
	m := b.ed.track(&statusMsg{id: d.ID, chatID: ref.ChatID, msgID: ref.StatusMessageID, isText: ref.StatusIsText})
	logError(b.log, "edit a status message", b.update(ctx, m, false), "download", d.ID)
}

// again downloads a skipped item once more. The tapped message follows the
// new download.
func (b *bot) again(ctx context.Context, q *models.CallbackQuery, arg string) {
	id, ok := downloadID(arg)
	if !ok {
		b.answer(ctx, q, answerBroken)
		return
	}
	det, err := b.dl.Get(ctx, id)
	if errors.Is(err, downloads.ErrNotFound) {
		b.answer(ctx, q, answerGone)
		return
	}
	if err != nil {
		b.log.Error("telegram: read a download", "download", id, "error", err)
		b.answer(ctx, q, answerBroken)
		return
	}
	src, ok := b.enabled(ctx, q, det.Ref.Source)
	if !ok {
		return
	}
	b.answer(ctx, q, answerQueued)
	msg, chatID := tapped(q)
	o := origin{chatID: chatID, user: q.From, requestID: det.OriginRef.RequestMessageID}
	r := itemRequest{ref: det.Ref, src: src, url: det.URL, format: det.Format, force: true}
	if msg != nil {
		if !b.claim(msgKey{chatID, msg.ID}) {
			return
		}
		r.reuse = &statusMsg{chatID: chatID, msgID: msg.ID, isText: len(msg.Photo) == 0, title: det.Title, artist: det.Artist}
	}
	b.queueItem(ctx, o, r)
}

// queueFromSearch queues an album from search results. It gets a status
// message of its own; the results stay for another tap.
func (b *bot) queueFromSearch(ctx context.Context, q *models.CallbackQuery, arg string) {
	ref, err := source.ParseRef(arg)
	if err != nil {
		b.answer(ctx, q, answerBroken)
		return
	}
	src, ok := b.enabled(ctx, q, ref.Source)
	if !ok {
		return
	}
	b.answer(ctx, q, answerQueued)
	msg, chatID := tapped(q)
	o := origin{chatID: chatID, user: q.From}
	if msg != nil {
		o.requestID = msg.ID
	}
	b.queueItem(ctx, o, itemRequest{ref: ref, src: src})
}

// queueAll queues a confirmed artist or label. The confirmation becomes its
// status message, which counts the albums.
func (b *bot) queueAll(ctx context.Context, q *models.CallbackQuery, arg string) {
	ref, err := source.ParseRef(arg)
	if err != nil || !ref.Kind.Collection() {
		b.answer(ctx, q, answerBroken)
		return
	}
	src, ok := b.enabled(ctx, q, ref.Source)
	if !ok {
		return
	}
	b.answer(ctx, q, answerQueued)
	msg, chatID := tapped(q)
	o := origin{chatID: chatID, user: q.From}
	r := itemRequest{ref: ref, src: src}
	if msg != nil {
		if !b.claim(msgKey{chatID, msg.ID}) {
			return
		}
		c := b.takeConfirmation(msgKey{chatID, msg.ID})
		r.reuse = &statusMsg{chatID: chatID, msgID: msg.ID, isText: len(msg.Photo) == 0, title: c.title, artist: c.artist}
		if msg.ReplyToMessage != nil {
			o.requestID = msg.ReplyToMessage.ID
		}
	}
	b.queueItem(ctx, o, r)
}

// dismiss answers Cancel on a confirmation: nothing is queued.
func (b *bot) dismiss(ctx context.Context, q *models.CallbackQuery) {
	b.answer(ctx, q, "")
	msg, chatID := tapped(q)
	if msg == nil {
		return
	}
	b.takeConfirmation(msgKey{chatID, msg.ID})
	b.editTo(ctx, chatID, msg.ID, len(msg.Photo) == 0, textDismissed, keyboard())
}

// enabled finds a switched-on source, or tells the user it is off.
func (b *bot) enabled(ctx context.Context, q *models.CallbackQuery, id string) (source.Source, bool) {
	s, ok := b.sources.Get(id)
	if !ok {
		b.answer(ctx, q, answerBroken)
		return nil, false
	}
	if !source.IsEnabled(s) {
		b.answer(ctx, q, textNotSetUp(s.Name()))
		return nil, false
	}
	return s, true
}
