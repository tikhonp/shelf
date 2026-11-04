package downloads

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/tikhonp/shelf/internal/platform/jobs"
	"github.com/tikhonp/shelf/internal/platform/web"
	"github.com/tikhonp/shelf/internal/source"
)

// downloadJSON is a download as the API answers it.
type downloadJSON struct {
	ID           int64  `json:"id"`
	ParentID     *int64 `json:"parent_id"`
	Source       string `json:"source"`
	Kind         string `json:"kind"`
	ItemID       string `json:"item_id"`
	URL          string `json:"url"`
	Format       string `json:"format"`
	Title        string `json:"title"`
	Artist       string `json:"artist"`
	CoverURL     string `json:"cover_url"`
	Status       Status `json:"status"`
	Error        string `json:"error"`
	TracksTotal  int    `json:"tracks_total"`
	TracksDone   int    `json:"tracks_done"`
	TracksFailed int    `json:"tracks_failed"`
	Bytes        int64  `json:"bytes"`
	Folder       string `json:"folder"`
	Origin       string `json:"origin"`
	// OriginUser is the Telegram user who asked, when the bot knows the name.
	OriginUser string     `json:"origin_user"`
	CreatedAt  time.Time  `json:"created_at"`
	StartedAt  *time.Time `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
}

type trackJSON struct {
	Position   int    `json:"position"`
	Disc       int    `json:"disc"`
	ItemID     string `json:"item_id"`
	Title      string `json:"title"`
	Artist     string `json:"artist"`
	ISRC       string `json:"isrc"`
	Status     string `json:"status"`
	Path       string `json:"path"`
	Codec      string `json:"codec"`
	BitDepth   int    `json:"bit_depth"`
	SampleRate int    `json:"sample_rate"`
	Size       int64  `json:"size"`
	Error      string `json:"error"`
}

type detailJSON struct {
	downloadJSON
	Tracks         []trackJSON    `json:"tracks"`
	Children       []downloadJSON `json:"children"`
	ProvenancePath string         `json:"provenance_path"`
}

type listJSON struct {
	Downloads []downloadJSON `json:"downloads"`
	// Next is the before cursor of the next page, or null on the last.
	Next *int64 `json:"next"`
}

func optTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

func toJSON(d Download) downloadJSON {
	j := downloadJSON{
		ID: d.ID, Source: d.Ref.Source, Kind: string(d.Ref.Kind), ItemID: d.Ref.ID, URL: d.URL, Format: d.Format,
		Title: d.Title, Artist: d.Artist, CoverURL: d.CoverURL, Status: d.Status, Error: d.Error,
		TracksTotal: d.TracksTotal, TracksDone: d.TracksDone, TracksFailed: d.TracksFailed, Bytes: d.Bytes,
		Folder: d.Folder, Origin: d.Origin, OriginUser: d.OriginRef.Username, CreatedAt: d.CreatedAt,
		StartedAt: optTime(d.StartedAt), FinishedAt: optTime(d.FinishedAt),
	}
	if d.ParentID != 0 {
		p := d.ParentID
		j.ParentID = &p
	}
	return j
}

func detailToJSON(det Detail) detailJSON {
	j := detailJSON{downloadJSON: toJSON(det.Download), Tracks: []trackJSON{}, Children: []downloadJSON{},
		ProvenancePath: det.ProvenancePath}
	for _, t := range det.Tracks {
		j.Tracks = append(j.Tracks, trackJSON(t))
	}
	for _, c := range det.Children {
		j.Children = append(j.Children, toJSON(c))
	}
	return j
}

// apiError maps the service's errors to answers. Their texts are shelf's
// own, so the client may read them.
func apiError(err error) error {
	switch {
	case errors.Is(err, ErrNotFound):
		return echo.NewHTTPError(http.StatusNotFound, "no such download")
	case errors.Is(err, ErrRefusedLink):
		return echo.NewHTTPError(http.StatusUnprocessableEntity, source.RefusedLinkText)
	case errors.Is(err, ErrUnknownLink), errors.Is(err, ErrUnknownSource), errors.Is(err, ErrUnknownFormat):
		return echo.NewHTTPError(http.StatusUnprocessableEntity, strings.TrimPrefix(err.Error(), "downloads: "))
	case errors.Is(err, ErrNotRetryable), errors.Is(err, ErrNotActive):
		return echo.NewHTTPError(http.StatusConflict, strings.TrimPrefix(err.Error(), "downloads: "))
	case errors.Is(err, ErrBadRequest):
		return echo.NewHTTPError(http.StatusBadRequest, strings.TrimPrefix(err.Error(), "downloads: "))
	}
	return err
}

func (m *Module) routes(api *echo.Group) {
	api.GET("/downloads", m.list)
	api.POST("/downloads", m.queue)
	api.GET("/downloads/:id", m.get)
	api.POST("/downloads/:id/cancel", m.cancel)
	api.POST("/downloads/:id/retry", m.retry)
	api.GET("/downloads/:id/log", m.logStream)
	api.GET("/events", m.events)
}

func idParam(c *echo.Context) (int64, error) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, echo.NewHTTPError(http.StatusNotFound, "no such download")
	}
	return id, nil
}

type queueBody struct {
	Ref    *source.Ref `json:"ref"`
	URL    string      `json:"url"`
	Format string      `json:"format"`
	Force  bool        `json:"force"`
}

func (m *Module) queue(c *echo.Context) error {
	var b queueBody
	if err := json.NewDecoder(c.Request().Body).Decode(&b); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the body is not JSON with ref or url")
	}
	b.URL = strings.TrimSpace(b.URL)
	if (b.Ref == nil) == (b.URL == "") {
		return echo.NewHTTPError(http.StatusBadRequest, "send either ref or url")
	}
	r := QueueRequest{URL: b.URL, Format: b.Format, Force: b.Force, Origin: OriginWeb}
	if b.Ref != nil {
		r.Ref = *b.Ref
	}
	d, err := m.svc.Queue(c.Request().Context(), r)
	if err != nil {
		return apiError(err)
	}
	return c.JSON(http.StatusCreated, toJSON(d))
}

func (m *Module) list(c *echo.Context) error {
	var f Filter
	if v := c.QueryParam("status"); v != "" {
		for _, st := range strings.Split(v, ",") {
			switch s := Status(strings.TrimSpace(st)); s {
			case Queued, Running, Done, Partial, Failed, Canceled, Skipped:
				f.Statuses = append(f.Statuses, s)
			default:
				return echo.NewHTTPError(http.StatusBadRequest, "unknown status "+strconv.Quote(st))
			}
		}
	}
	f.Source = c.QueryParam("source")
	if v := c.QueryParam("before"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			return echo.NewHTTPError(http.StatusBadRequest, "before must be a download ID")
		}
		f.Before = n
	}
	f.Limit = 50
	if v := c.QueryParam("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 200 {
			return echo.NewHTTPError(http.StatusBadRequest, "limit must be 1 to 200")
		}
		f.Limit = n
	}
	list, err := m.svc.List(c.Request().Context(), f)
	if err != nil {
		return err
	}
	out := listJSON{Downloads: make([]downloadJSON, len(list))}
	for i, d := range list {
		out.Downloads[i] = toJSON(d)
	}
	if len(list) == f.Limit {
		next := list[len(list)-1].ID
		out.Next = &next
	}
	return c.JSON(http.StatusOK, out)
}

func (m *Module) get(c *echo.Context) error {
	id, err := idParam(c)
	if err != nil {
		return err
	}
	det, err := m.svc.Get(c.Request().Context(), id)
	if err != nil {
		return apiError(err)
	}
	return c.JSON(http.StatusOK, detailToJSON(det))
}

func (m *Module) cancel(c *echo.Context) error {
	id, err := idParam(c)
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	if err := m.svc.Cancel(ctx, id, OriginWeb); err != nil {
		return apiError(err)
	}
	d, err := m.svc.load(ctx, id)
	if err != nil {
		return apiError(err)
	}
	return c.JSON(http.StatusOK, toJSON(d))
}

func (m *Module) retry(c *echo.Context) error {
	id, err := idParam(c)
	if err != nil {
		return err
	}
	d, err := m.svc.Retry(c.Request().Context(), id, OriginWeb)
	if err != nil {
		return apiError(err)
	}
	return c.JSON(http.StatusOK, toJSON(d))
}

// events streams the hub's download, scan, source, library and change
// events. A client that reconnects reloads what it shows, so nothing is
// replayed.
func (m *Module) events(c *echo.Context) error {
	ch, stop := m.svc.hub.Subscribe(Topic, "scan", "source", "library", "change")
	defer stop()
	st, err := web.OpenStream(c)
	if err != nil {
		return nil
	}
	keep := time.NewTicker(m.KeepAlive)
	defer keep.Stop()
	ctx := c.Request().Context()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-m.closing:
			return nil
		case e, ok := <-ch:
			if !ok || st.Send(e.Topic, "", e.Data) != nil {
				return nil
			}
		case <-keep.C:
			if st.Ping() != nil {
				return nil
			}
		}
	}
}

// LogLine is one line of the log stream.
type LogLine struct {
	Time  time.Time `json:"time"`
	Level string    `json:"level"`
	Step  string    `json:"step"`
	Text  string    `json:"text"`
}

const logPage = 200

// logStream sends the latest job's log lines with their row IDs as event
// IDs, so a reconnecting client resumes after Last-Event-ID, or after
// ?after= on a fresh connection. When the job has ended and every line is
// sent, it sends a done event; the client closes then, or EventSource would
// reconnect.
func (m *Module) logStream(c *echo.Context) error {
	id, err := idParam(c)
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	d, err := m.svc.load(ctx, id)
	if err != nil {
		return apiError(err)
	}
	after, _ := strconv.ParseInt(c.QueryParam("after"), 10, 64)
	if last, err := strconv.ParseInt(web.LastEventID(c), 10, 64); err == nil && last > after {
		after = last
	}
	st, err := web.OpenStream(c)
	if err != nil {
		return nil
	}
	if d.JobID == 0 {
		_ = st.Send("done", "", map[string]string{"state": string(d.Status)})
		return nil
	}
	watch, stopWatch := m.svc.jobs.Watch(d.JobID)
	defer stopWatch()
	keep := time.NewTicker(m.KeepAlive)
	defer keep.Stop()
	for {
		j, err := m.svc.jobs.Job(ctx, d.JobID)
		if errors.Is(err, jobs.ErrNotFound) {
			// Pruned by retention.
			_ = st.Send("done", "", map[string]string{"state": string(d.Status)})
			return nil
		}
		if err != nil {
			return nil
		}
		// The lines are read after the state: a job that ended had flushed
		// its last lines before it changed state.
		for {
			lines, err := m.svc.jobs.LogAfter(ctx, d.JobID, after, logPage)
			if err != nil {
				return nil
			}
			for _, l := range lines {
				if st.Send("line", strconv.FormatInt(l.ID, 10), LogLine{Time: l.Time.Time, Level: l.Level, Step: l.Step, Text: l.Text}) != nil {
					return nil
				}
				after = l.ID
			}
			if len(lines) < logPage {
				break
			}
		}
		switch j.State {
		case jobs.Succeeded, jobs.Failed, jobs.Cancelled:
			_ = st.Send("done", "", map[string]string{"state": string(j.State)})
			return nil
		}
		select {
		case <-ctx.Done():
			return nil
		case <-m.closing:
			return nil
		case <-watch:
		case <-keep.C:
			if st.Ping() != nil {
				return nil
			}
		}
	}
}
