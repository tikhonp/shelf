package qobuz

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/url"
	"path"
	"path/filepath"
	"strings"

	"github.com/tikhonp/shelf/internal/modules/downloads"
	"github.com/tikhonp/shelf/internal/platform/config"
	"github.com/tikhonp/shelf/internal/source"
)

// oldRoot is where the old bot's container mounted music/qobuz, so its rows
// say /downloads/<folder> for what shelf sees as <SHELF_QOBUZ_DIR>/<folder>.
const oldRoot = "/downloads"

// Importer takes the rows, keeps those already there, and says how many it
// added.
type Importer interface {
	ImportDownloaded(ctx context.Context, rows []downloads.Downloaded) (int, error)
}

// ImportResult counts what an import did with the file's rows.
type ImportResult struct {
	Rows  int
	Added int
	// Kept are rows shelf had already, and second rows of one item: qobuz-dl
	// keeps a row per quality.
	Kept int
	// OtherKinds are rows that are neither album nor track.
	OtherKinds int
	// Outside are rows whose path stayed as it was. They count as added or
	// kept too.
	Outside int
}

// Import reads the old bot's qobuz_dl.db into the history. The best quality
// of an item comes first, so its row wins.
func Import(ctx context.Context, file string, cfg *config.Config, to Importer, log *slog.Logger) (ImportResult, error) {
	var res ImportResult
	abs, err := filepath.Abs(file)
	if err != nil {
		return res, err
	}
	// Read only, and not through db.Open, which would turn the file to WAL.
	dsn := url.URL{Scheme: "file", Path: abs, RawQuery: "mode=ro"}
	conn, err := sql.Open("sqlite", dsn.String())
	if err != nil {
		return res, err
	}
	defer func() { _ = conn.Close() }()
	rows, err := conn.QueryContext(ctx, `
		SELECT id, media_type, file_format, bit_depth, sampling_rate, saved_path FROM downloads ORDER BY quality DESC, rowid`)
	if err != nil {
		return res, fmt.Errorf("read %s: %w", file, err)
	}
	defer func() { _ = rows.Close() }()

	music, err := filepath.Abs(cfg.MusicDir)
	if err != nil {
		return res, err
	}
	var items []downloads.Downloaded
	for rows.Next() {
		var id, kind, format, saved string
		var bits, khz sql.NullString
		if err := rows.Scan(&id, &kind, &format, &bits, &khz, &saved); err != nil {
			return res, fmt.Errorf("read %s: %w", file, err)
		}
		res.Rows++
		if kind != string(source.KindAlbum) && kind != string(source.KindTrack) {
			res.OtherKinds++
			continue
		}
		p, inside := importPath(saved, music, cfg.Qobuz.Dir)
		if !inside {
			res.Outside++
			log.Warn("qobuz: import: a path outside the music folder stays as it is", "id", id, "path", saved)
		}
		items = append(items, downloads.Downloaded{
			Ref: ref(source.Kind(kind), id), Quality: qualityText(format, nullable(bits), nullable(khz)), Path: p,
		})
	}
	if err := rows.Err(); err != nil {
		return res, fmt.Errorf("read %s: %w", file, err)
	}
	res.Added, err = to.ImportDownloaded(ctx, items)
	if err != nil {
		return res, err
	}
	res.Kept = len(items) - res.Added
	return res, nil
}

func nullable(s sql.NullString) *string {
	if !s.Valid {
		return nil
	}
	return &s.String
}

// importPath makes a saved path relative to the music root. inside is false
// for a path under neither root.
func importPath(saved, music, dir string) (p string, inside bool) {
	if saved == "" {
		return "", true
	}
	if rest, ok := under(saved, oldRoot); ok {
		return path.Join(dir, rest), true
	}
	if rest, ok := under(saved, music); ok {
		return rest, true
	}
	return saved, false
}

func under(p, root string) (string, bool) {
	p = path.Clean(p)
	if p == root {
		return "", true
	}
	return strings.CutPrefix(p, root+"/")
}
