package downloads

import (
	"path/filepath"
	"slices"
	"strings"
)

// work records a folder a running download writes in.
func (s *Service) work(id int64, folder string) {
	rel := filepath.ToSlash(filepath.Clean(folder))
	if !filepath.IsLocal(rel) && rel != "." {
		s.log.Warn("downloads: a working folder outside the music folder", "download", id, "folder", folder)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.working == nil {
		s.working = map[int64][]string{}
	}
	if !slices.Contains(s.working[id], rel) {
		s.working[id] = append(s.working[id], rel)
	}
}

func (s *Service) workDone(id int64) {
	s.mu.Lock()
	delete(s.working, id)
	s.mu.Unlock()
}

// Busy reports whether a running download writes in dir, an absolute
// folder, or in a folder above it. It is the library's port, so a tag
// write keeps out of a folder a source is filling or renaming.
func (s *Service) Busy(dir string) bool {
	rel, err := filepath.Rel(s.cfg.MusicDir, dir)
	if err != nil {
		return false
	}
	rel = filepath.ToSlash(rel)
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, folders := range s.working {
		for _, w := range folders {
			if w == "." || rel == w || strings.HasPrefix(rel, w+"/") {
				return true
			}
		}
	}
	return false
}
