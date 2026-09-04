// Package platform holds the process-local platform store: a single-slot,
// concurrency-safe holder of the platform module generated and built from the
// cluster-singleton Platform CR. It is written by the PlatformReconciler and
// read by the render path.
//
// The store records one [Generated] platform module per Platform CR
// generation: the module directory on the operator's own disk, the platform
// value the kernel built from it and the resolved catalog-skew policy
// (enhancement 0019 D6, D7). A render leases the record for its duration
// ([Store.Lease]) so the PlatformReconciler never prunes a module directory a
// render build is still reading from.
package platform

import (
	"slices"
	"sync"

	"github.com/open-platform-model/library/opm/kernel"
	"github.com/open-platform-model/library/opm/platform"
)

// Generated is the process-local record of the platform module the
// reconciler generated and built for one Platform CR generation: the
// generation, the module directory on the operator's own disk (a
// per-generation directory under the manager's --platform-dir), the
// source-carrying platform the kernel's shape-gated loader built from it and
// the skew policy the CR resolved to. The render path consumes it through
// [Store.Lease]; the module is never published, written to the cluster or
// served elsewhere (0019 D6).
type Generated struct {
	Generation int64
	Dir        string
	Platform   *platform.Platform

	// Skew is the resolved Platform.spec.skewPolicy (Warn when unset), passed
	// verbatim as RenderInput.Skew by every render of this generation
	// (0019 D7/D18).
	Skew kernel.SkewPolicy
}

// Store holds at most one generated platform, keyed on the Platform CR's
// .metadata.generation it was built for. Enhancement 0001 §8.3: one global
// Platform per cluster needs one slot, not the library's content-hash LRU.
//
// The Store also carries the kernel gate (AcquireKernel). The single
// process-wide library Kernel is not safe for concurrent method calls that
// evaluate in its own cue.Context (module acquisition, instance synthesis,
// on-disk acquisition, the platform build); those are serialised behind the
// gate. Kernel.Render shares nothing between renders (library ADR-005,
// 0019 D8): it builds in a fresh context and reads only the inputs' staged
// sources, so it runs outside the gate and renders of different objects
// overlap.
type Store struct {
	mu         sync.RWMutex
	generated  *Generated
	generation int64

	// leases counts the renders currently reading each generation's module
	// directory. A generation with a positive count is reported by Leased and
	// kept on disk by the PlatformReconciler's prune, whatever the current
	// generation is.
	leases map[int64]int

	// kernelMu serializes every context-owning use of the shared Kernel: the
	// platform build in the PlatformReconciler, and acquire + synthesize (or
	// on-disk acquisition) in the render paths. Separate from mu, which only
	// guards the record and the lease counts, so a render never blocks Lease.
	kernelMu sync.Mutex
}

// NewStore returns an empty Store holding no platform.
func NewStore() *Store {
	return &Store{leases: make(map[int64]int)}
}

// Generation returns the .metadata.generation the held platform was built for,
// or 0 when no platform is held.
func (s *Store) Generation() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.generation
}

// SetGenerated records g as the current generated platform module, replacing
// any earlier record, and reports g.Generation from Generation. Leases on the
// replaced generation are unaffected: the render holding one finishes against
// the directory it started with.
func (s *Store) SetGenerated(g Generated) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.generated = &g
	s.generation = g.Generation
}

// Generated returns the current generated-module record and true, or the
// zero record and false when none is held. Safe for concurrent callers. A
// render that will read the module directory takes Lease instead.
func (s *Store) Generated() (Generated, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.generated == nil {
		return Generated{}, false
	}
	return *s.generated, true
}

// Lease returns the current record and a release function, holding the
// record's generation leased until release is called; it returns ok false
// (a zero record and a no-op release) when no platform is held. The caller
// defers release immediately: a leased generation's module directory survives
// the PlatformReconciler's prune until every lease on it is released. Release
// is idempotent.
func (s *Store) Lease() (rec Generated, release func(), ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.generated == nil {
		return Generated{}, func() {}, false
	}
	rec = *s.generated
	gen := rec.Generation
	if s.leases == nil {
		s.leases = make(map[int64]int)
	}
	s.leases[gen]++
	var once sync.Once
	release = func() {
		once.Do(func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			if s.leases[gen] <= 1 {
				delete(s.leases, gen)
				return
			}
			s.leases[gen]--
		})
	}
	return rec, release, true
}

// Leased returns the generations at least one render currently holds a
// lease on, in ascending order. The PlatformReconciler adds them to the
// prune keep set.
func (s *Store) Leased() []int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]int64, 0, len(s.leases))
	for gen := range s.leases {
		out = append(out, gen)
	}
	slices.Sort(out)
	return out
}

// AcquireKernel takes the kernel gate and returns the function that releases
// it. Callers hold it across the context-owning Kernel calls of one operation
// (acquisition, synthesis, on-disk acquisition, the platform build) and
// release it before Kernel.Render and before writing status. A nil Store
// returns a no-op release so unit fixtures without a store keep working.
func (s *Store) AcquireKernel() (release func()) {
	if s == nil {
		return func() {}
	}
	s.kernelMu.Lock()
	return s.kernelMu.Unlock
}

// Clear drops the held record so the store reports no platform held. Called
// when the Platform CR is deleted. Leases outstanding on the dropped
// generation are kept: the renders holding them still read its directory.
func (s *Store) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.generated = nil
	s.generation = 0
}
