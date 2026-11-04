package qobuz

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	qobuzdl "github.com/tikhonp/qobuz-dl-go"
	"github.com/tikhonp/qobuz-dl-go/qobuzdltest"
	"github.com/tikhonp/shelf/internal/modules/downloads"
	"github.com/tikhonp/shelf/internal/platform/config"
	"github.com/tikhonp/shelf/internal/platform/module"
	"github.com/tikhonp/shelf/internal/platform/testwait"
	"github.com/tikhonp/shelf/internal/source"
)

func recorded() downloads.Downloaded { return downloads.Downloaded{Path: "qobuz/elsewhere"} }

// ownClient points the module at the fake and leaves it its own HTTP client,
// which Configure would replace.
func ownClient(r *rig, seen func(*qobuzdl.Options)) {
	c := r.srv.ClientOptions()
	r.m.Options = func(o *qobuzdl.Options) {
		o.APIBase, o.PlayBase, o.Settings.UserAuthToken = c.APIBase, c.PlayBase, c.UserAuthToken
		o.TrackPause = 0
		if seen != nil {
			seen(o)
		}
	}
}

func TestHTTPClientHasNoTimeout(t *testing.T) {
	var client atomic.Pointer[http.Client]
	r := newRig(t, func(r *rig) { ownClient(r, func(o *qobuzdl.Options) { client.Store(o.HTTPClient) }) }).up()
	if client.Load() == nil || client.Load() != r.s.http {
		t.Fatal("the module's client wasn't used")
	}
	if client.Load().Timeout != 0 {
		t.Fatalf("Timeout = %v", client.Load().Timeout)
	}
	if _, _, err := r.download(source.KindAlbum, qobuzdltest.AlbumHiRes96, formatMax, false); err != nil {
		t.Fatal(err)
	}
}

func TestProxyOptIn(t *testing.T) {
	for _, tc := range []struct {
		name string
		use  bool
	}{{"on", true}, {"off", false}} {
		t.Run(tc.name, func(t *testing.T) {
			var through atomic.Int32
			var proxy *httptest.Server
			r := newRig(t, func(r *rig) {
				target, err := url.Parse(r.srv.URL)
				if err != nil {
					t.Fatal(err)
				}
				forward := httputil.NewSingleHostReverseProxy(target)
				proxy = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
					through.Add(1)
					forward.ServeHTTP(w, q)
				}))
				t.Cleanup(proxy.Close)
				r.cfg.Proxy, err = url.Parse(proxy.URL)
				if err != nil {
					t.Fatal(err)
				}
				r.cfg.Qobuz.UseProxy = tc.use
				ownClient(r, nil)
			}).up()
			if _, err := r.s.Search(bg, source.Query{Kind: source.KindAlbum, Text: "kind of blue"}); err != nil {
				t.Fatal(err)
			}
			h := r.s.Health(bg)
			if tc.use {
				if through.Load() == 0 {
					t.Error("no request went through the proxy")
				}
				if h.Message != "Studio, through the proxy" || h.Detail != (Detail{Label: "Studio", Proxy: true}) {
					t.Errorf("%+v", h)
				}
				return
			}
			if n := through.Load(); n != 0 {
				t.Errorf("%d requests went through the proxy", n)
			}
			if h.Message != "Studio" || h.Detail != (Detail{Label: "Studio"}) {
				t.Errorf("%+v", h)
			}
		})
	}
}

// expired fails the test unless err is the module's own "token expired".
func expired(t *testing.T, what string, err error) {
	t.Helper()
	var un *source.Unavailable
	if !errors.As(err, &un) || un.Reason != "token expired" {
		t.Errorf("%s: %v", what, err)
	}
}

func TestHealthTokenExpired(t *testing.T) {
	for _, tc := range []struct {
		name string
		call func(r *rig) error
	}{
		{"search", func(r *rig) error {
			_, err := r.s.Search(bg, source.Query{Kind: source.KindAlbum, Text: "kind of blue"})
			return err
		}},
		{"item", func(r *rig) error {
			_, err := r.s.Item(bg, ref(source.KindAlbum, qobuzdltest.AlbumCD))
			return err
		}},
		// The port returns no error here: the 401 is in Release.Err.
		{"album", func(r *rig) error {
			_, _, err := r.download(source.KindAlbum, qobuzdltest.AlbumCD, formatMax, false)
			return err
		}},
		{"track", func(r *rig) error {
			_, _, err := r.download(source.KindTrack, qobuzdltest.TrackSingle, formatMax, false)
			return err
		}},
		// Here the 401 is inside ErrUnhandled.
		{"playlist", func(r *rig) error {
			_, _, err := r.download(source.KindPlaylist, qobuzdltest.Playlist, formatMax, false)
			return err
		}},
		// A format not used yet logs in, and user/get answers the 401.
		{"new format", func(r *rig) error {
			_, _, err := r.download(source.KindAlbum, qobuzdltest.AlbumCD, "qobuz:6", false)
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t).up()
			events, stop := r.hub.Subscribe(SourceTopic)
			defer stop()
			r.srv.ExpireToken()
			expired(t, "first call", tc.call(r))
			if h := r.s.Health(bg); h.OK || h.Message != "token expired" {
				t.Fatalf("%+v", h)
			}
			select {
			case e := <-events:
				if got := e.Data.(SourceEvent); got != (SourceEvent{ID: ID, Message: "token expired"}) {
					t.Errorf("%+v", got)
				}
			case <-time.After(testwait.For(5 * time.Second)):
				t.Error("no source event")
			}

			// It stays that way without asking Qobuz again.
			r.srv.Reset()
			expired(t, "next call", tc.call(r))
			if n := len(r.srv.Requests()); n != 0 {
				t.Errorf("%d requests after the token expired", n)
			}
		})
	}
}

func TestExpiredTrackDuringAPlaylist(t *testing.T) {
	r := newRig(t).up()
	rec := newRecorder()
	var once atomic.Bool
	rec.onStart = func() {
		if once.CompareAndSwap(false, true) {
			r.srv.ExpireToken()
		}
	}
	_, err := r.s.Download(bg, r.request(source.KindPlaylist, qobuzdltest.Playlist, formatMax, false), rec)
	if err != nil {
		var un *source.Unavailable
		if !errors.As(err, &un) {
			t.Fatalf("%v", err)
		}
	}
	if h := r.s.Health(bg); h.Message != "token expired" {
		t.Fatalf("%+v", h)
	}
	var d source.TrackResult
	for pos := 2; pos <= 10; pos++ {
		if d = rec.last(pos); d.Err != nil {
			break
		}
	}
	expired(t, "a later track", d.Err)
}

func TestTokenExpiredAtStart(t *testing.T) {
	r := newRig(t)
	r.srv.ExpireToken()
	r.run()
	r.waitHealth(func(h source.Health) bool { return h.Message == "token expired" })
	_, err := r.s.Item(bg, ref(source.KindAlbum, qobuzdltest.AlbumCD))
	expired(t, "item", err)
}

func TestStartRetries(t *testing.T) {
	var down atomic.Bool
	down.Store(true)
	r := newRig(t, func(r *rig) {
		r.m.RetryEvery = time.Millisecond
		r.m.Options = func(o *qobuzdl.Options) {
			r.srv.Configure(o)
			if down.Load() {
				o.PlayBase = "http://127.0.0.1:1"
			}
		}
	})
	events, stop := r.hub.Subscribe(SourceTopic)
	defer stop()
	r.run()
	h := r.waitHealth(func(h source.Health) bool { return h.Message != "starting" })
	if h.OK || h.Message != textNoAnswer {
		t.Fatalf("%+v", h)
	}
	var un *source.Unavailable
	if _, err := r.s.Item(bg, ref(source.KindAlbum, qobuzdltest.AlbumCD)); !errors.As(err, &un) || un.Reason != textNoAnswer {
		t.Fatalf("%v", err)
	}
	down.Store(false)
	r.waitHealth(func(h source.Health) bool { return h.OK })
	if got := r.item(source.KindAlbum, qobuzdltest.AlbumCD); got.Title == "" {
		t.Fatalf("%+v", got)
	}
	// One event per change, however many starts failed.
	if e := (<-events).Data.(SourceEvent); e.OK || e.Message != textNoAnswer {
		t.Errorf("%+v", e)
	}
	if e := (<-events).Data.(SourceEvent); !e.OK || e.Message != "Studio" {
		t.Errorf("%+v", e)
	}
}

func TestHealthStartingAndInit(t *testing.T) {
	r := newRig(t, func(r *rig) { r.m.StartWait = time.Millisecond })
	if h := r.s.Health(bg); h.OK || h.Message != "starting" {
		t.Fatalf("%+v", h)
	}
	if !r.s.Enabled() {
		t.Fatal("switched off with a token")
	}
	// Init stays off the network and the disk, since manage runs it too.
	if n := len(r.srv.Requests()); n != 0 {
		t.Errorf("Init made %d requests", n)
	}
	if r.exists("qobuz") {
		t.Error("Init created the music folder")
	}
	var un *source.Unavailable
	if _, err := r.s.Item(bg, ref(source.KindAlbum, qobuzdltest.AlbumCD)); !errors.As(err, &un) || un.Reason != "starting" {
		t.Fatalf("%v", err)
	}
}

// A download resumed after a restart reaches the source while it logs in.
func TestCallsWaitForTheStart(t *testing.T) {
	hold, entered := make(chan struct{}), make(chan struct{})
	var once atomic.Bool
	r := newRig(t, func(r *rig) {
		r.m.Options = func(o *qobuzdl.Options) {
			r.srv.Configure(o)
			if once.CompareAndSwap(false, true) {
				close(entered)
				<-hold
			}
		}
	})
	r.run()
	<-entered
	got := make(chan error, 1)
	go func() {
		_, err := r.s.Item(bg, ref(source.KindAlbum, qobuzdltest.AlbumCD))
		got <- err
	}()
	select {
	case err := <-got:
		t.Fatalf("Item didn't wait: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(hold)
	if err := <-got; err != nil {
		t.Fatal(err)
	}

	// A caller that gives up doesn't wait for the rest.
	r = newRig(t)
	ctx, cancel := context.WithCancel(bg)
	cancel()
	if _, err := r.s.Item(ctx, ref(source.KindAlbum, qobuzdltest.AlbumCD)); !errors.Is(err, context.Canceled) {
		t.Fatalf("%v", err)
	}
}

func TestSwitchedOffWithoutToken(t *testing.T) {
	r := newRig(t, func(r *rig) { r.cfg.Qobuz.Token = "" })
	if r.s.Enabled() {
		t.Fatal("switched on")
	}
	if h := r.s.Health(bg); h.OK || h.Message != textOff {
		t.Fatalf("%+v", h)
	}
	if err := r.m.Run(bg); err != nil {
		t.Fatal(err)
	}
	var un *source.Unavailable
	if _, err := r.s.Item(bg, ref(source.KindAlbum, "1")); !errors.As(err, &un) {
		t.Fatalf("%v", err)
	}
	if _, ok := r.s.ParseLink("https://play.qobuz.com/album/1"); !ok {
		t.Fatal("a switched-off source must still know its links")
	}
	if n := len(r.srv.Requests()); n != 0 {
		t.Errorf("%d requests", n)
	}
}

func TestOptionsKeepTheBotsSettings(t *testing.T) {
	r := newRig(t, func(r *rig) {
		r.m.Options = nil
		r.cfg.Qobuz.Workers, r.cfg.Qobuz.Delay, r.cfg.Qobuz.Dir = 5, 2*time.Second, "music/qobuz"
		r.cfg.Qobuz.Token = config.Secret("the-token")
	})
	o := r.s.options(variant{quality: 7})
	if o.Quality != 7 || !o.QualityFallback || o.Settings.MaxWorkers != 5 || o.Settings.Delay != 2*time.Second ||
		o.Settings.UserAuthToken != "the-token" || o.Directory != filepath.Join(r.s.root, "music/qobuz") || o.HTTPClient != r.s.http {
		t.Fatalf("%+v", o)
	}
	if o.FolderFormat != "{artist} - {album} ({year}) [{bit_depth}B-{sampling_rate}kHz]" || o.TrackFormat != "{track_number} - {track_title}" ||
		o.EmbedArt || o.Settings.EmbedArt || o.SmartDiscography || o.NoM3UForPlaylists || o.IgnoreSinglesEPs || o.Settings.SavedArtSize != "org" {
		t.Fatalf("%+v", o)
	}
	if o.DownloadsDB == nil {
		t.Fatal("no database")
	}
	if o := r.s.options(variant{quality: 7, noDB: true}); o.DownloadsDB != nil {
		t.Fatal("a forced download has a database")
	}
}

func TestTokenStaysOutOfLogs(t *testing.T) {
	var logs bytes.Buffer
	r := newRig(t)
	r.m = New(r.history)
	r.m.Options = func(o *qobuzdl.Options) { r.srv.Configure(o) }
	log := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	if err := r.m.Init(module.Deps{Cfg: r.cfg, Log: log, Hub: r.hub}); err != nil {
		t.Fatal(err)
	}
	r.s = r.m.Source()
	r.up()
	rec, _, err := r.download(source.KindAlbum, qobuzdltest.AlbumCD, formatMax, false)
	if err != nil {
		t.Fatal(err)
	}
	r.srv.ExpireToken()
	_, _, err = r.download(source.KindAlbum, qobuzdltest.AlbumHiRes96, formatMax, false)
	all := logs.String() + strings.Join(rec.lines, "\n") + err.Error()
	if strings.Contains(all, qobuzdltest.Token) || strings.Contains(all, qobuzdltest.Secret) {
		t.Fatal("a secret reached the logs")
	}
}
