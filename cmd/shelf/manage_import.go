package main

import (
	"context"
	"errors"
	"log/slog"

	"github.com/tikhonp/shelf/internal/modules/downloads"
	"github.com/tikhonp/shelf/internal/modules/qobuz"
	"github.com/tikhonp/shelf/internal/platform"
	"github.com/tikhonp/shelf/internal/platform/config"
)

// importQobuzDB brings the old bot's history into downloaded_items.
func (c cli) importQobuzDB(ctx context.Context, app *platform.App, cfg *config.Config, log *slog.Logger, file string) error {
	m, ok := app.Module(downloads.Name)
	if !ok {
		return errors.New("the downloads module is not enabled")
	}
	dm := m.(*downloads.Module)
	if !dm.Enabled() {
		return errors.New("downloads are not enabled; set SHELF_DOWNLOADS_ENABLED=true")
	}
	r, err := qobuz.Import(ctx, file, cfg, dm.Service(), log)
	if err != nil {
		return err
	}
	sayf(c.stdout, "Read %d rows: %d added, %d already on record, %d of another kind, %d with a path outside the music folder.\n",
		r.Rows, r.Added, r.Kept, r.OtherKinds, r.Outside)
	return nil
}
