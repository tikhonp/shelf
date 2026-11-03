package hub_test

import (
	"testing"

	"github.com/tikhonp/shelf/internal/platform/hub"
)

func TestSubscribersGetTheirTopics(t *testing.T) {
	h := hub.New()
	downloads, stopDownloads := h.Subscribe("download")
	defer stopDownloads()
	all, stopAll := h.Subscribe()
	defer stopAll()

	h.Publish("download", 1)
	h.Publish("scan", 2)

	if e := <-downloads; e.Topic != "download" || e.Data != 1 {
		t.Fatalf("download subscriber got %+v", e)
	}
	select {
	case e := <-downloads:
		t.Fatalf("download subscriber got %+v", e)
	default:
	}
	if a, b := <-all, <-all; a.Data != 1 || b.Data != 2 {
		t.Fatalf("every-topic subscriber got %+v %+v", a, b)
	}
}

func TestSlowSubscriberDropsInsteadOfBlocking(t *testing.T) {
	h := hub.New()
	ch, stop := h.Subscribe("download")
	defer stop()
	for i := range hub.Buffer + 10 {
		h.Publish("download", i) // would block forever if Publish waited
	}
	if n := len(ch); n != hub.Buffer {
		t.Fatalf("%d buffered, want %d", n, hub.Buffer)
	}
	if e := <-ch; e.Data != 0 {
		t.Fatalf("first event %v: the oldest are kept, the newest dropped", e.Data)
	}
}

func TestUnsubscribeClosesTheChannel(t *testing.T) {
	h := hub.New()
	ch, stop := h.Subscribe()
	stop()
	stop() // twice is harmless
	if _, open := <-ch; open {
		t.Fatal("channel still open")
	}
	h.Publish("download", 1) // no subscriber left, no panic on a closed channel
}
