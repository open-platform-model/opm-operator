package reconcile

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/fluxcd/pkg/runtime/conditions"
	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/open-platform-model/library/opm/k8s/labels"
	"github.com/open-platform-model/library/opm/k8s/lifecycle"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	"github.com/open-platform-model/opm-operator/internal/status"
)

// cleanupFixture describes one deleting object for the cleanup tests of both
// kinds.
type cleanupFixture struct {
	prune      bool
	sa         string
	annotation string
	dataPolicy releasesv1alpha1.DataPolicy
	entries    []releasesv1alpha1.InventoryEntry
	conditions []metav1.Condition
}

// cleanupKind runs the deletion cleanup of one kind, so every test of the
// cleanup covers ModuleInstance and ModulePackage with one body.
type cleanupKind struct {
	name   string
	object func(cleanupFixture) conditions.Setter
	run    func(context.Context, cleanupEnv, client.Object) (ctrl.Result, error)
}

// cleanupEnv is the injected part of the reconciler params of both kinds.
type cleanupEnv struct {
	client     client.Client
	apiReader  client.Reader
	restConfig *rest.Config
	recorder   events.EventRecorder
	wait       DeletionWait
}

const cleanupUUID = "11111111-1111-1111-1111-111111111111"

var cleanupKinds = []cleanupKind{
	{
		name: "ModuleInstance",
		object: func(f cleanupFixture) conditions.Setter {
			mi := deletingMR(f.sa, f.annotation)
			mi.Spec.Prune, mi.Spec.DataPolicy = f.prune, f.dataPolicy
			mi.Status.InstanceUUID = cleanupUUID
			mi.Status.Inventory.Entries = f.entries
			mi.Status.Conditions = f.conditions
			return mi
		},
		run: func(ctx context.Context, env cleanupEnv, obj client.Object) (ctrl.Result, error) {
			return handleDeletion(ctx, &ModuleInstanceParams{
				Client: env.client, APIReader: env.apiReader, RestConfig: env.restConfig, EventRecorder: env.recorder,
				DeletionWait: env.wait,
			}, obj.(*releasesv1alpha1.ModuleInstance))
		},
	},
	{
		name: "ModulePackage",
		object: func(f cleanupFixture) conditions.Setter {
			pkg := deletingModulePackage(f.sa, f.annotation)
			pkg.Spec.Prune, pkg.Spec.DataPolicy = f.prune, f.dataPolicy
			pkg.Status.InstanceUUID = cleanupUUID
			pkg.Status.Inventory.Entries = f.entries
			pkg.Status.Conditions = f.conditions
			return pkg
		},
		run: func(ctx context.Context, env cleanupEnv, obj client.Object) (ctrl.Result, error) {
			return handleModulePackageDeletion(ctx, &ModulePackageParams{
				Client: env.client, APIReader: env.apiReader, RestConfig: env.restConfig, EventRecorder: env.recorder,
				DeletionWait: env.wait,
			}, obj.(*releasesv1alpha1.ModulePackage))
		},
	},
}

var stubRestConfig = &rest.Config{Host: "https://localhost:6443"}

func configMapEntry(name string) releasesv1alpha1.InventoryEntry {
	return releasesv1alpha1.InventoryEntry{Version: "v1", Kind: "ConfigMap", Namespace: deletionTestNamespace, Name: name}
}

func claimEntry(name string) releasesv1alpha1.InventoryEntry {
	return releasesv1alpha1.InventoryEntry{Version: "v1", Kind: "PersistentVolumeClaim", Namespace: deletionTestNamespace, Name: name}
}

// ownedMeta is the metadata of an object the fixture instance owns.
func ownedMeta(name string, finalizers ...string) metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Name: name, Namespace: deletionTestNamespace, Finalizers: finalizers,
		Labels: map[string]string{
			labels.ManagedBy:          labels.ManagedByController,
			labels.ModuleInstanceUUID: cleanupUUID,
		},
	}
}

func ownedConfigMap(name string, finalizers ...string) *corev1.ConfigMap {
	return &corev1.ConfigMap{ObjectMeta: ownedMeta(name, finalizers...)}
}

func ownedClaim(name string) *corev1.PersistentVolumeClaim {
	return &corev1.PersistentVolumeClaim{ObjectMeta: ownedMeta(name)}
}

// requestLog counts the reads and deletes of inventory objects (every kind
// but the deleting object's own and ServiceAccount) that reach a client.
type requestLog struct {
	mu      sync.Mutex
	reads   []string
	deletes []string

	// reviews names each SelfSubjectAccessReview the cleanup asked, as
	// "verb resource namespace/name". The answer is allowed unless
	// denyImpersonate is set; reviewErr fails the request.
	reviews         []string
	denyImpersonate bool
	reviewErr       error
}

func isInventoryObject(obj client.Object) bool {
	switch obj.(type) {
	case *releasesv1alpha1.ModuleInstance, *releasesv1alpha1.ModulePackage, *corev1.ServiceAccount:
		return false
	}
	return true
}

// logged wraps c so the log sees every read and delete of an inventory
// object. funcs may fail a request before it is sent.
func (l *requestLog) logged(c client.WithWatch, getErr, deleteErr func(client.Object) error) client.WithWatch {
	return interceptor.NewClient(c, interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			if isInventoryObject(obj) {
				l.mu.Lock()
				l.reads = append(l.reads, obj.GetObjectKind().GroupVersionKind().Kind+"/"+key.Name)
				l.mu.Unlock()
				if getErr != nil {
					if err := getErr(withKey(obj, key)); err != nil {
						return err
					}
				}
			}
			return c.Get(ctx, key, obj, opts...)
		},
		Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
			review, ok := obj.(*authorizationv1.SelfSubjectAccessReview)
			if !ok {
				return c.Create(ctx, obj, opts...)
			}
			// The API server answers a review in the response; the fake
			// client stores nothing useful, so the test answers.
			attrs := review.Spec.ResourceAttributes
			l.mu.Lock()
			defer l.mu.Unlock()
			l.reviews = append(l.reviews, attrs.Verb+" "+attrs.Resource+" "+attrs.Namespace+"/"+attrs.Name)
			if l.reviewErr != nil {
				return l.reviewErr
			}
			review.Status.Allowed = !l.denyImpersonate
			return nil
		},
		Delete: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
			if isInventoryObject(obj) {
				l.mu.Lock()
				l.deletes = append(l.deletes, obj.GetObjectKind().GroupVersionKind().Kind+"/"+obj.GetName())
				l.mu.Unlock()
				if deleteErr != nil {
					if err := deleteErr(obj); err != nil {
						return err
					}
				}
			}
			return c.Delete(ctx, obj, opts...)
		},
	})
}

func withKey(obj client.Object, key client.ObjectKey) client.Object {
	named := obj.DeepCopyObject().(client.Object)
	named.SetName(key.Name)
	named.SetNamespace(key.Namespace)
	return named
}

func forbidden(obj client.Object) error {
	return apierrors.NewForbidden(schema.GroupResource{Resource: "objects"}, obj.GetName(), errors.New("denied"))
}

func serverError(client.Object) error { return apierrors.NewInternalError(errors.New("injected")) }

// cleanupRun is one deletion cleanup under test.
type cleanupRun struct {
	t    *testing.T
	kind cleanupKind
	base client.WithWatch
	log  *requestLog
	env  cleanupEnv
	rec  *events.FakeRecorder
	key  client.ObjectKey
	// proto is an empty object of the kind, for reads.
	proto conditions.Setter
}

// newCleanupRun stores the fixture object and objs in a fake client whose
// inventory reads and deletes are logged and can be failed.
func newCleanupRun(
	t *testing.T, kind cleanupKind, f cleanupFixture, getErr, deleteErr func(client.Object) error, objs ...client.Object,
) *cleanupRun {
	t.Helper()
	obj := kind.object(f)
	base := fake.NewClientBuilder().
		WithScheme(deletionTestScheme(t)).
		WithObjects(append([]client.Object{obj}, objs...)...).
		WithStatusSubresource(&releasesv1alpha1.ModuleInstance{}, &releasesv1alpha1.ModulePackage{}).
		Build()
	log := &requestLog{}
	c := log.logged(base, getErr, deleteErr)
	rec := events.NewFakeRecorder(32)
	return &cleanupRun{
		t: t, kind: kind, base: base, log: log, rec: rec,
		env:   cleanupEnv{client: c, apiReader: c, recorder: rec},
		key:   client.ObjectKeyFromObject(obj),
		proto: kind.object(cleanupFixture{}),
	}
}

// reconcile reads the object as a reconcile does and runs its cleanup.
func (r *cleanupRun) reconcile() (ctrl.Result, error) {
	r.t.Helper()
	obj := r.proto.DeepCopyObject().(conditions.Setter)
	if err := r.base.Get(context.Background(), r.key, obj); err != nil {
		r.t.Fatalf("reading the deleting object: %v", err)
	}
	return r.kind.run(context.Background(), r.env, obj)
}

// released reports whether the finalizer was removed: the fake client then
// deletes the object.
func (r *cleanupRun) released() bool {
	r.t.Helper()
	err := r.base.Get(context.Background(), r.key, r.proto.DeepCopyObject().(client.Object))
	if err != nil && !apierrors.IsNotFound(err) {
		r.t.Fatalf("reading the deleting object: %v", err)
	}
	return apierrors.IsNotFound(err)
}

// ready returns the Ready condition of the stored object; nil when the
// object is gone or has none.
func (r *cleanupRun) ready() *metav1.Condition {
	r.t.Helper()
	obj := r.proto.DeepCopyObject().(conditions.Setter)
	if err := r.base.Get(context.Background(), r.key, obj); err != nil {
		return nil
	}
	return apimeta.FindStatusCondition(obj.GetConditions(), status.ReadyCondition)
}

func (r *cleanupRun) readyReason() string {
	if ready := r.ready(); ready != nil {
		return ready.Reason
	}
	return ""
}

func (r *cleanupRun) exists(obj client.Object) bool {
	r.t.Helper()
	err := r.base.Get(context.Background(), client.ObjectKeyFromObject(obj), obj.DeepCopyObject().(client.Object))
	return err == nil
}

func forEachCleanupKind(t *testing.T, test func(t *testing.T, kind cleanupKind)) {
	t.Helper()
	for _, kind := range cleanupKinds {
		t.Run(kind.name, func(t *testing.T) { test(t, kind) })
	}
}

// TestCleanupFollowsTheHoldVerdict pairs every reason of the library's hold
// verdict with what the operator does for it: the finalizer, the Ready
// reason, the event and the requeue. One row per lifecycle.HoldReason.
func TestCleanupFollowsTheHoldVerdict(t *testing.T) {
	saGone := func(r *cleanupRun) { r.env.restConfig = stubRestConfig }
	rows := []struct {
		reason      lifecycle.HoldReason
		fixture     cleanupFixture
		getErr      func(client.Object) error
		deleteErr   func(client.Object) error
		setup       func(*cleanupRun)
		released    bool
		readyReason string
		event       string
		requeue     bool
		wantErr     bool
		reads       int
		deletes     int
	}{
		{
			reason:   lifecycle.HoldPruneDisabled,
			fixture:  cleanupFixture{prune: false, entries: []releasesv1alpha1.InventoryEntry{configMapEntry("cm")}},
			released: true,
		},
		{
			reason:   lifecycle.HoldInventoryEmpty,
			fixture:  cleanupFixture{prune: true},
			released: true,
		},
		{
			reason:   lifecycle.HoldCleanupComplete,
			fixture:  cleanupFixture{prune: true, entries: []releasesv1alpha1.InventoryEntry{configMapEntry("cm")}},
			released: true, reads: 2, deletes: 1,
		},
		{
			reason: lifecycle.HoldForceOrphan,
			fixture: cleanupFixture{prune: true, sa: "gone", annotation: "true",
				entries: []releasesv1alpha1.InventoryEntry{configMapEntry("cm")}},
			setup: saGone, released: true, event: status.OrphanedOnDeletionReason,
		},
		{
			reason:    lifecycle.HoldCleanupIncomplete,
			fixture:   cleanupFixture{prune: true, entries: []releasesv1alpha1.InventoryEntry{configMapEntry("cm")}},
			deleteErr: serverError, wantErr: true, reads: 1, deletes: 1,
		},
		{
			reason:  lifecycle.HoldCleanupForbidden,
			fixture: cleanupFixture{prune: true, sa: "deploy-sa", entries: []releasesv1alpha1.InventoryEntry{configMapEntry("cm")}},
			getErr:  forbidden, readyReason: status.ImpersonationFailedReason, event: status.ImpersonationFailedReason,
			requeue: true, reads: 1,
		},
		{
			reason:  lifecycle.HoldIdentityUnavailable,
			fixture: cleanupFixture{prune: true, sa: "gone", entries: []releasesv1alpha1.InventoryEntry{configMapEntry("cm")}},
			setup:   saGone, readyReason: status.DeletionSAMissingReason, event: status.DeletionSAMissingReason, requeue: true,
		},
	}
	if len(rows) != 7 {
		t.Fatalf("the table has %d rows, the hold verdict has 7 reasons", len(rows))
	}
	for _, row := range rows {
		forEachCleanupKind(t, func(t *testing.T, kind cleanupKind) {
			t.Run(string(row.reason), func(t *testing.T) {
				run := newCleanupRun(t, kind, row.fixture, row.getErr, row.deleteErr, ownedConfigMap("cm"))
				if row.setup != nil {
					row.setup(run)
				}
				result, err := run.reconcile()
				if (err != nil) != row.wantErr {
					t.Fatalf("error = %v, want an error: %t", err, row.wantErr)
				}
				if got := run.released(); got != row.released {
					t.Errorf("finalizer removed = %t, want %t", got, row.released)
				}
				if got := run.readyReason(); got != row.readyReason {
					t.Errorf("Ready reason = %q, want %q", got, row.readyReason)
				}
				evs := drainEvents(run.rec)
				if row.event == "" && len(evs) != 0 {
					t.Errorf("events = %v, want none", evs)
				}
				if row.event != "" && (len(evs) != 1 || countEventsWithReason(evs, row.event) != 1) {
					t.Errorf("events = %v, want one %s", evs, row.event)
				}
				wantRequeue := ctrl.Result{}
				if row.requeue {
					wantRequeue.RequeueAfter = StalledRecheckInterval
				}
				if result != wantRequeue {
					t.Errorf("result = %+v, want %+v", result, wantRequeue)
				}
				if len(run.log.reads) != row.reads || len(run.log.deletes) != row.deletes {
					t.Errorf("reads = %v, deletes = %v; want %d and %d",
						run.log.reads, run.log.deletes, row.reads, row.deletes)
				}
			})
		})
	}
}

// TestCleanupWithoutIdentityTouchesNothing: with a missing or a failed
// identity and no record that every delete was sent, no inventory object is
// read or deleted, also not with the controller's own client.
func TestCleanupWithoutIdentityTouchesNothing(t *testing.T) {
	cases := []struct {
		name   string
		saErr  error
		reason string
	}{
		{"missing", nil, status.DeletionSAMissingReason},
		{"failed", apierrors.NewInternalError(errors.New("injected")), status.ImpersonationFailedReason},
	}
	for _, tc := range cases {
		forEachCleanupKind(t, func(t *testing.T, kind cleanupKind) {
			t.Run(tc.name, func(t *testing.T) {
				cm := ownedConfigMap("cm")
				run := newCleanupRun(t, kind, cleanupFixture{prune: true, sa: "deploy-sa",
					entries: []releasesv1alpha1.InventoryEntry{configMapEntry("cm")}}, nil, nil, cm)
				run.env.restConfig = stubRestConfig
				if tc.saErr != nil {
					run.env.apiReader = interceptor.NewClient(run.base, interceptor.Funcs{
						Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
							if _, ok := obj.(*corev1.ServiceAccount); ok {
								return tc.saErr
							}
							return c.Get(ctx, key, obj, opts...)
						},
					})
				}
				result, err := run.reconcile()
				if err != nil {
					t.Fatalf("reconcile: %v", err)
				}
				if result.RequeueAfter != StalledRecheckInterval {
					t.Errorf("RequeueAfter = %v, want the stalled recheck", result.RequeueAfter)
				}
				if run.released() {
					t.Error("the finalizer was removed")
				}
				if got := run.readyReason(); got != tc.reason {
					t.Errorf("Ready reason = %q, want %q", got, tc.reason)
				}
				if len(run.log.reads) != 0 || len(run.log.deletes) != 0 {
					t.Errorf("reads = %v, deletes = %v; want none", run.log.reads, run.log.deletes)
				}
				if !run.exists(cm) {
					t.Error("the ConfigMap was deleted")
				}
			})
		})
	}
}

// TestForceOrphanDoesNotLiftAFailedStep: the annotation releases a missing
// ServiceAccount only. With the identity available and a failed delete the
// finalizer stays.
func TestForceOrphanDoesNotLiftAFailedStep(t *testing.T) {
	forEachCleanupKind(t, func(t *testing.T, kind cleanupKind) {
		run := newCleanupRun(t, kind, cleanupFixture{prune: true, annotation: "true",
			entries: []releasesv1alpha1.InventoryEntry{configMapEntry("a"), configMapEntry("b")}},
			nil, func(obj client.Object) error {
				if obj.GetName() == "a" {
					return serverError(obj)
				}
				return nil
			}, ownedConfigMap("a"), ownedConfigMap("b"))
		if _, err := run.reconcile(); err == nil {
			t.Fatal("reconcile returned no error for a failed delete")
		}
		if run.released() {
			t.Error("the finalizer was removed although a step failed")
		}
		if len(run.log.deletes) != 2 {
			t.Errorf("deletes = %v, want both entries attempted", run.log.deletes)
		}
		if evs := drainEvents(run.rec); countEventsWithReason(evs, status.OrphanedOnDeletionReason) != 0 {
			t.Errorf("events = %v, want no OrphanedOnDeletion", evs)
		}
	})
}

// TestCleanupOfKeptClaimsOnly: an inventory that holds only claims the data
// policy keeps is empty for the plan. With the ServiceAccount missing the
// finalizer goes without a read; with spec.dataPolicy Delete the claims are
// plan entries and the same object stalls.
func TestCleanupOfKeptClaimsOnly(t *testing.T) {
	entries := []releasesv1alpha1.InventoryEntry{claimEntry("data"), claimEntry("cache")}
	forEachCleanupKind(t, func(t *testing.T, kind cleanupKind) {
		t.Run("kept claims are released without a read", func(t *testing.T) {
			data, cache := ownedClaim("data"), ownedClaim("cache")
			run := newCleanupRun(t, kind, cleanupFixture{prune: true, sa: "gone", entries: entries}, nil, nil, data, cache)
			run.env.restConfig = stubRestConfig
			result, err := run.reconcile()
			if err != nil || result != (ctrl.Result{}) {
				t.Fatalf("reconcile = %+v, %v; want a plain success", result, err)
			}
			if !run.released() {
				t.Errorf("the finalizer stays; Ready reason %q", run.readyReason())
			}
			if len(run.log.reads) != 0 || len(run.log.deletes) != 0 {
				t.Errorf("reads = %v, deletes = %v; want none", run.log.reads, run.log.deletes)
			}
			if !run.exists(data) || !run.exists(cache) {
				t.Error("a claim was deleted")
			}
			evs := drainEvents(run.rec)
			if len(evs) != 1 || countEventsWithReason(evs, status.DeletionUnconfirmedReason) != 1 {
				t.Fatalf("events = %v, want one DeletionUnconfirmed", evs)
			}
			for _, want := range []string{"Warning", "2 PersistentVolumeClaim(s)", `"team-a/gone" is missing`, "may still exist"} {
				if !strings.Contains(evs[0], want) {
					t.Errorf("event %q does not contain %q", evs[0], want)
				}
			}
			if countEventsWithReason(evs, status.ClaimsKeptReason) != 0 || countEventsWithReason(evs, status.DeletionSAMissingReason) != 0 {
				t.Errorf("events = %v, want no ClaimsKept and no DeletionSAMissing", evs)
			}
		})

		t.Run("claims the instance may delete need the ServiceAccount", func(t *testing.T) {
			run := newCleanupRun(t, kind, cleanupFixture{prune: true, sa: "gone", entries: entries,
				dataPolicy: releasesv1alpha1.DataPolicyDelete}, nil, nil, ownedClaim("data"), ownedClaim("cache"))
			run.env.restConfig = stubRestConfig
			if _, err := run.reconcile(); err != nil {
				t.Fatalf("reconcile: %v", err)
			}
			if run.released() {
				t.Error("the finalizer was removed")
			}
			if got := run.readyReason(); got != status.DeletionSAMissingReason {
				t.Errorf("Ready reason = %q, want DeletionSAMissing", got)
			}
		})

		t.Run("with the identity available the claims are read and reported", func(t *testing.T) {
			run := newCleanupRun(t, kind, cleanupFixture{prune: true, entries: entries}, nil, nil,
				ownedClaim("data"), ownedClaim("cache"))
			if _, err := run.reconcile(); err != nil {
				t.Fatalf("reconcile: %v", err)
			}
			if !run.released() {
				t.Error("the finalizer stays")
			}
			evs := drainEvents(run.rec)
			if len(evs) != 1 || countEventsWithReason(evs, status.ClaimsKeptReason) != 1 {
				t.Errorf("events = %v, want one ClaimsKept", evs)
			}
			if len(run.log.reads) != 2 || len(run.log.deletes) != 0 {
				t.Errorf("reads = %v, deletes = %v; want 2 and 0", run.log.reads, run.log.deletes)
			}
		})
	})
}
