package reconcile

import (
	"testing"
	"time"
)

func TestInstanceRequeue(t *testing.T) {
	const interval = 10 * time.Minute

	t.Run("a health requeue is returned unchanged", func(t *testing.T) {
		for _, health := range []time.Duration{healthRequeueFloor, healthRequeueCeiling, StalledRecheckInterval} {
			if got := instanceRequeue(health, interval); got != health {
				t.Errorf("instanceRequeue(%v, %v) = %v, want %v", health, interval, got, health)
			}
		}
	})

	t.Run("a zero or negative interval disables the periodic requeue", func(t *testing.T) {
		for _, off := range []time.Duration{0, -time.Minute} {
			if got := instanceRequeue(0, off); got != 0 {
				t.Errorf("instanceRequeue(0, %v) = %v, want 0", off, got)
			}
		}
	})

	t.Run("no health requeue gives the interval plus at most the jitter", func(t *testing.T) {
		upper := interval + time.Duration(requeueJitter*float64(interval))
		for range 200 {
			got := instanceRequeue(0, interval)
			if got < interval || got > upper {
				t.Fatalf("instanceRequeue(0, %v) = %v, want within [%v, %v]", interval, got, interval, upper)
			}
		}
	})
}
