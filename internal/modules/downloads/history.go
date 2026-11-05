package downloads

import (
	"context"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/tikhonp/shelf/internal/platform/db"
	"github.com/tikhonp/shelf/internal/source"
)

// Downloaded is a row of downloaded_items that didn't come from a download's
// finish step.
type Downloaded struct {
	Ref source.Ref
	// Quality is what was delivered, such as "FLAC 24/96".
	Quality string
	// Path is the release folder, relative to the music root.
	Path string
	// Title and Artist are what the older tool knew. A skipped download
	// shows them when no download of the item has.
	Title, Artist string
}

// IsDownloaded reports whether ref is in downloaded_items at any quality.
// Queue asks the same question.
func (s *Service) IsDownloaded(ctx context.Context, ref source.Ref) (bool, error) {
	var n int
	err := s.db.R.GetContext(ctx, &n, `SELECT count(*) FROM downloaded_items WHERE source = ? AND kind = ? AND item_id = ?`,
		ref.Source, string(ref.Kind), ref.ID)
	return n > 0, err
}

// RecordDownloaded adds an item a source wrote inside another download, such
// as a playlist's track. A row already there stays.
func (s *Service) RecordDownloaded(ctx context.Context, d Downloaded) error {
	_, err := s.addDownloaded(ctx, []Downloaded{d}, false)
	return err
}

// ImportDownloaded adds rows from an older tool's history, marked imported,
// and says how many it added. Rows already there stay, so a second import
// changes nothing.
func (s *Service) ImportDownloaded(ctx context.Context, rows []Downloaded) (int, error) {
	return s.addDownloaded(ctx, rows, true)
}

func (s *Service) addDownloaded(ctx context.Context, rows []Downloaded, imported bool) (int, error) {
	added := 0
	err := s.db.Write(ctx, func(tx *sqlx.Tx) error {
		for _, d := range rows {
			res, err := tx.ExecContext(ctx, `
				INSERT INTO downloaded_items (source, kind, item_id, quality, path, title, artist, imported, at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
				ON CONFLICT (source, kind, item_id) DO NOTHING`,
				d.Ref.Source, string(d.Ref.Kind), d.Ref.ID, d.Quality, d.Path, d.Title, d.Artist, imported, s.now())
			if err != nil {
				return err
			}
			n, err := res.RowsAffected()
			if err != nil {
				return err
			}
			added += int(n)
		}
		return nil
	})
	return added, err
}

// Record is a downloaded item and the release folder it went to.
type Record struct {
	Ref source.Ref
	// Path is the release folder, relative to the music root.
	Path string
	// DownloadID is the download that wrote it, 0 for an older tool's row.
	DownloadID int64
	// Quality is what was delivered, such as "FLAC 24/96".
	Quality string
	// Imported is a row from an older tool's history.
	Imported bool
	// At is when the row was written: the download's finish, or the import.
	At time.Time
}

// Records lists the downloaded items that name a release folder. It is the
// library's port, which tells the source of each album from them.
func (s *Service) Records(ctx context.Context) ([]Record, error) {
	var rows []struct {
		Source     string  `db:"source"`
		Kind       string  `db:"kind"`
		ItemID     string  `db:"item_id"`
		DownloadID *int64  `db:"download_id"`
		Quality    string  `db:"quality"`
		Path       string  `db:"path"`
		Imported   bool    `db:"imported"`
		At         db.Time `db:"at"`
	}
	if err := s.db.R.SelectContext(ctx, &rows, `SELECT source, kind, item_id, download_id, quality, path, imported, at
		FROM downloaded_items WHERE path <> '' ORDER BY at, source, kind, item_id`); err != nil {
		return nil, err
	}
	out := make([]Record, len(rows))
	for i, r := range rows {
		out[i] = Record{Ref: source.Ref{Source: r.Source, Kind: source.Kind(r.Kind), ID: r.ItemID}, Path: r.Path,
			Quality: r.Quality, Imported: r.Imported, At: r.At.Time}
		if r.DownloadID != nil {
			out[i].DownloadID = *r.DownloadID
		}
	}
	return out, nil
}

// Forget drops the downloaded items in folder, given inside the music
// folder, and in the folders under it, so a request for one of them
// downloads it again. It is the library's port, for a deleted folder. The
// downloads themselves stay. It says how many items it dropped.
func (s *Service) Forget(ctx context.Context, folder string) (int64, error) {
	folder = strings.Trim(folder, "/")
	if folder == "" || folder == "." {
		return 0, nil
	}
	var n int64
	err := s.db.Write(ctx, func(tx *sqlx.Tx) error {
		res, err := tx.ExecContext(ctx, `DELETE FROM downloaded_items WHERE path = ? OR substr(path, 1, ?) = ?`,
			folder, len(folder)+1, folder+"/")
		if err != nil {
			return err
		}
		n, err = res.RowsAffected()
		return err
	})
	return n, err
}
