package platform

import (
	"sync"
	"testing"

	"github.com/open-platform-model/library/opm/materialize"
)

func TestStore_EmptyGet(t *testing.T) {
	s := NewStore()
	if mp, ok := s.Get(); ok || mp != nil {
		t.Fatalf("empty store should report no platform, got (%v, %v)", mp, ok)
	}
	if g := s.Generation(); g != 0 {
		t.Fatalf("empty store generation should be 0, got %d", g)
	}
}

func TestStore_SetGet(t *testing.T) {
	s := NewStore()
	mp := &materialize.MaterializedPlatform{}
	s.Set(7, mp)

	got, ok := s.Get()
	if !ok {
		t.Fatal("Get after Set should report a held platform")
	}
	if got != mp {
		t.Fatalf("Get returned a different platform than Set")
	}
	if g := s.Generation(); g != 7 {
		t.Fatalf("Generation = %d, want 7", g)
	}
}

func TestStore_SetReplacesSlot(t *testing.T) {
	s := NewStore()
	first := &materialize.MaterializedPlatform{}
	second := &materialize.MaterializedPlatform{}

	s.Set(1, first)
	s.Set(2, second)

	got, ok := s.Get()
	if !ok || got != second {
		t.Fatalf("Get should return the most recently Set platform")
	}
	if g := s.Generation(); g != 2 {
		t.Fatalf("Generation = %d, want 2", g)
	}
}

func TestStore_Clear(t *testing.T) {
	s := NewStore()
	s.Set(3, &materialize.MaterializedPlatform{})
	s.Clear()

	if mp, ok := s.Get(); ok || mp != nil {
		t.Fatalf("Get after Clear should report no platform, got (%v, %v)", mp, ok)
	}
	if g := s.Generation(); g != 0 {
		t.Fatalf("Generation after Clear = %d, want 0", g)
	}
}

// TestStore_ConcurrentReadDuringWrite exercises many concurrent readers while a
// writer replaces the slot. Run with -race to detect data races; the assertion
// is only that reads return a coherent (platform, ok) pair without tearing.
func TestStore_ConcurrentReadDuringWrite(t *testing.T) {
	s := NewStore()
	s.Set(1, &materialize.MaterializedPlatform{})

	const readers = 16
	const iterations = 1000

	var readerWg, writerWg sync.WaitGroup
	stop := make(chan struct{})

	// Writer: continuously replaces the slot under increasing generations
	// until the readers finish.
	writerWg.Go(func() {
		gen := int64(1)
		for {
			select {
			case <-stop:
				return
			default:
				gen++
				s.Set(gen, &materialize.MaterializedPlatform{})
			}
		}
	})

	// Readers: a held slot must always read back as present and non-nil.
	for range readers {
		readerWg.Go(func() {
			for range iterations {
				mp, ok := s.Get()
				if ok && mp == nil {
					t.Errorf("Get reported present with a nil platform")
					return
				}
				_ = s.Generation()
			}
		})
	}

	// Let readers run to completion, then stop the writer.
	readerWg.Wait()
	close(stop)
	writerWg.Wait()
}
