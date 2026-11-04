//go:build unix

package tagio

import (
	"os"
	"syscall"
)

// owner gives a file's owner, group and link count.
func owner(fi os.FileInfo) (uid, gid int, links uint64, ok bool) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, 0, false
	}
	return int(st.Uid), int(st.Gid), uint64(st.Nlink), true
}

// writableDir reports whether this process may create and rename files in
// dir, which a temp copy and its rename need.
func writableDir(dir string) bool { return syscall.Access(dir, 0x2) == nil }

// keepGroup gives a new file the group of the file it replaces. A new file
// takes the process's group, and the rename would otherwise change the
// group of a file shelf didn't create.
func keepGroup(f *os.File, gid int) error {
	st, err := f.Stat()
	if err != nil {
		return err
	}
	if _, g, _, ok := owner(st); ok && g == gid {
		return nil
	}
	return f.Chown(-1, gid)
}
