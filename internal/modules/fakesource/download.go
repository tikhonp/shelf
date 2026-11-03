package fakesource

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tikhonp/shelf/internal/source"
)

// Formats are FLAC, the default, and MP3.
func (s *Source) Formats() []source.Format {
	return []source.Format{
		{ID: s.id + ":flac", Label: "FLAC", Description: s.name + " FLAC, 24-bit 96 kHz", Default: true},
		{ID: s.id + ":mp3", Label: "MP3", Description: s.name + " MP3"},
	}
}

// TrackSize is the size of every fake file.
const TrackSize = 1024

// Download writes one small file per track into <music>/<id>/<release>,
// skipping files that are already complete.
func (s *Source) Download(ctx context.Context, req source.DownloadRequest, rep source.Reporter) (source.DownloadResult, error) {
	res := source.DownloadResult{Tool: "fakesource", TrackNoun: "track"}
	if _, ok := source.FindFormat(s.Formats(), req.Format); !ok {
		return res, fmt.Errorf("fake: unknown format %q", req.Format)
	}
	it, err := s.item(req.Ref)
	if err != nil {
		return res, err
	}
	var folder string
	tracks := it.Tracks
	switch req.Ref.Kind {
	case source.KindAlbum:
		folder = it.Artist + " - " + it.Title
	case source.KindPlaylist:
		folder = it.Title
	case source.KindTrack:
		folder = "Singles"
		tracks = []source.Track{{ID: it.Ref.ID, Position: 1, Disc: 1, Title: it.Title, Artist: it.Artist, ISRC: it.ISRC, Quality: it.Quality}}
	default:
		return res, fmt.Errorf("fake: can't download a %s", req.Ref.Kind)
	}
	rel := filepath.Join(s.id, folder)
	if err := os.MkdirAll(filepath.Join(req.MusicDir, rel), 0o755); err != nil {
		return res, err
	}
	res.Folders = []string{rel}
	ext, q := ".flac", source.Quality{Codec: "flac", BitDepth: 24, SampleRate: 96000}
	if strings.HasSuffix(req.Format, ":mp3") {
		ext, q = ".mp3", source.Quality{Codec: "mp3"}
	}
	rep.Plan(tracks)
	rep.Log().Info("Downloading %d tracks of %s into %s", len(tracks), it.Title, rel)
	for _, t := range tracks {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		if s.Hook != nil {
			if err := s.Hook(ctx, req.Ref, t.Position); err != nil {
				return res, err
			}
		}
		rep.Start(t.Position)
		if strings.HasSuffix(req.Ref.ID, "-slow") {
			select {
			case <-ctx.Done():
				return res, ctx.Err()
			case <-time.After(200 * time.Millisecond):
			}
		}
		if strings.HasSuffix(req.Ref.ID, "-fail") && t.Position == 2 {
			rep.Log().Error("Track %d failed: the fake source fails it on purpose", t.Position)
			rep.Done(t.Position, source.TrackResult{Err: errors.New("fake failure")})
			continue
		}
		name := fmt.Sprintf("%02d - %s%s", t.Position, t.Title, ext)
		path := filepath.Join(rel, name)
		skipped, err := writeTrack(filepath.Join(req.MusicDir, path), t.ID)
		if err != nil {
			rep.Done(t.Position, source.TrackResult{Err: err})
			continue
		}
		rep.Progress(t.Position, TrackSize)
		rep.Done(t.Position, source.TrackResult{Path: path, Quality: q, Size: TrackSize, Skipped: skipped})
		res.Files = append(res.Files, path)
	}
	return res, nil
}

// writeTrack writes the file through a temporary name, so a cut download
// never leaves a short file behind. A complete file is kept.
func writeTrack(path, id string) (skipped bool, err error) {
	if fi, err := os.Stat(path); err == nil && fi.Size() == TrackSize {
		return true, nil
	}
	data := []byte(strings.Repeat(fmt.Sprintf("fake audio %s\n", id), TrackSize)[:TrackSize])
	tmp := filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".tmp")
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return false, err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return false, err
	}
	return false, nil
}
