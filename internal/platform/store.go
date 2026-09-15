// Package platform holds the process-local platform store: a single-slot,
// concurrency-safe holder of the platform module generated and built from the
// cluster-singleton Platform CR and the set of active transformer claims. It
// is written by the PlatformReconciler and read by the render path. It also
// owns [Layout], the on-disk lifecycle of the generated module directories the
// store records (per-identity directories, staging swaps, retention, boot
// reset); the module content itself comes from the library's
// opm/helper/platformmodule generator.
//
// The store records one [Generated] platform module per [PackageIdentity]:
// the module directory on the operator's own disk, the platform value the
// kernel built from it and the resolved catalog-skew policy (enhancement 0019
// D6, D7). A render leases the record for its duration ([Store.Lease]) so the
// PlatformReconciler never prunes a module directory a render build is still
// reading from.
package platform

import (
	"slices"
	"strings"
	"sync"

	"github.com/open-platform-model/library/opm/kernel"
	"github.com/open-platform-model/library/opm/platform"
)

// Generated is the process-local record of the platform module the
// reconciler generated and built for one [PackageIdentity]: the identity, the
// module directory on the operator's own disk (a per-identity directory under
// the manager's --platform-dir), the source-carrying platform the kernel's
// shape-gated loader built from it and the skew policy the CR resolved to.
// The render path consumes it through [Store.Lease]; the module is never
// published, written to the cluster or served elsewhere (0019 D6).
type Generated struct {
	// Identity is what the package is a function of: the Platform CR
	// generation plus the active claims' catalog coordinates (0015 D13). It
	// is the store's key and the name of Dir's last path element.
	Identity PackageIdentity

	Dir      string
	Platform *platform.Platform

	// Skew is the resolved Platform.spec.skewPolicy (Warn when unset), passed
	// verbatim as RenderInput.Skew by every render of this package
	// (0019 D7/D18).
	Skew kernel.SkewPolicy
}

// Store holds at most one current generated platform, keyed on the
// [PackageIdentity] it was built for. Enhancement 0001 §8.3: one global
// Platform per cluster needs one slot, not the library's content-hash LRU.
//
// The key is the identity rather than the CR generation (enhancement 0015
// D17) because a claim activating produces a different package while leaving
// the generation untouched: keyed on the generation alone, a render would
// keep consuming a platform that does not contain the provider just accepted.
//
// The Store carries no kernel gate. The single process-wide library Kernel
// is safe for concurrent use across its method calls (library ADR-007): every
// verb (module acquisition, instance synthesis, on-disk acquisition, the
// platform build, the render) builds in a cue.Context of its own and
// retains nothing, so acquisitions, syntheses and renders of different
// objects overlap with no mutex.
type Store struct {
	mu        sync.RWMutex
	generated *Generated
	identity  PackageIdentity

	// leases counts the renders currently reading each identity's module
	// directory. An identity with a positive count is reported by Leased and
	// kept on disk by the PlatformReconciler's prune, whatever the current
	// identity is.
	leases map[PackageIdentity]int
}

// NewStore returns an empty Store holding no platform.
func NewStore() *Store {
	return &Store{leases: make(map[PackageIdentity]int)}
}

// Identity returns the [PackageIdentity] of the held platform, or the zero
// identity when no platform is held.
func (s *Store) Identity() PackageIdentity {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.identity
}

// SetGenerated records g as the current generated platform module, replacing
// any earlier record, and reports g.Identity from Identity. Leases on the
// replaced identity are unaffected: the render holding one finishes against
// the directory it started with.
func (s *Store) SetGenerated(g Generated) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.generated = &g
	s.identity = g.Identity
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
// record's identity leased until release is called; it returns ok false
// (a zero record and a no-op release) when no platform is held. The caller
// defers release immediately: a leased identity's module directory survives
// the PlatformReconciler's prune until every lease on it is released. Release
// is idempotent.
func (s *Store) Lease() (rec Generated, release func(), ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.generated == nil {
		return Generated{}, func() {}, false
	}
	rec = *s.generated
	id := rec.Identity
	if s.leases == nil {
		s.leases = make(map[PackageIdentity]int)
	}
	s.leases[id]++
	var once sync.Once
	release = func() {
		once.Do(func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			if s.leases[id] <= 1 {
				delete(s.leases, id)
				return
			}
			s.leases[id]--
		})
	}
	return rec, release, true
}

// Leased returns the identities at least one render currently holds a lease
// on, ordered by their string form so the result is deterministic. The
// PlatformReconciler adds them to the prune keep set.
func (s *Store) Leased() []PackageIdentity {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]PackageIdentity, 0, len(s.leases))
	for id := range s.leases {
		out = append(out, id)
	}
	slices.SortFunc(out, func(a, b PackageIdentity) int {
		return strings.Compare(a.String(), b.String())
	})
	return out
}

// Clear drops the held record so the store reports no platform held. Called
// when the Platform CR is deleted. Leases outstanding on the dropped identity
// are kept: the renders holding them still read its directory.
func (s *Store) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.generated = nil
	s.identity = PackageIdentity{}
}
