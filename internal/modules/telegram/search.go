package telegram

import (
	"context"
	"slices"
	"strconv"
	"strings"

	"github.com/go-telegram/bot/models"
	"github.com/tikhonp/shelf/internal/source"
)

// maxQuery bounds the text a search sends, and the answer repeats.
const maxQuery = 200

// maxButton bounds a result button's text; Telegram shortens long ones
// anyway.
const maxButton = 120

// search answers plain text with albums from every source that is on, up to
// five each, as buttons that queue them. Each source gets SearchWait; one
// that fails or answers later is left out.
func (b *bot) search(ctx context.Context, msg *models.Message, text string) {
	query := cut(strings.Join(strings.Fields(text), " "), maxQuery)
	var searchers []source.Source
	for _, s := range b.sources.All() {
		sr, ok := s.(source.Searcher)
		if ok && source.IsEnabled(s) && slices.Contains(sr.SearchKinds(), source.KindAlbum) {
			searchers = append(searchers, s)
		}
	}
	if len(searchers) == 0 {
		b.reply(ctx, msg, textNoSearch, nil)
		return
	}

	sctx, cancel := context.WithTimeout(ctx, b.searchWait)
	defer cancel()
	type result struct {
		i     int
		items []source.Item
		err   error
	}
	// Buffered, so a source that answers after the limit doesn't block.
	results := make(chan result, len(searchers))
	for i, s := range searchers {
		go func() {
			page, err := s.(source.Searcher).Search(sctx, source.Query{Kind: source.KindAlbum, Text: query, Limit: searchLimit})
			results <- result{i, page.Items, err}
		}()
	}
	found := make([][]source.Item, len(searchers))
	answered := 0
collect:
	for range searchers {
		select {
		case r := <-results:
			if r.err != nil {
				b.log.Warn("telegram: search", "source", searchers[r.i].ID(), "error", r.err)
				continue
			}
			answered++
			found[r.i] = r.items[:min(len(r.items), searchLimit)]
		case <-sctx.Done():
			b.log.Warn("telegram: a search took too long", "query_runes", len([]rune(query)))
			break collect
		}
	}
	if answered == 0 {
		b.reply(ctx, msg, textSearchDown, nil)
		return
	}

	var rows [][]models.InlineKeyboardButton
	for i, items := range found {
		for _, it := range items {
			if it.Ref.Source == "" {
				it.Ref.Source = searchers[i].ID()
			}
			data := dataQueue + it.Ref.String()
			if len(data) > maxData {
				b.log.Warn("telegram: an item ID too long for a button", "ref", it.Ref.String())
				continue
			}
			rows = append(rows, []models.InlineKeyboardButton{callbackButton(albumLabel(it, searchers[i].Name()), data)})
		}
	}
	if len(rows) == 0 {
		b.reply(ctx, msg, textNothingFound(query), nil)
		return
	}
	b.reply(ctx, msg, textSearchHeader(query), keyboard(rows...))
}

// albumLabel is a result button: "Low Tide Hymns, Northern Static, 2019,
// Qobuz Hi-Res 24/96".
func albumLabel(it source.Item, sourceName string) string {
	title := it.Title
	if it.Version != "" {
		title += " (" + it.Version + ")"
	}
	parts := []string{title}
	if it.Artist != "" {
		parts = append(parts, it.Artist)
	}
	if it.Year > 0 {
		parts = append(parts, strconv.Itoa(it.Year))
	}
	parts = append(parts, strings.TrimSpace(sourceName+" "+it.Quality.Badge))
	return cut(strings.Join(parts, ", "), maxButton)
}
