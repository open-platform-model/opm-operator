package reconcile

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	goruntime "runtime"
	"testing"
	"time"

	"cuelang.org/go/cue/cuecontext"
	fluxmeta "github.com/fluxcd/pkg/apis/meta"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/open-platform-model/library/opm/k8s/object"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	"github.com/open-platform-model/opm-operator/internal/inventory"
	"github.com/open-platform-model/opm-operator/internal/render"
	opmsource "github.com/open-platform-model/opm-operator/internal/source"
	"github.com/open-platform-model/opm-operator/internal/status"
)

const (
	loopTestNamespace = "team-a"
	loopTestName      = "app"
	loopTestPath      = "releases/app"
)

// loopTestClient is a fake client holding objs, with the status subresource
// enabled for both workload kinds so the deferred status commit can patch.
func loopTestClient(t *testing.T, objs ...client.Object) client.Client {
	t.Helper()
	s := runtime.NewScheme()
	if err := releasesv1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("add releasesv1alpha1: %v", err)
	}
	if err := sourcev1.AddToScheme(s); err != nil {
		t.Fatalf("add sourcev1: %v", err)
	}
	return fake.NewClientBuilder().
		WithScheme(s).
		WithObjects(objs...).
		WithStatusSubresource(&releasesv1alpha1.ModuleInstance{}, &releasesv1alpha1.ModulePackage{}).
		Build()
}

// configMapRenderResult is a fresh one-ConfigMap render result whose
// resource carries a real CUE value, so convertRender exports it as in
// production.
func configMapRenderResult(t *testing.T) *render.RenderResult {
	t.Helper()
	v := cuecontext.New().CompileString(fmt.Sprintf(`{
	apiVersion: "v1"
	kind:       "ConfigMap"
	metadata: {name: "app-config", namespace: %q}
	data: message: "hello"
}`, loopTestNamespace))
	if v.Err() != nil {
		t.Fatalf("compiling ConfigMap: %v", v.Err())
	}
	resource := &object.Resource{Value: v, Instance: loopTestName, Component: "web", Transformer: "kubernetes#simple"}
	u, err := resource.ToUnstructured()
	if err != nil {
		t.Fatalf("converting ConfigMap: %v", err)
	}
	return &render.RenderResult{
		Resources:        []*object.Resource{resource},
		InventoryEntries: []releasesv1alpha1.InventoryEntry{inventory.NewEntryFromResource(u)},
	}
}

// moduleRendererFunc adapts a func to render.ModuleRenderer.
type moduleRendererFunc func() (*render.RenderResult, error)

func (f moduleRendererFunc) RenderModule(
	context.Context, string, string, string, string, *releasesv1alpha1.RawValues,
) (*render.RenderResult, error) {
	return f()
}

// packageRendererFunc adapts a func to render.PackageRenderer.
type packageRendererFunc func() (string, *render.RenderResult, error)

func (f packageRendererFunc) Render(context.Context, string) (string, *render.RenderResult, error) {
	return f()
}

// instanceFileFetcher "extracts" an artifact holding loopTestPath/instance.cue.
type instanceFileFetcher struct{}

func (instanceFileFetcher) Fetch(_ context.Context, _, _, dir string, _ opmsource.FetchOptions) error {
	pkgDir := filepath.Join(dir, loopTestPath)
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(pkgDir, "instance.cue"), []byte("package app\n"), 0o600)
}

// operatorInstance is an operator-owned ModuleInstance past its finalizer
// registration, carrying counters.
func operatorInstance(counters *releasesv1alpha1.FailureCounters) *releasesv1alpha1.ModuleInstance {
	return &releasesv1alpha1.ModuleInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name: loopTestName, Namespace: loopTestNamespace, Generation: 1,
			Finalizers: []string{FinalizerName},
		},
		Spec: releasesv1alpha1.ModuleInstanceSpec{
			Module: releasesv1alpha1.ModuleReference{Path: "opmodel.dev/test/module", Version: "v0.1.0"},
		},
		Status: releasesv1alpha1.ModuleInstanceStatus{FailureCounters: counters},
	}
}

// readyOCIRepository is a Flux source with a ready artifact.
func readyOCIRepository() *sourcev1.OCIRepository {
	return &sourcev1.OCIRepository{
		ObjectMeta: metav1.ObjectMeta{Name: "app-src", Namespace: loopTestNamespace},
		Spec:       sourcev1.OCIRepositorySpec{URL: "oci://example.com/repo", Interval: metav1.Duration{Duration: time.Minute}},
		Status: sourcev1.OCIRepositoryStatus{
			Conditions: []metav1.Condition{{
				Type: fluxmeta.ReadyCondition, Status: metav1.ConditionTrue,
				Reason: "Succeeded", LastTransitionTime: metav1.Now(),
			}},
			Artifact: &fluxmeta.Artifact{
				URL: "http://source-controller/artifact.tar.gz", Revision: "main@sha256:aaa",
				Digest: "sha256:aaa", Path: "a.tar.gz", LastUpdateTime: metav1.Now(),
			},
		},
	}
}

// operatorPackage is a ModulePackage past its finalizer registration,
// sourcing loopTestPath from readyOCIRepository and carrying counters.
func operatorPackage(counters *releasesv1alpha1.FailureCounters) *releasesv1alpha1.ModulePackage {
	return &releasesv1alpha1.ModulePackage{
		ObjectMeta: metav1.ObjectMeta{
			Name: loopTestName, Namespace: loopTestNamespace, Generation: 1,
			Finalizers: []string{FinalizerName},
		},
		Spec: releasesv1alpha1.ModulePackageSpec{
			SourceRef: releasesv1alpha1.SourceReference{Kind: opmsource.SourceKindOCIRepository, Name: "app-src"},
			Path:      loopTestPath,
			Interval:  metav1.Duration{Duration: time.Minute},
			Prune:     true,
		},
		Status: releasesv1alpha1.ModulePackageStatus{FailureCounters: counters},
	}
}

var loopTestRequest = ctrl.Request{NamespacedName: types.NamespacedName{Name: loopTestName, Namespace: loopTestNamespace}}

// panicValue runs fn and returns the value it panicked with, nil when it
// returned normally.
func panicValue(fn func()) (recovered any) {
	defer func() { recovered = recover() }()
	fn()
	return nil
}

// expectNilReceiverPanic fails the test unless fn panics with a runtime
// error, the nil-pointer dereference of the nil ResourceManager.
func expectNilReceiverPanic(t *testing.T, fn func()) {
	t.Helper()
	got := panicValue(fn)
	if _, ok := got.(goruntime.Error); !ok {
		t.Fatalf("panicked with %#v, want a runtime error from the nil ResourceManager", got)
	}
}

// expectPanicStatus asserts the Ready condition a recovered panic leaves.
func expectPanicStatus(t *testing.T, conditions []metav1.Condition) {
	t.Helper()
	ready := apimeta.FindStatusCondition(conditions, status.ReadyCondition)
	if ready == nil || ready.Status != metav1.ConditionFalse || ready.Reason != status.ReconcilePanicReason {
		t.Fatalf("Ready = %+v, want False with reason %s", ready, status.ReconcilePanicReason)
	}
}

// staleAttempt is an old lastAttemptedAt, and pendingRetry a nextRetryAt
// left by an earlier failed attempt, both seeded before a panic so the test
// sees the panicking attempt overwrite or clear them.
var (
	staleAttempt = metav1.NewTime(time.Now().Add(-time.Hour).Truncate(time.Second))
	pendingRetry = metav1.NewTime(time.Now().Add(time.Hour).Truncate(time.Second))
)

// expectPanicAttempt asserts that a recovered panic recorded the attempt and
// cleared the retry time, which controller-runtime's rate limiter owns.
func expectPanicAttempt(t *testing.T, action string, attemptedAt, nextRetryAt *metav1.Time) {
	t.Helper()
	if action != reconcileAction {
		t.Fatalf("lastAttemptedAction = %q, want %q", action, reconcileAction)
	}
	if attemptedAt == nil || !attemptedAt.After(staleAttempt.Time) {
		t.Fatalf("lastAttemptedAt = %v, want later than the seeded %v", attemptedAt, staleAttempt)
	}
	if nextRetryAt != nil {
		t.Fatalf("nextRetryAt = %v, want cleared", nextRetryAt)
	}
}

// TestPanicKeepsPhaseCounters pins that a panicking attempt moves only the
// reconcile counter. The loops set a phase's Ran flag before the phase runs
// and its Failed flag only on a returned error, so a panic inside the phase
// would read as a success of that phase if the in-flight phases reached the
// counter update. It also pins that the panic records the attempt and clears
// a pending nextRetryAt. A nil ResourceManager makes the first use of it panic on a
// nil receiver, after the phase flag is set.
func TestPanicKeepsPhaseCounters(t *testing.T) {
	t.Run("ModuleInstance panics in drift detection", func(t *testing.T) {
		mi := operatorInstance(&releasesv1alpha1.FailureCounters{Drift: 2, Apply: 3, Prune: 1})
		mi.Status.LastAttemptedAt = &staleAttempt
		mi.Status.NextRetryAt = &pendingRetry
		c := loopTestClient(t, mi)
		params := &ModuleInstanceParams{
			Client:        c,
			EventRecorder: events.NewFakeRecorder(32),
			Renderer:      moduleRendererFunc(func() (*render.RenderResult, error) { return configMapRenderResult(t), nil }),
		}

		expectNilReceiverPanic(t, func() { _, _ = ReconcileModuleInstance(context.Background(), params, loopTestRequest) })

		var got releasesv1alpha1.ModuleInstance
		if err := c.Get(context.Background(), loopTestRequest.NamespacedName, &got); err != nil {
			t.Fatalf("get: %v", err)
		}
		expectPanicStatus(t, got.Status.Conditions)
		expectPanicAttempt(t, got.Status.LastAttemptedAction, got.Status.LastAttemptedAt, got.Status.NextRetryAt)
		want := releasesv1alpha1.FailureCounters{Reconcile: 1, Drift: 2, Apply: 3, Prune: 1}
		if got.Status.FailureCounters == nil || *got.Status.FailureCounters != want {
			t.Fatalf("failureCounters = %+v, want %+v", got.Status.FailureCounters, want)
		}
	})

	t.Run("ModulePackage panics in apply", func(t *testing.T) {
		pkg := operatorPackage(&releasesv1alpha1.FailureCounters{Apply: 3, Prune: 1})
		pkg.Status.LastAttemptedAt = &staleAttempt
		pkg.Status.NextRetryAt = &pendingRetry
		c := loopTestClient(t, readyOCIRepository(), pkg)
		params := &ModulePackageParams{
			Client:        c,
			EventRecorder: events.NewFakeRecorder(32),
			Fetcher:       instanceFileFetcher{},
			Renderer: packageRendererFunc(func() (string, *render.RenderResult, error) {
				return render.KindModuleInstance, configMapRenderResult(t), nil
			}),
		}

		expectNilReceiverPanic(t, func() { _, _ = ReconcileModulePackage(context.Background(), params, loopTestRequest) })

		var got releasesv1alpha1.ModulePackage
		if err := c.Get(context.Background(), loopTestRequest.NamespacedName, &got); err != nil {
			t.Fatalf("get: %v", err)
		}
		expectPanicStatus(t, got.Status.Conditions)
		expectPanicAttempt(t, got.Status.LastAttemptedAction, got.Status.LastAttemptedAt, got.Status.NextRetryAt)
		want := releasesv1alpha1.FailureCounters{Reconcile: 1, Apply: 3, Prune: 1}
		if got.Status.FailureCounters == nil || *got.Status.FailureCounters != want {
			t.Fatalf("failureCounters = %+v, want %+v", got.Status.FailureCounters, want)
		}
	})
}
