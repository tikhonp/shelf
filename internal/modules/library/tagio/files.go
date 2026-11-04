package tagio

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// FrontCover gives the bytes of a file's front cover, or none.
func FrontCover(path string) ([]byte, error) {
	f, err := Read(path)
	if err != nil {
		return nil, err
	}
	i := f.frontIndex()
	if i < 0 {
		return nil, nil
	}
	return readImage(path, i)
}

// HasFrontCover reports whether a file has an embedded front cover.
func (f File) HasFrontCover() bool { return f.frontIndex() >= 0 }

// checkReplace sees that shelf may replace or remove path: its folder is
// writable, and the file, when there is one, is a regular file with one
// link that uid owns. It gives the file's info, or nil when there is none.
func checkReplace(path string, uid int) (os.FileInfo, error) {
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("%w: the path isn't absolute", ErrRefused)
	}
	if !writableDir(filepath.Dir(path)) {
		return nil, fmt.Errorf("%w: the folder isn't writable", ErrLocked)
	}
	st, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: not a regular file", ErrRefused)
	}
	if u, _, links, ok := owner(st); ok {
		if u != uid {
			return nil, fmt.Errorf("%w: owned by UID %d", ErrLocked, u)
		}
		if links > 1 {
			return nil, fmt.Errorf("%w: the file has %d hard links", ErrRefused, links)
		}
	}
	return st, nil
}

// CheckFile runs the checks of ReplaceFile and RemoveFile without touching
// anything.
func CheckFile(path string, uid int) error {
	_, err := checkReplace(path, uid)
	return err
}

// ReplaceFile writes data to path through a temp copy beside it and a
// rename. A file it replaces keeps its mode and group. A new one is 0644,
// what shelf's umask of 022 gives a file.
func ReplaceFile(path string, data []byte, uid int) error {
	st, err := checkReplace(path, uid)
	if err != nil {
		return err
	}
	tmp, err := createTemp(filepath.Dir(path))
	if err != nil {
		return err
	}
	name := tmp.Name()
	done := false
	defer func() {
		if !done {
			_ = os.Remove(name)
		}
	}()
	_, err = tmp.Write(data)
	if err == nil {
		err = tmp.Sync()
	}
	if err == nil {
		mode := fs.FileMode(0o644)
		if st != nil {
			mode = st.Mode().Perm()
		}
		err = tmp.Chmod(mode)
	}
	if err == nil && st != nil {
		if _, gid, _, ok := owner(st); ok {
			err = keepGroup(tmp, gid)
		}
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	done = true
	return syncDir(filepath.Dir(path))
}

// RemoveFile removes path after the checks of ReplaceFile. A file that
// isn't there is no error.
func RemoveFile(path string, uid int) error {
	st, err := checkReplace(path, uid)
	if err != nil || st == nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	return syncDir(filepath.Dir(path))
}
