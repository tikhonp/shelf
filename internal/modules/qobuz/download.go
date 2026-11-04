package qobuz

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"

	qobuzdl "github.com/tikhonp/qobuz-dl-go"
	"github.com/tikhonp/shelf/internal/source"
)

// formats are qobuz-dl's format IDs, best first.
var formats = []struct {
	id           int
	label, words string
}{
	{27, "Max", "FLAC up to 24-bit 192 kHz"},
	{7, "24/96", "FLAC up to 24-bit 96 kHz"},
	{6, "CD", "FLAC 16-bit 44.1 kHz"},
	{5, "MP3 320", "MP3 320 kbps"},
}

// Formats lists Max, 24/96, CD and MP3 320, with SHELF_QOBUZ_QUALITY as the
// default.
func (s *Source) Formats() []source.Format {
	out := make([]source.Format, len(formats))
	for i, f := range formats {
		out[i] = source.Format{
			ID: ID + ":" + strconv.Itoa(f.id), Label: f.label,
			Description: "Qobuz " + strconv.Itoa(f.id) + ", " + f.words, Default: f.id == s.quality,
		}
	}
	return out
}

const (
	portModule = "github.com/tikhonp/qobuz-dl-go"
	// portedVersion is the qobuz-dl release the port follows, from its
	// UPSTREAM.md. The port has no exported version.
	portedVersion = "2.5.9"
)

// tool is the line shelf.txt records.
func tool() string {
	v := "unknown"
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, d := range bi.Deps {
			if d.Path == portModule {
				v = d.Version
			}
		}
	}
	return "qobuz-dl-go " + v + " porting qobuz-dl " + portedVersion
}

// Reasons a download wrote nothing, which reach the download's error.
var (
	errNoFormat = errors.New("qobuz: this format isn't offered, and SHELF_QOBUZ_FALLBACK is off")
	errDemo     = errors.New("qobuz: only a demo of this track is offered")
	errNothing  = errors.New("qobuz: nothing was delivered")
)

// Download fetches an album, track or playlist. An album or track ignores
// the database, because Queue has asked it already and a job that gets here
// must run. Asked again, it would find the row of a copy queued next to it,
// or its own row after a restart, and skip with no tracks to report. The
// check for files on disk still skips what is complete.
func (s *Source) Download(ctx context.Context, req source.DownloadRequest, rep source.Reporter) (source.DownloadResult, error) {
	res := source.DownloadResult{Tool: tool()}
	if _, ok := source.FindFormat(s.Formats(), req.Format); !ok {
		return res, fmt.Errorf("qobuz: unknown format %q", req.Format)
	}
	quality, err := strconv.Atoi(strings.TrimPrefix(req.Format, ID+":"))
	if err != nil {
		return res, fmt.Errorf("qobuz: format %q: %w", req.Format, err)
	}
	switch req.Ref.Kind {
	case source.KindAlbum, source.KindTrack, source.KindPlaylist:
	default:
		return res, fmt.Errorf("qobuz: can't download a %s", req.Ref.Kind)
	}
	root, err := filepath.Abs(req.MusicDir)
	if err != nil {
		return res, err
	}
	d, err := s.downloader(ctx, quality, req.Force)
	if err != nil {
		return res, s.failed(err)
	}
	p := &progress{s: s, rep: rep, root: root}
	var out *qobuzdl.Result
	if req.Ref.Kind == source.KindPlaylist {
		// Only HandleID writes the m3u8.
		out, err = d.HandleID(ctx, string(source.KindPlaylist), req.Ref.ID, p)
	} else {
		out, err = d.DownloadFromID(ctx, qobuzdl.DownloadRequest{
			ID: req.Ref.ID, Album: req.Ref.Kind == source.KindAlbum, IgnoreDB: true, Progress: p,
		})
	}
	p.collect(out, &res)
	if err != nil {
		return res, s.failed(err)
	}
	return res, s.failed(wroteNothing(out))
}

// wroteNothing says why a download has nothing to show. The port returns no
// error for a release it skipped or couldn't start, such as an album/get
// answering 401. A release that partly failed went through the Reporter.
func wroteNothing(out *qobuzdl.Result) error {
	var why error
	for i := range out.Releases {
		r := &out.Releases[i]
		var err error
		switch {
		case r.Status == qobuzdl.StatusDone, r.Status == qobuzdl.StatusPartial, r.Skip == qobuzdl.SkipInDatabase:
			return nil
		case r.Skip == qobuzdl.SkipQuality:
			err = errNoFormat
		case r.Skip == qobuzdl.SkipDemo:
			err = errDemo
		case r.Err != nil:
			err = r.Err
		default:
			err = errNothing
		}
		if why == nil {
			why = err
		}
	}
	return why
}

// progress is qobuz-dl-go's Progress over a Reporter. A track ends in the
// work folder, so Done goes out twice: at TrackEnd, to move the counts while
// the download runs, and in collect with the final path.
type progress struct {
	s    *Source
	rep  source.Reporter
	root string
}

// ReleaseStart plans the tracks. The plan replaces the tracks resolve stored
// and PlannedTrack has no artist, so the artists come from the release.
func (p *progress) ReleaseStart(r qobuzdl.ReleaseInfo) {
	known := map[int]source.Track{}
	fallback := ""
	if r.Album != nil {
		fallback = r.Album.Artist.Name
		for _, t := range albumTracks(r.Album) {
			known[t.Position] = t
		}
	}
	if r.Playlist != nil {
		for _, t := range playlistTracks(r.Playlist) {
			known[t.Position] = t
		}
	}
	tracks := make([]source.Track, len(r.Tracks))
	for i, t := range r.Tracks {
		tracks[i] = source.Track{ID: t.ID, Position: t.Position, Disc: t.DiscNumber, Title: t.Title, ISRC: t.ISRC,
			Duration: t.Duration, Artist: cmp.Or(known[t.Position].Artist, fallback)}
		if r.Playlist != nil {
			tracks[i].Disc = 0
		}
	}
	p.rep.Plan(tracks)
	// The work folder is the album's own folder with a state prefix, and
	// qobuz-dl-go renames it back when the release ends. Both names are
	// busy until then.
	if rel, ok := p.rel(r.Folder); ok {
		p.rep.Working(rel)
		dir, base := filepath.Split(rel)
		if name, cut := strings.CutPrefix(base, "[IN PROGRESS] "); cut {
			p.rep.Working(filepath.Join(dir, name))
			p.rep.Working(filepath.Join(dir, "[INCOMPLETE] "+name))
		}
	}
}

func (p *progress) TrackStart(t qobuzdl.TrackInfo) { p.rep.Start(t.Position) }

func (p *progress) TrackBytes(position int, done, _ int64) { p.rep.Progress(position, done) }

func (p *progress) TrackEnd(t qobuzdl.TrackResult) {
	if t.Outcome != qobuzdl.OutcomeFailed {
		p.rep.Done(t.Position, source.TrackResult{Quality: delivered(t), Skipped: t.Outcome == qobuzdl.OutcomeSkipped})
		return
	}
	err := cmp.Or(t.Err, errNothing)
	if isExpired(err) {
		err = p.s.failed(err)
	}
	p.rep.Done(t.Position, source.TrackResult{Err: err})
}

// Log passes on the lines Python shows. Some end in a newline.
func (p *progress) Log(level slog.Level, msg string) {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return
	}
	log := p.rep.Log()
	switch {
	case level >= slog.LevelError:
		log.Error("%s", msg)
	case level >= slog.LevelWarn:
		log.Warn("%s", msg)
	case level >= slog.LevelInfo:
		log.Info("%s", msg)
	default:
		log.Debug("%s", msg)
	}
}

// delivered is a track's quality. An MP3's tier reads 16/44.1, which says
// nothing about it, so only the codec counts.
func delivered(t qobuzdl.TrackResult) source.Quality {
	q := source.Quality{Codec: strings.ToLower(t.Codec)}
	if q.Codec == "flac" {
		q.BitDepth, q.SampleRate, q.HiRes = t.BitDepth, hz(t.SamplingRate), t.BitDepth > 16
	}
	return q
}

// collect reports each file at its final path and fills in the result.
func (p *progress) collect(out *qobuzdl.Result, res *source.DownloadResult) {
	folders := []string{out.Folder}
	if out.Kind != string(source.KindPlaylist) {
		folders = folders[:0]
		for i := range out.Releases {
			folders = append(folders, out.Releases[i].Folder)
		}
	}
	for _, f := range folders {
		if rel, ok := p.rel(f); ok {
			res.Folders = append(res.Folders, rel)
		}
	}
	for _, t := range out.Tracks() {
		path, ok := p.rel(t.Path)
		if !ok {
			continue
		}
		var size int64
		if fi, err := os.Stat(t.Path); err == nil {
			size = fi.Size()
		}
		res.Files = append(res.Files, path)
		p.rep.Done(t.Position, source.TrackResult{Path: path, Quality: delivered(t), Size: size, Skipped: t.Outcome == qobuzdl.OutcomeSkipped})
	}
	if m3u, ok := p.rel(out.M3U); ok {
		res.Files = append(res.Files, m3u)
	}
}

// rel is path relative to the music root. Empty paths are the tracks and
// folders that never came to be.
func (p *progress) rel(path string) (string, bool) {
	if path == "" {
		return "", false
	}
	rel, err := filepath.Rel(p.root, path)
	if err != nil || !filepath.IsLocal(rel) {
		p.rep.Log().Warn("Qobuz wrote %s, outside the music folder", path)
		return "", false
	}
	return rel, true
}
