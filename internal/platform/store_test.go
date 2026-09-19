package platform

import (
	"slices"
	"sync"
	"testing"

	"github.com/open-platform-model/library/opm/kernel"
	"github.com/open-platform-model/library/opm/platform"
)

func generated(id PackageIdentity) Generated {
	return Generated{Identity: id, Dir: "/tmp/opm-platform/" + id.String(), Platform: &platform.Platform{}}
}

func identityStrings(ids []PackageIdentity) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, id.String())
	}
	return out
}

func TestStore_Empty(t *testing.T) {
	s := NewStore()
	if _, ok := s.Generated(); ok {
		t.Fatal("empty store should report no generated record")
	}
	if id := s.Identity(); !id.IsZero() {
		t.Fatalf("empty store identity should be the zero identity, got %v", id)
	}
	rec, release, ok := s.Lease()
	if ok || rec.Platform != nil {
		t.Fatalf("empty store should not lease, got (%+v, %v)", rec, ok)
	}
	release() // no-op release must be safe
	if leased := s.Leased(); len(leased) != 0 {
		t.Fatalf("empty store should report no leases, got %v", leased)
	}
}

func TestStore_GeneratedRecord(t *testing.T) {
	s := NewStore()
	s.SetGenerated(Generated{Identity: gen(4), Dir: "/tmp/opm-platform/gen-4", Platform: &platform.Platform{}, Skew: kernel.SkewRefuse})
	got, ok := s.Generated()
	if !ok {
		t.Fatal("Generated after SetGenerated should report a record")
	}
	if got.Identity != gen(4) || got.Dir != "/tmp/opm-platform/gen-4" || got.Platform == nil || got.Skew != kernel.SkewRefuse {
		t.Fatalf("unexpected record %+v", got)
	}
	if id := s.Identity(); id != gen(4) {
		t.Fatalf("Identity should follow the generated record, got %v", id)
	}

	s.SetGenerated(Generated{Identity: gen(5), Dir: "/tmp/opm-platform/gen-5", Platform: &platform.Platform{}})
	if got, _ := s.Generated(); got.Identity != gen(5) || got.Skew != kernel.SkewWarn {
		t.Fatalf("a later SetGenerated should replace the record, got %+v", got)
	}

	s.Clear()
	if _, ok := s.Generated(); ok {
		t.Fatal("Clear should drop the generated record")
	}
	if id := s.Identity(); !id.IsZero() {
		t.Fatalf("Clear should reset the identity, got %v", id)
	}
}

// A claim activating leaves the CR generation untouched, so the store must
// treat it as a different package: this is the bug 0015:D17 exists to prevent.
func TestStore_ClaimChangeReplacesTheRecordAtTheSameGeneration(t *testing.T) {
	s := NewStore()
	bare := gen(6)
	claimed := genWith(6, "opmodel.dev/catalogs/k8up@v1", "1.2.0")
	if bare == claimed {
		t.Fatal("a claim activating must yield a different identity at the same generation")
	}

	s.SetGenerated(generated(bare))
	rec, release, ok := s.Lease()
	if !ok || rec.Identity != bare {
		t.Fatalf("Lease should return the claimless record, got (%+v, %v)", rec, ok)
	}
	defer release()

	s.SetGenerated(generated(claimed))
	if got, _ := s.Generated(); got.Identity != claimed {
		t.Fatalf("the claimed package should be current, got %+v", got)
	}
	if got := identityStrings(s.Leased()); !slices.Equal(got, []string{bare.String()}) {
		t.Fatalf("the superseded package stays leased, got %v", got)
	}
}

func TestStore_LeaseCountsAndReleases(t *testing.T) {
	s := NewStore()
	s.SetGenerated(generated(gen(1)))

	rec, release1, ok := s.Lease()
	if !ok || rec.Identity != gen(1) {
		t.Fatalf("Lease should return the held record, got (%+v, %v)", rec, ok)
	}
	_, release2, ok := s.Lease()
	if !ok {
		t.Fatal("second Lease should succeed")
	}
	if leased := s.Leased(); len(leased) != 1 || leased[0] != gen(1) {
		t.Fatalf("Leased = %v, want [gen-1]", leased)
	}

	release1()
	if leased := s.Leased(); len(leased) != 1 || leased[0] != gen(1) {
		t.Fatalf("one outstanding lease must keep the identity leased, got %v", leased)
	}
	release1() // idempotent: must not release the second lease
	if leased := s.Leased(); len(leased) != 1 {
		t.Fatalf("a repeated release must not drop another lease, got %v", leased)
	}
	release2()
	if leased := s.Leased(); len(leased) != 0 {
		t.Fatalf("all leases released, got %v", leased)
	}
}

func TestStore_LeasedIdentitiesAcrossSwap(t *testing.T) {
	s := NewStore()
	s.SetGenerated(generated(gen(1)))
	rec1, release1, ok := s.Lease()
	if !ok {
		t.Fatal("lease on gen-1")
	}

	// A new package lands while the render on gen-1 is in flight.
	s.SetGenerated(generated(gen(2)))
	rec2, release2, ok := s.Lease()
	if !ok || rec2.Identity != gen(2) {
		t.Fatalf("Lease after the swap should return gen-2, got (%+v, %v)", rec2, ok)
	}
	if rec1.Identity != gen(1) {
		t.Fatalf("the earlier lease keeps its own record, got %+v", rec1)
	}
	if got, want := identityStrings(s.Leased()), []string{"gen-1", "gen-2"}; !slices.Equal(got, want) {
		t.Fatalf("Leased = %v, want %v in string order", got, want)
	}

	release1()
	if got, want := identityStrings(s.Leased()), []string{"gen-2"}; !slices.Equal(got, want) {
		t.Fatalf("after releasing gen-1, Leased = %v, want %v", got, want)
	}

	// Clear (Platform deleted) keeps the outstanding lease visible: the render
	// holding it still reads the directory.
	s.Clear()
	if got, want := identityStrings(s.Leased()), []string{"gen-2"}; !slices.Equal(got, want) {
		t.Fatalf("Clear must not drop outstanding leases, got %v", got)
	}
	release2()
	if leased := s.Leased(); len(leased) != 0 {
		t.Fatalf("Leased after every release = %v, want none", leased)
	}
}

// TestStore_ConcurrentLeaseDuringWrite exercises many concurrent lessees while
// a writer replaces the record. Run with -race to detect data races; the
// assertions are that a lease returns a coherent record and that the lease
// counts end exact.
func TestStore_ConcurrentLeaseDuringWrite(t *testing.T) {
	s := NewStore()
	s.SetGenerated(generated(gen(1)))

	const lessees = 16
	const iterations = 1000

	var lesseeWg, writerWg sync.WaitGroup
	stop := make(chan struct{})

	writerWg.Go(func() {
		n := int64(1)
		for {
			select {
			case <-stop:
				return
			default:
				n++
				s.SetGenerated(Generated{Identity: gen(n), Platform: &platform.Platform{}})
			}
		}
	})

	for range lessees {
		lesseeWg.Go(func() {
			for range iterations {
				rec, release, ok := s.Lease()
				if !ok || rec.Platform == nil {
					t.Errorf("Lease reported (%+v, %v) while a record is held", rec, ok)
					release()
					return
				}
				_ = s.Leased()
				release()
			}
		})
	}

	lesseeWg.Wait()
	close(stop)
	writerWg.Wait()

	if leased := s.Leased(); len(leased) != 0 {
		t.Fatalf("every lease was released, but Leased = %v", leased)
	}
}
