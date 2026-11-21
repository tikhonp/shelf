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
	// amdl names its folders only when it ends, so all of its folder is
	// busy while it runs.
	rep.Working(s.dir)
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
		if ctx.Err() != nil {
			return res, err
		}
		var written []writtenTrack
		if out.Listed {
			written = existingTracks(out.Tracks)
		} else {
			written = s.recoverWritten(req.MusicDir, rel, tr)
		}
		done := s.collect(req, f.ID, rel, written, tr, rep, &res, true)
		if done > 0 && errors.As(err, &ee) && amdl.FailedPass(out.Tail) {
			return res, nil
		}
		return res, err
	}
	if !out.Listed {
		return res, fmt.Errorf("amdl stopped before it listed its songs: %s", strings.Join(amdl.ErrorLines(out.Tail), " | "))
	}
	if done := s.collect(req, f.ID, rel, fromAMDL(out.Tracks), tr, rep, &res, false); done == 0 && len(rel.item.Tracks) > 0 {
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

type writtenTrack struct {
	path string
	song string
	// pos is set when recovery already matched the file to a release track,
	// so duplicate titles on different discs cannot shift onto another track.
	pos int
}

func fromAMDL(tracks []amdl.Track) []writtenTrack {
	out := make([]writtenTrack, len(tracks))
	for i, t := range tracks {
		out[i] = writtenTrack{path: t.Path, song: t.Song}
	}
	return out
}

// collect matches amdl's list to the release's tracks and reports each
// track. amdl lists songs in release order with their catalog names, so a
// song matches the next track of that name. When fromErr is set, tracks
// amdl never started stay unreported so finish marks them not downloaded.
// It returns how many tracks amdl wrote or found complete.
func (s *Source) collect(req source.DownloadRequest, format string, rel release, written []writtenTrack, tr *amdl.Tracker, rep source.Reporter, res *source.DownloadResult, fromErr bool) int {
	log := rep.Log()
	tracks := rel.item.Tracks
	assigned := map[int]bool{}
	folders := map[string]bool{}
	cursor := 0
	for _, w := range written {
		path, err := filepath.Rel(req.MusicDir, w.path)
		if err != nil || !filepath.IsLocal(path) {
			log.Warn("amdl wrote %s, outside the music folder", w.path)
			continue
		}
		if dir := filepath.Dir(path); !folders[dir] {
			folders[dir] = true
			res.Folders = append(res.Folders, dir)
		}
		res.Files = append(res.Files, path)
		pos := 0
		if w.pos > 0 && !assigned[w.pos] {
			pos = w.pos
		} else {
			pos = match(tracks, assigned, &cursor, w.song)
		}
		if pos == 0 {
			log.Warn("amdl wrote %s, which matches no song of this release", path)
			continue
		}
		assigned[pos] = true
		q, size := fileQuality(w.path, format)
		rep.Done(pos, source.TrackResult{Path: path, Quality: q, Size: size, Skipped: tr.Skipped(rel.task(pos, tr))})
	}
	for _, t := range tracks {
		if assigned[t.Position] {
			continue
		}
		task := rel.task(t.Position, tr)
		msg := tr.Failure(task)
		if msg == "" && fromErr {
			msg = tr.PrevFailure(task)
		}
		if msg == "" {
			if fromErr && !tr.Started(task) {
				continue
			}
			msg = "amdl did not write this song"
		}
		rep.Done(t.Position, source.TrackResult{Err: errors.New(msg)})
	}
	return len(assigned)
}

// existingTracks keeps only tracks whose file exists on disk.
func existingTracks(tracks []amdl.Track) []writtenTrack {
	var out []writtenTrack
	for _, t := range tracks {
		if fi, err := os.Stat(t.Path); err == nil && !fi.IsDir() {
			out = append(out, writtenTrack{path: t.Path, song: t.Song})
		}
	}
	return out
}

// recoverWritten finds the files amdl wrote or found complete on disk when
// a non-zero exit prevented it from printing its JSON list. Every file is
// verified on disk before being returned.
func (s *Source) recoverWritten(musicDir string, rel release, tr *amdl.Tracker) []writtenTrack {
	folder := tr.Folder()
	if folder == "" {
		return nil
	}
	dir := findReleaseDir(filepath.Join(musicDir, s.dir), folder, rel.item.Artist)
	if dir == "" {
		return nil
	}
	audioFiles := listAudioFiles(dir)
	used := map[string]bool{}
	var out []writtenTrack
	for _, t := range rel.item.Tracks {
		task := rel.task(t.Position, tr)
		if task == 0 || !tr.Completed(task) {
			continue
		}
		full := matchTrackFile(dir, audioFiles, used, tr.File(task), tr.Title(task), t.Title)
		if full == "" {
			continue
		}
		used[full] = true
		out = append(out, writtenTrack{path: full, song: t.Title, pos: t.Position})
	}
	return out
}

// findReleaseDir locates the directory under root that matches the folder
// name amdl printed, allowing for amdl's filename sanitisation. It checks
// direct children first, then an artist subfolder matching artist.
func findReleaseDir(root, printed, artist string) string {
	want := sanitizeAMDLName(printed)
	if want == "" {
		return ""
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if e.IsDir() && (e.Name() == printed || strings.EqualFold(sanitizeAMDLName(e.Name()), want)) {
			return filepath.Join(root, e.Name())
		}
	}
	wantArtist := sanitizeAMDLName(artist)
	if wantArtist == "" {
		return ""
	}
	var match string
	for _, e := range entries {
		if !e.IsDir() || (e.Name() != artist && !strings.EqualFold(sanitizeAMDLName(e.Name()), wantArtist)) {
			continue
		}
		sub := filepath.Join(root, e.Name())
		children, err := os.ReadDir(sub)
		if err != nil {
			continue
		}
		for _, c := range children {
			if c.IsDir() && (c.Name() == printed || strings.EqualFold(sanitizeAMDLName(c.Name()), want)) {
				if match != "" {
					return ""
				}
				match = filepath.Join(sub, c.Name())
			}
		}
	}
	return match
}

func listAudioFiles(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		switch strings.ToLower(filepath.Ext(e.Name())) {
		case ".flac", ".m4a", ".aac", ".mp3", ".opus", ".ogg", ".wav":
			full := filepath.Join(dir, e.Name())
			if fi, err := os.Stat(full); err == nil && !fi.IsDir() {
				out = append(out, full)
			}
		}
	}
	return out
}

func matchTrackFile(dir string, audioFiles []string, used map[string]bool, converted, header, title string) string {
	if converted != "" {
		full := filepath.Join(dir, filepath.Base(converted))
		if !used[full] {
			if fi, err := os.Stat(full); err == nil && !fi.IsDir() {
				return full
			}
		}
	}
	if header != "" {
		want := sanitizeAMDLName(header)
		for _, full := range audioFiles {
			if used[full] {
				continue
			}
			base := filepath.Base(full)
			stem := strings.TrimSuffix(base, filepath.Ext(base))
			if stem == header || strings.EqualFold(sanitizeAMDLName(stem), want) {
				return full
			}
		}
	}
	if title != "" {
		want := strings.ToLower(sanitizeAMDLName(title))
		for _, full := range audioFiles {
			if used[full] {
				continue
			}
			base := filepath.Base(full)
			stem := strings.ToLower(sanitizeAMDLName(strings.TrimSuffix(base, filepath.Ext(base))))
			if stem == want || strings.HasSuffix(stem, ". "+want) {
				return full
			}
		}
	}
	return ""
}

var amdlSanitizer = strings.NewReplacer(
	"<", "_", ">", "_", ":", "_", `"`, "_", "/", "_", `\`, "_", "|", "_", "?", "_", "*", "_",
)

func sanitizeAMDLName(s string) string {
	return strings.TrimSpace(amdlSanitizer.Replace(s))
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
