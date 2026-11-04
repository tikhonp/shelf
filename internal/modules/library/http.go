package library

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/tikhonp/shelf/internal/modules/library/tagio"
)

func (m *Module) routes(api *echo.Group) {
	api.GET("/library/index", m.getIndex)
	api.POST("/library/index", m.postIndex)
	api.GET("/library/filters", m.filters)
	api.GET("/library/albums", m.list)
	api.GET("/library/albums/:id", m.get)
	api.GET("/library/tracks/:id/audio", m.audio)
	api.POST("/library/albums/:id/save", m.save)
	api.GET("/library/changes/:id", m.getChange)
	api.POST("/library/changes/:id/undo", m.undo)
	api.POST("/library/changes/:id/redo", m.redo)
	api.GET("/library/history", m.history)
	api.DELETE("/library/history", m.clearHistory)
}

func (m *Module) getIndex(c *echo.Context) error { return c.JSON(http.StatusOK, m.ix.status()) }

func (m *Module) postIndex(c *echo.Context) error {
	m.Kick()
	return c.JSON(http.StatusAccepted, m.ix.status())
}

// albumJSON is an album as the list shows it.
type albumJSON struct {
	ID       int64   `json:"id"`
	Folder   string  `json:"folder"`
	Title    string  `json:"title"`
	Artist   string  `json:"artist"`
	Year     int     `json:"year"`
	Tracks   int     `json:"tracks"`
	Quality  string  `json:"quality"`
	Duration float64 `json:"duration"` // seconds
	Size     int64   `json:"size"`
	AddedAt  string  `json:"added_at"`
	// NavidromeID is the album's ID in Navidrome.
	NavidromeID string `json:"navidrome_id"`
	HasMBID     bool   `json:"has_mbid"`
	// Lamp is true when the album has an issue that isn't style only.
	Lamp   bool    `json:"lamp"`
	Issues []Issue `json:"issues"`
}

func toJSON(r albumRow) albumJSON {
	j := albumJSON{ID: r.ID, Folder: r.Folder, Title: r.Title, Artist: r.Artist, Year: r.Year, Tracks: r.Tracks,
		Quality: r.Quality, Duration: float64(r.LengthMS) / 1000, Size: r.Size,
		AddedAt: r.AddedAt.UTC().Format(time.RFC3339), NavidromeID: r.NDID, HasMBID: r.HasMBID, Lamp: r.Lamp}
	if err := json.Unmarshal([]byte(r.Issues), &j.Issues); err != nil || j.Issues == nil {
		j.Issues = []Issue{}
	}
	return j
}

type listJSON struct {
	Albums []albumJSON `json:"albums"`
	Total  int         `json:"total"`
	Limit  int         `json:"limit"`
	Offset int         `json:"offset"`
}

func (m *Module) list(c *echo.Context) error {
	q := listQuery{Search: strings.TrimSpace(c.QueryParam("q")), Folder: strings.Trim(c.QueryParam("folder"), "/"),
		Sort: "album", Limit: 100}
	switch f := c.QueryParam("filter"); {
	case f == "" || f == "all":
	case f == "issues" || f == "guide" || f == "no_mbid" || slices.Contains(Kinds, Kind(f)):
		q.Filter = f
	default:
		return echo.NewHTTPError(http.StatusBadRequest, "unknown filter "+strconv.Quote(f))
	}
	switch s := c.QueryParam("sort"); s {
	case "":
	case "album", "artist", "year", "added":
		q.Sort = s
	default:
		return echo.NewHTTPError(http.StatusBadRequest, "sort must be album, artist, year or added")
	}
	switch o := c.QueryParam("order"); o {
	case "", "asc":
	case "desc":
		q.Desc = true
	default:
		return echo.NewHTTPError(http.StatusBadRequest, "order must be asc or desc")
	}
	var err error
	if q.Limit, err = intParam(c, "limit", 100, 1, 500); err != nil {
		return err
	}
	if q.Offset, err = intParam(c, "offset", 0, 0, 1<<30); err != nil {
		return err
	}
	rows, total, err := m.store.albums(c.Request().Context(), q)
	if err != nil {
		return err
	}
	out := listJSON{Albums: make([]albumJSON, len(rows)), Total: total, Limit: q.Limit, Offset: q.Offset}
	for i, r := range rows {
		out.Albums[i] = toJSON(r)
	}
	return c.JSON(http.StatusOK, out)
}

func intParam(c *echo.Context, name string, def, lo, hi int) (int, error) {
	v := c.QueryParam(name)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < lo || n > hi {
		return 0, echo.NewHTTPError(http.StatusBadRequest, name+" must be "+strconv.Itoa(lo)+" to "+strconv.Itoa(hi))
	}
	return n, nil
}

type countJSON struct {
	Name   string `json:"name"`
	Albums int    `json:"albums"`
}

// filtersJSON is what the filter menu offers, with each entry's albums.
type filtersJSON struct {
	Albums  int         `json:"albums"`
	Issues  int         `json:"issues"`
	NoMBID  int         `json:"no_mbid"`
	Kinds   []countJSON `json:"kinds"`
	Folders []countJSON `json:"folders"`
}

func (m *Module) filters(c *echo.Context) error {
	ctx := c.Request().Context()
	n, err := m.store.counts(ctx)
	if err != nil {
		return err
	}
	_, issues, err := m.store.albums(ctx, listQuery{Filter: "issues", Sort: "album", Limit: 1})
	if err != nil {
		return err
	}
	out := filtersJSON{Albums: n.Albums, Issues: issues, NoMBID: n.NoMBID, Kinds: []countJSON{}, Folders: []countJSON{}}
	for _, k := range Kinds {
		out.Kinds = append(out.Kinds, countJSON{Name: string(k), Albums: n.Kinds[k]})
	}
	for f, a := range n.Folders {
		if f != "" {
			out.Folders = append(out.Folders, countJSON{Name: f, Albums: a})
		}
	}
	slices.SortFunc(out.Folders, func(a, b countJSON) int { return strings.Compare(a.Name, b.Name) })
	return c.JSON(http.StatusOK, out)
}

func idParam(c *echo.Context, what string) (int64, error) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, echo.NewHTTPError(http.StatusNotFound, "no such "+what)
	}
	return id, nil
}

// keyJSON is one tag of a file.
type keyJSON struct {
	Key string `json:"key"`
	// Fields are the Navidrome fields the key feeds.
	Fields   []string `json:"fields,omitempty"`
	Values   []string `json:"values"`
	ReadOnly bool     `json:"read_only,omitempty"`
}

type pictureJSON struct {
	Type        string `json:"type"`
	MIME        string `json:"mime"`
	Description string `json:"description"`
}

// trackJSON is one file of an opened album, as read now.
type trackJSON struct {
	ID       int64   `json:"id"`
	Path     string  `json:"path"`
	Disc     int     `json:"disc"`
	Track    int     `json:"track"`
	Title    string  `json:"title"`
	Artist   string  `json:"artist"`
	Duration float64 `json:"duration"`
	Quality  string  `json:"quality"`
	Format   string  `json:"format"`
	Size     int64   `json:"size"`
	// Revision goes back with a Save, so a file changed since is a
	// conflict.
	Revision string        `json:"revision"`
	Error    string        `json:"error"`
	Issues   []Issue       `json:"issues"`
	Standard []keyJSON     `json:"standard"`
	Extended []keyJSON     `json:"extended"`
	Pictures []pictureJSON `json:"pictures"`
}

type detailJSON struct {
	albumJSON
	Tracks []trackJSON `json:"tracks"`
	// Stale is true when the files changed since the index read them. A walk
	// is asked for, and the list catches up after it.
	Stale bool `json:"stale"`
}

// get opens an album. Its files are read again now, so the editor never
// shows what the index read before another tool changed them.
func (m *Module) get(c *echo.Context) error {
	id, err := idParam(c, "album")
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	row, err := m.store.album(ctx, id)
	if errors.Is(err, errNotFound) {
		return echo.NewHTTPError(http.StatusNotFound, "no such album")
	}
	if err != nil {
		return err
	}
	indexed, err := m.store.albumFiles(ctx, id)
	if err != nil {
		return err
	}
	rules := m.currentRules()
	out := detailJSON{albumJSON: toJSON(row), Tracks: []trackJSON{}}
	var fresh []*file
	for _, old := range indexed {
		abs := filepath.Join(m.root, filepath.FromSlash(old.Path))
		tj := trackJSON{ID: old.ID, Path: old.Path, Issues: []Issue{}, Standard: []keyJSON{}, Extended: []keyJSON{}, Pictures: []pictureJSON{}}
		tf, err := tagio.Read(abs)
		if err != nil {
			tj.Error = strings.ReplaceAll(err.Error(), abs+": ", "")
			out.Stale = out.Stale || old.Error == ""
			out.Tracks = append(out.Tracks, tj)
			continue
		}
		f := &file{ID: old.ID, Path: old.Path, Size: tf.Size, ModNS: tf.ModTime.UnixNano(), Revision: tf.Revision(),
			FirstSeen: old.FirstSeen, Facts: read(rules, tf, old.Path)}
		out.Stale = out.Stale || f.Revision != old.Revision || f.Facts.AlbumID != row.NDID
		fresh = append(fresh, f)
		fc := f.Facts
		tj.Disc, tj.Track, tj.Title, tj.Artist = fc.Disc, fc.Track, fc.Title, fc.Artist
		tj.Duration, tj.Quality, tj.Format, tj.Size, tj.Revision = float64(fc.LengthMS)/1000, fc.quality().Short(), tf.Format.Name, tf.Size, f.Revision
		if fc.Issues != nil {
			tj.Issues = fc.Issues
		}
		standard, extended := tf.Groups()
		for _, k := range standard {
			kj := keyJSON{Key: k, Values: tf.Tags[k], ReadOnly: tf.Format.ReadOnly(k)}
			for _, fd := range tagio.FieldsOf(tf.Format, k) {
				kj.Fields = append(kj.Fields, fd.Name)
			}
			tj.Standard = append(tj.Standard, kj)
		}
		for _, k := range extended {
			tj.Extended = append(tj.Extended, keyJSON{Key: k, Values: tf.Tags[k]})
		}
		for _, p := range tf.Pictures {
			tj.Pictures = append(tj.Pictures, pictureJSON{Type: p.Type, MIME: p.MIME, Description: p.Description})
		}
		out.Tracks = append(out.Tracks, tj)
	}
	slices.SortFunc(out.Tracks, func(a, b trackJSON) int {
		if a.Disc != b.Disc {
			return a.Disc - b.Disc
		}
		if a.Track != b.Track {
			return a.Track - b.Track
		}
		return strings.Compare(a.Path, b.Path)
	})
	if len(fresh) > 0 {
		a, err := m.recheck(c, row, fresh, rules)
		if err != nil {
			return err
		}
		out.Issues = a.Issues
		if out.Issues == nil {
			out.Issues = []Issue{}
		}
		out.Lamp = a.lamp()
	}
	if out.Stale {
		m.Kick()
	}
	return c.JSON(http.StatusOK, out)
}

// recheck runs the album checks on files read now. The split check needs
// the other albums of the folder, which come from the index.
func (m *Module) recheck(c *echo.Context, row albumRow, fresh []*file, rules *tagio.Rules) (*album, error) {
	ctx := c.Request().Context()
	a := &album{Folder: row.Folder, AlbumID: row.NDID, Files: fresh}
	a.summarize()
	a.check(folderImages(m.root, a.folders()), rules)
	others, err := m.store.folderAlbums(ctx, row.Folder)
	if err != nil {
		return nil, err
	}
	all := []*album{a}
	for _, o := range others {
		if o.ID == row.ID {
			continue
		}
		files, err := m.store.albumFiles(ctx, o.ID)
		if err != nil {
			return nil, err
		}
		files = slices.DeleteFunc(files, func(f *file) bool { return f.Error != "" })
		if len(files) > 0 {
			all = append(all, &album{Folder: o.Folder, AlbumID: o.NDID, Files: files})
		}
	}
	splits(all)
	sortIssues(a.Issues)
	return a, nil
}

// folderImages lists the image files in folders, as the walk does.
func folderImages(root string, dirs []string) map[string][]string {
	out := map[string][]string{}
	for _, d := range dirs {
		entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(d)))
		if err != nil {
			continue
		}
		for _, e := range entries {
			if n := e.Name(); e.Type().IsRegular() && !dotEntry(n) && imageExts[strings.ToLower(path.Ext(n))] {
				out[d] = append(out[d], strings.ToLower(n))
			}
		}
	}
	return out
}

// audioTypes are the media types the player gets, by format. Go's own
// table lacks most of them.
var audioTypes = map[string]string{
	"flac": "audio/flac", "m4a": "audio/mp4", "mp3": "audio/mpeg", "ogg": "audio/ogg", "opus": "audio/ogg",
	"wav": "audio/wav", "aiff": "audio/aiff", "wma": "audio/x-ms-wma",
}

// audio serves a track's file as it is, with ranges, for the player.
func (m *Module) audio(c *echo.Context) error {
	id, err := idParam(c, "track")
	if err != nil {
		return err
	}
	f, err := m.store.fileByID(c.Request().Context(), id)
	if errors.Is(err, errNotFound) {
		return echo.NewHTTPError(http.StatusNotFound, "no such track")
	}
	if err != nil {
		return err
	}
	abs := filepath.Join(m.root, filepath.FromSlash(f.Path))
	// The index never holds a symlink, and a file swapped for one since is
	// not served.
	st, err := os.Lstat(abs)
	if err != nil || !st.Mode().IsRegular() {
		return echo.NewHTTPError(http.StatusNotFound, "the track's file is gone")
	}
	fh, err := os.Open(abs)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "the track's file is gone")
	}
	defer func() { _ = fh.Close() }()
	ft := tagio.FormatOf(abs)
	w := c.Response()
	if ft != nil {
		w.Header().Set("Content-Type", audioTypes[ft.Name])
	}
	w.Header().Set("Cache-Control", "private, no-cache")
	http.ServeContent(w, c.Request(), path.Base(f.Path), st.ModTime(), fh)
	return nil
}
