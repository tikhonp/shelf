// Package catalog is the catalog module: it lists the sources and searches
// and reads their catalogs for the UI, marking each item with what shelf
// knows of its downloads.
package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/tikhonp/shelf/internal/platform/module"
	"github.com/tikhonp/shelf/internal/source"
)

// Statuses is the port to the downloads module.
type Statuses interface {
	Statuses(ctx context.Context, refs []source.Ref) (map[source.Ref]source.Status, error)
}

// Module is the catalog module.
type Module struct {
	statuses Statuses
	sources  *source.Registry
	log      *slog.Logger
}

// New returns the module. statuses marks items with their download status.
func New(statuses Statuses) *Module { return &Module{statuses: statuses} }

func (*Module) Name() string      { return "catalog" }
func (*Module) Migrations() fs.FS { return nil }

// Init keeps the registry and the logger.
func (m *Module) Init(d module.Deps) error {
	m.sources, m.log = d.Sources, d.Log
	return nil
}

// Routes serves /api/sources, /api/search, /api/resolve and /api/items.
func (m *Module) Routes(api *echo.Group) {
	api.GET("/sources", m.listSources)
	api.GET("/search", m.search)
	api.POST("/resolve", m.resolve)
	api.GET("/items/:source/:kind/:id", m.item)
}

// healthWait bounds a source's Health, which should answer at once.
const healthWait = 2 * time.Second

// SourceJSON is one source in /api/sources.
type SourceJSON struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	Enabled      bool            `json:"enabled"`
	Capabilities Capabilities    `json:"capabilities"`
	Formats      []source.Format `json:"formats"`
	Health       source.Health   `json:"health"`
}

// Capabilities say which interfaces a source implements.
type Capabilities struct {
	Links    bool          `json:"links"`
	Search   []source.Kind `json:"search"`
	Resolve  bool          `json:"resolve"`
	Download bool          `json:"download"`
}

func (m *Module) listSources(c *echo.Context) error {
	out := []SourceJSON{}
	for _, s := range m.sources.All() {
		j := SourceJSON{ID: s.ID(), Name: s.Name(), Enabled: source.IsEnabled(s), Formats: []source.Format{},
			Capabilities: Capabilities{Search: []source.Kind{}}}
		if _, ok := s.(source.LinkParser); ok {
			j.Capabilities.Links = true
		}
		if sr, ok := s.(source.Searcher); ok {
			j.Capabilities.Search = sr.SearchKinds()
		}
		if _, ok := s.(source.Resolver); ok {
			j.Capabilities.Resolve = true
		}
		if dl, ok := s.(source.Downloader); ok {
			j.Capabilities.Download = true
			j.Formats = dl.Formats()
		}
		if j.Enabled {
			ctx, cancel := context.WithTimeout(c.Request().Context(), healthWait)
			j.Health = s.Health(ctx)
			cancel()
		} else {
			j.Health = source.Health{Message: s.Name() + " is not set up in Shelf"}
		}
		out = append(out, j)
	}
	return c.JSON(http.StatusOK, out)
}

// enabled finds a switched-on source, or answers 422.
func (m *Module) enabled(id string) (source.Source, error) {
	s, ok := m.sources.Get(id)
	if !ok {
		return nil, echo.NewHTTPError(http.StatusUnprocessableEntity, "no source "+strconv.Quote(id))
	}
	if !source.IsEnabled(s) {
		return nil, echo.NewHTTPError(http.StatusUnprocessableEntity, s.Name()+" is not set up in Shelf")
	}
	return s, nil
}

// Search limits.
const (
	defaultLimit = 20
	maxLimit     = 100
)

func (m *Module) search(c *echo.Context) error {
	id := c.QueryParam("source")
	if id == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "source is missing")
	}
	q := source.Query{Kind: source.Kind(c.QueryParam("type")), Text: strings.TrimSpace(c.QueryParam("q")), Limit: defaultLimit}
	if q.Text == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "q is empty")
	}
	if v := c.QueryParam("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > maxLimit {
			return echo.NewHTTPError(http.StatusBadRequest, "limit must be 1 to "+strconv.Itoa(maxLimit))
		}
		q.Limit = n
	}
	if v := c.QueryParam("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return echo.NewHTTPError(http.StatusBadRequest, "offset must be 0 or more")
		}
		q.Offset = n
	}
	s, err := m.enabled(id)
	if err != nil {
		return err
	}
	sr, ok := s.(source.Searcher)
	if !ok {
		return echo.NewHTTPError(http.StatusUnprocessableEntity, s.Name()+" can't search")
	}
	kinds := sr.SearchKinds()
	known := false
	for _, k := range kinds {
		known = known || k == q.Kind
	}
	if !known {
		names := make([]string, len(kinds))
		for i, k := range kinds {
			names[i] = string(k)
		}
		return echo.NewHTTPError(http.StatusBadRequest, "type must be one of "+strings.Join(names, ", "))
	}
	ctx := c.Request().Context()
	page, err := sr.Search(ctx, q)
	if err != nil {
		return m.sourceError(ctx, s, "search", err)
	}
	if page.Items == nil {
		page.Items = []source.Item{}
	}
	ptrs := make([]*source.Item, len(page.Items))
	for i := range page.Items {
		ptrs[i] = &page.Items[i]
	}
	m.mark(ctx, ptrs...)
	return c.JSON(http.StatusOK, page)
}

// sourceError hides a source's error from the client, which may hold a
// URL with a token, and logs it.
func (m *Module) sourceError(ctx context.Context, s source.Source, what string, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, source.ErrNotFound) {
		return echo.NewHTTPError(http.StatusNotFound, s.Name()+" has no such item")
	}
	if errors.Is(err, source.ErrRateLimited) {
		return echo.NewHTTPError(http.StatusTooManyRequests, s.Name()+" is slowing us down, try again in a minute")
	}
	var un *source.Unavailable
	if errors.As(err, &un) {
		return echo.NewHTTPError(http.StatusServiceUnavailable, s.Name()+": "+un.Reason)
	}
	m.log.Warn("catalog: "+what+" failed", "source", s.ID(), "error", err)
	return echo.NewHTTPError(http.StatusBadGateway, s.Name()+" "+what+" failed")
}

type resolveBody struct {
	URL string `json:"url"`
}

func (m *Module) resolve(c *echo.Context) error {
	var b resolveBody
	if err := json.NewDecoder(c.Request().Body).Decode(&b); err != nil || strings.TrimSpace(b.URL) == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "send {\"url\"}")
	}
	ref, s, ok := m.sources.ParseLink(strings.TrimSpace(b.URL))
	if !ok {
		if _, refused := m.sources.RefusedLink(strings.TrimSpace(b.URL)); refused {
			return echo.NewHTTPError(http.StatusUnprocessableEntity, source.RefusedLinkText)
		}
		return echo.NewHTTPError(http.StatusUnprocessableEntity, "link not from a known source")
	}
	return m.answerItem(c, s, ref)
}

func (m *Module) item(c *echo.Context) error {
	ref := source.Ref{Source: c.Param("source"), Kind: source.Kind(c.Param("kind")), ID: c.Param("id")}
	if !ref.Valid() {
		return echo.NewHTTPError(http.StatusNotFound, "no such kind of item")
	}
	s, ok := m.sources.Get(ref.Source)
	if !ok {
		return echo.NewHTTPError(http.StatusNotFound, "no source "+strconv.Quote(ref.Source))
	}
	return m.answerItem(c, s, ref)
}

func (m *Module) answerItem(c *echo.Context, s source.Source, ref source.Ref) error {
	if _, err := m.enabled(s.ID()); err != nil {
		return err
	}
	res, ok := s.(source.Resolver)
	if !ok {
		return echo.NewHTTPError(http.StatusUnprocessableEntity, s.Name()+" can't read items")
	}
	ctx := c.Request().Context()
	it, err := res.Item(ctx, ref)
	if err != nil {
		return m.sourceError(ctx, s, "lookup", err)
	}
	m.mark(ctx, &it)
	return c.JSON(http.StatusOK, it)
}

// mark sets the status of items, their tracks and their sections' items,
// in one lookup. A failing lookup leaves them unmarked: the catalog still
// answers.
func (m *Module) mark(ctx context.Context, items ...*source.Item) {
	if m.statuses == nil || len(items) == 0 {
		return
	}
	trackRef := func(it *source.Item, t source.Track) source.Ref {
		return source.Ref{Source: it.Ref.Source, Kind: source.KindTrack, ID: t.ID}
	}
	var refs []source.Ref
	for _, it := range items {
		refs = append(refs, it.Ref)
		for _, t := range it.Tracks {
			refs = append(refs, trackRef(it, t))
		}
		for _, sec := range it.Sections {
			for _, x := range sec.Items {
				refs = append(refs, x.Ref)
			}
		}
	}
	st, err := m.statuses.Statuses(ctx, refs)
	if err != nil {
		m.log.Warn("catalog: download statuses", "error", err)
		return
	}
	get := func(r source.Ref) *source.Status {
		if s, ok := st[r]; ok {
			return &s
		}
		return nil
	}
	for _, it := range items {
		it.Status = get(it.Ref)
		for i := range it.Tracks {
			it.Tracks[i].Status = get(trackRef(it, it.Tracks[i]))
		}
		for i := range it.Sections {
			for j := range it.Sections[i].Items {
				it.Sections[i].Items[j].Status = get(it.Sections[i].Items[j].Ref)
			}
		}
	}
}
