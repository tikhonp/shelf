package telegram

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/tikhonp/shelf/internal/modules/downloads"
	"github.com/tikhonp/shelf/internal/platform/hub"
	"github.com/tikhonp/shelf/internal/source"
)

// statusMsg is the message that shows one download.
type statusMsg struct {
	id     int64 // the download
	chatID int64
	msgID  int
	isText bool

	// The rest is the editor's, under editor.mu. title, artist and quality
	// come from the lookup before the message was sent; they fill in while
	// the download doesn't know them yet.
	title, artist, quality string

	status   downloads.Status
	lastEdit time.Time
	busy     bool // an edit waits in the send queue or runs
	again    bool // a change came in while busy
	urgent   bool // and it was a state change
	timer    *time.Timer
	sent     string // the text and buttons the message shows
}

// editor edits status messages while their downloads move: at most every
// EditEvery for progress, and at once for a state change. It keeps one edit
// per message in the send queue, and the edit reads the download's state
// when its turn comes, so the message never goes back to an older state.
// The last edit belongs to the notify step.
type editor struct {
	b     *bot
	every time.Duration
	edits sync.WaitGroup

	mu     sync.Mutex
	ctx    context.Context
	closed bool
	msgs   map[int64]*statusMsg
	// none holds downloads without a message of their own: from the web or
	// the shell, or an album of an artist download.
	none map[int64]bool
}

func newEditor(b *bot, every time.Duration) *editor {
	return &editor{b: b, every: every, ctx: context.Background(), msgs: map[int64]*statusMsg{}, none: map[int64]bool{}}
}

// run follows the hub's download events until ctx ends, then waits for the
// edits it started.
func (e *editor) run(ctx context.Context, events <-chan hub.Event) {
	e.mu.Lock()
	e.ctx = ctx
	e.mu.Unlock()
	defer func() {
		e.mu.Lock()
		e.closed = true
		for _, m := range e.msgs {
			if m.timer != nil {
				m.timer.Stop()
				m.timer = nil
			}
		}
		e.mu.Unlock()
		e.edits.Wait()
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-events:
			if !ok {
				return
			}
			if d, ok := ev.Data.(downloads.Event); ok {
				e.onEvent(ctx, d)
			}
		}
	}
}

func (e *editor) onEvent(ctx context.Context, ev downloads.Event) {
	m := e.find(ctx, ev.ID)
	if m == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	change := ev.Status != m.status
	m.status = ev.Status
	if ev.Status.Final() {
		return
	}
	e.schedule(m, change)
}

// find returns the download's message, and learns about a download from
// Telegram that this process didn't queue, as after a restart.
func (e *editor) find(ctx context.Context, id int64) *statusMsg {
	e.mu.Lock()
	m, ok := e.msgs[id]
	none := e.none[id]
	e.mu.Unlock()
	if ok {
		return m
	}
	if none {
		return nil
	}
	det, err := e.b.dl.Get(ctx, id)
	if err != nil {
		return nil
	}
	d, ref := det.Download, det.OriginRef
	if d.ParentID != 0 || d.Origin != downloads.OriginTelegram || ref.ChatID == 0 || ref.StatusMessageID == 0 {
		e.mu.Lock()
		e.none[id] = true
		e.mu.Unlock()
		return nil
	}
	if d.Status.Final() {
		return nil
	}
	// No status yet, so the first event edits at once, since the message may
	// show a state from before a restart.
	return e.track(&statusMsg{id: id, chatID: ref.ChatID, msgID: ref.StatusMessageID, isText: ref.StatusIsText})
}

// track starts following a message, or returns the one already followed.
func (e *editor) track(m *statusMsg) *statusMsg {
	e.mu.Lock()
	defer e.mu.Unlock()
	if old, ok := e.msgs[m.id]; ok && old.chatID == m.chatID && old.msgID == m.msgID {
		old.title, old.artist, old.quality = first(old.title, m.title), first(old.artist, m.artist), first(old.quality, m.quality)
		return old
	}
	delete(e.none, m.id)
	e.msgs[m.id] = m
	return m
}

// forget stops following a message after its last edit. A retry finds it
// again through its events.
func (e *editor) forget(id int64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if m, ok := e.msgs[id]; ok && m.timer != nil {
		m.timer.Stop()
		m.timer = nil
	}
	delete(e.msgs, id)
}

// schedule edits m now, or when EditEvery has passed since its last edit.
// It must hold e.mu.
func (e *editor) schedule(m *statusMsg, urgent bool) {
	if e.closed {
		return
	}
	if m.busy {
		m.again = true
		m.urgent = m.urgent || urgent
		return
	}
	if wait := e.every - time.Since(m.lastEdit); !urgent && wait > 0 {
		if m.timer == nil {
			m.timer = time.AfterFunc(wait, func() {
				e.mu.Lock()
				defer e.mu.Unlock()
				m.timer = nil
				e.schedule(m, true)
			})
		}
		return
	}
	if m.timer != nil {
		m.timer.Stop()
		m.timer = nil
	}
	m.busy = true
	ctx := e.ctx
	e.edits.Go(func() {
		err := e.b.update(ctx, m, false)
		logError(e.b.log, "edit a status message", err, "download", m.id)
		e.mu.Lock()
		defer e.mu.Unlock()
		m.busy = false
		if m.again {
			urgent := m.urgent
			m.again, m.urgent = false, false
			e.schedule(m, urgent)
		}
	})
}

// names returns what the lookup knew of m's item; track may fill it in.
func (e *editor) names(m *statusMsg) (title, artist, quality string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return m.title, m.artist, m.quality
}

func (e *editor) sentAs(m *statusMsg) string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return m.sent
}

func (e *editor) markSent(m *statusMsg, key string) {
	e.mu.Lock()
	m.sent, m.lastEdit = key, time.Now()
	e.mu.Unlock()
}

// update edits m to show its download. It runs in the chat's send queue and
// reads the download there, so of two edits the later always shows the
// newer state. final allows the last state, which belongs to the notify
// step; without it a download that has ended is left alone.
func (b *bot) update(ctx context.Context, m *statusMsg, final bool) error {
	return b.queue.Do(ctx, m.chatID, func(ctx context.Context) error {
		det, err := b.dl.Get(ctx, m.id)
		if err != nil {
			return err
		}
		if det.Status.Final() && !final {
			return nil
		}
		text, kb := b.render(ctx, m, det)
		key := text + "\x00" + keyOf(kb)
		if b.ed.sentAs(m) == key {
			return nil
		}
		if err := b.edit(ctx, m, text, kb); err != nil && !isNotModified(err) {
			return err
		}
		b.ed.markSent(m, key)
		return nil
	})
}

// notify gives a download's message its last text. The notify step calls it
// once; the downloads module's notified flag keeps a restart from calling
// it again.
func (b *bot) notify(ctx context.Context, d downloads.Download) error {
	if b.client() == nil {
		return errNotConnected
	}
	ref := d.OriginRef
	if ref.ChatID == 0 || ref.StatusMessageID == 0 {
		return nil
	}
	// The notify step runs on the source's queue, so a Telegram that doesn't
	// answer must not hold the next download for long.
	ctx, cancel := context.WithTimeout(ctx, notifyWait)
	defer cancel()
	m := b.ed.track(&statusMsg{id: d.ID, chatID: ref.ChatID, msgID: ref.StatusMessageID, isText: ref.StatusIsText})
	if err := b.update(ctx, m, true); err != nil {
		return err
	}
	b.ed.forget(d.ID)
	return nil
}

// edit changes a message's caption, or its text when it has no photo.
func (b *bot) edit(ctx context.Context, m *statusMsg, text string, kb *models.InlineKeyboardMarkup) error {
	if m.isText {
		_, err := b.client().EditMessageText(ctx, &tgbot.EditMessageTextParams{
			ChatID: m.chatID, MessageID: m.msgID, Text: text, ReplyMarkup: kb, LinkPreviewOptions: noPreview(),
		})
		return err
	}
	_, err := b.client().EditMessageCaption(ctx, &tgbot.EditMessageCaptionParams{
		ChatID: m.chatID, MessageID: m.msgID, Caption: text, ReplyMarkup: kb,
	})
	return err
}

// editTo replaces a message's text or caption and buttons, outside any
// download, such as a confirmation that was dismissed.
func (b *bot) editTo(ctx context.Context, chatID int64, msgID int, isText bool, text string, kb *models.InlineKeyboardMarkup) {
	m := &statusMsg{chatID: chatID, msgID: msgID, isText: isText}
	err := b.queue.Do(ctx, chatID, func(ctx context.Context) error {
		err := b.edit(ctx, m, text, kb)
		if isNotModified(err) {
			return nil
		}
		return err
	})
	logError(b.log, "edit a message", err, "chat_id", chatID)
}

// sendStatus sends a new message: the cover as a photo with text as its
// caption, or text alone when there is no cover or Telegram can't fetch it.
func (b *bot) sendStatus(ctx context.Context, chatID int64, replyTo int, cover, text string, kb *models.InlineKeyboardMarkup) (msgID int, isText bool, err error) {
	var reply *models.ReplyParameters
	if replyTo != 0 {
		reply = &models.ReplyParameters{MessageID: replyTo, AllowSendingWithoutReply: true}
	}
	err = b.queue.Do(ctx, chatID, func(ctx context.Context) error {
		if cover != "" {
			p := &tgbot.SendPhotoParams{ChatID: chatID, Photo: &models.InputFileString{Data: cover}, Caption: text, ReplyParameters: reply}
			if kb != nil {
				p.ReplyMarkup = kb
			}
			msg, err := b.client().SendPhoto(ctx, p)
			if err == nil {
				msgID, isText = msg.ID, false
				return nil
			}
			if _, limited := retryAfter(err); limited || ctx.Err() != nil {
				return err
			}
			logError(b.log, "send a cover", err, "chat_id", chatID)
			cover = "" // a retry after a 429 sends the text
		}
		p := &tgbot.SendMessageParams{ChatID: chatID, Text: text, ReplyParameters: reply, LinkPreviewOptions: noPreview()}
		if kb != nil {
			p.ReplyMarkup = kb
		}
		msg, err := b.client().SendMessage(ctx, p)
		if err != nil {
			return err
		}
		msgID, isText = msg.ID, true
		return nil
	})
	return msgID, isText, err
}

// render is a download's message: what it is, then where it stands, with
// the buttons for that state.
func (b *bot) render(ctx context.Context, m *statusMsg, det downloads.Detail) (string, *models.InlineKeyboardMarkup) {
	v := view{det: det, source: b.sourceName(det.Ref.Source), tz: b.tz, public: b.public}
	v.title, v.artist, v.quality = b.ed.names(m)
	switch det.Status {
	case downloads.Queued:
		v.ahead, _ = b.dl.Ahead(ctx, det.ID)
	case downloads.Skipped:
		v.since, _ = b.dl.DownloadedAt(ctx, det.Ref)
	}
	return v.text(), v.keyboard()
}

// view is what a status message shows.
type view struct {
	det                    downloads.Detail
	title, artist, quality string
	source                 string
	ahead                  int
	since                  time.Time
	tz                     *time.Location
	public                 *url.URL
}

// Part lengths that keep a caption under Telegram's limit.
const (
	maxName  = 200
	maxError = 300
)

func (v view) text() string {
	d := v.det
	title := first(d.Title, v.title)
	if title == "" {
		title = first(d.URL, v.source+" "+string(d.Ref.Kind)+" "+d.Ref.ID)
	}
	lines := []string{cut(title, maxName)}
	if artist := first(d.Artist, v.artist); artist != "" && artist != title {
		lines = append(lines, cut(artist, maxName))
	}
	src := v.source
	if v.quality != "" && !d.Ref.Kind.Collection() {
		src += ", " + v.quality
	}
	lines = append(lines, src, "", v.state())
	return cut(strings.Join(lines, "\n"), maxCaption)
}

// state is the line that says where the download stands.
func (v view) state() string {
	d := v.det
	noun, done, total := "track", d.TracksDone, d.TracksTotal
	if d.Ref.Kind.Collection() {
		noun, total = "album", len(v.det.Children)
		done = 0
		for _, c := range v.det.Children {
			if c.Status == downloads.Done || c.Status == downloads.Skipped {
				done++
			}
		}
	}
	switch d.Status {
	case downloads.Queued:
		if v.ahead > 0 {
			return textQueuedAhead(v.ahead)
		}
		return textQueued
	case downloads.Running:
		return progressLine(done, total, noun)
	case downloads.Done:
		if d.Ref.Kind.Collection() {
			if total == 0 {
				return textNothingToDownload()
			}
			return textDone(count(total, noun), size(d.Bytes))
		}
		return textDone(count(done, noun), delivered(v.det.Tracks).Words(), size(d.Bytes))
	case downloads.Partial:
		return textPartial(done, total, noun)
	case downloads.Failed:
		return textFailedWith(cut(d.Error, maxError))
	case downloads.Canceled:
		return textCanceled
	case downloads.Skipped:
		if v.since.IsZero() {
			return textAlready
		}
		return textAlreadyOn(v.since.In(v.tz).Format("2 Jan 2006"))
	}
	return string(d.Status)
}

// progressLine is "Downloading, 3 of 12 tracks", or "Downloading" before
// the count is known.
func progressLine(done, total int, noun string) string {
	if total == 0 {
		return textDownloading
	}
	return textProgress(done, total, noun)
}

// keyboard holds the buttons of a state, as docs/telegram.md lists them.
func (v view) keyboard() *models.InlineKeyboardMarkup {
	d := v.det
	var row []models.InlineKeyboardButton
	switch d.Status {
	case downloads.Queued, downloads.Running:
		row = append(row, callbackButton(buttonCancel, dataCancel+itoa(d.ID)))
	case downloads.Partial, downloads.Failed, downloads.Canceled:
		row = append(row, callbackButton(buttonRetry, dataRetry+itoa(d.ID)))
	case downloads.Skipped:
		row = append(row, callbackButton(buttonMissing, dataMissing+itoa(d.ID)))
	}
	if d.Status != downloads.Canceled {
		if u := v.openURL(); u != "" {
			row = append(row, models.InlineKeyboardButton{Text: buttonOpen, URL: u})
		}
	}
	return keyboard(row)
}

// openURL is the download's page in the UI, or empty without
// SHELF_PUBLIC_URL.
func (v view) openURL() string {
	if v.public == nil {
		return ""
	}
	return strings.TrimRight(v.public.String(), "/") + "/downloads/" + itoa(v.det.ID)
}

// delivered is the highest quality among the files written.
func delivered(tracks []downloads.Track) source.Quality {
	var best source.Quality
	for _, t := range tracks {
		q := source.Quality{Codec: t.Codec, BitDepth: t.BitDepth, SampleRate: t.SampleRate}
		if (t.Status == "done" || t.Status == "skipped") && q.Better(best) {
			best = q
		}
	}
	return best
}

// size is "1.2 GB", "340 MB" or "512 KB", or empty for nothing.
func size(n int64) string {
	if n <= 0 {
		return ""
	}
	units := []string{"B", "KB", "MB", "GB", "TB"}
	v, i := float64(n), 0
	for v >= 1000 && i < len(units)-1 {
		v /= 1000
		i++
	}
	if v < 10 && i > 0 {
		return strconv.FormatFloat(v, 'f', 1, 64) + " " + units[i]
	}
	return strconv.FormatFloat(v, 'f', 0, 64) + " " + units[i]
}

func keyOf(kb *models.InlineKeyboardMarkup) string {
	b, _ := json.Marshal(kb)
	return string(b)
}

func first(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
