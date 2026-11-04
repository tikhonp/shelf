//go:build !unix

package tagio

import "os"

// shelf runs on Linux. Elsewhere files have no owner to check.
func owner(os.FileInfo) (uid, gid int, links uint64, ok bool) { return 0, 0, 1, false }
func writableDir(string) bool                                 { return true }
func keepGroup(*os.File, int) error                           { return nil }
func canKeepGroup(int) bool                                   { return true }

// Locked reports whether shelf can't replace a file. Without owners, no
// file is.
func Locked(os.FileInfo, int) bool { return false }
