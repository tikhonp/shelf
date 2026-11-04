package tagio

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Why a write was refused or stopped. Each comes wrapped in a *FileError
// that names the file.
var (
	// ErrConflict is a file whose revision moved since the editor loaded it.
	ErrConflict = errors.New("the file changed since it was loaded")
	// ErrLocked is a file shelf may not replace: another user owns it, or
	// its folder isn't writable.
	ErrLocked = errors.New("shelf can't write this file")
	// ErrBusy is a file in a folder a download is working in.
	ErrBusy = errors.New("a download is working in this folder")
	// ErrRefused is a change this file can't take.
	ErrRefused = errors.New("this file can't take the change")
	// ErrVerify is a temp copy whose tags, pictures or audio came out
	// different from what was asked. The originals are untouched.
	ErrVerify = errors.New("the written copy didn't match")
	// ErrPartial is a rename that failed after others in the same write
	// had replaced their files.
	ErrPartial = errors.New("only some files were replaced")
)

// FileError ties an error to the file it stopped at.
type FileError struct {
	Path string
	Err  error
}

func (e *FileError) Error() string { return e.Path + ": " + e.Err.Error() }
func (e *FileError) Unwrap() error { return e.Err }

// Edit changes one tag of one file. It names either a field, which changes
// every key of the file that feeds it, or a raw key, for tags Navidrome
// doesn't read.
type Edit struct {
	Field string
	Key   string
	// Values are the new values in order. None removes the tag.
	Values []string
}

// Cover replaces the embedded front cover, or adds one. Nil Data removes
// it. Other pictures stay.
type Cover struct {
	Data []byte
}

// FileChange is what one file of a write should end with.
type FileChange struct {
	// Path is absolute.
	Path string
	// Revision is File.Revision as the editor loaded it.
	Revision string
	Edits    []Edit
	Cover    *Cover
}

// Change is one write: every file of an album, replaced together.
type Change struct {
	Files []FileChange
}

// FileResult is one file of a write. Files whose change came to nothing
// aren't listed.
type FileResult struct {
	Path   string
	Before map[string][]string
	// After is what the file was read back with, or what it was to get
	// when it wasn't written.
	After    map[string][]string
	Pictures []Picture
	// Revision is the file's revision after the write, or before it when it
	// wasn't written.
	Revision string
	Written  bool
}

// Result is what a write did.
type Result struct {
	Files []FileResult
}

// Journal records a write before any file is touched and again when it
// ends, so a crash in between leaves a row that names every file.
type Journal interface {
	Begin(ctx context.Context, r Result) (int64, error)
	End(ctx context.Context, id int64, r Result, err error) error
}

// Writer changes tags through temp copies. It runs one write at a time; the
// tags queue sees to that.
type Writer struct {
	// UID is the owner a file needs before shelf replaces it.
	UID int
	// FFmpeg runs the audio check of formats other than FLAC.
	FFmpeg string
	// Busy reports a folder a download is working in. Nil means none is.
	Busy    func(dir string) bool
	Journal Journal

	// fault runs before each stage of each file, so tests can fail or
	// damage one.
	fault func(stage, path string) error
	// groups, when set, replaces the check that shelf may give a file a
	// group, for tests that can't make a file of another group.
	groups func(gid int) bool
}

// NewWriter returns a writer for files this process owns.
func NewWriter() *Writer { return &Writer{UID: os.Getuid(), FFmpeg: "ffmpeg"} }

// The stages of a write, in order.
const (
	stageCopy     = "copy"
	stageTags     = "tags"
	stageCover    = "cover"
	stageReadBack = "readback"
	stageAudio    = "audio"
	stageSync     = "sync"
	stageRestat   = "restat"
	stageRename   = "rename"
	stageDirSync  = "dirsync"
)

// plan is one file of a write, from its checks to its rename.
type plan struct {
	file     File
	stat     os.FileInfo
	gid      int
	ops      map[string][]string
	want     map[string][]string
	cover    *coverOp
	wantPics []Picture
	// picSums hash the pictures the file should end with, in order.
	picSums []string
	tmp     string
	// after is the verified temp copy.
	after File
}

type coverOp struct {
	index int
	data  []byte // nil removes
	mime  string
}

// Write replaces the files of ch with temp copies that hold the change. It
// checks every file first, then writes, reads back and verifies every copy,
// and renames them over the originals only when all passed. A failure before
// the renames leaves every original untouched.
func (w *Writer) Write(ctx context.Context, ch Change) (res Result, err error) {
	plans, err := w.plan(ch)
	if err != nil {
		return Result{}, err
	}
	res = planned(plans)
	if len(plans) == 0 {
		return res, nil
	}

	var id int64
	if w.Journal != nil {
		// The journal gets its own copy: res keeps changing as files land.
		if id, err = w.Journal.Begin(ctx, Result{Files: slices.Clone(res.Files)}); err != nil {
			return Result{}, fmt.Errorf("journal: %w", err)
		}
		defer func() {
			if jerr := w.Journal.End(context.WithoutCancel(ctx), id, Result{Files: slices.Clone(res.Files)}, err); jerr != nil {
				err = errors.Join(err, fmt.Errorf("journal: %w", jerr))
			}
		}()
	}
	defer func() {
		for _, p := range plans {
			if p.tmp != "" {
				if rerr := os.Remove(p.tmp); rerr != nil && !errors.Is(rerr, fs.ErrNotExist) {
					err = errors.Join(err, rerr)
				}
			}
		}
	}()

	for i := range plans {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		if err := w.stage(ctx, &plans[i]); err != nil {
			return res, &FileError{Path: plans[i].file.Path, Err: err}
		}
	}

	// Nothing has been replaced yet. A file that moved since its checks, or
	// a download that started in its folder, stops the write here.
	for _, p := range plans {
		if err := w.restat(p); err != nil {
			return res, &FileError{Path: p.file.Path, Err: err}
		}
	}

	// The renames run back to back and ignore ctx. A write that has started
	// replacing files finishes.
	for i := range plans {
		p := &plans[i]
		err := w.at(stageRename, p.file.Path)
		if err == nil {
			err = os.Rename(p.tmp, p.file.Path)
		}
		if err != nil {
			fe := &FileError{Path: p.file.Path, Err: err}
			if i == 0 {
				return res, fe
			}
			return res, fmt.Errorf("%w: %d of %d: %w", ErrPartial, i, len(plans), fe)
		}
		p.tmp = ""
		after := p.after
		after.Path = p.file.Path
		res.Files[i].After, res.Files[i].Pictures = after.Tags, after.Pictures
		res.Files[i].Revision, res.Files[i].Written = after.Revision(), true
	}

	var dirs []string
	for _, p := range plans {
		if d := filepath.Dir(p.file.Path); !slices.Contains(dirs, d) {
			dirs = append(dirs, d)
		}
	}
	for _, d := range dirs {
		if err := w.at(stageDirSync, d); err != nil {
			return res, fmt.Errorf("sync folder %s: %w", d, err)
		}
		if err := syncDir(d); err != nil {
			return res, fmt.Errorf("sync folder %s: %w", d, err)
		}
	}
	return res, nil
}

// Check runs every check of a write and works out its change, touching no
// file. The result lists the files that would change, with what each is to
// end with.
func (w *Writer) Check(ch Change) (Result, error) {
	plans, err := w.plan(ch)
	if err != nil {
		return Result{}, err
	}
	return planned(plans), nil
}

func planned(plans []plan) Result {
	var res Result
	for _, p := range plans {
		res.Files = append(res.Files, FileResult{
			Path: p.file.Path, Before: p.file.Tags, After: p.want, Pictures: p.wantPics, Revision: p.file.Revision(),
		})
	}
	return res
}

func (w *Writer) keepsGroup(gid int) bool {
	if w.groups != nil {
		return w.groups(gid)
	}
	return canKeepGroup(gid)
}

func (w *Writer) at(stage, path string) error {
	if w.fault == nil {
		return nil
	}
	return w.fault(stage, path)
}

// plan checks every file and works out its change. It touches no file.
func (w *Writer) plan(ch Change) ([]plan, error) {
	var out []plan
	seen := map[string]bool{}
	for _, fc := range ch.Files {
		p, changed, err := w.planFile(fc)
		if err == nil && seen[fc.Path] {
			err = fmt.Errorf("%w: the file is listed twice", ErrRefused)
		}
		if err != nil {
			return nil, &FileError{Path: fc.Path, Err: err}
		}
		seen[fc.Path] = true
		if changed {
			out = append(out, p)
		}
	}
	return out, nil
}

func (w *Writer) planFile(fc FileChange) (plan, bool, error) {
	var p plan
	if !filepath.IsAbs(fc.Path) {
		return p, false, fmt.Errorf("%w: the path isn't absolute", ErrRefused)
	}
	ft := FormatOf(fc.Path)
	if ft == nil {
		return p, false, fmt.Errorf("%w: %w", ErrRefused, ErrNotAudio)
	}
	st, err := os.Lstat(fc.Path)
	if err != nil {
		return p, false, err
	}
	if !st.Mode().IsRegular() {
		return p, false, fmt.Errorf("%w: not a regular file", ErrRefused)
	}
	if uid, gid, links, ok := owner(st); ok {
		if uid != w.UID {
			return p, false, fmt.Errorf("%w: owned by UID %d", ErrLocked, uid)
		}
		// A rename would split the file from its other names.
		if links > 1 {
			return p, false, fmt.Errorf("%w: the file has %d hard links", ErrRefused, links)
		}
		// The copy must end with the file's group, and shelf can give a
		// file only a group it is in.
		if !w.keepsGroup(gid) {
			return p, false, fmt.Errorf("%w: its group %d isn't one of shelf's", ErrLocked, gid)
		}
		p.gid = gid
	}
	dir := filepath.Dir(fc.Path)
	if !writableDir(dir) {
		return p, false, fmt.Errorf("%w: the folder isn't writable", ErrLocked)
	}
	if w.Busy != nil && w.Busy(dir) {
		return p, false, ErrBusy
	}
	f, err := readAs(fc.Path, ft, st)
	if err != nil {
		return p, false, err
	}
	if fc.Revision == "" || fc.Revision != f.Revision() {
		return p, false, ErrConflict
	}
	p.file, p.stat = f, st

	if p.ops, err = resolve(f, fc.Edits); err != nil {
		return p, false, err
	}
	p.want = apply(f.Tags, p.ops)
	if p.cover, p.wantPics, err = planCover(f, fc.Cover); err != nil {
		return p, false, err
	}
	return p, len(p.ops) > 0 || p.cover != nil, nil
}

// resolve turns edits into the keys to set or remove. A field edit changes
// every key of the file that feeds the field, because Navidrome takes the
// first alias of a single value and merges the aliases of a multi-value
// tag: a stale alias would win or add a value. With none present it adds
// the field's key.
func resolve(f File, edits []Edit) (map[string][]string, error) {
	ops := map[string][]string{}
	set := func(k string, vs []string) error {
		if _, dup := ops[k]; dup {
			return fmt.Errorf("%w: two edits change %s", ErrRefused, k)
		}
		if f.Format.ReadOnly(k) {
			return fmt.Errorf("%w: TagLib can't change %s in %s files", ErrRefused, k, f.Format.Label)
		}
		ops[k] = vs
		return nil
	}
	for _, e := range edits {
		for _, v := range e.Values {
			if !validValue(v) {
				return nil, fmt.Errorf("%w: an empty value or one with a control separator", ErrRefused)
			}
		}
		if !f.Format.Multi && len(e.Values) > 1 {
			return nil, fmt.Errorf("%w: %s files keep one value per tag", ErrRefused, f.Format.Label)
		}
		switch {
		case e.Field != "" && e.Key != "":
			return nil, fmt.Errorf("%w: an edit names a field and a key", ErrRefused)
		case e.Field != "":
			fd, ok := FieldByName(e.Field)
			if !ok {
				return nil, fmt.Errorf("%w: no field %q", ErrRefused, e.Field)
			}
			var present []string
			for k := range f.Tags {
				if fd.feeds(f.Format, k) {
					present = append(present, k)
				}
			}
			slices.Sort(present)
			if len(present) == 0 && len(e.Values) > 0 {
				present = []string{fd.Key}
			}
			for _, k := range present {
				if err := set(k, e.Values); err != nil {
					return nil, err
				}
			}
		case e.Key != "":
			if !validKey(e.Key) {
				return nil, fmt.Errorf("%w: key %q", ErrRefused, e.Key)
			}
			if !f.Format.Custom && len(FieldsOf(f.Format, e.Key)) == 0 {
				return nil, fmt.Errorf("%w: %s files drop custom fields", ErrRefused, f.Format.Label)
			}
			if err := set(existingKey(f.Tags, e.Key), e.Values); err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("%w: an edit names no field or key", ErrRefused)
		}
	}
	for k, vs := range ops {
		if old, has := f.Tags[k]; slices.Equal(old, vs) || (!has && len(vs) == 0) {
			delete(ops, k)
		}
	}
	return ops, nil
}

// existingKey finds the file's spelling of a key. TagLib reads keys in
// uppercase, so a new key is uppercase too.
func existingKey(tags map[string][]string, key string) string {
	if _, ok := tags[key]; ok {
		return key
	}
	for k := range tags {
		if strings.EqualFold(k, key) {
			return k
		}
	}
	return strings.ToUpper(key)
}

func apply(tags map[string][]string, ops map[string][]string) map[string][]string {
	out := make(map[string][]string, len(tags))
	for k, vs := range tags {
		out[k] = slices.Clone(vs)
	}
	for k, vs := range ops {
		if len(vs) == 0 {
			delete(out, k)
		} else {
			out[k] = slices.Clone(vs)
		}
	}
	return out
}

// flacPictureMax is the largest picture a FLAC block holds. TagLib drops a
// bigger one without an error.
const flacPictureMax = 1<<24 - 1

func (f File) frontIndex() int {
	return slices.IndexFunc(f.Pictures, func(p Picture) bool {
		return p.Type == frontCover || (f.Format.mp4 && p.Type == "")
	})
}

func planCover(f File, c *Cover) (*coverOp, []Picture, error) {
	pics := slices.Clone(f.Pictures)
	if c == nil {
		return nil, pics, nil
	}
	front := f.frontIndex()
	if c.Data == nil {
		if front < 0 {
			return nil, pics, nil
		}
		return &coverOp{index: front}, slices.Delete(pics, front, front+1), nil
	}
	mime := http.DetectContentType(c.Data)
	if mime != "image/jpeg" && mime != "image/png" {
		return nil, nil, fmt.Errorf("%w: a cover is JPEG or PNG, not %s", ErrRefused, mime)
	}
	if f.Format.flac && len(c.Data) > flacPictureMax {
		return nil, nil, fmt.Errorf("%w: FLAC holds pictures up to 16 MiB", ErrRefused)
	}
	pic := Picture{Type: frontCover, MIME: mime}
	if f.Format.mp4 {
		pic.Type = ""
	}
	if front < 0 {
		return &coverOp{index: len(pics), data: c.Data, mime: mime}, append(pics, pic), nil
	}
	pics[front] = pic
	return &coverOp{index: front, data: c.Data, mime: mime}, pics, nil
}

// stage writes and verifies one file's temp copy.
func (w *Writer) stage(ctx context.Context, p *plan) error {
	src := p.file.Path
	// The pictures to keep are hashed from the original, so a copy that
	// lost or damaged one fails the check.
	p.picSums = make([]string, 0, len(p.wantPics))
	for i := range p.file.Pictures {
		if p.cover != nil && i == p.cover.index {
			if p.cover.data != nil {
				p.picSums = append(p.picSums, sum(p.cover.data))
			}
			continue
		}
		b, err := readImage(src, i)
		if err != nil {
			return err
		}
		p.picSums = append(p.picSums, sum(b))
	}
	if p.cover != nil && p.cover.data != nil && p.cover.index == len(p.file.Pictures) {
		p.picSums = append(p.picSums, sum(p.cover.data))
	}

	tmp, err := createTemp(filepath.Dir(src))
	if err != nil {
		return err
	}
	p.tmp = tmp.Name()
	audio, err := w.copy(tmp, p)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}

	if len(p.ops) > 0 {
		if err := w.at(stageTags, p.tmp); err != nil {
			return err
		}
		if err := writeTags(p.tmp, p.ops); err != nil {
			return err
		}
	}
	if c := p.cover; c != nil {
		if err := w.at(stageCover, p.tmp); err != nil {
			return err
		}
		if err := writeImage(p.tmp, c.data, c.index, c.mime); err != nil {
			return err
		}
	}

	if err := w.at(stageReadBack, p.tmp); err != nil {
		return err
	}
	if err := p.readBack(); err != nil {
		return err
	}

	if err := w.at(stageAudio, p.tmp); err != nil {
		return err
	}
	if err := w.checkAudio(ctx, p, audio); err != nil {
		return err
	}

	if err := w.at(stageSync, p.tmp); err != nil {
		return err
	}
	if err := syncFile(p.tmp); err != nil {
		return err
	}
	st, err := os.Stat(p.tmp)
	if err != nil {
		return err
	}
	p.after.Size, p.after.ModTime = st.Size(), st.ModTime()
	return nil
}

// copy fills the temp file from the original, with its mode and group, and
// hashes a FLAC file's audio on the way.
func (w *Writer) copy(tmp *os.File, p *plan) (flacSum string, err error) {
	if err := w.at(stageCopy, tmp.Name()); err != nil {
		return "", err
	}
	src, err := os.Open(p.file.Path)
	if err != nil {
		return "", err
	}
	defer func() { _ = src.Close() }()
	var dst io.Writer = tmp
	var rh *rangeHash
	if p.file.Format.flac {
		start, end, err := flacAudio(src, p.stat.Size())
		if err != nil {
			return "", fmt.Errorf("%w: %w", ErrRefused, err)
		}
		rh = &rangeHash{h: sha256.New(), start: start, end: end}
		dst = io.MultiWriter(tmp, rh)
	}
	n, err := io.Copy(dst, src)
	if err != nil {
		return "", err
	}
	if n != p.stat.Size() {
		return "", fmt.Errorf("%w: copied %d of %d bytes", ErrConflict, n, p.stat.Size())
	}
	if err := tmp.Chmod(p.stat.Mode().Perm()); err != nil {
		return "", err
	}
	if _, _, _, ok := owner(p.stat); ok {
		if err := keepGroup(tmp, p.gid); err != nil {
			return "", fmt.Errorf("%w: can't keep group %d: %w", ErrLocked, p.gid, err)
		}
	}
	if rh != nil {
		return rh.sum(), nil
	}
	return "", nil
}

// readBack reads the temp copy's tags and pictures and compares them with
// what the write asked for. TagLib's write ignores keys a format rejects, so
// this is where a refused key shows.
func (p *plan) readBack() error {
	got, err := readAs(p.tmp, p.file.Format, p.stat)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrVerify, err)
	}
	if k, ok := diffTags(p.want, got.Tags); !ok {
		return fmt.Errorf("%w: %s reads back as %q, not %q", ErrVerify, k, got.Tags[k], p.want[k])
	}
	if len(got.Pictures) != len(p.wantPics) {
		return fmt.Errorf("%w: %d pictures, not %d", ErrVerify, len(got.Pictures), len(p.wantPics))
	}
	for i, want := range p.wantPics {
		g := got.Pictures[i]
		if g.MIME != want.MIME || (!p.file.Format.mp4 && (g.Type != want.Type || g.Description != want.Description)) {
			return fmt.Errorf("%w: picture %d reads back as %+v, not %+v", ErrVerify, i+1, g, want)
		}
		b, err := readImage(p.tmp, i)
		if err != nil {
			return fmt.Errorf("%w: %w", ErrVerify, err)
		}
		if sum(b) != p.picSums[i] {
			return fmt.Errorf("%w: picture %d has other bytes", ErrVerify, i+1)
		}
	}
	p.after = got
	return nil
}

func (w *Writer) checkAudio(ctx context.Context, p *plan, flacSum string) error {
	if p.file.Format.flac {
		got, err := flacHash(p.tmp)
		if err != nil {
			return fmt.Errorf("%w: %w", ErrVerify, err)
		}
		if got != flacSum {
			return fmt.Errorf("%w: the audio frames changed", ErrVerify)
		}
		return nil
	}
	want, err := ffmpegHash(ctx, w.FFmpeg, p.file.Path)
	if err != nil {
		return err
	}
	got, err := ffmpegHash(ctx, w.FFmpeg, p.tmp)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrVerify, err)
	}
	if got != want {
		return fmt.Errorf("%w: the audio packets changed", ErrVerify)
	}
	return nil
}

// restat sees that the original is as it was when it was planned.
func (w *Writer) restat(p plan) error {
	if err := w.at(stageRestat, p.file.Path); err != nil {
		return err
	}
	st, err := os.Lstat(p.file.Path)
	if err != nil {
		return err
	}
	if st.Size() != p.stat.Size() || !st.ModTime().Equal(p.stat.ModTime()) {
		return ErrConflict
	}
	if w.Busy != nil && w.Busy(filepath.Dir(p.file.Path)) {
		return ErrBusy
	}
	return nil
}

// diffTags finds a key whose values differ, or reports none.
func diffTags(want, got map[string][]string) (string, bool) {
	keys := make([]string, 0, len(want)+len(got))
	for k := range want {
		keys = append(keys, k)
	}
	for k := range got {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		if !slices.Equal(want[k], got[k]) {
			return k, false
		}
	}
	return "", true
}

func sum(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func syncFile(path string) error {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

func syncDir(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}
