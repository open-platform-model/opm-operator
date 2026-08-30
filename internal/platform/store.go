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
// The Store also carries the render gate (AcquireKernel): the held
// *MaterializedPlatform is NOT safe to render against from several goroutines
// at once (the kernel's Compile fills into values reached through the
// platform, and a fill writes evaluation state; library ADR-002 is superseded
// on that measurement), and the single process-wide library Kernel is not safe
// for concurrent method calls either. Every reconciler that touches the Kernel
// or the held platform holds the gate for the duration. This is the stopgap the
// library documents until enhancement 0019 D8 (one CUE build per render, in a
// context that does not outlive it) removes the shared value.
type Store struct {
	mu         sync.RWMutex
	current    *materialize.MaterializedPlatform
	generation int64

	// kernelMu serializes every use of the shared Kernel and of the held
	// platform: SynthesizePlatform + Materialize in the PlatformReconciler,
	// and acquire + synthesize + Compile in the render paths. Separate from
	// mu, which only guards the slot pointer, so a render never blocks Get.
	kernelMu sync.Mutex
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

// AcquireKernel takes the render gate and returns the function that releases
// it. Callers hold it across every Kernel call of one operation and across any
// use of the platform returned by Get, and release it before writing status.
// A nil Store returns a no-op release so unit fixtures without a store keep
// working.
func (s *Store) AcquireKernel() (release func()) {
	if s == nil {
		return func() {}
	}
	s.kernelMu.Lock()
	return s.kernelMu.Unlock
}

// Clear drops the held platform so the store reports no platform held. Called
// when the Platform CR is deleted.
func (s *Store) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.current = nil
	s.generation = 0
}
