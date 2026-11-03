package downloads

import (
	"context"
	"strings"

	"github.com/tikhonp/shelf/internal/source"
)

// statusBatch keeps a query's variables far under SQLite's limit.
const statusBatch = 300

// Statuses says what shelf knows of each item's downloads: queued or
// running when a download of it is under way, in_library when it is in
// downloaded_items, failed or partial when its last download ended so. It
// leaves out items shelf knows nothing of.
func (s *Service) Statuses(ctx context.Context, refs []source.Ref) (map[source.Ref]source.Status, error) {
	out := map[source.Ref]source.Status{}
	seen := map[source.Ref]bool{}
	var uniq []source.Ref
	for _, r := range refs {
		if !seen[r] {
			seen[r] = true
			uniq = append(uniq, r)
		}
	}
	for len(uniq) > 0 {
		batch := uniq[:min(statusBatch, len(uniq))]
		uniq = uniq[len(batch):]
		if err := s.statuses(ctx, batch, out); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *Service) statuses(ctx context.Context, refs []source.Ref, out map[source.Ref]source.Status) error {
	values := strings.TrimSuffix(strings.Repeat("(?, ?, ?),", len(refs)), ",")
	args := make([]any, 0, 3*len(refs))
	for _, r := range refs {
		args = append(args, r.Source, string(r.Kind), r.ID)
	}
	var rows []struct {
		ID          int64  `db:"id"`
		Source      string `db:"source"`
		Kind        string `db:"kind"`
		ItemID      string `db:"item_id"`
		Status      string `db:"status"`
		TracksDone  int    `db:"tracks_done"`
		TracksTotal int    `db:"tracks_total"`
	}
	if err := s.db.R.SelectContext(ctx, &rows, `
		SELECT id, source, kind, item_id, status, tracks_done, tracks_total FROM downloads
		WHERE (source, kind, item_id) IN (VALUES `+values+`) ORDER BY id`, args...); err != nil {
		return err
	}
	var have []struct {
		Source     string `db:"source"`
		Kind       string `db:"kind"`
		ItemID     string `db:"item_id"`
		DownloadID int64  `db:"download_id"`
	}
	if err := s.db.R.SelectContext(ctx, &have, `
		SELECT source, kind, item_id, COALESCE(download_id, 0) AS download_id FROM downloaded_items
		WHERE (source, kind, item_id) IN (VALUES `+values+`)`, args...); err != nil {
		return err
	}
	active := map[source.Ref]source.Status{}
	last := map[source.Ref]source.Status{}
	for _, r := range rows { // oldest first, so the newest wins
		ref := source.Ref{Source: r.Source, Kind: source.Kind(r.Kind), ID: r.ItemID}
		st := source.Status{State: r.Status, DownloadID: r.ID, TracksDone: r.TracksDone, TracksTotal: r.TracksTotal}
		if !Status(r.Status).Final() {
			active[ref] = st
		}
		last[ref] = st
	}
	inLibrary := map[source.Ref]int64{}
	for _, h := range have {
		inLibrary[source.Ref{Source: h.Source, Kind: source.Kind(h.Kind), ID: h.ItemID}] = h.DownloadID
	}
	for _, ref := range refs {
		if st, ok := active[ref]; ok {
			out[ref] = st
			continue
		}
		if id, ok := inLibrary[ref]; ok {
			out[ref] = source.Status{State: "in_library", DownloadID: id}
			continue
		}
		if st, ok := last[ref]; ok && (st.State == string(Failed) || st.State == string(Partial)) {
			out[ref] = st
		}
	}
	return nil
}
