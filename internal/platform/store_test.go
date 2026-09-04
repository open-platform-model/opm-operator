package platform

import (
	"sync"
	"testing"

	"github.com/open-platform-model/library/opm/kernel"
	"github.com/open-platform-model/library/opm/platform"
)

func generated(gen int64) Generated {
	return Generated{Generation: gen, Dir: "/tmp/opm-platform/gen-" + string(rune('0'+gen)), Platform: &platform.Platform{}}
}

func TestStore_Empty(t *testing.T) {
	s := NewStore()
	if _, ok := s.Generated(); ok {
		t.Fatal("empty store should report no generated record")
	}
	if g := s.Generation(); g != 0 {
		t.Fatalf("empty store generation should be 0, got %d", g)
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
	s.SetGenerated(Generated{Generation: 4, Dir: "/tmp/opm-platform/gen-4", Platform: &platform.Platform{}, Skew: kernel.SkewRefuse})
	got, ok := s.Generated()
	if !ok {
		t.Fatal("Generated after SetGenerated should report a record")
	}
	if got.Generation != 4 || got.Dir != "/tmp/opm-platform/gen-4" || got.Platform == nil || got.Skew != kernel.SkewRefuse {
		t.Fatalf("unexpected record %+v", got)
	}
	if g := s.Generation(); g != 4 {
		t.Fatalf("Generation should follow the generated record, got %d", g)
	}

	s.SetGenerated(Generated{Generation: 5, Dir: "/tmp/opm-platform/gen-5", Platform: &platform.Platform{}})
	if got, _ := s.Generated(); got.Generation != 5 || got.Skew != kernel.SkewWarn {
		t.Fatalf("a later SetGenerated should replace the record, got %+v", got)
	}

	s.Clear()
	if _, ok := s.Generated(); ok {
		t.Fatal("Clear should drop the generated record")
	}
	if g := s.Generation(); g != 0 {
		t.Fatalf("Clear should reset generation, got %d", g)
	}
}

func TestStore_LeaseCountsAndReleases(t *testing.T) {
	s := NewStore()
	s.SetGenerated(generated(1))

	rec, release1, ok := s.Lease()
	if !ok || rec.Generation != 1 {
		t.Fatalf("Lease should return the held record, got (%+v, %v)", rec, ok)
	}
	_, release2, ok := s.Lease()
	if !ok {
		t.Fatal("second Lease should succeed")
	}
	if leased := s.Leased(); len(leased) != 1 || leased[0] != 1 {
		t.Fatalf("Leased = %v, want [1]", leased)
	}

	release1()
	if leased := s.Leased(); len(leased) != 1 || leased[0] != 1 {
		t.Fatalf("one outstanding lease must keep the generation leased, got %v", leased)
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

func TestStore_LeasedGenerationsAcrossSwap(t *testing.T) {
	s := NewStore()
	s.SetGenerated(generated(1))
	rec1, release1, ok := s.Lease()
	if !ok {
		t.Fatal("lease on generation 1")
	}

	// A new generation lands while the render on generation 1 is in flight.
	s.SetGenerated(generated(2))
	rec2, release2, ok := s.Lease()
	if !ok || rec2.Generation != 2 {
		t.Fatalf("Lease after the swap should return generation 2, got (%+v, %v)", rec2, ok)
	}
	if rec1.Generation != 1 {
		t.Fatalf("the earlier lease keeps its own record, got %+v", rec1)
	}
	if leased := s.Leased(); len(leased) != 2 || leased[0] != 1 || leased[1] != 2 {
		t.Fatalf("Leased = %v, want [1 2] in ascending order", leased)
	}

	release1()
	if leased := s.Leased(); len(leased) != 1 || leased[0] != 2 {
		t.Fatalf("after releasing generation 1, Leased = %v, want [2]", leased)
	}

	// Clear (Platform deleted) keeps the outstanding lease visible: the render
	// holding it still reads the directory.
	s.Clear()
	if leased := s.Leased(); len(leased) != 1 || leased[0] != 2 {
		t.Fatalf("Clear must not drop outstanding leases, got %v", leased)
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
	s.SetGenerated(generated(1))

	const lessees = 16
	const iterations = 1000

	var lesseeWg, writerWg sync.WaitGroup
	stop := make(chan struct{})

	writerWg.Go(func() {
		gen := int64(1)
		for {
			select {
			case <-stop:
				return
			default:
				gen++
				s.SetGenerated(Generated{Generation: gen, Platform: &platform.Platform{}})
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
