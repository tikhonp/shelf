//go:build !unix

package tagio

import "os"

// shelf runs on Linux. Elsewhere files have no owner to check.
func owner(os.FileInfo) (uid, gid int, links uint64, ok bool) { return 0, 0, 1, false }
func writableDir(string) bool                                 { return true }
func keepGroup(*os.File, int) error                           { return nil }

// Owner gives a file's owner, when the system has one.
func Owner(os.FileInfo) (uid int, ok bool) { return 0, false }
