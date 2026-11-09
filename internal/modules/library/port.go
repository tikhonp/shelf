package library

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/tikhonp/shelf/internal/libraryport"
	"github.com/tikhonp/shelf/internal/modules/library/tagio"
	"github.com/tikhonp/shelf/internal/platform/db"
)

// Limits of the port's reads.
const (
	portPageAlbums  = 100
	portPageHistory = 50
	// historyFiles bounds the files of one change in a history page, since
	// a run's album may have hundreds.
	historyFiles = 200
	// checkedKeep is how long a cover CoverInfo, PutCover or
	// MaterializeOffer checked may be pinned without another check. It is
	// shorter than uploadKeep, and each check renews the upload, so a
	// pinned cover is never swept between its check and its pin.
	checkedKeep = 23 * time.Hour
)

// Port is the library as other modules see it. It needs Init to have run
// before its first call.
func (m *Module) Port() libraryport.Port { return port{m} }

type port struct{ m *Module }

var _ libraryport.Port = port{}

// checked records a cover the port read and decoded, so Pin can take it in
// a transaction without reading the store.
func (m *Module) checked(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.checkedCovers == nil {
		m.checkedCovers = map[string]time.Time{}
	}
	now := time.Now()
	for k, at := range m.checkedCovers {
		if now.Sub(at) > checkedKeep {
			delete(m.checkedCovers, k)
		}
	}
	m.checkedCovers[id] = now
}

func (m *Module) recentlyChecked(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	at, ok := m.checkedCovers[id]
	return ok && time.Since(at) <= checkedKeep
}

func refused(err error) error {
	return fmt.Errorf("%w: %s", libraryport.ErrRefused, strings.TrimPrefix(err.Error(), tagio.ErrRefused.Error()+": "))
}

func notFound(err error) error {
	if errors.Is(err, errNotFound) {
		return libraryport.ErrNotFound
	}
	return err
}

func (p port) Albums(ctx context.Context, q libraryport.AlbumQuery) (libraryport.AlbumPage, error) {
	m := p.m
	lq := listQuery{Search: strings.TrimSpace(q.Search), Folder: strings.Trim(q.Folder, "/"), Source: q.Source,
		Sort: cmp.Or(q.Sort, "album"), Desc: q.Desc, Limit: q.Limit, Offset: q.Offset}
	bad := func(msg string) (libraryport.AlbumPage, error) {
		return libraryport.AlbumPage{}, fmt.Errorf("%w: %s", libraryport.ErrRefused, msg)
	}
	if lq.Limit == 0 {
		lq.Limit = portPageAlbums
	}
	if lq.Limit < 1 || lq.Limit > portPageAlbums || lq.Offset < 0 {
		return bad("limit must be 1 to " + strconv.Itoa(portPageAlbums) + " and offset at least 0")
	}
	if !m.validSource(lq.Source) {
		return bad("unknown source " + strconv.Quote(lq.Source))
	}
	switch f := q.Filter; {
	case f == "" || f == "all":
	case f == "issues" || f == "guide" || f == "no_mbid" || slices.Contains(Kinds, Kind(f)):
		lq.Filter = f
	default:
		return bad("unknown filter " + strconv.Quote(f))
	}
	switch lq.Sort {
	case "album", "artist", "year", "added":
	default:
		return bad("sort must be album, artist, year or added")
	}
	rows, total, err := m.store.albums(ctx, lq)
	if err != nil {
		return libraryport.AlbumPage{}, err
	}
	out := libraryport.AlbumPage{Albums: make([]libraryport.AlbumSummary, len(rows)), Total: total}
	for i, r := range rows {
		out.Albums[i] = summary(toJSON(r))
	}
	return out, nil
}

func summary(a albumJSON) libraryport.AlbumSummary {
	return libraryport.AlbumSummary{ID: a.ID, Folder: a.Folder, Title: a.Title, Artist: a.Artist, Year: a.Year, Tracks: a.Tracks,
		Quality: a.Quality, HasMBID: a.HasMBID, Issues: portIssues(a.Issues), Source: a.Source.ID}
}

func portIssues(is []Issue) []libraryport.Issue {
	out := make([]libraryport.Issue, len(is))
	for i, x := range is {
		out[i] = libraryport.Issue{Kind: string(x.Kind), Fields: x.Fields}
	}
	return out
}

func (p port) Album(ctx context.Context, id int64) (libraryport.Album, error) {
	m := p.m
	d, opened, err := m.openAlbum(ctx, id)
	if err != nil {
		return libraryport.Album{}, notFound(err)
	}
	if d.Stale {
		m.Kick()
	}
	out := libraryport.Album{AlbumSummary: summary(d.albumJSON), Stale: d.Stale, Tracks: make([]libraryport.Track, len(d.Tracks))}
	for i, t := range d.Tracks {
		pt := libraryport.Track{ID: t.ID, Path: t.Path, Revision: t.Revision, Disc: t.Disc, Track: t.Track, Title: t.Title,
			Artist: t.Artist, Format: t.Format, Quality: t.Quality, Error: t.Error, Issues: portIssues(t.Issues),
			Tags: []libraryport.Tag{}, HasCover: opened.front[t.ID]}
		for _, k := range t.Standard {
			pt.Tags = append(pt.Tags, libraryport.Tag{Key: k.Key, Fields: k.Fields, Values: nonNil(k.Values), ReadOnly: k.ReadOnly})
		}
		for _, k := range t.Extended {
			pt.Tags = append(pt.Tags, libraryport.Tag{Key: k.Key, Values: nonNil(k.Values), Extended: true})
		}
		out.Tracks[i] = pt
	}
	return out, nil
}

func (p port) AlbumsOf(ctx context.Context, paths []string) (map[string]int64, error) {
	out := make(map[string]int64, len(paths))
	for _, path := range paths {
		id, err := p.m.store.albumOf(ctx, path)
		if err != nil {
			return nil, err
		}
		out[path] = id
	}
	return out, nil
}

func (p port) Suggestions(ctx context.Context, albumID int64, external bool) (json.RawMessage, error) {
	s, err := p.m.readSubject(ctx, albumID)
	if err != nil {
		return nil, notFound(err)
	}
	s.noPrints = true
	out, err := p.m.suggestFrom(ctx, s, "", external)
	if err != nil {
		return nil, err
	}
	return json.Marshal(out)
}

func (p port) AlbumCover(ctx context.Context, albumID int64) ([]byte, string, error) {
	m := p.m
	row, err := m.store.album(ctx, albumID)
	if err != nil {
		return nil, "", notFound(err)
	}
	files, err := m.store.albumFiles(ctx, albumID)
	if err != nil {
		return nil, "", err
	}
	b := m.shownCover(row.Folder, files)
	if b == nil {
		return nil, "", libraryport.ErrNotFound
	}
	return b, http.DetectContentType(b), nil
}

func (p port) History(ctx context.Context, after int64, limit int) (libraryport.HistoryPage, error) {
	return p.history(ctx, after, limit, false)
}

func (p port) SaveHistory(ctx context.Context, after int64, limit int) (libraryport.HistoryPage, error) {
	return p.history(ctx, after, limit, true)
}

func (p port) history(ctx context.Context, after int64, limit int, review bool) (libraryport.HistoryPage, error) {
	m := p.m
	if limit < 1 || limit > portPageHistory {
		limit = portPageHistory
	}
	// Clear can remove a later completed change while an earlier write is pending.
	// Neither returned rows nor the trim marker may let learning skip that write.
	var barrier int64
	if err := m.store.db.R.GetContext(ctx, &barrier, `SELECT coalesce(min(id),9223372036854775807) FROM library_changes WHERE parent IS NULL AND id > ? AND state IN ('queued','writing')`, after); err != nil {
		return libraryport.HistoryPage{}, err
	}
	var rows []changeRow
	if err := m.store.db.R.SelectContext(ctx, &rows, `SELECT `+changeCols+` FROM library_changes
		WHERE parent IS NULL AND id > ? AND id < ? ORDER BY id LIMIT ?`, after, barrier, limit); err != nil {
		return libraryport.HistoryPage{}, err
	}
	trimmed, err := m.store.trimmedThrough(ctx, m.store.db.R)
	if err != nil {
		return libraryport.HistoryPage{}, err
	}
	out := libraryport.HistoryPage{Changes: []libraryport.HistoryChange{}, Next: after, TrimmedThrough: min(trimmed, barrier-1)}
	for _, r := range rows {
		hc, err := p.historyChange(ctx, r, review)
		if err != nil {
			return libraryport.HistoryPage{}, err
		}
		out.Changes = append(out.Changes, hc)
		out.Next = r.ID
	}
	return out, nil
}

func (p port) historyChange(ctx context.Context, r changeRow, review bool) (libraryport.HistoryChange, error) {
	m := p.m
	j := m.changeJSON(ctx, r)
	hc := libraryport.HistoryChange{ID: r.ID, Kind: r.Kind, Target: r.Target, Origin: deref(r.Origin), AuthorID: deref(r.AuthorID),
		Author: j.Author, Folder: r.Folder, Title: r.Title, Artist: r.Artist, State: r.State, Undone: r.Undone,
		CreatedAt: r.CreatedAt.UTC().Format(time.RFC3339Nano), Files: []libraryport.HistoryFile{}}
	if j.Run != nil {
		// A run's album changes are its children, which the history by ID
		// never lists, and the run's own row has no files.
		if r.Kind == KindSave {
			hc.Kind = "run"
		}
		return hc, nil
	}
	if r.Kind == KindDelete {
		return hc, nil
	}
	files, err := m.store.changeFiles(ctx, r.ID)
	if err != nil {
		return hc, err
	}
	if !review && len(files) > historyFiles {
		files, hc.Truncated = files[:historyFiles], true
	}
	for _, f := range files {
		pf := libraryport.PreviewFile{Path: f.Path, Keys: portKeys(diffKeys(f.Before, f.After)), CoverBefore: f.CoverBefore, CoverAfter: f.CoverAfter}
		if f.Kind == fileAudio {
			if id, err := m.store.fileByPath(ctx, f.Path); err == nil {
				pf.TrackID = id
			}
		}
		hf := libraryport.HistoryFile{PreviewFile: pf, Written: f.Written}
		if review && f.Kind == fileAudio {
			keys := map[string][]string{}
			for key, values := range f.After {
				keys[key] = values
			}
			for key := range f.Before {
				if _, ok := keys[key]; !ok {
					keys[key] = nil
				}
			}
			for key, values := range keys {
				tag := libraryport.Tag{Key: key, Values: values}
				for _, field := range tagio.FieldsOf(tagio.FormatOf(f.Path), key) {
					tag.Fields = append(tag.Fields, field.Name)
				}
				hf.Tags = append(hf.Tags, tag)
			}
		}
		hc.Files = append(hc.Files, hf)
	}
	return hc, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func portKeys(ks []keyChangeJSON) []libraryport.KeyChange {
	out := make([]libraryport.KeyChange, len(ks))
	for i, k := range ks {
		out[i] = libraryport.KeyChange{Key: k.Key, Before: k.Before, After: k.After}
	}
	return out
}

func (p port) Changes(ctx context.Context, ids []int64) (map[int64]libraryport.ChangeStatus, error) {
	m := p.m
	out := map[int64]libraryport.ChangeStatus{}
	for _, id := range ids {
		c, err := m.store.change(ctx, m.store.db.R, id)
		if errors.Is(err, errNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		var n struct {
			Files   int `db:"files"`
			Written int `db:"written"`
		}
		if err := m.store.db.R.GetContext(ctx, &n, `SELECT count(*) AS files, coalesce(sum(written), 0) AS written
			FROM library_change_files WHERE change_id = ? OR change_id IN (SELECT id FROM library_changes WHERE parent = ?)`, id, id); err != nil {
			return nil, err
		}
		st := libraryport.ChangeStatus{ID: c.ID, State: c.State, Reason: c.Reason, Error: c.Error, Undone: c.Undone,
			Files: n.Files, Written: n.Written}
		if !c.FinishedAt.IsZero() {
			s := c.FinishedAt.UTC().Format(time.RFC3339)
			st.FinishedAt = &s
		}
		out[id] = st
	}
	return out, nil
}

func portImage(info imageInfo) libraryport.Image {
	return libraryport.Image{ID: info.ID, MIME: info.MIME, Width: info.Width, Height: info.Height, Size: info.Size}
}

// keep stores a checked cover, renews its time in the store, and lets Pin
// take it.
func (m *Module) keep(b []byte) (libraryport.Image, error) {
	m.coverInputMu.Lock()
	defer m.coverInputMu.Unlock()
	return m.keepUnlocked(b)
}

func (m *Module) keepUnlocked(b []byte) (libraryport.Image, error) {
	info, err := inspect(b)
	if err != nil {
		return libraryport.Image{}, refused(err)
	}
	// Agent uploads are external input. Decode only after bounding the
	// dimensions, so a small compressed file cannot allocate an unbounded image.
	if info.Width < 1 || info.Height < 1 || info.Width > 16384 || info.Height > 16384 || int64(info.Width)*int64(info.Height) > 40_000_000 {
		return libraryport.Image{}, fmt.Errorf("%w: artwork is limited to 40 million pixels and 16384 pixels per side", libraryport.ErrRefused)
	}
	if _, _, err := image.Decode(bytes.NewReader(b)); err != nil {
		return libraryport.Image{}, fmt.Errorf("%w: the image cannot be decoded", libraryport.ErrRefused)
	}
	if _, err := m.covers.put(b); err != nil {
		return libraryport.Image{}, err
	}
	if err := m.covers.touch(info.ID, time.Now()); err != nil {
		return libraryport.Image{}, err
	}
	m.checked(info.ID)
	return portImage(info), nil
}

func (p port) PutCover(_ context.Context, b []byte) (libraryport.Image, error) {
	if len(b) > uploadLimit {
		return libraryport.Image{}, fmt.Errorf("%w: a cover is up to 32 MiB", libraryport.ErrRefused)
	}
	return p.m.keep(b)
}

func (p port) MaterializeOffer(ctx context.Context, offer string) (libraryport.Image, error) {
	m := p.m
	r, err := m.store.remote(ctx, offer)
	if err != nil {
		return libraryport.Image{}, notFound(err)
	}
	b, err := m.fetchImage(ctx, r.URL)
	if errors.Is(err, tagio.ErrRefused) {
		return libraryport.Image{}, fmt.Errorf("%w: the image is over 32 MiB", libraryport.ErrRefused)
	}
	if err != nil {
		m.log.Warn("library: fetch a suggested cover", "error", err)
		return libraryport.Image{}, errors.New("the image can't be fetched")
	}
	return m.keep(b)
}

func (p port) CoverInfo(_ context.Context, id string) (libraryport.Image, error) {
	p.m.coverInputMu.Lock()
	defer p.m.coverInputMu.Unlock()
	b, err := p.m.covers.get(id)
	if err != nil {
		return libraryport.Image{}, notFound(err)
	}
	return p.m.keepUnlocked(b)
}

func (p port) CoverBytes(_ context.Context, id string) ([]byte, error) {
	b, err := p.m.covers.get(id)
	return b, notFound(err)
}

// Pin keeps covers through retention. It reads no file, so it can run in
// the caller's transaction: a cover qualifies when CoverInfo, PutCover or
// MaterializeOffer checked it lately, or when it is pinned already.
func (p port) Pin(ctx context.Context, tx *sqlx.Tx, holder string, ids []string) error {
	if holder == "" || len(holder) > 200 {
		return fmt.Errorf("%w: a pin's holder is 1 to 200 characters", libraryport.ErrRefused)
	}
	now := db.At(time.Now())
	for _, id := range ids {
		if !coverID.MatchString(id) {
			return fmt.Errorf("%w: cover %q", libraryport.ErrNotFound, id)
		}
		if !p.m.recentlyChecked(id) {
			var n int
			if err := tx.GetContext(ctx, &n, `SELECT count(*) FROM library_cover_pins WHERE cover = ?`, id); err != nil {
				return err
			}
			if n == 0 {
				return fmt.Errorf("%w: cover %s isn't checked; read it with CoverInfo before the transaction", libraryport.ErrNotFound, id)
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO library_cover_pins (holder, cover, created_at) VALUES (?, ?, ?)
			ON CONFLICT (holder, cover) DO NOTHING`, holder, id, now); err != nil {
			return err
		}
	}
	return nil
}

func (port) Unpin(ctx context.Context, tx *sqlx.Tx, holder string) error {
	_, err := tx.ExecContext(ctx, `DELETE FROM library_cover_pins WHERE holder = ?`, holder)
	return err
}

// portStored is a Plan's opaque part: the change Enqueue stores.
type portStored struct {
	AlbumID int64        `json:"album_id"`
	Folder  string       `json:"folder"`
	Title   string       `json:"title"`
	Artist  string       `json:"artist"`
	Change  storedChange `json:"change"`
}

func (p port) Plan(ctx context.Context, r libraryport.PlanRequest) (libraryport.Plan, error) {
	m := p.m
	stop := func(albumID int64, reason, msg string, track int64) (libraryport.Plan, error) {
		return libraryport.Plan{AlbumID: albumID, Refusal: &libraryport.Refusal{Error: msg, Reason: reason, TrackID: track}}, nil
	}
	if len(r.Files) == 0 {
		return stop(0, ReasonRefused, "a plan names at least one track", 0)
	}
	// One track may come from several operations.
	var order []int64
	edits := map[int64][]editJSON{}
	n := 0
	for _, f := range r.Files {
		if _, ok := edits[f.TrackID]; !ok {
			order = append(order, f.TrackID)
			edits[f.TrackID] = []editJSON{}
		}
		for _, e := range f.Edits {
			edits[f.TrackID] = append(edits[f.TrackID], editJSON{Field: e.Field, Key: e.Key, Values: e.Values, Anchor: e.Anchor})
			n++
		}
	}
	if n > maxEdits {
		return stop(0, ReasonRefused, "a plan holds up to "+strconv.Itoa(maxEdits)+" edits", 0)
	}
	albumID := int64(0)
	for _, id := range order {
		f, err := m.store.fileByID(ctx, id)
		if errors.Is(err, errNotFound) {
			return stop(0, "regrouped", fmt.Sprintf("track %d is gone from the library", id), id)
		}
		if err != nil {
			return libraryport.Plan{}, err
		}
		a, err := m.store.albumOf(ctx, f.Path)
		if err != nil {
			return libraryport.Plan{}, err
		}
		switch {
		case a == 0:
			return stop(0, "regrouped", fmt.Sprintf("track %d is on no album now", id), id)
		case albumID != 0 && a != albumID:
			return stop(0, "regrouped", "the tracks are on more than one album now", id)
		}
		albumID = a
	}
	row, err := m.store.album(ctx, albumID)
	if errors.Is(err, errNotFound) {
		return stop(0, "regrouped", "the album is gone from the library", 0)
	}
	if err != nil {
		return libraryport.Plan{}, err
	}
	files, err := m.store.albumFiles(ctx, albumID)
	if err != nil {
		return libraryport.Plan{}, err
	}
	var b saveBody
	if r.Cover != nil {
		for _, f := range files {
			if _, ok := edits[f.ID]; !ok {
				order = append(order, f.ID)
				edits[f.ID] = []editJSON{}
			}
		}
		b.Cover = &coverEdit{Upload: r.Cover.Image, Delete: r.Cover.Remove, EveryTrack: r.Cover.EveryTrack}
	}
	byID := map[int64]*file{}
	for _, f := range files {
		byID[f.ID] = f
	}
	for _, id := range order {
		f := byID[id]
		if f == nil {
			return stop(row.ID, "regrouped", fmt.Sprintf("track %d is not on the album now", id), id)
		}
		// The revision is the file's now, so a Plan never conflicts with an
		// unrelated write; Enqueue's job still checks it.
		abs := m.abs(f.Path)
		tf, err := tagio.Read(abs)
		if err != nil {
			_, st, err := m.stopFor(&tagio.FileError{Path: abs, Err: err}, map[string]int64{abs: id})
			if err != nil {
				return libraryport.Plan{}, err
			}
			return stop(row.ID, st.body.Reason, st.body.Error, id)
		}
		b.Tracks = append(b.Tracks, trackEdits{ID: id, Revision: tf.Revision(), Edits: edits[id]})
	}
	ps, st, err := m.planSave(ctx, row, files, b)
	if err != nil {
		return libraryport.Plan{}, err
	}
	if st != nil {
		return stop(row.ID, st.body.Reason, st.body.Error, st.body.Track)
	}
	preview, err := m.preview(ps, byID)
	if err != nil {
		return libraryport.Plan{}, err
	}
	stored, err := json.Marshal(portStored{AlbumID: row.ID, Folder: row.Folder, Title: row.Title, Artist: row.Artist, Change: ps.stored})
	if err != nil {
		return libraryport.Plan{}, err
	}
	out := libraryport.Plan{AlbumID: row.ID, Preview: preview, Stored: stored, Missing: []string{}}
	for _, w := range preview.Warnings {
		if !slices.Contains(r.Confirm, w.Kind) && !slices.Contains(out.Missing, w.Kind) {
			out.Missing = append(out.Missing, w.Kind)
		}
	}
	return out, nil
}

// previewDigest is what a reviewer accepts: each file's changing keys and
// cover, the folder's cover files, and the warnings. Revisions, track and
// album IDs and keys that don't change stay out, so an unrelated write
// leaves it alone.
type previewDigest struct {
	Album   libraryport.PreviewAlbum `json:"album"`
	Members []string                 `json:"members"`
	Files   []struct {
		Path        string                  `json:"path"`
		Keys        []libraryport.KeyChange `json:"keys"`
		CoverBefore string                  `json:"cover_before"`
		CoverAfter  string                  `json:"cover_after"`
	} `json:"files"`
	Folder   []libraryport.PreviewCover `json:"folder"`
	Warnings []libraryport.Warning      `json:"warnings"`
}

// preview is what a planned Save does, as the inbox shows it. A file's
// cover before and after are "" when the Save leaves its cover alone.
func (m *Module) preview(ps plannedSave, byID map[int64]*file) (libraryport.Preview, error) {
	trackOf := map[string]int64{}
	for _, f := range byID {
		trackOf[f.Path] = f.ID
	}
	covers := map[string]string{}
	for _, f := range ps.stored.Files {
		if f.Cover != nil {
			covers[f.Path] = f.Cover.ID
		}
	}
	out := libraryport.Preview{
		Album:    libraryport.PreviewAlbum{ID: ps.row.ID, Folder: ps.row.Folder, Title: ps.row.Title, Artist: ps.row.Artist},
		Files:    []libraryport.PreviewFile{},
		Folder:   []libraryport.PreviewCover{},
		Warnings: []libraryport.Warning{},
	}
	for _, pf := range ps.planned.Files {
		rel := m.rel(pf.Path)
		f := libraryport.PreviewFile{TrackID: trackOf[rel], Path: rel, Keys: portKeys(diffKeys(pf.Before, pf.After))}
		if after, ok := covers[rel]; ok {
			before, err := tagio.FrontCover(pf.Path)
			if err != nil {
				return out, err
			}
			f.CoverBefore, f.CoverAfter = coverHash(before), after
			if len(before) > 0 {
				if err := m.keepPreviewCover(before); err != nil {
					return out, err
				}
			}
			if after != "" {
				b, err := m.covers.get(after)
				if err != nil {
					return out, err
				}
				if err := m.keepPreviewCover(b); err != nil {
					return out, err
				}
			}
		}
		if len(f.Keys) == 0 && f.CoverBefore == f.CoverAfter {
			continue
		}
		out.Files = append(out.Files, f)
	}
	slices.SortFunc(out.Files, func(a, b libraryport.PreviewFile) int { return strings.Compare(a.Path, b.Path) })
	for _, op := range ps.stored.Folder {
		if op.Was != "" {
			hash, b, err := fileHash(m.abs(op.Path))
			if err != nil {
				return out, err
			}
			if hash != op.Was {
				return out, fmt.Errorf("%w: artwork changed during preview", tagio.ErrConflict)
			}
			if err := m.keepPreviewCover(b); err != nil {
				return out, err
			}
		}
		if op.ID != "" {
			b, err := m.covers.get(op.ID)
			if err != nil {
				return out, err
			}
			if err := m.keepPreviewCover(b); err != nil {
				return out, err
			}
		}
		out.Folder = append(out.Folder, libraryport.PreviewCover{Path: op.Path, Before: op.Was, After: op.ID})
	}
	slices.SortFunc(out.Folder, func(a, b libraryport.PreviewCover) int { return strings.Compare(a.Path, b.Path) })
	for _, w := range ps.warns {
		out.Warnings = append(out.Warnings, libraryport.Warning{Kind: w.Kind, Message: w.Message})
	}
	slices.SortStableFunc(out.Warnings, func(a, b libraryport.Warning) int { return strings.Compare(a.Kind, b.Kind) })
	var d previewDigest
	d.Album = out.Album
	d.Album.ID = 0
	for _, f := range byID {
		d.Members = append(d.Members, f.Path)
	}
	slices.Sort(d.Members)
	for _, f := range out.Files {
		d.Files = append(d.Files, struct {
			Path        string                  `json:"path"`
			Keys        []libraryport.KeyChange `json:"keys"`
			CoverBefore string                  `json:"cover_before"`
			CoverAfter  string                  `json:"cover_after"`
		}{f.Path, f.Keys, f.CoverBefore, f.CoverAfter})
	}
	d.Folder, d.Warnings = out.Folder, out.Warnings
	b, err := json.Marshal(d)
	if err != nil {
		return out, err
	}
	sum := sha256.Sum256(b)
	out.Digest = hex.EncodeToString(sum[:])
	return out, nil
}

// Old artwork may be damaged or use another format. Review must preserve
// those exact bytes, just as the write journal does for undo.
func (m *Module) keepPreviewCover(b []byte) error {
	id, err := m.covers.put(b)
	if err != nil {
		return err
	}
	if err := m.covers.touch(id, time.Now()); err != nil {
		return err
	}
	m.checked(id)
	return nil
}

func (p port) Enqueue(ctx context.Context, tx *sqlx.Tx, pl libraryport.Plan, origin string) (int64, func(), error) {
	m := p.m
	if pl.Refusal != nil {
		return 0, nil, fmt.Errorf("%w: %s", libraryport.ErrRefused, pl.Refusal.Error)
	}
	if len(pl.Missing) > 0 {
		return 0, nil, fmt.Errorf("%w: %s", libraryport.ErrMissingConfirm, strings.Join(pl.Missing, ", "))
	}
	var ps portStored
	if err := json.Unmarshal(pl.Stored, &ps); err != nil || ps.AlbumID == 0 || len(ps.Change.Files) == 0 && len(ps.Change.Folder) == 0 {
		return 0, nil, fmt.Errorf("%w: the plan holds no change", libraryport.ErrRefused)
	}
	req, err := json.Marshal(ps.Change)
	if err != nil {
		return 0, nil, err
	}
	cr := changeRow{Kind: KindSave, AlbumID: ps.AlbumID, Folder: ps.Folder, Title: ps.Title, Artist: ps.Artist,
		State: StateQueued, Request: string(req), CreatedAt: db.At(time.Now())}
	if origin != "" {
		var n int
		if err := tx.GetContext(ctx, &n, `SELECT count(*) FROM library_changes WHERE origin = ?`, origin); err != nil {
			return 0, nil, err
		}
		if n > 0 {
			return 0, nil, libraryport.ErrOriginTaken
		}
		cr.Origin = &origin
	}
	if err := m.queueJobTx(ctx, tx, &cr, "web", WriteJob); err != nil {
		if strings.Contains(err.Error(), "library_changes.origin") {
			return 0, nil, libraryport.ErrOriginTaken
		}
		return 0, nil, err
	}
	after := func() {
		m.jobs.Kick()
		m.publishChange(context.WithoutCancel(ctx), cr)
	}
	return cr.ID, after, nil
}

// touch renews a stored cover's time, so the sweep counts its keep from now.
func (cs coverStore) touch(id string, at time.Time) error {
	for _, ext := range storeExts {
		err := os.Chtimes(filepath.Join(cs.dir, id+ext), at, at)
		if err == nil || !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return errNotFound
}
