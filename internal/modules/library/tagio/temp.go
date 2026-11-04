package tagio

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// A temp copy sits beside the file it replaces, so the rename stays in one
// folder. The leading dot hides it from Navidrome, and TagLib finds the
// format from the content, so the name needs no extension.
const tempPrefix = ".shelf-tmp-"

// tempHex is the length of the random part of a temp name.
const tempHex = 16

// createTemp makes a new empty temp file in dir, readable only by shelf
// until it has been verified.
func createTemp(dir string) (*os.File, error) {
	for range 10 {
		var b [tempHex / 2]byte
		_, _ = rand.Read(b[:])
		f, err := os.OpenFile(filepath.Join(dir, tempPrefix+hex.EncodeToString(b[:])), os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		return f, err
	}
	return nil, errors.New("no free temp name")
}

// IsTemp reports whether name is one of shelf's temp files.
func IsTemp(name string) bool {
	rest, ok := strings.CutPrefix(name, tempPrefix)
	if !ok || len(rest) != tempHex {
		return false
	}
	_, err := hex.DecodeString(rest)
	return err == nil
}

// Sweep removes the temp files a crash left under root. It deletes only
// regular files with shelf's temp name that uid owns, and skips dot folders,
// where shelf never writes. A folder it can't read is skipped, and the
// errors come back joined after the walk.
func Sweep(root string, uid int) (removed []string, err error) {
	var errs []error
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if path == root {
				return err
			}
			errs = append(errs, err)
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if path != root && strings.HasPrefix(d.Name(), ".") {
				return fs.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() || !IsTemp(d.Name()) {
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			errs = append(errs, err)
			return nil
		}
		if u, _, _, ok := owner(fi); ok && u != uid {
			return nil
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, err)
			return nil
		}
		removed = append(removed, path)
		return nil
	})
	return removed, errors.Join(append([]error{walkErr}, errs...)...)
}
