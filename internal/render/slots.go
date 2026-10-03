package render

import (
	"context"
	"fmt"
	"sync"
)

// Slots bounds how many renders are in flight across the process. The
// manager builds one pool from --max-concurrent-renders and hands it to both
// the ModuleInstance and the ModulePackage reconcilers, so the flag counts
// renders of both kinds together. A slot is a memory bound, never a
// correctness gate: it does not order or exclude particular renders.
//
// A nil *Slots never blocks, so callers built without a pool keep unbounded
// renders.
type Slots struct {
	ch chan struct{}
}

// NewSlots returns a pool of n slots. It panics when n is below 1: a pool of
// no slots would block every render forever. The manager refuses a smaller
// --max-concurrent-renders before it gets here.
func NewSlots(n int) *Slots {
	if n < 1 {
		panic(fmt.Sprintf("render.NewSlots: n must be at least 1, got %d", n))
	}
	return &Slots{ch: make(chan struct{}, n)}
}

// Acquire blocks until a slot is free or ctx is done, and returns ctx's error
// in the second case. The returned release gives the slot back; calling it
// again is a no-op.
func (s *Slots) Acquire(ctx context.Context) (release func(), err error) {
	if s == nil {
		return func() {}, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case s.ch <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	var once sync.Once
	return func() { once.Do(func() { <-s.ch }) }, nil
}

// Run takes a slot, calls fn, and gives the slot back with a deferred call,
// so a panic in fn frees the slot too: controller-runtime recovers reconcile
// panics and keeps the worker, and a slot leaked that way would block every
// later render. Run returns only the wait's error; fn reports its own result
// through its closure.
func (s *Slots) Run(ctx context.Context, fn func()) error {
	release, err := s.Acquire(ctx)
	if err != nil {
		return err
	}
	defer release()
	fn()
	return nil
}
