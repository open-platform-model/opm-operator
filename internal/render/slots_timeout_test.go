package render

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-logr/logr/funcr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// eventually polls cond until it holds or a bounded wait runs out.
func eventually(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	assert.Eventually(t, cond, 5*time.Second, time.Millisecond, msg)
}

// logSink records every log line as one string, safe for concurrent use.
type logSink struct {
	mu    sync.Mutex
	lines []string
}

func (l *logSink) ctx() context.Context {
	logger := funcr.New(func(prefix, args string) {
		l.mu.Lock()
		defer l.mu.Unlock()
		l.lines = append(l.lines, args)
	}, funcr.Options{})
	return logf.IntoContext(context.Background(), logger)
}

func (l *logSink) find(substr string) string {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, line := range l.lines {
		if strings.Contains(line, substr) {
			return line
		}
	}
	return ""
}

// (a) A body that blocks past its deadline, as a CUE stage that is still
// running does: Run returns ErrRenderTimedOut at the deadline, and the slot
// stays held until the body really returns.
func TestRun_TimeoutKeepsSlotUntilBodyReturns(t *testing.T) {
	s := NewSlots(1)
	stage := make(chan struct{})
	bodyDone := make(chan struct{})

	start := time.Now()
	err := s.Run(context.Background(), "mi/ns/a", 20*time.Millisecond, func(ctx context.Context) {
		defer close(bodyDone)
		<-ctx.Done()
		<-stage
	})
	require.ErrorIs(t, err, ErrRenderTimedOut)
	assert.Less(t, time.Since(start), 2*time.Second, "Run returned at the deadline, not when the body returned")
	assert.Equal(t, 1, s.Held(), "the abandoned render keeps its slot")

	close(stage)
	<-bodyDone
	eventually(t, func() bool { return s.Held() == 0 }, "the slot is freed once the body returns")
}

// (b) A body that returns after its deadline is a timeout, even when it
// ignores the context.
func TestRun_ReturnAfterDeadlineIsTimeout(t *testing.T) {
	s := NewSlots(1)
	err := s.Run(context.Background(), "mi/ns/a", 10*time.Millisecond, func(ctx context.Context) {
		<-ctx.Done()
		time.Sleep(5 * time.Millisecond) // the stage ends just after the check
	})
	require.ErrorIs(t, err, ErrRenderTimedOut)
	eventually(t, func() bool { return s.Held() == 0 }, "the slot is freed")
}

// (c) A body that returns in time gives nil, and its writes are visible.
func TestRun_InTimeReturnsNilAndWritesAreVisible(t *testing.T) {
	s := NewSlots(1)
	var got string
	err := s.Run(context.Background(), "mi/ns/a", time.Minute, func(ctx context.Context) {
		got = "rendered"
	})
	require.NoError(t, err)
	assert.Equal(t, "rendered", got)
	assert.Equal(t, 0, s.Held(), "the slot is free when Run returns nil")
}

// (i) An instant body never reads as a timeout: the render context is
// cancelled by Run, not by the goroutine, so its Done channel is never ready
// beside done for a render that finished in time.
func TestRun_InstantBodyNeverTimesOut(t *testing.T) {
	s := NewSlots(1)
	for i := range 2000 {
		var got int
		err := s.Run(context.Background(), "mi/ns/a", time.Minute, func(context.Context) { got = i })
		require.NoError(t, err, "iteration %d", i)
		require.Equal(t, i, got)
	}
}

// (d) and (k) A panic before the deadline is raised again on the caller with
// its original value, after the slot is free, and logged with the stack of
// the panicking frame.
func TestRun_PanicBeforeDeadlineReachesCaller(t *testing.T) {
	s := NewSlots(1)
	var logs logSink

	var recovered any
	var heldAtRecover int
	func() {
		defer func() {
			recovered = recover()
			heldAtRecover = s.Held()
		}()
		_ = s.Run(logs.ctx(), "mi/ns/a", time.Minute, func(context.Context) { panicInRenderFrame() })
	}()
	assert.Equal(t, "render blew up", recovered, "the original panic value")
	assert.Equal(t, 0, heldAtRecover, "the slot is free before the panic reaches the caller")

	line := logs.find("Render panicked")
	require.NotEmpty(t, line, "the panic is logged")
	assert.Contains(t, line, "panicInRenderFrame", "the logged stack names the panicking frame")
}

func panicInRenderFrame() { panic("render blew up") }

// (e) A panic after the reconcile stopped waiting does not crash the
// process, frees the slot, and is logged with its stack.
func TestRun_PanicAfterDeadlineIsLogged(t *testing.T) {
	s := NewSlots(1)
	var logs logSink
	stage := make(chan struct{})

	err := s.Run(logs.ctx(), "mi/ns/a", 10*time.Millisecond, func(ctx context.Context) {
		<-ctx.Done()
		<-stage
		panicInRenderFrame()
	})
	require.ErrorIs(t, err, ErrRenderTimedOut)
	close(stage)

	eventually(t, func() bool { return s.Held() == 0 }, "the slot is freed after a late panic")
	eventually(t, func() bool { return logs.find("Abandoned render panicked") != "" }, "the late panic is logged")
	assert.Contains(t, logs.find("Abandoned render panicked"), "panicInRenderFrame")
}

// An abandoned render that returns logs how long it ran.
func TestRun_AbandonedReturnIsLogged(t *testing.T) {
	s := NewSlots(1)
	var logs logSink
	stage := make(chan struct{})

	err := s.Run(logs.ctx(), "mi/ns/a", 10*time.Millisecond, func(ctx context.Context) {
		<-ctx.Done()
		<-stage
	})
	require.ErrorIs(t, err, ErrRenderTimedOut)
	close(stage)
	eventually(t, func() bool { return logs.find("Abandoned render returned") != "" }, "the return is logged")
	assert.Contains(t, logs.find("Abandoned render returned"), "elapsed")
}

// (f) timeout = 0 calls the body on the caller's goroutine with the caller's
// context: no deadline, and the body runs before Run returns.
func TestRun_ZeroTimeoutIsSynchronous(t *testing.T) {
	s := NewSlots(1)
	type key struct{}
	parent := context.WithValue(context.Background(), key{}, "caller")

	var sawDeadline bool
	var sawValue any
	var sameCtx bool
	err := s.Run(parent, "mi/ns/a", 0, func(ctx context.Context) {
		_, sawDeadline = ctx.Deadline()
		sawValue = ctx.Value(key{})
		sameCtx = ctx == parent
	})
	require.NoError(t, err)
	assert.False(t, sawDeadline, "no deadline with timeout 0")
	assert.Equal(t, "caller", sawValue)
	assert.True(t, sameCtx, "the body gets the caller's context unchanged")
}

// (g) A parent cancelled during the body (manager shutdown): Run waits for
// the body and returns nil, as it did before the timeout existed.
func TestRun_ParentCancelWaitsForBody(t *testing.T) {
	s := NewSlots(1)
	ctx, cancel := context.WithCancel(context.Background())
	var returned bool
	err := s.Run(ctx, "mi/ns/a", time.Minute, func(rctx context.Context) {
		cancel()
		<-rctx.Done()
		time.Sleep(10 * time.Millisecond)
		returned = true
	})
	require.NoError(t, err)
	assert.True(t, returned, "Run waited for the body")
	assert.Equal(t, 0, s.Held())
}

// (h) The wait for a slot is not counted: a render queued longer than the
// timeout still gets its full timeout once it holds the slot.
func TestRun_SlotWaitIsNotCounted(t *testing.T) {
	s := NewSlots(1)
	release, err := s.Acquire(context.Background())
	require.NoError(t, err)

	const timeout = 50 * time.Millisecond
	errc := make(chan error, 1)
	var remaining time.Duration
	go func() {
		errc <- s.Run(context.Background(), "mi/ns/b", timeout, func(ctx context.Context) {
			dl, _ := ctx.Deadline()
			remaining = time.Until(dl)
		})
	}()
	time.Sleep(3 * timeout)
	release()

	require.NoError(t, <-errc, "the queued render is not timed out")
	assert.Greater(t, remaining, timeout/2, "the deadline started when the slot was taken")
}

// (j) One render per object: while a key's abandoned render still runs, Run
// with that key neither takes a slot nor calls its body, so a hung object
// holds at most one slot across retries. Other keys still render, and the
// key renders again once its abandoned body returns.
func TestRun_AbandonedKeyHoldsAtMostOneSlot(t *testing.T) {
	s := NewSlots(2)
	stage := make(chan struct{})
	bodyDone := make(chan struct{})

	err := s.Run(context.Background(), "mi/ns/hung", 10*time.Millisecond, func(ctx context.Context) {
		defer close(bodyDone)
		<-ctx.Done()
		<-stage
	})
	require.ErrorIs(t, err, ErrRenderTimedOut)

	for range 3 {
		called := false
		err := s.Run(context.Background(), "mi/ns/hung", 10*time.Millisecond, func(context.Context) { called = true })
		require.ErrorIs(t, err, ErrRenderStillRunning)
		assert.NotErrorIs(t, err, ErrRenderTimedOut, "the body was not called, so this is not a timeout of its own")
		assert.False(t, called, "no second render of the hung object")
		assert.Equal(t, 1, s.Held(), "the hung object holds one slot")
	}

	require.NoError(t, s.Run(context.Background(), "mi/ns/other", time.Minute, func(context.Context) {}),
		"another object still renders on the free slot")

	close(stage)
	<-bodyDone
	eventually(t, func() bool {
		return s.Run(context.Background(), "mi/ns/hung", time.Minute, func(context.Context) {}) == nil
	}, "the key renders again once the abandoned body returned")
	assert.Equal(t, 0, s.Held())
}

// An empty key is not tracked: two abandoned renders without a key each take
// a slot, as two different objects would.
func TestRun_EmptyKeyIsNotTracked(t *testing.T) {
	s := NewSlots(2)
	stage := make(chan struct{})
	defer close(stage)
	for range 2 {
		err := s.Run(context.Background(), "", 10*time.Millisecond, func(ctx context.Context) {
			<-ctx.Done()
			<-stage
		})
		require.ErrorIs(t, err, ErrRenderTimedOut)
	}
	assert.Equal(t, 2, s.Held())
}

// A nil pool with a timeout still bounds the body and never blocks.
func TestRun_NilPoolWithTimeout(t *testing.T) {
	var s *Slots
	stage := make(chan struct{})
	defer close(stage)
	err := s.Run(context.Background(), "mi/ns/a", 10*time.Millisecond, func(ctx context.Context) {
		<-ctx.Done()
		<-stage
	})
	require.ErrorIs(t, err, ErrRenderTimedOut)
	require.NoError(t, s.Run(context.Background(), "mi/ns/a", time.Minute, func(context.Context) {}),
		"a nil pool tracks no keys")
}

// The two sentinels are distinct, so a caller can tell a body that was
// called from one that was not.
func TestRun_SentinelsAreDistinct(t *testing.T) {
	assert.False(t, errors.Is(ErrRenderStillRunning, ErrRenderTimedOut))
	assert.False(t, errors.Is(fmt.Errorf("x: %w", ErrRenderTimedOut), ErrRenderStillRunning))
}
