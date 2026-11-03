//go:build !unix

package platform

// SetUmask does nothing where there is no umask; shelf runs on Linux.
func SetUmask() {}
