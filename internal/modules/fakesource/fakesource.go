// Package fakesource is a source with canned catalog data and fake
// downloads that write small files. Tests use it, and so does UI work
// without accounts, when SHELF_DEV_FAKE_SOURCE is on.
//
// Item IDs choose the behavior: an ID ending in -fail fails its second
// track, -slow sleeps 200 ms per track, -big has 40 tracks, an artist or
// label ID ending in -empty has no albums, and an ID starting with missing
// is not in the catalog. Links to /video/ and /station/ are refused, as a
// real source refuses music videos.
package fakesource

import (
	"context"
	"io/fs"
	"net/url"
	"strings"
	"sync/atomic"

	"github.com/tikhonp/shelf/internal/source"
)

// ID is the fake source's ID.
const ID = "fake"

// Module registers one fake source.
type Module struct{ src *Source }

// New returns the module with the source "fake".
func New() *Module { return &Module{src: NewSource(ID, "Fake")} }

func (*Module) Name() string               { return "fakesource" }
func (*Module) Migrations() fs.FS          { return nil }
func (m *Module) Sources() []source.Source { return []source.Source{m.src} }
func (m *Module) Source() *Source          { return m.src }

// Source is a fake source. Its links are https://<id>.test/<kind>/<item id>.
type Source struct {
	id, name string
	// Off registers the source switched off.
	Off bool
	// Hook, when set, runs before each track a download writes. Tests use it
	// to hold a download at a track or to see its context end. An error
	// fails the download.
	Hook func(ctx context.Context, ref source.Ref, position int) error

	items atomic.Int32
}

// NewSource returns a fake source with its own ID, so tests can register
// two of them.
func NewSource(id, name string) *Source { return &Source{id: id, name: name} }

func (s *Source) ID() string    { return s.id }
func (s *Source) Name() string  { return s.name }
func (s *Source) Enabled() bool { return !s.Off }

// Health is always fine.
func (s *Source) Health(context.Context) source.Health {
	return source.Health{OK: true, Message: "Canned data"}
}

// ItemCalls counts calls to Item.
func (s *Source) ItemCalls() int { return int(s.items.Load()) }

// Link is the link of an item.
func (s *Source) Link(kind source.Kind, id string) string {
	return "https://" + s.id + ".test/" + string(kind) + "/" + url.PathEscape(id)
}

// RefuseLink knows https://<id>.test/video/<id> and /station/<id>, which
// stand for links a real source can't download.
func (s *Source) RefuseLink(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host != s.id+".test" {
		return false
	}
	kind, id, ok := strings.Cut(strings.Trim(u.Path, "/"), "/")
	return ok && id != "" && (kind == "video" || kind == "station")
}

// ParseLink claims https://<id>.test/<kind>/<item id>.
func (s *Source) ParseLink(raw string) (source.Ref, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host != s.id+".test" {
		return source.Ref{}, false
	}
	kind, id, ok := strings.Cut(strings.Trim(u.Path, "/"), "/")
	if !ok || id == "" || strings.Contains(id, "/") || !source.Kind(kind).Valid() {
		return source.Ref{}, false
	}
	return source.Ref{Source: s.id, Kind: source.Kind(kind), ID: id}, true
}
