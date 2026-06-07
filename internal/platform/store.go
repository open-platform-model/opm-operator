// Package platform holds the process-local materialized-platform store: a
// single-slot, concurrency-safe holder of the current
// *materialize.MaterializedPlatform built from the cluster-singleton Platform
// CR. It is written by the PlatformReconciler and read (in a later slice) by
// the render path.
package platform

import (
	"sync"

	"github.com/open-platform-model/library/opm/materialize"
)

// Store holds at most one materialized platform, keyed on the Platform CR's
// .metadata.generation it was built for. Enhancement 0001 §8.3: one global
// Platform per cluster needs one slot, not the library's content-hash LRU.
//
// The held *MaterializedPlatform is safe for concurrent read-only sharing
// (library v0.17 guarantee), so the RWMutex lets future render-path readers
// run concurrently with reconciler writes.
type Store struct {
	mu         sync.RWMutex
	current    *materialize.MaterializedPlatform
	generation int64
}

// NewStore returns an empty Store holding no platform.
func NewStore() *Store {
	return &Store{}
}

// Get returns the held materialized platform and true, or (nil, false) when no
// platform is held. Safe for concurrent callers.
func (s *Store) Get() (*materialize.MaterializedPlatform, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.current == nil {
		return nil, false
	}
	return s.current, true
}

// Generation returns the .metadata.generation the held platform was built for,
// or 0 when no platform is held.
func (s *Store) Generation() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.generation
}

// Set replaces the held platform with mp, recording the generation it was
// built for. A later Set with a newer generation replaces the slot.
func (s *Store) Set(gen int64, mp *materialize.MaterializedPlatform) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.current = mp
	s.generation = gen
}

// Clear drops the held platform so the store reports no platform held. Called
// when the Platform CR is deleted.
func (s *Store) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.current = nil
	s.generation = 0
}
