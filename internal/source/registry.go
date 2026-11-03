package source

import (
	"fmt"
	"sync"
)

// Registry holds every registered source, switched on or not.
type Registry struct {
	mu   sync.RWMutex
	list []Source
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry { return &Registry{} }

// Add registers s. Two sources can't share an ID.
func (r *Registry) Add(s Source) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, o := range r.list {
		if o.ID() == s.ID() {
			return fmt.Errorf("source %q is registered twice", s.ID())
		}
	}
	r.list = append(r.list, s)
	return nil
}

// All lists the sources in the order they were added.
func (r *Registry) All() []Source {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]Source(nil), r.list...)
}

// Get returns a source by ID, switched on or not.
func (r *Registry) Get(id string) (Source, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, s := range r.list {
		if s.ID() == id {
			return s, true
		}
	}
	return nil, false
}

// Enabled returns a source by ID when it is switched on.
func (r *Registry) Enabled(id string) (Source, bool) {
	s, ok := r.Get(id)
	if !ok || !IsEnabled(s) {
		return nil, false
	}
	return s, true
}

// IsEnabled reports whether s is switched on.
func IsEnabled(s Source) bool {
	sw, ok := s.(Switch)
	return !ok || sw.Enabled()
}

// ParseLink asks every source, switched on or not, whether raw is its link.
// The first source that claims it wins.
func (r *Registry) ParseLink(raw string) (Ref, Source, bool) {
	for _, s := range r.All() {
		lp, ok := s.(LinkParser)
		if !ok {
			continue
		}
		if ref, ok := lp.ParseLink(raw); ok {
			ref.Source = s.ID()
			return ref, s, true
		}
	}
	return Ref{}, nil, false
}
