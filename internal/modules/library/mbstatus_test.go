package library

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tikhonp/shelf/internal/modules/library/musicbrainz"
)

func TestMusicBrainzStatus(t *testing.T) {
	r := newRig(t)
	var calls atomic.Int32
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)
	r.m.MB.Base, r.m.MB.Limit = srv.URL, musicbrainz.NewLimiter(0)
	ask := func() MusicBrainzJSON {
		t.Helper()
		var out MusicBrainzJSON
		r.getJSON("/api/library/musicbrainz", &out)
		return out
	}
	// stale makes the last probe old enough to ask again.
	stale := func() { r.m.mb.last.CheckedAt = time.Now().Add(-mbFresh) }

	if got := ask(); !got.OK || got.Message != "" || got.CheckedAt.IsZero() {
		t.Fatalf("answers: %+v", got)
	}
	// A fresh probe answers for the next caller.
	ask()
	if calls.Load() != 1 {
		t.Fatalf("%d calls", calls.Load())
	}

	status = http.StatusServiceUnavailable
	stale()
	if got := ask(); got.OK || got.Message != textMBBusy {
		t.Fatalf("busy: %+v", got)
	}
	status = http.StatusInternalServerError
	stale()
	if got := ask(); got.OK || got.Message != textMBFailing {
		t.Fatalf("failing: %+v", got)
	}
	srv.Close()
	stale()
	if got := ask(); got.OK || got.Message != textMBDown {
		t.Fatalf("down: %+v", got)
	}
}
