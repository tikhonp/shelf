package telegram

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/tikhonp/shelf/internal/platform/config"
)

// TG is the part of the Bot API the bot uses. *tgbot.Bot implements it, and
// tests use a fake.
type TG interface {
	SendMessage(ctx context.Context, params *tgbot.SendMessageParams) (*models.Message, error)
	SendPhoto(ctx context.Context, params *tgbot.SendPhotoParams) (*models.Message, error)
	EditMessageText(ctx context.Context, params *tgbot.EditMessageTextParams) (*models.Message, error)
	EditMessageCaption(ctx context.Context, params *tgbot.EditMessageCaptionParams) (*models.Message, error)
	AnswerCallbackQuery(ctx context.Context, params *tgbot.AnswerCallbackQueryParams) (bool, error)
	SetMyCommands(ctx context.Context, params *tgbot.SetMyCommandsParams) (bool, error)
}

var _ TG = (*tgbot.Bot)(nil)

// allowedUpdates are the update kinds the bot asks for.
var allowedUpdates = tgbot.AllowedUpdates{models.AllowedUpdateMessage, models.AllowedUpdateCallbackQuery}

// pollTimeout is the long poll's length. The HTTP client's timeout matches
// it, as go-telegram/bot's own client does.
const pollTimeout = time.Minute

// httpClient is the bot's HTTP client. It goes through SHELF_PROXY_URL only
// when SHELF_TELEGRAM_USE_PROXY is on, and ignores the HTTP_PROXY
// variables, so each module chooses for itself.
func httpClient(cfg *config.Config) *http.Client {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.Proxy = nil
	if cfg.Telegram.UseProxy && cfg.Proxy != nil {
		t.Proxy = http.ProxyURL(cfg.Proxy)
	}
	return &http.Client{Timeout: pollTimeout, Transport: t}
}

// newClient builds a Bot API client that long-polls updates into handle once
// started. It reaches no network; GetMe checks the token.
func newClient(cfg *config.Config, handle tgbot.HandlerFunc, onPollError func(error), opts ...tgbot.Option) (*tgbot.Bot, error) {
	return tgbot.New(cfg.Telegram.Token.Reveal(), append([]tgbot.Option{
		tgbot.WithSkipGetMe(),
		tgbot.WithDefaultHandler(handle),
		tgbot.WithErrorsHandler(onPollError),
		tgbot.WithAllowedUpdates(allowedUpdates),
		// One worker that calls the handler in order. The handler hands
		// each update to its own goroutine, so a slow search holds nothing.
		tgbot.WithWorkers(1),
		tgbot.WithNotAsyncHandlers(),
		tgbot.WithHTTPClient(pollTimeout, httpClient(cfg)),
	}, opts...)...)
}

// retryAfter returns how long Telegram asked to wait when err is a 429.
func retryAfter(err error) (time.Duration, bool) {
	var tooMany *tgbot.TooManyRequestsError
	if !errors.As(err, &tooMany) {
		return 0, false
	}
	return time.Duration(max(tooMany.RetryAfter, 1)) * time.Second, true
}

// isNotModified reports whether an edit failed only because nothing changed.
func isNotModified(err error) bool {
	return errors.Is(err, tgbot.ErrorBadRequest) && strings.Contains(err.Error(), "message is not modified")
}

// logError logs a failed Bot API call. go-telegram/bot keeps the token out
// of its errors.
func logError(log *slog.Logger, what string, err error, args ...any) {
	if err == nil || errors.Is(err, context.Canceled) {
		return
	}
	log.Warn("telegram: "+what, append(args, "error", err)...)
}
