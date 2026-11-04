//go:build unix

package tagio

import (
	"os"
	"slices"
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

// canKeepGroup reports whether this process may give a file group gid:
// root may give any, and others only their own groups.
func canKeepGroup(gid int) bool {
	if os.Geteuid() == 0 || gid == os.Getegid() {
		return true
	}
	groups, err := os.Getgroups()
	return err == nil && slices.Contains(groups, gid)
}

// Locked reports whether shelf, running as uid, can't replace a file:
// another user owns it, or its group isn't one of shelf's.
func Locked(fi os.FileInfo, uid int) bool {
	u, gid, _, ok := owner(fi)
	return ok && (u != uid || !canKeepGroup(gid))
}
