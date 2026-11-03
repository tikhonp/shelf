package telegram

import (
	"context"
	"errors"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf16"

	"github.com/go-telegram/bot/models"
	"github.com/tikhonp/shelf/internal/modules/downloads"
	"github.com/tikhonp/shelf/internal/source"
)

// linkPattern finds http and https links that Telegram didn't mark, as in
// a message a test or another bot built.
var linkPattern = regexp.MustCompile(`https?://[^\s<>"'«»]+`)

// links finds the links in a message, in its text or a photo's caption:
// those Telegram marked, with or without a scheme, then any other http or
// https link.
func links(msg *models.Message) []string {
	text, entities := msg.Text, msg.Entities
	if text == "" {
		text, entities = msg.Caption, msg.CaptionEntities
	}
	var out []string
	seen := map[string]bool{}
	add := func(s string) {
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	// Entity offsets count UTF-16 units.
	units := utf16.Encode([]rune(text))
	for _, e := range entities {
		switch e.Type {
		case models.MessageEntityTypeURL:
			if e.Offset < 0 || e.Length <= 0 || e.Offset+e.Length > len(units) {
				continue
			}
			s := string(utf16.Decode(units[e.Offset : e.Offset+e.Length]))
			if !strings.Contains(s, "://") {
				s = "https://" + s
			}
			add(s)
		case models.MessageEntityTypeTextLink:
			add(e.URL)
		}
	}
	for _, s := range linkPattern.FindAllString(text, -1) {
		add(strings.TrimRight(s, ".,;:!?)]}"))
	}
	return out
}

// origin is who asked, and where.
type origin struct {
	chatID    int64
	user      models.User
	requestID int
}

func (o origin) ref(msgID int, isText bool) downloads.OriginRef {
	return downloads.OriginRef{ChatID: o.chatID, RequestMessageID: o.requestID, StatusMessageID: msgID,
		StatusIsText: isText, UserID: o.user.ID, Username: o.user.Username}
}

// handleLinks queues one download per distinct item the links name, and
// answers each link it can't use.
func (b *bot) handleLinks(ctx context.Context, msg *models.Message, raws []string) {
	o := origin{chatID: msg.Chat.ID, user: *msg.From, requestID: msg.ID}
	seen := map[source.Ref]bool{}
	for _, raw := range raws {
		ref, src, ok := b.sources.ParseLink(raw)
		if !ok {
			if _, refused := b.sources.RefusedLink(raw); refused {
				b.reply(ctx, msg, textUnsupported, nil)
			} else {
				b.reply(ctx, msg, textUnknownLink(b.linkSources()), nil)
			}
			continue
		}
		if seen[ref] {
			continue
		}
		seen[ref] = true
		if !source.IsEnabled(src) {
			b.reply(ctx, msg, textNotSetUp(src.Name()), nil)
			continue
		}
		if ref.Kind.Collection() {
			b.confirm(ctx, o, raw, ref, src)
			continue
		}
		b.queueItem(ctx, o, itemRequest{ref: ref, src: src, url: raw})
	}
}

// linkSources names every source whose links shelf knows, switched on or
// not.
func (b *bot) linkSources() []string {
	var names []string
	for _, s := range b.sources.All() {
		if _, ok := s.(source.LinkParser); ok {
			names = append(names, s.Name())
		}
	}
	return names
}

// itemRequest asks to queue one album, track or playlist, or an artist or
// label after its confirmation.
type itemRequest struct {
	ref    source.Ref
	src    source.Source
	url    string // the link, when the request came with one
	format string
	force  bool
	// reuse is a message that becomes the status message. Without it a new
	// one is sent.
	reuse *statusMsg
}

// queueItem sends the status message, queues the download with that
// message in its origin, then edits the message to the download's state.
// The message goes first so the download carries its ID from the start,
// and the notify step can always reach it.
func (b *bot) queueItem(ctx context.Context, o origin, r itemRequest) {
	m := r.reuse
	if m == nil {
		m = &statusMsg{chatID: o.chatID}
		cover := ""
		if it := b.lookup(ctx, r.src, r.ref); it != nil {
			m.title, m.artist, m.quality, cover = it.Title, it.Artist, it.Quality.Badge, it.CoverURL
		}
		v := view{det: downloads.Detail{Download: downloads.Download{Ref: r.ref, URL: r.url, Status: downloads.Queued}},
			title: m.title, artist: m.artist, quality: m.quality, source: r.src.Name(), tz: b.tz}
		var err error
		if m.msgID, m.isText, err = b.sendStatus(ctx, o.chatID, o.requestID, cover, v.text(), nil); err != nil {
			logError(b.log, "send a status message", err, "chat_id", o.chatID)
			return
		}
	}
	d, err := b.dl.Queue(ctx, downloads.QueueRequest{Ref: r.ref, URL: r.url, Format: r.format, Force: r.force,
		Origin: downloads.OriginTelegram, OriginRef: o.ref(m.msgID, m.isText)})
	if err != nil {
		b.log.Warn("telegram: queue", "ref", r.ref.String(), "error", err)
		b.editTo(ctx, o.chatID, m.msgID, m.isText, textQueueFailed(queueError(err)), keyboard())
		return
	}
	m.id, m.status = d.ID, d.Status
	m = b.ed.track(m)
	// The buttons need the download's ID, so the first edit adds them. A
	// skipped download has ended already and gets its last text here.
	logError(b.log, "edit a status message", b.update(ctx, m, d.Status == downloads.Skipped), "download", d.ID)
}

// queueError is why Queue refused, in words for the chat. The downloads
// module's own errors say it plainly; anything else stays in the log.
func queueError(err error) string {
	for _, known := range []error{downloads.ErrUnknownLink, downloads.ErrUnknownSource, downloads.ErrUnknownFormat, downloads.ErrBadRequest} {
		if errors.Is(err, known) {
			return strings.TrimPrefix(err.Error(), "downloads: ")
		}
	}
	return textSeeLog
}

// lookup reads the item for its status message. It gives up after
// lookupWait, and the message then shows the link until the download
// resolves.
func (b *bot) lookup(ctx context.Context, src source.Source, ref source.Ref) *source.Item {
	res, ok := src.(source.Resolver)
	if !ok {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, b.lookupWait)
	defer cancel()
	it, err := res.Item(ctx, ref)
	if err != nil {
		if !errors.Is(err, source.ErrNotFound) {
			b.log.Warn("telegram: look up", "ref", ref.String(), "error", err)
		}
		return nil
	}
	return &it
}

// msgKey names a message.
type msgKey struct {
	chatID int64
	msgID  int
}

// confirmation is what the bot remembers of an artist or label it asked
// about, so the status message shows the name before the download knows it.
type confirmation struct{ title, artist string }

// maxConfirms bounds the confirmations kept; an old one is only a name.
const maxConfirms = 100

// confirm asks before an artist or label download: "Download 34 albums by
// Example Artist?" Nothing is queued until Download all.
func (b *bot) confirm(ctx context.Context, o origin, raw string, ref source.Ref, src source.Source) {
	msg := &models.Message{ID: o.requestID, Chat: models.Chat{ID: o.chatID}}
	res, ok := src.(source.Resolver)
	if !ok {
		b.reply(ctx, msg, textLookupFailed(src.Name()), nil)
		return
	}
	lctx, cancel := context.WithTimeout(ctx, b.lookupWait)
	it, err := res.Item(lctx, ref)
	cancel()
	if errors.Is(err, source.ErrNotFound) {
		b.reply(ctx, msg, textNoSuch(src.Name(), string(ref.Kind)), nil)
		return
	}
	if err != nil {
		b.log.Warn("telegram: look up", "ref", ref.String(), "error", err)
		b.reply(ctx, msg, textLookupFailed(src.Name()), nil)
		return
	}
	n := albumCount(it)
	if n == 0 {
		b.reply(ctx, msg, textNoAlbums(first(it.Title, raw)), nil)
		return
	}
	data := dataAll + ref.String()
	if len(data) > maxData {
		b.log.Warn("telegram: an item ID too long for a button", "ref", ref.String())
		b.reply(ctx, msg, answerBroken, nil)
		return
	}
	row := []models.InlineKeyboardButton{callbackButton(buttonAll, data)}
	if b.public != nil {
		row = append(row, models.InlineKeyboardButton{Text: buttonOpen,
			URL: strings.TrimRight(b.public.String(), "/") + "/search?q=" + url.QueryEscape(raw)})
	}
	row = append(row, callbackButton(buttonNotNow, dataDismiss))
	text := cut(textConfirm(n, first(it.Title, raw), ref.Kind == source.KindLabel), maxCaption)
	msgID, _, err := b.sendStatus(ctx, o.chatID, o.requestID, it.CoverURL, text, keyboard(row))
	if err != nil {
		logError(b.log, "send a confirmation", err, "chat_id", o.chatID)
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.confirms) >= maxConfirms {
		for k := range b.confirms {
			delete(b.confirms, k)
			break
		}
	}
	b.confirms[msgKey{o.chatID, msgID}] = confirmation{title: it.Title, artist: it.Artist}
}

// takeConfirmation returns and forgets what the bot asked about in a
// message.
func (b *bot) takeConfirmation(k msgKey) confirmation {
	b.mu.Lock()
	defer b.mu.Unlock()
	c := b.confirms[k]
	delete(b.confirms, k)
	return c
}

// maxClaimed bounds the claimed messages. Telegram sends a double tap
// within a second, so forgetting old ones costs nothing.
const maxClaimed = 1000

// claim marks a message as turned into a status message. It reports false
// when a tap claimed it already.
func (b *bot) claim(k msgKey) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.claimed[k] {
		return false
	}
	if len(b.claimed) >= maxClaimed {
		clear(b.claimed)
	}
	b.claimed[k] = true
	return true
}

// albumCount counts what an artist or label download would queue: each
// album of every section once, as the downloads module expands it.
func albumCount(it source.Item) int {
	seen := map[string]bool{}
	for _, sec := range it.Sections {
		for _, x := range sec.Items {
			if !x.Ref.Kind.Collection() {
				seen[x.Ref.ID] = true
			}
		}
	}
	return len(seen)
}
