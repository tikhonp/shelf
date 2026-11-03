package source_test

import (
	"context"
	"strings"
	"testing"

	"github.com/tikhonp/shelf/internal/source"
)

func TestQualityWords(t *testing.T) {
	for _, tc := range []struct {
		q            source.Quality
		words, short string
	}{
		{source.Quality{Codec: "flac", BitDepth: 24, SampleRate: 96000}, "FLAC 24-bit 96 kHz", "FLAC 24/96"},
		{source.Quality{Codec: "flac", BitDepth: 16, SampleRate: 44100}, "FLAC 16-bit 44.1 kHz", "FLAC 16/44.1"},
		{source.Quality{Codec: "alac", BitDepth: 24, SampleRate: 192000}, "ALAC 24-bit 192 kHz", "ALAC 24/192"},
		{source.Quality{Codec: "mp3"}, "MP3", "MP3"},
	} {
		if got := tc.q.Words(); got != tc.words {
			t.Errorf("%+v Words = %q, want %q", tc.q, got, tc.words)
		}
		if got := tc.q.Short(); got != tc.short {
			t.Errorf("%+v Short = %q, want %q", tc.q, got, tc.short)
		}
	}
}

func TestQualityBetter(t *testing.T) {
	cd := source.Quality{Codec: "flac", BitDepth: 16, SampleRate: 44100}
	hi := source.Quality{Codec: "flac", BitDepth: 24, SampleRate: 96000}
	max := source.Quality{Codec: "flac", BitDepth: 24, SampleRate: 192000}
	mp3 := source.Quality{Codec: "mp3"}
	if !hi.Better(cd) || !max.Better(hi) || !cd.Better(mp3) || mp3.Better(cd) || hi.Better(hi) || !mp3.Better(source.Quality{}) {
		t.Fatal("wrong order")
	}
}

func TestParseRef(t *testing.T) {
	r, err := source.ParseRef("applemusic:album:pl.u-1:2")
	if err != nil || r != (source.Ref{Source: "applemusic", Kind: source.KindAlbum, ID: "pl.u-1:2"}) {
		t.Fatalf("%+v %v", r, err)
	}
	if r.String() != "applemusic:album:pl.u-1:2" {
		t.Fatal(r.String())
	}
	for _, bad := range []string{"", "qobuz:album", "qobuz:song:1", ":album:1", "qobuz:album:"} {
		if _, err := source.ParseRef(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

type fake struct {
	id string
	on bool
}

func (f fake) ID() string                           { return f.id }
func (f fake) Name() string                         { return strings.ToUpper(f.id) }
func (f fake) Health(context.Context) source.Health { return source.Health{OK: true} }
func (f fake) Enabled() bool                        { return f.on }
func (f fake) ParseLink(raw string) (source.Ref, bool) {
	if !strings.HasPrefix(raw, "https://"+f.id+"/") {
		return source.Ref{}, false
	}
	return source.Ref{Kind: source.KindAlbum, ID: strings.TrimPrefix(raw, "https://"+f.id+"/")}, true
}

func TestRegistry(t *testing.T) {
	r := source.NewRegistry()
	if err := r.Add(fake{"a", true}); err != nil {
		t.Fatal(err)
	}
	if err := r.Add(fake{"b", false}); err != nil {
		t.Fatal(err)
	}
	if err := r.Add(fake{"a", true}); err == nil {
		t.Fatal("a second source with one ID was accepted")
	}
	if _, ok := r.Enabled("b"); ok {
		t.Fatal("a switched-off source is enabled")
	}
	if _, ok := r.Get("b"); !ok {
		t.Fatal("a switched-off source is gone")
	}
	// A switched-off source still recognises its links.
	ref, s, ok := r.ParseLink("https://b/12")
	if !ok || s.ID() != "b" || ref != (source.Ref{Source: "b", Kind: source.KindAlbum, ID: "12"}) {
		t.Fatalf("%+v %v", ref, ok)
	}
	if _, _, ok := r.ParseLink("https://c/12"); ok {
		t.Fatal("a link no source knows was claimed")
	}
}
