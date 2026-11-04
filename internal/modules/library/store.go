package library

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jmoiron/sqlx"
	"github.com/tikhonp/shelf/internal/modules/library/tagio"
	"github.com/tikhonp/shelf/internal/platform/db"
)

// store keeps the index in the module's tables.
type store struct{ db *db.DB }

type fileRow struct {
	ID        int64   `db:"id"`
	Path      string  `db:"path"`
	Size      int64   `db:"size"`
	ModNS     int64   `db:"mtime_ns"`
	Revision  string  `db:"revision"`
	Rules     string  `db:"rules"`
	Version   int     `db:"version"`
	Error     string  `db:"error"`
	FirstSeen db.Time `db:"first_seen"`
	Facts     string  `db:"facts"`
	AlbumID   *int64  `db:"album_id"`
}

func (r fileRow) file() (*file, error) {
	f := &file{ID: r.ID, Path: r.Path, Size: r.Size, ModNS: r.ModNS, Revision: r.Revision, Rules: r.Rules,
		Version: r.Version, Error: r.Error, FirstSeen: r.FirstSeen.Time}
	if err := json.Unmarshal([]byte(r.Facts), &f.Facts); err != nil {
		return nil, fmt.Errorf("library: facts of %s: %w", r.Path, err)
	}
	return f, nil
}

const fileCols = `id, path, size, mtime_ns, revision, rules, version, error, first_seen, facts, album_id`

// files loads the whole index.
func (s store) files(ctx context.Context) ([]*file, error) {
	var rows []fileRow
	if err := s.db.R.SelectContext(ctx, &rows, `SELECT `+fileCols+` FROM library_files`); err != nil {
		return nil, err
	}
	out := make([]*file, 0, len(rows))
	for _, r := range rows {
		f, err := r.file()
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, nil
}

// albumFiles loads one album's files.
func (s store) albumFiles(ctx context.Context, albumID int64) ([]*file, error) {
	var rows []fileRow
	if err := s.db.R.SelectContext(ctx, &rows, `SELECT `+fileCols+` FROM library_files WHERE album_id = ?`, albumID); err != nil {
		return nil, err
	}
	out := make([]*file, 0, len(rows))
	for _, r := range rows {
		f, err := r.file()
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, nil
}

// fileByID loads one file.
func (s store) fileByID(ctx context.Context, id int64) (*file, error) {
	var r fileRow
	err := s.db.R.GetContext(ctx, &r, `SELECT `+fileCols+` FROM library_files WHERE id = ?`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errNotFound
	}
	if err != nil {
		return nil, err
	}
	return r.file()
}

var errNotFound = errors.New("library: not found")

// saveFiles writes changed files and removes gone ones. A new file gets its
// row ID here.
func (s store) saveFiles(ctx context.Context, changed []*file, gone []int64) error {
	return s.db.Write(ctx, func(tx *sqlx.Tx) error {
		for _, id := range gone {
			if _, err := tx.ExecContext(ctx, `DELETE FROM library_files WHERE id = ?`, id); err != nil {
				return err
			}
		}
		for _, f := range changed {
			facts, err := json.Marshal(f.Facts)
			if err != nil {
				return err
			}
			if f.ID == 0 {
				err = tx.GetContext(ctx, &f.ID, `INSERT INTO library_files (path, size, mtime_ns, revision, rules, version, error, first_seen, facts)
					VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
					f.Path, f.Size, f.ModNS, f.Revision, f.Rules, f.Version, cut(f.Error, 1000), db.At(f.FirstSeen), string(facts))
			} else {
				_, err = tx.ExecContext(ctx, `UPDATE library_files SET size = ?, mtime_ns = ?, revision = ?, rules = ?, version = ?,
					error = ?, facts = ? WHERE id = ?`,
					f.Size, f.ModNS, f.Revision, f.Rules, f.Version, cut(f.Error, 1000), string(facts), f.ID)
			}
			if err != nil {
				return fmt.Errorf("library: save %s: %w", f.Path, err)
			}
		}
		return nil
	})
}

// saveAlbums replaces the albums with the ones a walk found. An album keeps
// its row ID while its folder and Navidrome ID stay.
func (s store) saveAlbums(ctx context.Context, albums []*album) error {
	return s.db.Write(ctx, func(tx *sqlx.Tx) error {
		if _, err := tx.ExecContext(ctx, `CREATE TEMP TABLE IF NOT EXISTS library_keep (id INTEGER PRIMARY KEY)`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM temp.library_keep`); err != nil {
			return err
		}
		// A file that left its album, or can't be read, belongs to none.
		if _, err := tx.ExecContext(ctx, `UPDATE library_files SET album_id = NULL WHERE album_id IS NOT NULL`); err != nil {
			return err
		}
		for _, a := range albums {
			issues, err := json.Marshal(a.Issues)
			if err != nil {
				return err
			}
			kinds := ","
			for _, is := range a.Issues {
				kinds += string(is.Kind) + ","
			}
			var id int64
			err = tx.GetContext(ctx, &id, `INSERT INTO library_albums
				(folder, nd_id, title, artist, year, tracks, quality, length_ms, size, added_at, has_mbid, lamp, issues, kinds, search)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
				ON CONFLICT (folder, nd_id) DO UPDATE SET title = excluded.title, artist = excluded.artist, year = excluded.year,
					tracks = excluded.tracks, quality = excluded.quality, length_ms = excluded.length_ms, size = excluded.size,
					added_at = excluded.added_at, has_mbid = excluded.has_mbid, lamp = excluded.lamp, issues = excluded.issues,
					kinds = excluded.kinds, search = excluded.search
				RETURNING id`,
				a.Folder, a.AlbumID, a.Title, a.Artist, a.Year, len(a.Files), a.Quality, a.LengthMS, a.Size, db.At(a.Added),
				a.HasMBID, a.lamp(), string(issues), kinds, searchText(a.Title, a.Artist, a.Folder))
			if err != nil {
				return fmt.Errorf("library: save album %s: %w", a.Folder, err)
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO temp.library_keep (id) VALUES (?)`, id); err != nil {
				return err
			}
			for _, f := range a.Files {
				if _, err := tx.ExecContext(ctx, `UPDATE library_files SET album_id = ? WHERE id = ?`, id, f.ID); err != nil {
					return err
				}
			}
		}
		_, err := tx.ExecContext(ctx, `DELETE FROM library_albums WHERE id NOT IN (SELECT id FROM temp.library_keep)`)
		return err
	})
}

func cut(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// searchText is what a search matches, folded, so a search for "guns n' roses"
// finds a typographic apostrophe too.
func searchText(parts ...string) string {
	return fold(strings.Join(parts, "\n"))
}

func fold(s string) string { return tagio.Fold(s) }

// The Navidrome settings last read.
const rulesKey = "navidrome_config"

func (s store) cachedConfig(ctx context.Context) (tagio.NavidromeConfig, bool, error) {
	var v string
	err := s.db.R.GetContext(ctx, &v, `SELECT value FROM library_state WHERE key = ?`, rulesKey)
	if errors.Is(err, sql.ErrNoRows) {
		return tagio.NavidromeConfig{}, false, nil
	}
	if err != nil {
		return tagio.NavidromeConfig{}, false, err
	}
	var c tagio.NavidromeConfig
	if err := json.Unmarshal([]byte(v), &c); err != nil {
		return tagio.NavidromeConfig{}, false, err
	}
	return c, true, nil
}

func (s store) cacheConfig(ctx context.Context, c tagio.NavidromeConfig) error {
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return s.db.Write(ctx, func(tx *sqlx.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO library_state (key, value) VALUES (?, ?)
			ON CONFLICT (key) DO UPDATE SET value = excluded.value`, rulesKey, string(b))
		return err
	})
}

// albumRow is an album as the list reads it.
type albumRow struct {
	ID       int64   `db:"id"`
	Folder   string  `db:"folder"`
	NDID     string  `db:"nd_id"`
	Title    string  `db:"title"`
	Artist   string  `db:"artist"`
	Year     int     `db:"year"`
	Tracks   int     `db:"tracks"`
	Quality  string  `db:"quality"`
	LengthMS int64   `db:"length_ms"`
	Size     int64   `db:"size"`
	AddedAt  db.Time `db:"added_at"`
	HasMBID  bool    `db:"has_mbid"`
	Lamp     bool    `db:"lamp"`
	Issues   string  `db:"issues"`
}

const albumCols = `id, folder, nd_id, title, artist, year, tracks, quality, length_ms, size, added_at, has_mbid, lamp, issues`

// listQuery is what the album list asks for.
type listQuery struct {
	Search string
	// Filter is "", "issues", "guide", "no_mbid" or a check's kind.
	Filter string
	// Folder is a top folder, such as "qobuz".
	Folder string
	// Sort is "album", "artist", "year" or "added".
	Sort   string
	Desc   bool
	Limit  int
	Offset int
}

func like(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

func (s store) albums(ctx context.Context, q listQuery) ([]albumRow, int, error) {
	var where []string
	var args []any
	if q.Search != "" {
		for _, w := range strings.Fields(fold(q.Search)) {
			where = append(where, `search LIKE ? ESCAPE '\'`)
			args = append(args, "%"+like(w)+"%")
		}
	}
	switch q.Filter {
	case "":
	case "issues":
		where = append(where, `lamp = 1`)
	case "guide":
		where = append(where, `kinds LIKE '%,guide\_%' ESCAPE '\'`)
	case "no_mbid":
		where = append(where, `has_mbid = 0`)
	default:
		where = append(where, `kinds LIKE ? ESCAPE '\'`)
		args = append(args, "%,"+like(q.Filter)+",%")
	}
	if q.Folder != "" {
		where = append(where, `(folder = ? OR substr(folder, 1, ?) = ?)`)
		args = append(args, q.Folder, len(q.Folder)+1, q.Folder+"/")
	}
	cond := ""
	if where != nil {
		cond = " WHERE " + strings.Join(where, " AND ")
	}
	var total int
	if err := s.db.R.GetContext(ctx, &total, `SELECT count(*) FROM library_albums`+cond, args...); err != nil {
		return nil, 0, err
	}
	dir := "ASC"
	if q.Desc {
		dir = "DESC"
	}
	order := map[string]string{
		"album":  "title COLLATE NOCASE %[1]s, artist COLLATE NOCASE %[1]s",
		"artist": "artist COLLATE NOCASE %[1]s, year %[1]s, title COLLATE NOCASE %[1]s",
		"year":   "year %[1]s, artist COLLATE NOCASE %[1]s, title COLLATE NOCASE %[1]s",
		"added":  "added_at %[1]s, folder %[1]s",
	}[q.Sort]
	var rows []albumRow
	err := s.db.R.SelectContext(ctx, &rows, `SELECT `+albumCols+` FROM library_albums`+cond+
		` ORDER BY `+fmt.Sprintf(order, dir)+`, id `+dir+` LIMIT ? OFFSET ?`, append(args, q.Limit, q.Offset)...)
	return rows, total, err
}

func (s store) album(ctx context.Context, id int64) (albumRow, error) {
	var r albumRow
	err := s.db.R.GetContext(ctx, &r, `SELECT `+albumCols+` FROM library_albums WHERE id = ?`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return r, errNotFound
	}
	return r, err
}

// folderAlbums lists the albums of one release folder, for the split check
// of an opened album.
func (s store) folderAlbums(ctx context.Context, folder string) ([]albumRow, error) {
	var rows []albumRow
	err := s.db.R.SelectContext(ctx, &rows, `SELECT `+albumCols+` FROM library_albums WHERE folder = ?`, folder)
	return rows, err
}

// counts are the numbers the filter menu shows.
type counts struct {
	Albums int
	Kinds  map[Kind]int
	// Folders maps each top folder to its albums.
	Folders map[string]int
	NoMBID  int
}

func (s store) counts(ctx context.Context) (counts, error) {
	c := counts{Kinds: map[Kind]int{}, Folders: map[string]int{}}
	var rows []struct {
		Folder  string `db:"folder"`
		Kinds   string `db:"kinds"`
		HasMBID bool   `db:"has_mbid"`
	}
	if err := s.db.R.SelectContext(ctx, &rows, `SELECT folder, kinds, has_mbid FROM library_albums`); err != nil {
		return c, err
	}
	for _, r := range rows {
		c.Albums++
		top, _, _ := strings.Cut(r.Folder, "/")
		if r.Folder == "." {
			top = ""
		}
		c.Folders[top]++
		for k := range strings.SplitSeq(strings.Trim(r.Kinds, ","), ",") {
			if k != "" {
				c.Kinds[Kind(k)]++
			}
		}
		if !r.HasMBID {
			c.NoMBID++
		}
	}
	return c, nil
}

// navidromeAlbums counts distinct Navidrome album IDs. Navidrome merges an
// album that spans two folders, so this is the number to hold against its
// album count.
func (s store) navidromeAlbums(ctx context.Context) (int, error) {
	var n int
	err := s.db.R.GetContext(ctx, &n, `SELECT count(DISTINCT nd_id) FROM library_albums`)
	return n, err
}

// albumIDTaken reports whether an album other than except has a Navidrome
// album ID, so a Save that moves tracks to it merges them in Navidrome.
func (s store) albumIDTaken(ctx context.Context, ndID string, except int64) (bool, error) {
	var n int
	err := s.db.R.GetContext(ctx, &n, `SELECT count(*) FROM library_albums WHERE nd_id = ? AND id != ?`, ndID, except)
	return n > 0, err
}

// albumOf is the album a file belongs to now, or 0.
func (s store) albumOf(ctx context.Context, path string) (int64, error) {
	var id sql.NullInt64
	err := s.db.R.GetContext(ctx, &id, `SELECT album_id FROM library_files WHERE path = ?`, path)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return id.Int64, err
}

// fileByPath is a file's row ID.
func (s store) fileByPath(ctx context.Context, path string) (int64, error) {
	var id int64
	err := s.db.R.GetContext(ctx, &id, `SELECT id FROM library_files WHERE path = ?`, path)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, errNotFound
	}
	return id, err
}
