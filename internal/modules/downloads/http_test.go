package downloads

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/tikhonp/shelf/internal/source"
)

// sse reads server-sent events from a live response.
type sse struct {
	t  *testing.T
	sc *bufio.Scanner
}

type sseEvent struct {
	event, id, data string
	comment         bool
}

func (s *sse) next() sseEvent {
	s.t.Helper()
	var e sseEvent
	for s.sc.Scan() {
		line := s.sc.Text()
		switch {
		case line == "":
			if e.event != "" || e.data != "" || e.comment {
				return e
			}
		case strings.HasPrefix(line, ":"):
			e.comment = true
		case strings.HasPrefix(line, "event: "):
			e.event = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "id: "):
			e.id = strings.TrimPrefix(line, "id: ")
		case strings.HasPrefix(line, "data: "):
			e.data = strings.TrimPrefix(line, "data: ")
		}
	}
	s.t.Fatalf("stream ended: %v", s.sc.Err())
	return e
}

func (r *rig) stream(path string, header map[string]string) (*http.Response, *sse) {
	r.t.Helper()
	srv := httptest.NewServer(r.http())
	r.t.Cleanup(srv.Close)
	req, err := http.NewRequest(http.MethodGet, srv.URL+path, nil)
	if err != nil {
		r.t.Fatal(err)
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}
	client := &http.Client{Transport: &http.Transport{DisableKeepAlives: true}, Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		r.t.Fatal(err)
	}
	r.t.Cleanup(func() { _ = resp.Body.Close() })
	return resp, &sse{t: r.t, sc: bufio.NewScanner(resp.Body)}
}

func TestEventsStream(t *testing.T) {
	r := newRig(t)
	resp, s := r.stream("/api/events", nil)
	h := resp.Header
	if h.Get("Content-Type") != "text/event-stream" || h.Get("Cache-Control") != "no-store" || h.Get("X-Accel-Buffering") != "no" {
		t.Fatalf("headers %v", h)
	}
	if e := s.next(); !e.comment { // nothing has happened: a keepalive
		t.Fatalf("first %+v", e)
	}
	d := r.queue(QueueRequest{Ref: album("album-1")})
	r.hub.Publish("scan", map[string]bool{"scanning": true})
	r.hub.Publish("other", 1) // not a stream topic
	var got []sseEvent
	for len(got) < 2 {
		if e := s.next(); !e.comment {
			got = append(got, e)
		}
	}
	var ev Event
	if got[0].event != "download" || json.Unmarshal([]byte(got[0].data), &ev) != nil || ev.ID != d.ID || ev.Status != Queued || ev.ParentID != nil {
		t.Fatalf("%+v", got[0])
	}
	if got[1].event != "scan" || got[1].data != `{"scanning":true}` {
		t.Fatalf("%+v", got[1])
	}
	// The stream ends when serve stops, not only when the client leaves.
	close(r.closing)
	for s.sc.Scan() {
	}
}

func TestLogStreamResumes(t *testing.T) {
	r := newRig(t)
	r.start()
	d := r.queue(QueueRequest{Ref: album("album-1")})
	r.h.Drain()

	read := func(header map[string]string) []sseEvent {
		_, s := r.stream("/api/downloads/"+strconv.FormatInt(d.ID, 10)+"/log", header)
		var out []sseEvent
		for {
			e := s.next()
			if e.comment {
				continue
			}
			out = append(out, e)
			if e.event == "done" {
				return out
			}
		}
	}
	all := read(nil)
	if len(all) < 6 || all[0].event != "line" || all[len(all)-1].data != `{"state":"succeeded"}` {
		t.Fatalf("%+v", all)
	}
	var first LogLine
	if err := json.Unmarshal([]byte(all[0].data), &first); err != nil || first.Level != "step" || first.Text != "resolve" {
		t.Fatalf("%+v %v", first, err)
	}
	resumed := read(map[string]string{"Last-Event-ID": all[2].id})
	if len(resumed) != len(all)-3 || resumed[0].id != all[3].id {
		t.Fatalf("resumed after %s: %+v", all[2].id, resumed)
	}
	// A skipped download has no job and no log.
	sk := r.queue(QueueRequest{Ref: album("album-1")})
	_, s := r.stream("/api/downloads/"+strconv.FormatInt(sk.ID, 10)+"/log", nil)
	if e := s.next(); e.event != "done" || e.data != `{"state":"skipped"}` {
		t.Fatalf("%+v", e)
	}
	if rec := r.do(http.MethodGet, "/api/downloads/999/log", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("%d", rec.Code)
	}
}

func TestListPagesAndFilters(t *testing.T) {
	r := newRig(t)
	for i := range 5 {
		r.queue(QueueRequest{Ref: album("album-" + strconv.Itoa(i))})
	}
	if err := r.svc.Cancel(bg, 2, "web"); err != nil {
		t.Fatal(err)
	}
	var page listJSON
	rec := r.do(http.MethodGet, "/api/downloads?limit=2", nil)
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil || len(page.Downloads) != 2 || page.Downloads[0].ID != 5 ||
		page.Next == nil || *page.Next != 4 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	rec = r.do(http.MethodGet, "/api/downloads?limit=2&before=2", nil)
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil || len(page.Downloads) != 1 || page.Downloads[0].ID != 1 || page.Next != nil {
		t.Fatalf("%s", rec.Body)
	}
	rec = r.do(http.MethodGet, "/api/downloads?status=canceled,failed&source=fake", nil)
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil || len(page.Downloads) != 1 || page.Downloads[0].ID != 2 {
		t.Fatalf("%s", rec.Body)
	}
	for _, bad := range []string{"?status=lost", "?limit=0", "?limit=500", "?before=x"} {
		if rec := r.do(http.MethodGet, "/api/downloads"+bad, nil); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d", bad, rec.Code)
		}
	}
	rec = r.do(http.MethodGet, "/api/downloads/3", nil)
	var det detailJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &det); err != nil || det.ID != 3 || det.Tracks == nil || det.Children == nil {
		t.Fatalf("%s", rec.Body)
	}
	if rec := r.do(http.MethodGet, "/api/downloads/x", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("%d", rec.Code)
	}
}

func TestOriginUser(t *testing.T) {
	r := newRig(t)
	d := r.queue(QueueRequest{Ref: album("album-1"), Origin: OriginTelegram, OriginRef: OriginRef{UserID: 7, Username: "tikhon"}})
	var got downloadJSON
	rec := r.do(http.MethodGet, "/api/downloads/"+strconv.FormatInt(d.ID, 10), nil)
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got.Origin != OriginTelegram || got.OriginUser != "tikhon" {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
}

func TestStatusesMarkItems(t *testing.T) {
	r := newRig(t)
	r.start()
	done := r.queue(QueueRequest{Ref: album("album-1")})
	failed := r.queue(QueueRequest{Ref: album("album-6-fail")})
	r.h.Drain()
	r.stop()
	queued := r.queue(QueueRequest{Ref: album("album-2")})
	// A newer download that is under way wins over the library.
	again := r.queue(QueueRequest{Ref: album("album-1"), Force: true})
	got, err := r.svc.Statuses(bg, []source.Ref{album("album-1"), album("album-2"), album("album-6-fail"), album("album-9"), album("album-2")})
	if err != nil {
		t.Fatal(err)
	}
	if got[album("album-1")].State != "queued" || got[album("album-1")].DownloadID != again.ID ||
		got[album("album-2")].DownloadID != queued.ID || got[album("album-6-fail")].State != "partial" ||
		got[album("album-6-fail")].DownloadID != failed.ID || got[album("album-6-fail")].TracksDone != 2 || len(got) != 3 {
		t.Fatalf("%+v", got)
	}
	if err := r.svc.Cancel(bg, again.ID, "web"); err != nil {
		t.Fatal(err)
	}
	got, _ = r.svc.Statuses(bg, []source.Ref{album("album-1")})
	if got[album("album-1")] != (source.Status{State: "in_library", DownloadID: done.ID}) {
		t.Fatalf("%+v", got)
	}
}
