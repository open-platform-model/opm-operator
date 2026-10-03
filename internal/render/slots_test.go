package render

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Many goroutines contend for a pool of n; at no point do more than n hold a
// slot at once, and every one of them gets a turn.
func TestSlots_BoundsHoldersUnderContention(t *testing.T) {
	const n, workers = 3, 40
	s := NewSlots(n)

	var inFlight, maxSeen, done atomic.Int32
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			err := s.Run(context.Background(), func() {
				cur := inFlight.Add(1)
				for {
					prev := maxSeen.Load()
					if cur <= prev || maxSeen.CompareAndSwap(prev, cur) {
						break
					}
				}
				time.Sleep(time.Millisecond)
				inFlight.Add(-1)
			})
			assert.NoError(t, err)
			done.Add(1)
		})
	}
	wg.Wait()

	assert.Equal(t, int32(workers), done.Load())
	assert.LessOrEqual(t, maxSeen.Load(), int32(n))
	assert.Positive(t, maxSeen.Load())
}

// A release called twice gives back one slot, not two: the second call must
// not drain a slot another holder took.
func TestSlots_SecondReleaseIsNoOp(t *testing.T) {
	s := NewSlots(1)

	release, err := s.Acquire(context.Background())
	require.NoError(t, err)
	release()
	release()

	other, err := s.Acquire(context.Background())
	require.NoError(t, err)
	release() // the first holder's stale release must leave other's slot held

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err = s.Acquire(ctx)
	assert.ErrorIs(t, err, context.DeadlineExceeded, "the pool is still full")
	other()
}

// A wait on a full pool ends with the context's error when it is cancelled.
func TestSlots_AcquireOnFullPoolReturnsContextError(t *testing.T) {
	s := NewSlots(1)
	release, err := s.Acquire(context.Background())
	require.NoError(t, err)
	defer release()

	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() {
		_, err := s.Acquire(ctx)
		errc <- err
	}()
	cancel()

	select {
	case err := <-errc:
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(5 * time.Second):
		t.Fatal("Acquire did not return after its context was cancelled")
	}

	ran := false
	err = s.Run(ctx, func() { ran = true })
	assert.ErrorIs(t, err, context.Canceled)
	assert.False(t, ran, "Run does not call fn when the wait is cut short")
}

// A nil pool never blocks: callers built without one keep unbounded renders.
func TestSlots_NilNeverBlocks(t *testing.T) {
	var s *Slots
	releases := make([]func(), 0, 3)
	for range 3 {
		release, err := s.Acquire(context.Background())
		require.NoError(t, err)
		releases = append(releases, release)
	}
	for _, release := range releases {
		release()
	}
	ran := false
	require.NoError(t, s.Run(context.Background(), func() { ran = true }))
	assert.True(t, ran)
}

// A panic inside Run frees the slot: controller-runtime recovers reconcile
// panics, so a leaked slot would block every later render.
func TestSlots_PanicInRunFreesSlot(t *testing.T) {
	s := NewSlots(1)

	func() {
		defer func() { assert.NotNil(t, recover()) }()
		_ = s.Run(context.Background(), func() { panic("render blew up") })
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	release, err := s.Acquire(ctx)
	require.NoError(t, err, "the slot was given back on panic")
	release()
}
