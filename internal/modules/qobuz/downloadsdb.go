package qobuz

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"

	qobuzdl "github.com/tikhonp/qobuz-dl-go"
	"github.com/tikhonp/shelf/internal/modules/downloads"
	"github.com/tikhonp/shelf/internal/source"
)

// History is shelf's record of what it has downloaded, downloaded_items in
// the downloads module.
type History interface {
	IsDownloaded(ctx context.Context, ref source.Ref) (bool, error)
	RecordDownloaded(ctx context.Context, d downloads.Downloaded) error
}

// downloadsDB is qobuz-dl-go's downloads database over shelf's history. Only
// a playlist's tracks reach Has, because Download passes IgnoreDB for albums
// and tracks.
type downloadsDB struct {
	history History
	root    string
}

// Has ignores the quality, which Python matches on: downloaded_items keeps
// what was delivered and not what was asked for, and Queue doesn't look at
// the format either.
func (d downloadsDB) Has(ctx context.Context, mediaType, id string, _ int) (bool, error) {
	return d.history.IsDownloaded(ctx, ref(source.Kind(mediaType), id))
}

// Add keeps a path outside the music root as it is, as the import does.
func (d downloadsDB) Add(ctx context.Context, rec qobuzdl.DownloadRecord) error {
	path, err := filepath.Rel(d.root, rec.SavedPath)
	if err != nil || !filepath.IsLocal(path) {
		path = rec.SavedPath
	}
	return d.history.RecordDownloaded(ctx, downloads.Downloaded{
		Ref:     ref(source.Kind(rec.MediaType), rec.ID),
		Quality: qualityText(rec.FileFormat, rec.BitDepth, rec.SamplingRate),
		Path:    path,
	})
}

// qualityText is what downloaded_items keeps, from a row of qobuz-dl's
// database: "FLAC 24/96", "MP3", or nothing for the format it calls Unknown.
// The numbers are str() of Python's values, so 96 may read 96.0.
func qualityText(format string, bits, khz *string) string {
	q := source.Quality{Codec: strings.ToLower(format)}
	switch q.Codec {
	case "mp3":
		return q.Short()
	case "flac":
	default:
		return ""
	}
	if bits != nil && khz != nil {
		b, berr := strconv.ParseFloat(*bits, 64)
		k, kerr := strconv.ParseFloat(*khz, 64)
		if berr == nil && kerr == nil {
			q.BitDepth, q.SampleRate = int(b), hz(k)
		}
	}
	return q.Short()
}
