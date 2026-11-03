//go:build unix

package platform

import "syscall"

// SetUmask makes new folders 0755 and new files 0644, whatever umask the
// container starts with. Navidrome reads the music folder as another user
// through a read-only mount, so everything shelf, amdl and ffmpeg write must
// stay readable by others.
func SetUmask() { syscall.Umask(0o022) }
