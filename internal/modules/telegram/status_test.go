package telegram

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/tikhonp/shelf/internal/modules/downloads"
	"github.com/tikhonp/shelf/internal/modules/fakesource"
	"github.com/tikhonp/shelf/internal/platform/config"
	"github.com/tikhonp/shelf/internal/platform/db/dbtest"
	"github.com/tikhonp/shelf/internal/platform/hub"
	"github.com/tikhonp/shelf/internal/platform/jobs"
	"github.com/tikhonp/shelf/internal/platform/module"
	"github.com/tikhonp/shelf/internal/source"
)

// fakePort is the downloads module with one download the test sets.
type fakePort struct {
	mu  sync.Mutex
	det downloads.Detail
}

func (p *fakePort) set(fn func(d *downloads.Detail)) {
	p.mu.Lock()
	fn(&p.det)
	p.mu.Unlock()
}

func (p *fakePort) Get(_ context.Context, id int64) (downloads.Detail, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if id != p.det.ID {
		return downloads.Detail{}, downloads.ErrNotFound
	}
	return p.det, nil
}

var errPort = errors.New("not in the fake")

func (p *fakePort) Queue(context.Context, downloads.QueueRequest) (downloads.Download, error) {
	return downloads.Download{}, errPort
}
func (p *fakePort) List(context.Context, downloads.Filter) ([]downloads.Download, error) {
	return nil, errPort
}
func (p *fakePort) Cancel(context.Context, int64, string) error { return errPort }
func (p *fakePort) Retry(context.Context, int64, string) (downloads.Download, error) {
	return downloads.Download{}, errPort
}
func (p *fakePort) Ahead(context.Context, int64) (int, error) { return 0, nil }
func (p *fakePort) DownloadedAt(context.Context, source.Ref) (time.Time, error) {
	return time.Time{}, downloads.ErrNotFound
}

// newPortBot is the bot on a fake port, without a database, so a test can
// run it in a synctest bubble.
func newPortBot(t *testing.T, port *fakePort) (*Module, *fakeTG) {
	t.Helper()
	reg := source.NewRegistry()
	if err := reg.Add(fakesource.NewSource("fake", "Fake")); err != nil {
		t.Fatal(err)
	}
	m := New(port)
	m.Limits = unlimited()
	cfg := &config.Config{TZ: time.UTC, Telegram: config.Telegram{Token: "123:abc", Users: []int64{owner}}}
	if err := m.Init(module.Deps{Cfg: cfg, Log: dbtest.Discard, Sources: reg}); err != nil {
		t.Fatal(err)
	}
	tg := newFakeTG()
	m.b.setClient(tg, &models.User{ID: 1, Username: "music_shelf_bot"})
	return m, tg
}

func TestEditThrottle(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		port := &fakePort{det: downloads.Detail{Download: downloads.Download{
			ID: 7, Ref: source.Ref{Source: "fake", Kind: source.KindAlbum, ID: "album-1"}, Title: "Low Tide Hymns",
			Artist: "Northern Static", Status: downloads.Queued, TracksTotal: 12, Origin: downloads.OriginTelegram,
			OriginRef: downloads.OriginRef{ChatID: owner, StatusMessageID: 500},
		}}}
		m, tg := newPortBot(t, port)
		events := make(chan hub.Event, hub.Buffer)
		ctx, cancel := context.WithCancel(bg)
		done := make(chan struct{})
		go func() {
			defer close(done)
			m.b.ed.run(ctx, events)
		}()
		start := time.Now()
		event := func(st downloads.Status, n int) {
			port.set(func(d *downloads.Detail) { d.Status, d.TracksDone = st, n })
			events <- hub.Event{Topic: downloads.Topic, Data: downloads.Event{ID: 7, Status: st, TracksDone: n, TracksTotal: 12}}
			synctest.Wait()
		}
		event(downloads.Queued, 0) // 0s: the first event edits at once
		time.Sleep(time.Second)
		event(downloads.Running, 0) // 1s: a state change edits at once
		time.Sleep(time.Second)
		event(downloads.Running, 1) // 2s: progress waits until 6s
		time.Sleep(time.Second)
		event(downloads.Running, 2) // 3s: still waiting
		time.Sleep(4 * time.Second)
		event(downloads.Running, 3) // 7s: waits until 11s
		time.Sleep(5 * time.Second)
		event(downloads.Running, 4) // 12s: waits until 16s
		time.Sleep(2 * time.Second)
		event(downloads.Done, 12) // 14s: the end is the notify step's, so the edit due at 16s doesn't happen
		time.Sleep(10 * time.Second)

		want := []struct {
			at   time.Duration
			line string
		}{{0, "Queued"}, {time.Second, "Downloading, 0 of 12 tracks"}, {6 * time.Second, "Downloading, 2 of 12 tracks"},
			{11 * time.Second, "Downloading, 3 of 12 tracks"}}
		edits := tg.editsOf(500)
		if len(edits) != len(want) {
			t.Fatalf("%d edits: %+v", len(edits), edits)
		}
		for i, w := range want {
			if at := edits[i].at.Sub(start); at != w.at || !strings.HasSuffix(edits[i].text, "\n\n"+w.line) {
				t.Errorf("edit %d at %v: %q, want %v: %q", i, at, edits[i].text, w.at, w.line)
			}
		}

		if err := m.Notify(ctx, port.det.Download); err != nil {
			t.Fatal(err)
		}
		if edits = tg.editsOf(500); !strings.HasSuffix(edits[len(edits)-1].text, "\n\nDone: 12 tracks") {
			t.Fatalf("last edit %q", edits[len(edits)-1].text)
		}
		cancel()
		<-done
	})
}

func TestFinalEditOnce(t *testing.T) {
	r := newRig(t)
	r.say("https://fake.test/album/album-1")
	first := r.tg.of("sendPhoto")[0]
	d := r.downloads()[0]
	r.h.Drain()
	r.waitEdit(first.msgID, "Done:")
	finals := func(msgID int) int {
		n := 0
		for _, c := range r.tg.editsOf(msgID) {
			if strings.Contains(c.text, "Done:") {
				n++
			}
		}
		return n
	}
	if n := finals(first.msgID); n != 1 {
		t.Fatalf("%d final edits", n)
	}

	// After a restart the notify step may run again. The notified flag stops
	// a second edit.
	r.restart()
	if _, err := r.h.Sys.EnqueueNow(bg, jobs.Request{Type: downloads.NotifyJobType,
		Payload: map[string]int64{"download_id": d.ID}, CreatedBy: "test"}); err != nil {
		t.Fatal(err)
	}
	r.h.Drain()
	if n := finals(first.msgID); n != 1 {
		t.Fatalf("%d final edits after a restart", n)
	}

	// shelf stops while the last edit waits on Telegram: the edit happens
	// once, after the restart.
	held := make(chan struct{}, 1)
	r.tg.setFail(func(ctx context.Context, c call) error {
		if c.method == "editMessageCaption" && strings.Contains(c.text, "Done:") {
			held <- struct{}{}
			<-ctx.Done()
			return ctx.Err()
		}
		return nil
	})
	r.say("https://fake.test/album/album-2")
	photos := r.tg.of("sendPhoto")
	second := photos[len(photos)-1]
	select {
	case <-held:
	case <-time.After(30 * time.Second):
		t.Fatal("the last edit never started")
	}
	r.restart()
	r.tg.setFail(nil)
	r.h.Drain()
	r.waitEdit(second.msgID, "Done:")
	id := r.downloads()[0].ID
	if n := finals(second.msgID); n != 1 || !r.get(id).Notified {
		t.Fatalf("%d final edits, notified %v", n, r.get(id).Notified)
	}
}

func TestNotModifiedIgnored(t *testing.T) {
	r := newRig(t)
	r.tg.setFail(func(_ context.Context, c call) error {
		if strings.HasPrefix(c.method, "edit") && strings.Contains(c.text, "Done:") {
			return fmt.Errorf("%w, %s", tgbot.ErrorBadRequest, "Bad Request: message is not modified: specified new message content "+
				"and reply markup are exactly the same as a current content and reply markup of the message")
		}
		return nil
	})
	r.say("https://fake.test/album/album-1")
	d := r.downloads()[0]
	r.h.Drain()
	if !r.get(d.ID).Notified {
		t.Fatal("a not-modified answer failed the last edit")
	}
	var warned int
	if err := r.h.DB.R.Get(&warned, `SELECT count(*) FROM job_log_lines WHERE text LIKE '%Telegram%'`); err != nil || warned != 0 {
		t.Fatalf("%d warnings, %v", warned, err)
	}
}

func TestStatusTexts(t *testing.T) {
	public, _ := url.Parse("https://shelf.test/")
	moscow, _ := time.LoadLocation("Europe/Moscow")
	album := source.Ref{Source: "qobuz", Kind: source.KindAlbum, ID: "a1"}
	artist := source.Ref{Source: "qobuz", Kind: source.KindArtist, ID: "r1"}
	hires := []downloads.Track{
		{Status: "done", Codec: "flac", BitDepth: 24, SampleRate: 96000},
		{Status: "skipped", Codec: "flac", BitDepth: 16, SampleRate: 44100},
		{Status: "failed"},
	}
	kids := func(done, other int) []downloads.Download {
		var out []downloads.Download
		for range done {
			out = append(out, downloads.Download{Status: downloads.Done})
		}
		for range other {
			out = append(out, downloads.Download{Status: downloads.Running})
		}
		return out
	}
	open := "Open in Shelf>https://shelf.test/downloads/7"
	cases := []struct {
		name    string
		d       downloads.Download
		tracks  []downloads.Track
		kids    []downloads.Download
		ahead   int
		since   time.Time
		line    string
		buttons []string
	}{
		{"queued", downloads.Download{Status: downloads.Queued}, nil, nil, 2, time.Time{}, "Queued, 2 ahead", []string{"Cancel=c:7", open}},
		{"queued first", downloads.Download{Status: downloads.Queued}, nil, nil, 0, time.Time{}, "Queued", []string{"Cancel=c:7", open}},
		{"resolving", downloads.Download{Status: downloads.Running}, nil, nil, 0, time.Time{}, "Downloading", []string{"Cancel=c:7", open}},
		{"running", downloads.Download{Status: downloads.Running, TracksDone: 3, TracksTotal: 12}, nil, nil, 0, time.Time{},
			"Downloading, 3 of 12 tracks", []string{"Cancel=c:7", open}},
		{"done", downloads.Download{Status: downloads.Done, TracksDone: 12, TracksTotal: 12, Bytes: 1_200_000_000}, hires, nil, 0, time.Time{},
			"Done: 12 tracks, FLAC 24-bit 96 kHz, 1.2 GB", []string{open}},
		{"partial", downloads.Download{Status: downloads.Partial, TracksDone: 11, TracksTotal: 12, TracksFailed: 1}, nil, nil, 0, time.Time{},
			"Done with errors: 11 of 12 tracks", []string{"Retry=r:7", open}},
		{"failed", downloads.Download{Status: downloads.Failed, Error: "Qobuz returned 401: token expired"}, nil, nil, 0, time.Time{},
			"Failed: Qobuz returned 401: token expired", []string{"Retry=r:7", open}},
		{"canceled", downloads.Download{Status: downloads.Canceled}, nil, nil, 0, time.Time{}, "Canceled", []string{"Retry=r:7"}},
		{"skipped", downloads.Download{Status: downloads.Skipped}, nil, nil, 0, time.Date(2026, 9, 30, 22, 30, 0, 0, time.UTC),
			"Already downloaded on 1 Oct 2026", []string{"Fetch missing=f:7", open}},
		{"artist running", downloads.Download{Ref: artist, Status: downloads.Running}, nil, kids(12, 22), 0, time.Time{},
			"Downloading, 12 of 34 albums", []string{"Cancel=c:7", open}},
		{"artist done", downloads.Download{Ref: artist, Status: downloads.Done, Bytes: 340_000_000}, nil, kids(34, 0), 0, time.Time{},
			"Done: 34 albums, 340 MB", []string{open}},
		{"artist partial", downloads.Download{Ref: artist, Status: downloads.Partial}, nil, kids(30, 4), 0, time.Time{},
			"Done with errors: 30 of 34 albums", []string{"Retry=r:7", open}},
		{"artist empty", downloads.Download{Ref: artist, Status: downloads.Done, Error: "nothing to download"}, nil, nil, 0, time.Time{},
			"Done: nothing to download", []string{open}},
	}
	for _, c := range cases {
		c.d.ID, c.d.Title, c.d.Artist = 7, "Low Tide Hymns", "Northern Static"
		if c.d.Ref.Kind == "" {
			c.d.Ref = album
		}
		v := view{det: downloads.Detail{Download: c.d, Tracks: c.tracks, Children: c.kids}, quality: "Hi-Res 24/96",
			source: "Qobuz", ahead: c.ahead, since: c.since, tz: moscow, public: public}
		head := "Low Tide Hymns\nNorthern Static\nQobuz, Hi-Res 24/96\n\n"
		if c.d.Ref.Kind.Collection() {
			head = "Low Tide Hymns\nNorthern Static\nQobuz\n\n"
		}
		if got := v.text(); got != head+c.line {
			t.Errorf("%s: %q", c.name, got)
		}
		if got := (call{kb: v.keyboard()}).buttons(); !slices.Equal(got, c.buttons) {
			t.Errorf("%s: buttons %q", c.name, got)
		}
	}

	// Without SHELF_PUBLIC_URL there is no Open in Shelf.
	v := view{det: downloads.Detail{Download: downloads.Download{ID: 7, Ref: album, Status: downloads.Done}}, source: "Qobuz", tz: moscow}
	if got := (call{kb: v.keyboard()}).buttons(); len(got) != 0 {
		t.Errorf("buttons without a public URL: %q", got)
	}
	// Before the lookup or the download knows the title, the link stands in.
	v.det.URL = "https://play.qobuz.com/album/a1"
	if got := v.text(); !strings.HasPrefix(got, "https://play.qobuz.com/album/a1\nQobuz\n\n") {
		t.Errorf("without a title: %q", got)
	}
}

func TestSizes(t *testing.T) {
	for n, want := range map[int64]string{0: "", 512: "512 B", 3072: "3.1 KB", 340_000_000: "340 MB", 1_200_000_000: "1.2 GB", 12_300_000_000: "12 GB"} {
		if got := size(n); got != want {
			t.Errorf("size(%d) = %q, want %q", n, got, want)
		}
	}
}
