package library

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/tikhonp/shelf/internal/modules/library/tagio"
)

// Covers.
const (
	// coverMax is the longest side of an embedded cover. The folder's
	// cover file keeps the upload's size.
	coverMax = 1000
	// uploadLimit is the largest cover upload.
	uploadLimit = 32 << 20
	// judgeTimeout bounds one ffmpeg check of a picture.
	judgeTimeout = 20 * time.Second
	// uploadKeep is how long an upload no change uses stays in the store.
	uploadKeep = 24 * time.Hour
)

// BadCover is an opened album's cover that ffmpeg can't decode. The walk
// reads no picture bytes, so the album list doesn't have it.
const BadCover Kind = "bad_cover"

// BodyLimits lets a cover upload and a Save with long lyrics past the
// platform's 1 MiB.
func (*Module) BodyLimits() map[string]int64 {
	return map[string]int64{"/api/library/covers": uploadLimit, "/api/library/albums/:id/save": 8 << 20}
}

// coverStore keeps cover images under /data by the SHA-256 of their bytes:
// uploads, the embedded copies shelf makes of them, and every cover a
// change replaced, so an undo can put it back.
type coverStore struct{ dir string }

var coverID = regexp.MustCompile(`^[0-9a-f]{64}$`)

func hashOf(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// put stores an image and gives its ID. It takes any bytes, because a
// cover a change replaces may be in any format, or damaged.
func (cs coverStore) put(b []byte) (string, error) {
	id := hashOf(b)
	ext := storeExt(b)
	p := filepath.Join(cs.dir, id+ext)
	if _, err := os.Stat(p); err == nil {
		return id, nil
	}
	if err := os.MkdirAll(cs.dir, 0o755); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(cs.dir, ".put-*")
	if err != nil {
		return "", err
	}
	_, err = tmp.Write(b)
	if err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), p)
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
	}
	return id, err
}

// storeExt names a stored image's file by its format.
func storeExt(b []byte) string {
	if ext, ok := map[string]string{"image/jpeg": ".jpg", "image/png": ".png"}[http.DetectContentType(b)]; ok {
		return ext
	}
	return ".bin"
}

var storeExts = []string{".jpg", ".png", ".bin"}

// get reads an image by its ID.
func (cs coverStore) get(id string) ([]byte, error) {
	if !coverID.MatchString(id) {
		return nil, errNotFound
	}
	for _, ext := range storeExts {
		b, err := os.ReadFile(filepath.Join(cs.dir, id+ext))
		if err == nil {
			return b, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
	}
	return nil, errNotFound
}

// sweep removes images no change refers to, except uploads younger than
// uploadKeep, which may still be saved.
func (cs coverStore) sweep(used map[string]bool, now time.Time) (int, error) {
	entries, err := os.ReadDir(cs.dir)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	n := 0
	for _, e := range entries {
		id := strings.TrimSuffix(e.Name(), path.Ext(e.Name()))
		if !coverID.MatchString(id) || used[id] {
			continue
		}
		info, err := e.Info()
		if err != nil || now.Sub(info.ModTime()) < uploadKeep {
			continue
		}
		if err := os.Remove(filepath.Join(cs.dir, e.Name())); err == nil {
			n++
		}
	}
	return n, nil
}

// coverHash is a front cover's store ID, "" for none.
func coverHash(b []byte) string {
	if b == nil {
		return ""
	}
	return hashOf(b)
}

// imageInfo is what the editor shows of a cover.
type imageInfo struct {
	ID     string `json:"id"`
	MIME   string `json:"mime"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Size   int    `json:"size"`
}

func inspect(b []byte) (imageInfo, error) {
	mime := http.DetectContentType(b)
	if mime != "image/jpeg" && mime != "image/png" {
		return imageInfo{}, fmt.Errorf("%w: a cover is JPEG or PNG, not %s", tagio.ErrRefused, mime)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(b))
	if err != nil {
		return imageInfo{}, fmt.Errorf("%w: the image can't be read: %w", tagio.ErrRefused, err)
	}
	return imageInfo{ID: hashOf(b), MIME: mime, Width: cfg.Width, Height: cfg.Height, Size: len(b)}, nil
}

// embedded is the copy of a cover that goes into the files: the cover
// itself up to coverMax on its longest side, else a copy scaled down to it
// in the same format.
func embedded(b []byte) ([]byte, error) {
	info, err := inspect(b)
	if err != nil {
		return nil, err
	}
	if info.Width <= coverMax && info.Height <= coverMax {
		return b, nil
	}
	src, _, err := image.Decode(bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("%w: the image can't be read: %w", tagio.ErrRefused, err)
	}
	w, h := info.Width, info.Height
	if w >= h {
		w, h = coverMax, max(1, h*coverMax/w)
	} else {
		w, h = max(1, w*coverMax/h), coverMax
	}
	dst := shrink(src, w, h)
	var out bytes.Buffer
	if info.MIME == "image/png" {
		err = png.Encode(&out, dst)
	} else {
		err = jpeg.Encode(&out, dst, &jpeg.Options{Quality: 90})
	}
	return out.Bytes(), err
}

// shrink scales src down to w by h, each pixel the average of the source
// pixels it covers.
func shrink(src image.Image, w, h int) *image.NRGBA {
	b := src.Bounds()
	sw, sh := b.Dx(), b.Dy()
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		y0, y1 := b.Min.Y+y*sh/h, b.Min.Y+max((y+1)*sh/h, y*sh/h+1)
		for x := range w {
			x0, x1 := b.Min.X+x*sw/w, b.Min.X+max((x+1)*sw/w, x*sw/w+1)
			var r, g, bl, a, n uint64
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					c := color.NRGBAModel.Convert(src.At(sx, sy)).(color.NRGBA)
					r, g, bl, a = r+uint64(c.R), g+uint64(c.G), bl+uint64(c.B), a+uint64(c.A)
					n++
				}
			}
			dst.SetNRGBA(x, y, color.NRGBA{R: uint8(r / n), G: uint8(g / n), B: uint8(bl / n), A: uint8(a / n)})
		}
	}
	return dst
}

// errCorrupt is a picture ffmpeg can't decode cleanly.
var errCorrupt = errors.New("ffmpeg can't decode the picture")

// judge has ffmpeg decode a picture. Any error it prints makes the picture
// corrupt, as a truncated JPEG makes ffmpeg say "overread" and still exit 0.
// An error without errCorrupt is ffmpeg that didn't run.
func (m *Module) judge(ctx context.Context, b []byte) error {
	ctx, cancel := context.WithTimeout(ctx, judgeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, m.FFmpeg, "-nostdin", "-v", "error", "-i", "pipe:0", "-f", "null", "-")
	cmd.Stdin = bytes.NewReader(b)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	if msg := strings.TrimSpace(stderr.String()); msg != "" {
		line, _, _ := strings.Cut(msg, "\n")
		return fmt.Errorf("%w: %s", errCorrupt, line)
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return errCorrupt
	}
	return err
}

// albumCovers are an album's pictures: its folder's cover files, then each
// different embedded front cover, by where they came from.
func (m *Module) albumCovers(folder string, paths []string) (from []string, pics [][]byte) {
	seen := map[string]bool{}
	add := func(src string, b []byte) {
		if b != nil && !seen[hashOf(b)] {
			seen[hashOf(b)] = true
			from, pics = append(from, src), append(pics, b)
		}
	}
	for _, name := range m.folderCovers(folder) {
		rel := path.Join(folder, name)
		if b, err := os.ReadFile(m.abs(rel)); err == nil {
			add(rel, b)
		}
	}
	slices.Sort(paths)
	for _, p := range paths {
		if b, err := tagio.FrontCover(m.abs(p)); err == nil {
			add(p, b)
		}
	}
	return from, pics
}

// badCovers names the pictures of an album that ffmpeg can't decode. A
// missing ffmpeg judges nothing.
func (m *Module) badCovers(ctx context.Context, folder string, paths []string) []string {
	from, pics := m.albumCovers(folder, paths)
	var bad []string
	for i, b := range pics {
		err := m.judge(ctx, b)
		if errors.Is(err, errCorrupt) {
			bad = append(bad, from[i])
		} else if err != nil {
			m.log.Warn("library: judge a cover", "error", err)
			return nil
		}
	}
	return bad
}

// repair finds a cover to stage in place of a damaged one: the first of the
// album's pictures ffmpeg decodes, else the damaged one decoded as far as
// it goes and written again as a JPEG.
func (m *Module) repair(c *echo.Context) error {
	id, err := idParam(c, "album")
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	row, err := m.store.album(ctx, id)
	if errors.Is(err, errNotFound) {
		return echo.NewHTTPError(http.StatusNotFound, "no such album")
	}
	if err != nil {
		return err
	}
	files, err := m.store.albumFiles(ctx, id)
	if err != nil {
		return err
	}
	paths := make([]string, len(files))
	for i, f := range files {
		paths[i] = f.Path
	}
	_, pics := m.albumCovers(row.Folder, paths)
	var good, bad []byte
	for _, b := range pics {
		switch err := m.judge(ctx, b); {
		case err == nil && good == nil:
			good = b
		case errors.Is(err, errCorrupt) && bad == nil:
			bad = b
		case err != nil && !errors.Is(err, errCorrupt):
			return err
		}
	}
	switch {
	case bad == nil:
		return echo.NewHTTPError(http.StatusUnprocessableEntity, "no cover of this album is damaged")
	case good == nil:
		fixed, err := m.reencode(ctx, bad)
		if err != nil || m.judge(ctx, fixed) != nil || len(fixed) == 0 {
			return echo.NewHTTPError(http.StatusUnprocessableEntity, "ffmpeg can't repair this cover; upload another")
		}
		good = fixed
	}
	info, err := inspect(good)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnprocessableEntity, "the album's good cover is neither JPEG nor PNG; upload another")
	}
	if _, err := m.covers.put(good); err != nil {
		return err
	}
	return c.JSON(http.StatusCreated, info)
}

// reencode has ffmpeg decode what it can of a picture and write it again as
// a JPEG.
func (m *Module) reencode(ctx context.Context, b []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, judgeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, m.FFmpeg, "-nostdin", "-v", "error", "-i", "pipe:0", "-frames:v", "1",
		"-c:v", "mjpeg", "-q:v", "2", "-f", "image2pipe", "pipe:1")
	cmd.Stdin = bytes.NewReader(b)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// coverName matches the cover files a cover edit replaces.
var coverName = regexp.MustCompile(`(?i)^cover\.(jpe?g|png|gif|webp|bmp|avif)$`)

// folderCovers lists the cover files of a release folder, by name.
func (m *Module) folderCovers(folder string) []string {
	entries, err := os.ReadDir(m.abs(folder))
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.Type().IsRegular() && coverName.MatchString(e.Name()) {
			out = append(out, e.Name())
		}
	}
	return out
}

// fileHash is the hash of a file's bytes, or "" when it isn't there.
func fileHash(p string) (string, []byte, error) {
	b, err := os.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil, nil
	}
	if err != nil {
		return "", nil, err
	}
	return hashOf(b), b, nil
}

// upload stores a cover the editor sends as the raw body.
func (m *Module) upload(c *echo.Context) error {
	b, err := io.ReadAll(c.Request().Body)
	if err != nil {
		return err
	}
	info, err := inspect(b)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnprocessableEntity, strings.TrimPrefix(err.Error(), tagio.ErrRefused.Error()+": "))
	}
	if _, err := m.covers.put(b); err != nil {
		return err
	}
	return c.JSON(http.StatusCreated, info)
}

// getCover serves a stored cover. Its ID is its hash, so it never changes.
func (m *Module) getCover(c *echo.Context) error {
	b, err := m.covers.get(c.Param("id"))
	if errors.Is(err, errNotFound) {
		return echo.NewHTTPError(http.StatusNotFound, "no such cover")
	}
	if err != nil {
		return err
	}
	c.Response().Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	return c.Blob(http.StatusOK, http.DetectContentType(b), b)
}

// albumCover serves the cover Navidrome shows for an album: the first
// folder image its cover priority names, else a track's embedded cover.
func (m *Module) albumCover(c *echo.Context) error {
	id, err := idParam(c, "album")
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	row, err := m.store.album(ctx, id)
	if errors.Is(err, errNotFound) {
		return echo.NewHTTPError(http.StatusNotFound, "no such album")
	}
	if err != nil {
		return err
	}
	files, err := m.store.albumFiles(ctx, id)
	if err != nil {
		return err
	}
	b := m.shownCover(row.Folder, files)
	if b == nil {
		return echo.NewHTTPError(http.StatusNotFound, "the album has no cover")
	}
	c.Response().Header().Set("Cache-Control", "private, no-cache")
	return c.Blob(http.StatusOK, http.DetectContentType(b), b)
}

// shownCover is the cover Navidrome shows for an album, or nil.
func (m *Module) shownCover(folder string, files []*file) []byte {
	rules := m.currentRules()
	names := m.folderImageNames(folder)
	for _, p := range rules.CoverNames() {
		for _, name := range names {
			if ok, _ := path.Match(p, strings.ToLower(name)); ok {
				if b, err := os.ReadFile(m.abs(path.Join(folder, name))); err == nil {
					return b
				}
			}
		}
	}
	if rules.EmbeddedCovers() {
		paths := make([]string, len(files))
		for i, f := range files {
			paths[i] = f.Path
		}
		slices.Sort(paths)
		for _, p := range paths {
			if b, err := tagio.FrontCover(m.abs(p)); err == nil && b != nil {
				return b
			}
		}
	}
	return nil
}

// folderImageNames lists a folder's image files with their own case.
func (m *Module) folderImageNames(folder string) []string {
	entries, err := os.ReadDir(m.abs(folder))
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if n := e.Name(); e.Type().IsRegular() && !dotEntry(n) && imageExts[strings.ToLower(path.Ext(n))] {
			out = append(out, n)
		}
	}
	slices.Sort(out)
	return out
}
