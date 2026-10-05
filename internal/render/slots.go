package render

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"sync"
	"time"

	"github.com/go-logr/logr"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// ErrRenderTimedOut reports that a render did not finish within its timeout.
// The render may still be running: its slot stays held until it returns.
var ErrRenderTimedOut = errors.New("render timed out")

// ErrRenderStillRunning reports that an earlier render of the same object
// timed out and has not returned yet, so Run started no new render and took
// no slot.
var ErrRenderStillRunning = errors.New("the previous render of this object is still running")

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

	// mu guards running, the keys whose bounded render is in flight. A key
	// is added when Run starts the render goroutine and removed when the
	// render returns, so an object whose render was abandoned cannot start a
	// second one beside it.
	mu      sync.Mutex
	running map[string]struct{}
}

// NewSlots returns a pool of n slots. It panics when n is below 1: a pool of
// no slots would block every render forever. The manager refuses a smaller
// --max-concurrent-renders before it gets here.
func NewSlots(n int) *Slots {
	if n < 1 {
		panic(fmt.Sprintf("render.NewSlots: n must be at least 1, got %d", n))
	}
	return &Slots{ch: make(chan struct{}, n), running: map[string]struct{}{}}
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

// Held reports how many slots are taken. A nil pool holds none. It exists so
// tests can observe that a step runs while its caller holds a slot; it is a
// snapshot and orders nothing.
func (s *Slots) Held() int {
	if s == nil {
		return 0
	}
	return len(s.ch)
}

// Run takes a slot, calls fn with a context, and gives the slot back when fn
// returns, also when fn panics: controller-runtime recovers reconcile panics
// and keeps the worker, and a slot leaked that way would block every later
// render. fn reports its own result through its closure. key names the
// object being rendered (kind/namespace/name); an empty key is not tracked.
//
// With timeout <= 0, fn runs on the caller's goroutine with ctx unchanged.
//
// With timeout > 0, fn runs in its own goroutine under a context whose
// deadline is timeout, counted from when the slot is taken: the wait for a
// slot is not counted. When the deadline passes before fn returns, Run
// returns ErrRenderTimedOut at once, and fn keeps its slot until it really
// returns, so the pool still bounds the renders held in memory. A fn that
// returns after its deadline is a timeout too, even if it succeeded. While a
// timed-out fn of a key still runs, Run with that key returns
// ErrRenderStillRunning without taking a slot or calling fn, so one object
// whose render hangs holds at most one slot. A panic in fn before Run stops
// waiting is logged with its stack and raised again with its original value
// after the slot is free; a panic after that is logged and goes no further.
// When ctx itself is cancelled while fn runs, Run waits for fn as it does
// without a timeout.
//
// Caller contract: after ErrRenderTimedOut, never read anything fn writes,
// because fn may still be writing it. When Run returns nil, fn has returned
// and its writes are visible. fn was called exactly when Run returned nil or
// ErrRenderTimedOut, or panicked; on every other error it was not called.
func (s *Slots) Run(ctx context.Context, key string, timeout time.Duration, fn func(context.Context)) error {
	if s.isRunning(key) {
		return ErrRenderStillRunning
	}
	release, err := s.Acquire(ctx)
	if err != nil {
		return err
	}
	if timeout <= 0 {
		defer release()
		fn(ctx)
		return nil
	}

	// cancel belongs to Run, never to the goroutine: renderCtx.Done() must
	// close only at the deadline or with the parent, or a render that
	// finished in time could race its own cancel at the select below.
	renderCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	done := make(chan renderReturn, 1)
	s.markRunning(key)
	start := time.Now()
	go func() {
		var r renderReturn
		// The slot and the key are freed before anyone hears of the return.
		defer func() {
			s.clearRunning(key)
			release()
			done <- r
		}()
		defer func() {
			if p := recover(); p != nil {
				r = renderReturn{panicked: true, value: p, stack: debug.Stack()}
			}
		}()
		fn(renderCtx)
	}()

	select {
	case r := <-done:
		return returned(ctx, renderCtx, r)
	case <-renderCtx.Done():
		select {
		case r := <-done: // fn returned at the same moment
			return returned(ctx, renderCtx, r)
		default:
		}
		if ctx.Err() != nil {
			// The parent was cancelled (manager shutdown): wait for fn, as
			// Run does without a timeout.
			return returned(ctx, renderCtx, <-done)
		}
		go logAbandoned(logf.FromContext(ctx), start, done)
		return ErrRenderTimedOut
	}
}

// renderReturn is how a bounded render ended: normally, or with a panic and
// the stack of the frame that panicked.
type renderReturn struct {
	panicked bool
	value    any
	stack    []byte
}

// returned reports a bounded render that returned while Run still waited. A
// panic is logged with its stack, because the re-raise below would carry
// only the stack of this frame, and raised again with its original value. A
// render that returned after its deadline, with the parent still live, is a
// timeout.
func returned(parent, renderCtx context.Context, r renderReturn) error {
	if r.panicked {
		logf.FromContext(parent).Error(fmt.Errorf("%v", r.value), "Render panicked", "stack", string(r.stack))
		panic(r.value)
	}
	if errors.Is(renderCtx.Err(), context.DeadlineExceeded) && parent.Err() == nil {
		return ErrRenderTimedOut
	}
	return nil
}

// logAbandoned waits for a render Run stopped waiting for and logs how long
// it ran, and its panic with the stack if it panicked.
func logAbandoned(log logr.Logger, start time.Time, done <-chan renderReturn) {
	r := <-done
	elapsed := time.Since(start).String()
	if r.panicked {
		log.Error(fmt.Errorf("%v", r.value), "Abandoned render panicked", "elapsed", elapsed, "stack", string(r.stack))
		return
	}
	log.Info("Abandoned render returned", "elapsed", elapsed)
}

func (s *Slots) isRunning(key string) bool {
	if s == nil || key == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.running[key]
	return ok
}

func (s *Slots) markRunning(key string) {
	if s == nil || key == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.running[key] = struct{}{}
}

func (s *Slots) clearRunning(key string) {
	if s == nil || key == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.running, key)
}
