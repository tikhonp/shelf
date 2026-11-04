package downloads

import (
	"context"

	"github.com/jmoiron/sqlx"
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
