package reconcile

import (
	"context"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	"github.com/open-platform-model/opm-operator/internal/render"
	opmsource "github.com/open-platform-model/opm-operator/internal/source"
	"github.com/open-platform-model/opm-operator/internal/status"
)

// A render that does not finish within --render-timeout is a transient
// failure with reason RenderTimedOut on the bounded backoff, recorded by the
// deferred status commit on the reconcile's own context, while the render
// keeps its slot until it returns. The stubs below stand for a CUE stage that
// keeps running after the render context is done.

const (
	testRenderTimeout = 20 * time.Millisecond
	lastSuccessDigest = "sha256:last-success"
)

// ctxModuleRenderer adapts a context-aware func to render.ModuleRenderer.
type ctxModuleRenderer func(ctx context.Context) (*render.RenderResult, error)

func (f ctxModuleRenderer) RenderModule(
	ctx context.Context, _, _, _, _ string, _ *releasesv1alpha1.RawValues,
) (*render.RenderResult, error) {
	return f(ctx)
}

// ctxPackageRenderer adapts a context-aware func to render.PackageRenderer.
type ctxPackageRenderer func(ctx context.Context, packageDir string) (string, *render.RenderResult, error)

func (f ctxPackageRenderer) Render(ctx context.Context, packageDir string) (string, *render.RenderResult, error) {
	return f(ctx, packageDir)
}

// stuckStage is a render that blocks past its deadline until release is
// closed, then returns what its after func returns. returned is closed when
// the render returns.
type stuckStage struct {
	calls    atomic.Int32
	release  chan struct{}
	returned chan struct{}
}

func newStuckStage() *stuckStage {
	return &stuckStage{release: make(chan struct{}), returned: make(chan struct{})}
}

func (s *stuckStage) block(ctx context.Context) {
	s.calls.Add(1)
	<-ctx.Done()
	<-s.release
}

// finish lets the stuck render return and waits until pool has freed its
// slot.
func (s *stuckStage) finish(t *testing.T, pool *render.Slots) {
	t.Helper()
	close(s.release)
	select {
	case <-s.returned:
	case <-time.After(5 * time.Second):
		t.Fatal("the stuck render did not return")
	}
	waitFor(t, func() bool { return pool.Held() == 0 }, "the slot is freed once the render returns")
}

func waitFor(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal(msg)
		}
		time.Sleep(time.Millisecond)
	}
}

func storedInstance(t *testing.T, params *ModuleInstanceParams) *releasesv1alpha1.ModuleInstance {
	t.Helper()
	var got releasesv1alpha1.ModuleInstance
	if err := params.Client.Get(context.Background(), loopTestRequest.NamespacedName, &got); err != nil {
		t.Fatalf("get: %v", err)
	}
	return &got
}

// expectTimedOutConditions asserts Ready=False and Reconciling=True with
// reason RenderTimedOut, and no Stalled.
func expectTimedOutConditions(t *testing.T, conds []metav1.Condition, wantMsg string) {
	t.Helper()
	ready := apimeta.FindStatusCondition(conds, status.ReadyCondition)
	if ready == nil || ready.Status != metav1.ConditionFalse || ready.Reason != status.RenderTimedOutReason {
		t.Fatalf("Ready = %+v, want False with reason %s", ready, status.RenderTimedOutReason)
	}
	if !strings.Contains(ready.Message, wantMsg) {
		t.Fatalf("Ready message = %q, want it to contain %q", ready.Message, wantMsg)
	}
	rec := apimeta.FindStatusCondition(conds, status.ReconcilingCondition)
	if rec == nil || rec.Status != metav1.ConditionTrue || rec.Reason != status.RenderTimedOutReason {
		t.Fatalf("Reconciling = %+v, want True with reason %s", rec, status.RenderTimedOutReason)
	}
	if stalled := apimeta.FindStatusCondition(conds, status.StalledCondition); stalled != nil {
		t.Fatalf("Stalled = %+v, want absent: a render timeout is transient", stalled)
	}
}

func expectEvent(t *testing.T, rec *events.FakeRecorder, reason string) {
	t.Helper()
	for {
		select {
		case e := <-rec.Events:
			if strings.Contains(e, reason) {
				return
			}
		default:
			t.Fatalf("no event with reason %s", reason)
		}
	}
}

// timeoutInstanceParams wires a ModuleInstance reconcile whose renderer is r
// on pool, with the test render timeout. The stored instance carries
// counters and a last success, which a timeout must leave alone.
func timeoutInstanceParams(t *testing.T, pool *render.Slots, r render.ModuleRenderer, reconcileFailures int64) *ModuleInstanceParams {
	t.Helper()
	mi := operatorInstance(&releasesv1alpha1.FailureCounters{Reconcile: reconcileFailures})
	mi.Status.LastAppliedRenderDigest = lastSuccessDigest
	mi.Status.Inventory = &releasesv1alpha1.Inventory{Revision: 3, Digest: "sha256:inventory"}
	return &ModuleInstanceParams{
		Client:        loopTestClient(t, mi),
		EventRecorder: events.NewFakeRecorder(32),
		Renderer:      r,
		RenderSlots:   pool,
		RenderTimeout: testRenderTimeout,
	}
}

func TestRenderTimeout_ModuleInstance(t *testing.T) {
	t.Run("a render past its deadline is RenderTimedOut on the backoff", func(t *testing.T) {
		pool := render.NewSlots(1)
		stage := newStuckStage()
		params := timeoutInstanceParams(t, pool, ctxModuleRenderer(func(ctx context.Context) (*render.RenderResult, error) {
			defer close(stage.returned)
			stage.block(ctx)
			return nil, ctx.Err()
		}), 0)

		res, err := ReconcileModuleInstance(context.Background(), params, loopTestRequest)
		if err != nil {
			t.Fatalf("reconcile: %v", err)
		}
		if res != (ctrl.Result{RequeueAfter: 5 * time.Second}) {
			t.Fatalf("result = %+v, want RequeueAfter 5s", res)
		}
		if held := pool.Held(); held != 1 {
			t.Fatalf("Held() after the reconcile = %d, want 1: the stuck render keeps its slot", held)
		}

		got := storedInstance(t, params)
		expectTimedOutConditions(t, got.Status.Conditions, "did not finish within 20ms")
		if got.Status.FailureCounters == nil || got.Status.FailureCounters.Reconcile != 1 {
			t.Fatalf("failureCounters = %+v, want reconcile 1", got.Status.FailureCounters)
		}
		if got.Status.NextRetryAt == nil {
			t.Fatal("nextRetryAt is nil, want the backoff time")
		}
		if n := len(got.Status.History); n == 0 || !strings.Contains(got.Status.History[0].Message, "did not finish within 20ms") {
			t.Fatalf("history = %+v, want a failure entry naming the timeout", got.Status.History)
		}
		if got.Status.LastAppliedRenderDigest != lastSuccessDigest {
			t.Fatalf("lastAppliedRenderDigest = %q, want the last success kept", got.Status.LastAppliedRenderDigest)
		}
		if got.Status.Inventory == nil || got.Status.Inventory.Digest != "sha256:inventory" {
			t.Fatalf("inventory = %+v, want the last success kept", got.Status.Inventory)
		}
		expectEvent(t, params.EventRecorder.(*events.FakeRecorder), status.RenderTimedOutReason)

		stage.finish(t, pool)
	})

	t.Run("repeated timeouts cap at five minutes", func(t *testing.T) {
		pool := render.NewSlots(1)
		stage := newStuckStage()
		params := timeoutInstanceParams(t, pool, ctxModuleRenderer(func(ctx context.Context) (*render.RenderResult, error) {
			defer close(stage.returned)
			stage.block(ctx)
			return nil, ctx.Err()
		}), 10)

		res, err := ReconcileModuleInstance(context.Background(), params, loopTestRequest)
		if err != nil {
			t.Fatalf("reconcile: %v", err)
		}
		if res != (ctrl.Result{RequeueAfter: 5 * time.Minute}) {
			t.Fatalf("result = %+v, want RequeueAfter 5m", res)
		}
		stage.finish(t, pool)
	})

	t.Run("a success after the deadline is still a timeout", func(t *testing.T) {
		pool := render.NewSlots(1)
		stage := newStuckStage()
		result := configMapRenderResult(t)
		params := timeoutInstanceParams(t, pool, ctxModuleRenderer(func(ctx context.Context) (*render.RenderResult, error) {
			defer close(stage.returned)
			stage.block(ctx)
			return result, nil
		}), 0)

		if _, err := ReconcileModuleInstance(context.Background(), params, loopTestRequest); err != nil {
			t.Fatalf("reconcile: %v", err)
		}
		got := storedInstance(t, params)
		expectTimedOutConditions(t, got.Status.Conditions, "did not finish within")
		if got.Status.LastAppliedRenderDigest != lastSuccessDigest {
			t.Fatalf("lastAppliedRenderDigest = %q, want the late success discarded", got.Status.LastAppliedRenderDigest)
		}
		stage.finish(t, pool)
	})

	t.Run("a retry while the render still runs starts no second render", func(t *testing.T) {
		pool := render.NewSlots(2)
		stage := newStuckStage()
		params := timeoutInstanceParams(t, pool, ctxModuleRenderer(func(ctx context.Context) (*render.RenderResult, error) {
			defer close(stage.returned)
			stage.block(ctx)
			return nil, ctx.Err()
		}), 0)

		if _, err := ReconcileModuleInstance(context.Background(), params, loopTestRequest); err != nil {
			t.Fatalf("first reconcile: %v", err)
		}
		res, err := ReconcileModuleInstance(context.Background(), params, loopTestRequest)
		if err != nil {
			t.Fatalf("second reconcile: %v", err)
		}
		if res != (ctrl.Result{RequeueAfter: 10 * time.Second}) {
			t.Fatalf("result = %+v, want RequeueAfter 10s, the second step of the backoff", res)
		}
		if calls := stage.calls.Load(); calls != 1 {
			t.Fatalf("renderer called %d times, want 1: the retry must not render beside the stuck render", calls)
		}
		if held := pool.Held(); held != 1 {
			t.Fatalf("Held() = %d, want 1: a hung object holds at most one slot", held)
		}
		got := storedInstance(t, params)
		expectTimedOutConditions(t, got.Status.Conditions, "is still running; no new render was started")
		if got.Status.FailureCounters == nil || got.Status.FailureCounters.Reconcile != 2 {
			t.Fatalf("failureCounters = %+v, want reconcile 2", got.Status.FailureCounters)
		}
		stage.finish(t, pool)
	})

	t.Run("a panic with a timeout set is recorded and frees the slot", func(t *testing.T) {
		pool := render.NewSlots(1)
		params := timeoutInstanceParams(t, pool, ctxModuleRenderer(func(context.Context) (*render.RenderResult, error) {
			panic("renderer exploded")
		}), 0)
		params.RenderTimeout = time.Minute

		got := panicValue(func() { _, _ = ReconcileModuleInstance(context.Background(), params, loopTestRequest) })
		if got != "renderer exploded" {
			t.Fatalf("panicked with %#v, want the renderer's own value", got)
		}
		if held := pool.Held(); held != 0 {
			t.Fatalf("Held() after the panic = %d, want 0", held)
		}
		expectPanicStatus(t, storedInstance(t, params).Status.Conditions)
	})
}

// timeoutPackageParams wires a ModulePackage reconcile whose renderer is r on
// pool, with the test render timeout.
func timeoutPackageParams(t *testing.T, pool *render.Slots, r render.PackageRenderer) *ModulePackageParams {
	t.Helper()
	return &ModulePackageParams{
		Client:        loopTestClient(t, readyOCIRepository(), operatorPackage(nil)),
		EventRecorder: events.NewFakeRecorder(32),
		Fetcher:       instanceFileFetcher{},
		Renderer:      r,
		RenderSlots:   pool,
		RenderTimeout: testRenderTimeout,
	}
}

func storedPackage(t *testing.T, params *ModulePackageParams) *releasesv1alpha1.ModulePackage {
	t.Helper()
	var got releasesv1alpha1.ModulePackage
	if err := params.Client.Get(context.Background(), loopTestRequest.NamespacedName, &got); err != nil {
		t.Fatalf("get: %v", err)
	}
	return &got
}

func dirExists(dir string) bool {
	_, err := os.Stat(dir)
	return err == nil
}

// recordingFetcher extracts as instanceFileFetcher does and records each
// extract dir it was handed.
type recordingFetcher struct {
	instanceFileFetcher
	dirs chan string
}

func (f recordingFetcher) Fetch(ctx context.Context, url, digest, dir string, opts opmsource.FetchOptions) error {
	f.dirs <- dir
	return f.instanceFileFetcher.Fetch(ctx, url, digest, dir, opts)
}

func TestRenderTimeout_ModulePackage(t *testing.T) {
	t.Run("a render past its deadline keeps its files until it returns", func(t *testing.T) {
		pool := render.NewSlots(1)
		stage := newStuckStage()
		dirc := make(chan string, 1)
		params := timeoutPackageParams(t, pool, ctxPackageRenderer(func(ctx context.Context, packageDir string) (string, *render.RenderResult, error) {
			defer close(stage.returned)
			dirc <- packageDir
			stage.block(ctx)
			return "", nil, ctx.Err()
		}))

		res, err := ReconcileModulePackage(context.Background(), params, loopTestRequest)
		if err != nil {
			t.Fatalf("reconcile: %v", err)
		}
		if res != (ctrl.Result{RequeueAfter: 5 * time.Second}) {
			t.Fatalf("result = %+v, want RequeueAfter 5s on the backoff", res)
		}
		packageDir := <-dirc
		if !dirExists(packageDir) {
			t.Fatalf("package dir %s is gone while its render still runs", packageDir)
		}
		if held := pool.Held(); held != 1 {
			t.Fatalf("Held() = %d, want 1", held)
		}

		got := storedPackage(t, params)
		expectTimedOutConditions(t, got.Status.Conditions, "did not finish within 20ms")
		if got.Status.FailureCounters == nil || got.Status.FailureCounters.Reconcile != 1 {
			t.Fatalf("failureCounters = %+v, want reconcile 1", got.Status.FailureCounters)
		}
		if got.Status.NextRetryAt == nil {
			t.Fatal("nextRetryAt is nil, want the backoff time")
		}
		expectEvent(t, params.EventRecorder.(*events.FakeRecorder), status.RenderTimedOutReason)

		stage.finish(t, pool)
		waitFor(t, func() bool { return !dirExists(packageDir) }, "the package dir is removed once the render returns")
	})

	t.Run("a retry while the render still runs starts no second render and removes its files", func(t *testing.T) {
		pool := render.NewSlots(2)
		stage := newStuckStage()
		params := timeoutPackageParams(t, pool, ctxPackageRenderer(func(ctx context.Context, _ string) (string, *render.RenderResult, error) {
			defer close(stage.returned)
			stage.block(ctx)
			return "", nil, ctx.Err()
		}))
		fetcher := recordingFetcher{dirs: make(chan string, 2)}
		params.Fetcher = fetcher

		if _, err := ReconcileModulePackage(context.Background(), params, loopTestRequest); err != nil {
			t.Fatalf("first reconcile: %v", err)
		}
		firstDir := <-fetcher.dirs
		res, err := ReconcileModulePackage(context.Background(), params, loopTestRequest)
		if err != nil {
			t.Fatalf("second reconcile: %v", err)
		}
		secondDir := <-fetcher.dirs
		if res != (ctrl.Result{RequeueAfter: 10 * time.Second}) {
			t.Fatalf("result = %+v, want RequeueAfter 10s, the second step of the backoff", res)
		}
		if calls := stage.calls.Load(); calls != 1 {
			t.Fatalf("renderer called %d times, want 1: the retry must not render beside the stuck render", calls)
		}
		if held := pool.Held(); held != 1 {
			t.Fatalf("Held() = %d, want 1: a hung object holds at most one slot", held)
		}
		if dirExists(secondDir) {
			t.Fatalf("the retry's extract dir %s is left behind although no render took it", secondDir)
		}
		if !dirExists(firstDir) {
			t.Fatalf("the running render's extract dir %s is gone while it still runs", firstDir)
		}
		got := storedPackage(t, params)
		expectTimedOutConditions(t, got.Status.Conditions, "is still running; no new render was started")
		if got.Status.FailureCounters == nil || got.Status.FailureCounters.Reconcile != 2 {
			t.Fatalf("failureCounters = %+v, want reconcile 2", got.Status.FailureCounters)
		}

		stage.finish(t, pool)
		waitFor(t, func() bool { return !dirExists(firstDir) }, "the running render's dir is removed once it returns")
	})

	t.Run("a panicking body still removes its files", func(t *testing.T) {
		pool := render.NewSlots(1)
		dirc := make(chan string, 1)
		params := timeoutPackageParams(t, pool, ctxPackageRenderer(func(_ context.Context, packageDir string) (string, *render.RenderResult, error) {
			dirc <- packageDir
			panic("package renderer exploded")
		}))
		params.RenderTimeout = time.Minute

		got := panicValue(func() { _, _ = ReconcileModulePackage(context.Background(), params, loopTestRequest) })
		if got != "package renderer exploded" {
			t.Fatalf("panicked with %#v, want the renderer's own value", got)
		}
		if held := pool.Held(); held != 0 {
			t.Fatalf("Held() after the panic = %d, want 0", held)
		}
		expectPanicStatus(t, storedPackage(t, params).Status.Conditions)
		if packageDir := <-dirc; dirExists(packageDir) {
			t.Fatalf("package dir %s left behind after a panic", packageDir)
		}
	})
}
