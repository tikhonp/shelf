package telegram

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/tikhonp/shelf/internal/platform/config"
)

func TestRateLimit(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m, tg := newPortBot(t, &fakePort{})
		calls := 0
		tg.setFail(func(context.Context, call) error {
			if calls++; calls == 1 {
				return &tgbot.TooManyRequestsError{Message: "too many requests", RetryAfter: 7}
			}
			return nil
		})
		start := time.Now()
		m.b.handle(bg, &models.Update{Message: &models.Message{ID: 1, Chat: models.Chat{ID: owner}, From: &models.User{ID: owner}, Text: "/help"}})
		sent := tg.of("sendMessage")
		if len(sent) != 1 || sent[0].at.Sub(start) != 7*time.Second {
			t.Fatalf("sent %+v", sent)
		}
	})
}

// The default limits send about one message every 3 seconds per chat,
// after a burst of 3.
func TestSendLimits(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m, tg := newPortBot(t, &fakePort{})
		m.b.queue = NewQueue(DefaultLimits(), m.log)
		start := time.Now()
		var wg sync.WaitGroup
		for i := range 5 {
			wg.Go(func() {
				m.b.handle(bg, &models.Update{Message: &models.Message{ID: i, Chat: models.Chat{ID: owner}, From: &models.User{ID: owner}, Text: "/help"}})
			})
			synctest.Wait()
		}
		wg.Wait()
		var at []time.Duration
		for _, c := range tg.of("sendMessage") {
			at = append(at, c.at.Sub(start))
		}
		want := []time.Duration{0, 0, 0, 3 * time.Second, 6 * time.Second}
		for i := range want {
			if i >= len(at) || at[i] != want[i] {
				t.Fatalf("sent at %v, want %v", at, want)
			}
		}
	})
}

// fakeAPI answers getMe, as Telegram or as a proxy in front of it, and
// records the hosts it was asked for.
type fakeAPI struct {
	srv   *httptest.Server
	mu    sync.Mutex
	hosts []string
}

func newFakeAPI(t *testing.T) *fakeAPI {
	f := &fakeAPI{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.hosts = append(f.hosts, r.Host)
		f.mu.Unlock()
		if !strings.HasSuffix(r.URL.Path, "/getMe") {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{"id":1,"is_bot":true,"first_name":"Shelf","username":"music_shelf_bot"}}`))
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeAPI) seen() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.hosts...)
}

func TestProxyOptIn(t *testing.T) {
	proxy, api := newFakeAPI(t), newFakeAPI(t)
	proxyURL, _ := url.Parse(proxy.srv.URL)
	nop := func(context.Context, *tgbot.Bot, *models.Update) {}

	// On: the bot's requests go through SHELF_PROXY_URL.
	cfg := &config.Config{Proxy: proxyURL, Telegram: config.Telegram{Token: "123:abc", UseProxy: true}}
	client, err := newClient(cfg, nop, func(error) {}, tgbot.WithServerURL("http://telegram.invalid"))
	if err != nil {
		t.Fatal(err)
	}
	me, err := client.GetMe(bg)
	if err != nil || me.Username != "music_shelf_bot" {
		t.Fatalf("getMe through the proxy: %v %+v", err, me)
	}
	if got := proxy.seen(); len(got) != 1 || got[0] != "telegram.invalid" {
		t.Fatalf("the proxy saw %q", got)
	}

	// Off: they go straight to Telegram, whatever HTTP_PROXY says.
	t.Setenv("HTTP_PROXY", proxy.srv.URL)
	cfg.Telegram.UseProxy = false
	client, err = newClient(cfg, nop, func(error) {}, tgbot.WithServerURL(api.srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.GetMe(bg); err != nil {
		t.Fatal(err)
	}
	if len(api.seen()) != 1 || len(proxy.seen()) != 1 {
		t.Fatalf("api saw %q, proxy saw %q", api.seen(), proxy.seen())
	}
}
