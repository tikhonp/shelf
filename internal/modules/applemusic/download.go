package applemusic

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tikhonp/shelf/internal/modules/applemusic/amdl"
	"github.com/tikhonp/shelf/internal/source"
)

// Format IDs.
const (
	FormatALAC  = ID + ":alac"
	FormatAtmos = ID + ":atmos"
	FormatAAC   = ID + ":aac"
)

// Formats are ALAC, the default, Dolby Atmos and AAC.
func (s *Source) Formats() []source.Format {
	return []source.Format{
		{ID: FormatALAC, Label: "ALAC", Description: "Apple Music ALAC, converted to FLAC by the amdl config", Default: true},
		{ID: FormatAtmos, Label: "Dolby Atmos", Description: "Apple Music Dolby Atmos"},
		{ID: FormatAAC, Label: "AAC", Description: "Apple Music AAC"},
	}
}

var formatFlags = map[string][]string{FormatALAC: nil, FormatAtmos: {"--atmos"}, FormatAAC: {"--aac"}}

// barEvery is how often a progress bar's redraw reaches the job log, and
// progressEvery how often it reaches the song's progress. A bar's last
// redraw, at 100%, always does.
const (
	barEvery      = 5 * time.Second
	progressEvery = time.Second
)

// Download runs amdl once for an album, playlist or song. amdl skips the
// files it finds complete, so a run after a restart picks up where the
// last one stopped.
func (s *Source) Download(ctx context.Context, req source.DownloadRequest, rep source.Reporter) (source.DownloadResult, error) {
	res := source.DownloadResult{Tool: "amdl " + amdl.Version, TrackNoun: "song"}
	f, ok := source.FindFormat(s.Formats(), req.Format)
	if !ok {
		return res, fmt.Errorf("apple music: unknown format %q", req.Format)
	}
	switch req.Ref.Kind {
	case source.KindAlbum, source.KindPlaylist, source.KindTrack:
	default:
		return res, fmt.Errorf("apple music: can't download a %s", req.Ref.Kind)
	}
	base, err := s.render.Ensure()
	if err != nil {
		return res, err
	}
	s.base.Store(&base)
	sf := s.store.get(ctx)
	res.Tool += ", wrapper-lite storefront " + sf
	rel, err := s.release(ctx, req.Ref)
	if err != nil {
		return res, err
	}
	// amdl would save the videos and list nothing.
	if len(rel.item.Tracks) == 0 {
		return res, fmt.Errorf("%s has no songs; music videos are not supported yet", rel.item.Title)
	}
	// amdl would fail every song, and leave a folder of lyrics and a cover.
	if f.ID == FormatAtmos && !hasAtmos(rel.item) {
		return res, fmt.Errorf("%s has no Dolby Atmos version", rel.item.Title)
	}
	rep.Plan(rel.item.Tracks)
	link := downloadLink(sf, req.Ref)
	log := rep.Log()
	log.Info("Running amdl %s for %s", strings.Join(append([]string{"--json"}, formatFlags[f.ID]...), " "), link)

	tr := amdl.NewTracker()
	var lastBar, lastProgress time.Time
	pos := 0
	line := func(l string) {
		if n := tr.Line(l); n > 0 {
			if pos = rel.position(n); pos > 0 {
				rep.Start(pos)
			}
		}
		if bar, rest := amdl.SplitBar(l); bar != "" {
			full := strings.Contains(bar, "100%")
			if n, ok := amdl.Bar(bar); ok && pos > 0 && (full || time.Since(lastProgress) >= progressEvery) {
				lastProgress = time.Now()
				rep.Progress(pos, n)
			}
			// A failure that cut the bar short always reaches the log.
			if rest == "" && !full && time.Since(lastBar) < barEvery {
				return
			}
			lastBar = time.Now()
		}
		log.Info("%s", l)
	}
	out, err := s.runner.Run(ctx, link, formatFlags[f.ID], line)
	// A song can fail once and pass on the next try, as when Apple's key
	// server refuses a key. amdl skips the files it finished, so a second
	// run costs little.
	var ee *amdl.ExitError
	if errors.As(err, &ee) && amdl.FailedPass(out.Tail) {
		log.Warn("A song failed, so amdl runs once more: %s", strings.Join(ee.Lines, " | "))
		tr.Again()
		pos = 0
		out, err = s.runner.Run(ctx, link, formatFlags[f.ID], line)
	}
	if err != nil {
		return res, err
	}
	if !out.Listed {
		return res, fmt.Errorf("amdl stopped before it listed its songs: %s", strings.Join(amdl.ErrorLines(out.Tail), " | "))
	}
	if done := s.collect(req, f.ID, rel, out.Tracks, tr, rep, &res); done == 0 && len(rel.item.Tracks) > 0 {
		reason := "amdl wrote no songs"
		if last := tr.LastFailure(); last != "" {
			reason += ": " + last
		}
		return res, errors.New(reason)
	}
	return res, nil
}

// hasAtmos reports whether an item may have a Dolby Atmos version. It may
// when the item or one of its songs says so, or when a song's traits are
// unknown.
func hasAtmos(it source.Item) bool {
	if it.Quality.Atmos {
		return true
	}
	for _, t := range it.Tracks {
		if t.Quality.Atmos || t.Quality.IsZero() {
			return true
		}
	}
	return false
}

// position is the track position of amdl's item number n.
func (r release) position(n int) int {
	if r.positions == nil {
		return 1
	}
	return r.positions[n]
}

// task is amdl's item number of a track position.
func (r release) task(pos int, tr *amdl.Tracker) int {
	if r.positions == nil {
		return tr.First()
	}
	for n, p := range r.positions {
		if p == pos {
			return n
		}
	}
	return 0
}

// collect matches amdl's list to the release's tracks and reports each
// track. amdl lists songs in release order with their catalog names, so a
// song matches the next track of that name. It returns how many tracks
// amdl wrote.
func (s *Source) collect(req source.DownloadRequest, format string, rel release, written []amdl.Track, tr *amdl.Tracker, rep source.Reporter, res *source.DownloadResult) int {
	log := rep.Log()
	tracks := rel.item.Tracks
	assigned := map[int]bool{}
	folders := map[string]bool{}
	cursor := 0
	for _, w := range written {
		path, err := filepath.Rel(req.MusicDir, w.Path)
		if err != nil || !filepath.IsLocal(path) {
			log.Warn("amdl wrote %s, outside the music folder", w.Path)
			continue
		}
		if dir := filepath.Dir(path); !folders[dir] {
			folders[dir] = true
			res.Folders = append(res.Folders, dir)
		}
		res.Files = append(res.Files, path)
		pos := match(tracks, assigned, &cursor, w.Song)
		if pos == 0 {
			log.Warn("amdl wrote %s, which matches no song of this release", path)
			continue
		}
		assigned[pos] = true
		q, size := fileQuality(w.Path, format)
		rep.Done(pos, source.TrackResult{Path: path, Quality: q, Size: size, Skipped: tr.Skipped(rel.task(pos, tr))})
	}
	for _, t := range tracks {
		if assigned[t.Position] {
			continue
		}
		msg := tr.Failure(rel.task(t.Position, tr))
		if msg == "" {
			msg = "amdl did not write this song"
		}
		rep.Done(t.Position, source.TrackResult{Err: errors.New(msg)})
	}
	return len(assigned)
}

func match(tracks []source.Track, assigned map[int]bool, cursor *int, song string) int {
	same := func(i int) bool {
		return !assigned[tracks[i].Position] && strings.EqualFold(strings.TrimSpace(tracks[i].Title), strings.TrimSpace(song))
	}
	for i := *cursor; i < len(tracks); i++ {
		if same(i) {
			*cursor = i + 1
			return tracks[i].Position
		}
	}
	for i := 0; i < *cursor && i < len(tracks); i++ {
		if same(i) {
			return tracks[i].Position
		}
	}
	// A single song is whatever amdl wrote, whatever its name.
	if len(tracks) == 1 && !assigned[tracks[0].Position] {
		return tracks[0].Position
	}
	return 0
}

// fileQuality reads a FLAC file's stream info. An .m4a file is what the
// format asked for, since reading MP4 boxes isn't worth it here.
func fileQuality(path, format string) (source.Quality, int64) {
	var size int64
	if fi, err := os.Stat(path); err == nil {
		size = fi.Size()
	}
	if strings.EqualFold(filepath.Ext(path), ".flac") {
		if bits, rate, ok := flacInfo(path); ok {
			return source.Quality{Codec: "flac", BitDepth: bits, SampleRate: rate, HiRes: bits > 16}, size
		}
		return source.Quality{Codec: "flac"}, size
	}
	switch format {
	case FormatAtmos:
		return source.Quality{Codec: "ec-3", Atmos: true}, size
	case FormatAAC:
		return source.Quality{Codec: "aac"}, size
	}
	return source.Quality{Codec: "alac"}, size
}

// flacInfo reads the bit depth and sample rate from a FLAC file's
// STREAMINFO block, which comes first.
func flacInfo(path string) (bits, rate int, ok bool) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, false
	}
	defer func() { _ = f.Close() }()
	var b [42]byte
	if _, err := io.ReadFull(f, b[:]); err != nil || string(b[:4]) != "fLaC" || b[4]&0x7f != 0 {
		return 0, 0, false
	}
	si := b[8:]
	rate = int(si[10])<<12 | int(si[11])<<4 | int(si[12])>>4
	bits = int((si[12]&0x01)<<4|si[13]>>4) + 1
	return bits, rate, rate > 0
}
