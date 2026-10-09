package reconcile

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/fluxcd/pkg/runtime/conditions"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/open-platform-model/library/opm/k8s/labels"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	"github.com/open-platform-model/opm-operator/internal/status"
)

// holdFinalizer keeps an object terminating in the fake client after its
// delete, as a dependent that has not gone keeps an object in a cluster.
const holdFinalizer = "example.com/hold"

func TestRecheckInterval(t *testing.T) {
	w := DeletionWait{}.withDefaults()
	if w.BlockedAfter != 10*time.Minute || w.MinRecheck != time.Second || w.MaxRecheck != 60*time.Second || w.Now == nil {
		t.Fatalf("defaults = %+v", w)
	}
	for age, want := range map[time.Duration]time.Duration{
		0:                time.Second,
		3 * time.Second:  time.Second,
		20 * time.Second: 5 * time.Second,
		2 * time.Minute:  30 * time.Second,
		4 * time.Minute:  60 * time.Second,
		11 * time.Minute: 60 * time.Second,
	} {
		if got := w.recheckAfter(age); got != want {
			t.Errorf("recheckAfter(%s) = %s, want %s", age, got, want)
		}
	}
}

// release lets a held object go: the fake client deletes it when its last
// finalizer is removed.
func (r *cleanupRun) release(obj client.Object) {
	r.t.Helper()
	live := obj.DeepCopyObject().(client.Object)
	if err := r.base.Get(context.Background(), client.ObjectKeyFromObject(obj), live); err != nil {
		r.t.Fatalf("reading %s: %v", obj.GetName(), err)
	}
	live.SetFinalizers(nil)
	if err := r.base.Update(context.Background(), live); err != nil {
		r.t.Fatalf("releasing %s: %v", obj.GetName(), err)
	}
}

func (r *cleanupRun) condition(kind string) *metav1.Condition {
	r.t.Helper()
	obj := r.proto.DeepCopyObject().(conditions.Setter)
	if err := r.base.Get(context.Background(), r.key, obj); err != nil {
		return nil
	}
	return apimeta.FindStatusCondition(obj.GetConditions(), kind)
}

func (r *cleanupRun) mustReconcile() ctrl.Result {
	r.t.Helper()
	result, err := r.reconcile()
	if err != nil {
		r.t.Fatalf("reconcile: %v", err)
	}
	return result
}

func waitRecord(reason string) []metav1.Condition {
	return []metav1.Condition{{
		Type: status.ReadyCondition, Status: metav1.ConditionFalse, Reason: reason,
		Message: "waiting", LastTransitionTime: metav1.Now(),
	}}
}

// TestCleanupWaitsUntilDeletedObjectsAreGone: after a release verdict the
// finalizer stays while a deleted object still exists. The reconcile marks
// DeletionInProgress, asks for a requeue, and emits its event once. Each
// recheck judges the entry again and names its delete again.
func TestCleanupWaitsUntilDeletedObjectsAreGone(t *testing.T) {
	forEachCleanupKind(t, func(t *testing.T, kind cleanupKind) {
		held := ownedConfigMap("held", holdFinalizer)
		run := newCleanupRun(t, kind, cleanupFixture{prune: true,
			entries: []releasesv1alpha1.InventoryEntry{configMapEntry("held"), configMapEntry("plain")}},
			nil, nil, held, ownedConfigMap("plain"))

		for i := range 3 {
			result := run.mustReconcile()
			if result.RequeueAfter != time.Second {
				t.Fatalf("reconcile %d: RequeueAfter = %s, want 1s", i, result.RequeueAfter)
			}
			if run.released() {
				t.Fatalf("reconcile %d: the finalizer was removed while an object terminates", i)
			}
		}
		ready := run.ready()
		if ready == nil || ready.Status != metav1.ConditionFalse || ready.Reason != status.DeletionInProgressReason {
			t.Fatalf("Ready = %+v, want False with DeletionInProgress", ready)
		}
		if !strings.Contains(ready.Message, "ConfigMap/team-a/held (finalizers: "+holdFinalizer+")") ||
			strings.Contains(ready.Message, "plain") {
			t.Errorf("the message must name the held object only: %s", ready.Message)
		}
		if c := run.condition(status.ReconcilingCondition); c == nil || c.Status != metav1.ConditionTrue {
			t.Errorf("Reconciling = %+v, want True", c)
		}
		if c := run.condition(status.StalledCondition); c != nil && c.Status == metav1.ConditionTrue {
			t.Errorf("Stalled = %+v, want absent or False", c)
		}
		evs := drainEvents(run.rec)
		if len(evs) != 1 || !strings.HasPrefix(evs[0], "Normal "+status.DeletionInProgressReason) ||
			!strings.Contains(evs[0], ready.Message) {
			t.Errorf("events = %v, want one Normal DeletionInProgress with the condition message", evs)
		}
		// held is read and deleted again at each recheck; plain was deleted
		// once and is read as absent afterwards.
		if got := strings.Join(run.log.deletes, " "); got != "ConfigMap/held ConfigMap/plain ConfigMap/held ConfigMap/held" {
			t.Errorf("deletes = %q", got)
		}

		run.release(held)
		if result := run.mustReconcile(); result != (ctrl.Result{}) {
			t.Errorf("result = %+v after the object went, want none", result)
		}
		if !run.released() {
			t.Error("the finalizer stays although every deleted object is gone")
		}
		if evs := drainEvents(run.rec); len(evs) != 0 {
			t.Errorf("events = %v, want none for a confirmed deletion", evs)
		}
	})
}

// TestCleanupReportsABlockedDeletion: once the oldest deleted object has
// been terminating for BlockedAfter by the controller's clock the deletion
// is DeletionBlocked. The clock is injected; the test does not sleep.
func TestCleanupReportsABlockedDeletion(t *testing.T) {
	forEachCleanupKind(t, func(t *testing.T, kind cleanupKind) {
		held := ownedConfigMap("held", holdFinalizer)
		run := newCleanupRun(t, kind, cleanupFixture{prune: true,
			entries: []releasesv1alpha1.InventoryEntry{configMapEntry("held")}}, nil, nil, held)

		if result := run.mustReconcile(); result.RequeueAfter != time.Second {
			t.Fatalf("RequeueAfter = %s, want 1s", result.RequeueAfter)
		}
		drainEvents(run.rec)

		run.env.wait.Now = func() time.Time { return time.Now().Add(11 * time.Minute) }
		for range 2 {
			if result := run.mustReconcile(); result.RequeueAfter != 60*time.Second {
				t.Fatalf("RequeueAfter = %s, want 60s", result.RequeueAfter)
			}
		}
		ready := run.ready()
		if ready == nil || ready.Status != metav1.ConditionFalse || ready.Reason != status.DeletionBlockedReason {
			t.Fatalf("Ready = %+v, want False with DeletionBlocked", ready)
		}
		for _, want := range []string{"ConfigMap/team-a/held (finalizers: " + holdFinalizer + ")", "spec.prune=false"} {
			if !strings.Contains(ready.Message, want) {
				t.Errorf("the message does not contain %q: %s", want, ready.Message)
			}
		}
		if c := run.condition(status.StalledCondition); c == nil || c.Status != metav1.ConditionTrue {
			t.Errorf("Stalled = %+v, want True", c)
		}
		evs := drainEvents(run.rec)
		if len(evs) != 1 || !strings.HasPrefix(evs[0], "Warning "+status.DeletionBlockedReason) || !strings.Contains(evs[0], "held") {
			t.Errorf("events = %v, want one Warning DeletionBlocked naming the object", evs)
		}
		if run.released() {
			t.Fatal("a blocked deletion gave up its finalizer")
		}

		run.release(held)
		run.mustReconcile()
		if !run.released() {
			t.Error("the finalizer stays after the blocking object went")
		}
	})
}

// A deletion whose object was terminating for longer than the threshold
// before the first reconcile is blocked at once, with no DeletionInProgress.
func TestCleanupBlockedAtTheFirstReconcile(t *testing.T) {
	forEachCleanupKind(t, func(t *testing.T, kind cleanupKind) {
		run := newCleanupRun(t, kind, cleanupFixture{prune: true,
			entries: []releasesv1alpha1.InventoryEntry{configMapEntry("held")}}, nil, nil,
			ownedConfigMap("held", holdFinalizer))
		run.env.wait.Now = func() time.Time { return time.Now().Add(11 * time.Minute) }
		run.mustReconcile()
		if got := run.readyReason(); got != status.DeletionBlockedReason {
			t.Errorf("Ready reason = %q, want DeletionBlocked", got)
		}
		evs := drainEvents(run.rec)
		if len(evs) != 1 || countEventsWithReason(evs, status.DeletionBlockedReason) != 1 {
			t.Errorf("events = %v, want the DeletionBlocked event only", evs)
		}
	})
}

// A name that a new object holds counts as gone, and the new object is not
// deleted.
func TestCleanupDoesNotWaitForANameTakenByANewObject(t *testing.T) {
	forEachCleanupKind(t, func(t *testing.T, kind cleanupKind) {
		old := ownedConfigMap("cm")
		old.UID = types.UID("uid-old")
		run := newCleanupRun(t, kind, cleanupFixture{prune: true,
			entries: []releasesv1alpha1.InventoryEntry{configMapEntry("cm")}}, nil, nil, old)
		// The object is created again right after its delete was accepted.
		inner := run.env.client.(client.WithWatch)
		run.env.client = interceptor.NewClient(inner, interceptor.Funcs{
			Delete: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
				if err := c.Delete(ctx, obj, opts...); err != nil {
					return err
				}
				again := ownedConfigMap("cm", holdFinalizer)
				again.UID = types.UID("uid-new")
				return run.base.Create(ctx, again)
			},
		})
		if result := run.mustReconcile(); result != (ctrl.Result{}) {
			t.Errorf("result = %+v, want no requeue", result)
		}
		if !run.released() {
			t.Error("the finalizer waits for an object that is not the one deleted")
		}
		var live corev1.ConfigMap
		if err := run.base.Get(context.Background(), client.ObjectKeyFromObject(old), &live); err != nil {
			t.Fatalf("the new object: %v", err)
		}
		if live.UID != "uid-new" || !live.DeletionTimestamp.IsZero() {
			t.Errorf("the new object was touched: uid %q, deletionTimestamp %v", live.UID, live.DeletionTimestamp)
		}
		if len(run.log.deletes) != 1 {
			t.Errorf("deletes = %v, want one", run.log.deletes)
		}
	})
}

// TestCleanupWaitsOnlyForDeletedObjects: a kept claim, an object left behind
// and an entry that is already absent are no deleted step, so they do not
// hold the finalizer.
func TestCleanupWaitsOnlyForDeletedObjects(t *testing.T) {
	forEachCleanupKind(t, func(t *testing.T, kind cleanupKind) {
		claim := ownedClaim("data")
		claim.Finalizers = []string{"kubernetes.io/pvc-protection"}
		other := ownedConfigMap("other", holdFinalizer)
		other.Labels[labels.ModuleInstanceUUID] = "22222222-2222-2222-2222-222222222222"
		run := newCleanupRun(t, kind, cleanupFixture{prune: true, entries: []releasesv1alpha1.InventoryEntry{
			claimEntry("data"), configMapEntry("other"), configMapEntry("absent"), configMapEntry("own"),
		}}, nil, nil, claim, other, ownedConfigMap("own"))

		if result := run.mustReconcile(); result != (ctrl.Result{}) {
			t.Errorf("result = %+v, want no requeue", result)
		}
		if !run.released() {
			t.Errorf("the finalizer waits; Ready reason %q", run.readyReason())
		}
		if !run.exists(claim) || !run.exists(other) {
			t.Error("a kept claim or another instance's object was deleted")
		}
		evs := drainEvents(run.rec)
		if countEventsWithReason(evs, status.ClaimsKeptReason) != 1 || countEventsWithReason(evs, status.LeftBehindReason) != 1 || len(evs) != 2 {
			t.Errorf("events = %v, want one ClaimsKept and one LeftBehind", evs)
		}
	})
}

// TestCleanupReportsOncePerDeletion: ClaimsKept and LeftBehind are emitted by
// the first reconcile that reaches a release verdict, and not again by the
// rechecks of the same wait.
func TestCleanupReportsOncePerDeletion(t *testing.T) {
	forEachCleanupKind(t, func(t *testing.T, kind cleanupKind) {
		held := ownedConfigMap("held", holdFinalizer)
		namespace := releasesv1alpha1.InventoryEntry{Version: "v1", Kind: "Namespace", Name: "team-a"}
		run := newCleanupRun(t, kind, cleanupFixture{prune: true, entries: []releasesv1alpha1.InventoryEntry{
			claimEntry("data"), namespace, configMapEntry("held"),
		}}, nil, nil, ownedClaim("data"), held)

		for range 3 {
			run.mustReconcile()
		}
		run.release(held)
		run.mustReconcile()
		if !run.released() {
			t.Fatal("the deletion did not complete")
		}
		evs := drainEvents(run.rec)
		for _, reason := range []string{status.ClaimsKeptReason, status.LeftBehindReason, status.DeletionInProgressReason} {
			if got := countEventsWithReason(evs, reason); got != 1 {
				t.Errorf("%d %s events, want exactly one; events = %v", got, reason, evs)
			}
		}
	})
}

// Setting spec.prune to false releases a waiting deletion at the next
// reconcile and deletes nothing more.
func TestTurningPruneOffReleasesAWaitingDeletion(t *testing.T) {
	forEachCleanupKind(t, func(t *testing.T, kind cleanupKind) {
		held := ownedConfigMap("held", holdFinalizer)
		run := newCleanupRun(t, kind, cleanupFixture{prune: true,
			entries: []releasesv1alpha1.InventoryEntry{configMapEntry("held")}}, nil, nil, held)
		run.mustReconcile()
		if run.readyReason() != status.DeletionInProgressReason {
			t.Fatalf("Ready reason = %q, want DeletionInProgress", run.readyReason())
		}
		reads, deletes := len(run.log.reads), len(run.log.deletes)

		obj := run.proto.DeepCopyObject().(client.Object)
		if err := run.base.Get(context.Background(), run.key, obj); err != nil {
			t.Fatal(err)
		}
		switch o := obj.(type) {
		case *releasesv1alpha1.ModuleInstance:
			o.Spec.Prune = false
		case *releasesv1alpha1.ModulePackage:
			o.Spec.Prune = false
		}
		if err := run.base.Update(context.Background(), obj); err != nil {
			t.Fatal(err)
		}

		if result := run.mustReconcile(); result != (ctrl.Result{}) {
			t.Errorf("result = %+v, want no requeue", result)
		}
		if !run.released() {
			t.Error("the finalizer stays with spec.prune=false")
		}
		if len(run.log.reads) != reads || len(run.log.deletes) != deletes {
			t.Errorf("the release read or deleted: reads %v, deletes %v", run.log.reads, run.log.deletes)
		}
		if !run.exists(held) {
			t.Error("the terminating object must be left as it is")
		}
	})
}

// TestRecordReleasesALostIdentity: with the record that every delete was
// sent, a deleting identity that is missing or cannot be impersonated no
// longer holds the finalizer. Nothing is read or deleted, and the event says
// what could not be confirmed.
func TestRecordReleasesALostIdentity(t *testing.T) {
	entries := []releasesv1alpha1.InventoryEntry{
		configMapEntry("a"), configMapEntry("b"), configMapEntry("c"),
		{Version: "v1", Kind: "Namespace", Name: "team-a"},
	}
	for _, reason := range []string{status.DeletionInProgressReason, status.DeletionBlockedReason} {
		forEachCleanupKind(t, func(t *testing.T, kind cleanupKind) {
			t.Run(reason, func(t *testing.T) {
				held := ownedConfigMap("a", holdFinalizer)
				run := newCleanupRun(t, kind, cleanupFixture{prune: true, sa: "gone", entries: entries,
					conditions: waitRecord(reason)}, nil, nil, held)
				run.env.restConfig = stubRestConfig

				if result := run.mustReconcile(); result != (ctrl.Result{}) {
					t.Errorf("result = %+v, want no requeue", result)
				}
				if !run.released() {
					t.Fatalf("the finalizer stays; Ready reason %q", run.readyReason())
				}
				if len(run.log.reads) != 0 || len(run.log.deletes) != 0 {
					t.Errorf("reads = %v, deletes = %v; want none", run.log.reads, run.log.deletes)
				}
				evs := drainEvents(run.rec)
				if len(evs) != 1 || !strings.HasPrefix(evs[0], "Warning "+status.DeletionUnconfirmedReason) {
					t.Fatalf("events = %v, want one Warning DeletionUnconfirmed", evs)
				}
				for _, want := range []string{"3 object(s)", `ServiceAccount "team-a/gone" is missing`, "may still exist"} {
					if !strings.Contains(evs[0], want) {
						t.Errorf("event %q does not contain %q", evs[0], want)
					}
				}
			})
		})
	}
}

// Without the record the same lost identity holds, however many deletes an
// earlier reconcile that failed already sent.
func TestIdentityLostBeforeEveryDeleteWasSentStalls(t *testing.T) {
	forEachCleanupKind(t, func(t *testing.T, kind cleanupKind) {
		failB := func(obj client.Object) error {
			if obj.GetName() == "b" {
				return serverError(obj)
			}
			return nil
		}
		run := newCleanupRun(t, kind, cleanupFixture{prune: true, sa: "deploy-sa",
			entries: []releasesv1alpha1.InventoryEntry{configMapEntry("a"), configMapEntry("b")}},
			nil, failB, ownedConfigMap("a"), ownedConfigMap("b"))
		// The first reconcile deletes a and fails on b: no record is written.
		if _, err := run.reconcile(); err == nil {
			t.Fatal("the first reconcile must fail on b")
		}
		if reason := run.readyReason(); reason != "" {
			t.Fatalf("Ready reason = %q after a failed cleanup, want none", reason)
		}
		// The ServiceAccount is now looked up, and it does not exist.
		run.env.restConfig = stubRestConfig
		if result := run.mustReconcile(); result.RequeueAfter != StalledRecheckInterval {
			t.Errorf("RequeueAfter = %s, want the stalled recheck", result.RequeueAfter)
		}
		if run.released() {
			t.Fatal("the finalizer was removed although b was never deleted")
		}
		if got := run.readyReason(); got != status.DeletionSAMissingReason {
			t.Errorf("Ready reason = %q, want DeletionSAMissing", got)
		}
	})
}

// TestRecordAndForbiddenRechecks covers the rows of the record table for a
// recheck that holds as cleanup-forbidden.
func TestRecordAndForbiddenRechecks(t *testing.T) {
	entries := []releasesv1alpha1.InventoryEntry{configMapEntry("a"), configMapEntry("b")}
	forbidName := func(name string) func(client.Object) error {
		return func(obj client.Object) error {
			if name == "" || obj.GetName() == name {
				return forbidden(obj)
			}
			return nil
		}
	}

	forEachCleanupKind(t, func(t *testing.T, kind cleanupKind) {
		t.Run("every read forbidden releases", func(t *testing.T) {
			run := newCleanupRun(t, kind, cleanupFixture{prune: true, sa: "deploy-sa", entries: entries,
				conditions: waitRecord(status.DeletionInProgressReason)}, forbidName(""), nil,
				ownedConfigMap("a", holdFinalizer), ownedConfigMap("b"))
			run.mustReconcile()
			if !run.released() {
				t.Fatalf("the finalizer stays; Ready reason %q", run.readyReason())
			}
			evs := drainEvents(run.rec)
			if len(evs) != 1 || !strings.HasPrefix(evs[0], "Warning "+status.DeletionUnconfirmedReason) ||
				!strings.Contains(evs[0], "2 object(s)") || !strings.Contains(evs[0], `"team-a/deploy-sa" is not allowed to read them`) {
				t.Errorf("events = %v, want one DeletionUnconfirmed for 2 objects", evs)
			}
			if len(run.log.deletes) != 0 {
				t.Errorf("deletes = %v, want none", run.log.deletes)
			}
		})

		t.Run("without the record every read forbidden stalls", func(t *testing.T) {
			run := newCleanupRun(t, kind, cleanupFixture{prune: true, sa: "deploy-sa", entries: entries},
				forbidName(""), nil, ownedConfigMap("a"), ownedConfigMap("b"))
			run.mustReconcile()
			if run.released() || run.readyReason() != status.ImpersonationFailedReason {
				t.Errorf("released = %t, Ready reason %q; want a stall with ImpersonationFailed", run.released(), run.readyReason())
			}
		})

		t.Run("a readable terminating object still holds when its repeated delete is forbidden", func(t *testing.T) {
			var deny func(client.Object) error
			held := ownedConfigMap("a", holdFinalizer)
			run := newCleanupRun(t, kind, cleanupFixture{prune: true, sa: "deploy-sa",
				entries: []releasesv1alpha1.InventoryEntry{configMapEntry("a")}},
				nil, func(obj client.Object) error {
					if deny != nil {
						return deny(obj)
					}
					return nil
				}, held)
			run.mustReconcile()
			if run.readyReason() != status.DeletionInProgressReason {
				t.Fatalf("Ready reason = %q, want DeletionInProgress", run.readyReason())
			}
			deny = forbidden
			if result := run.mustReconcile(); result.RequeueAfter != time.Second {
				t.Errorf("RequeueAfter = %s, want the wait's recheck", result.RequeueAfter)
			}
			if run.released() || run.readyReason() != status.DeletionInProgressReason {
				t.Errorf("released = %t, Ready reason %q; want the wait kept", run.released(), run.readyReason())
			}
			// The object goes: every deleted object was read as gone, so no
			// DeletionUnconfirmed event.
			run.release(held)
			drainEvents(run.rec)
			run.mustReconcile()
			if !run.released() {
				t.Error("the finalizer stays after the object went")
			}
			if evs := drainEvents(run.rec); len(evs) != 0 {
				t.Errorf("events = %v, want none", evs)
			}
		})

		t.Run("an unreadable entry does not hold, a readable terminating one does", func(t *testing.T) {
			held := ownedConfigMap("b", holdFinalizer)
			run := newCleanupRun(t, kind, cleanupFixture{prune: true, sa: "deploy-sa", entries: entries,
				conditions: waitRecord(status.DeletionInProgressReason)}, forbidName("a"), nil,
				ownedConfigMap("a", holdFinalizer), held)
			run.mustReconcile()
			if run.released() || run.readyReason() != status.DeletionInProgressReason {
				t.Fatalf("released = %t, Ready reason %q; want the wait kept for b", run.released(), run.readyReason())
			}
			run.release(held)
			drainEvents(run.rec)
			run.mustReconcile()
			if !run.released() {
				t.Fatal("the finalizer stays although no readable deleted object is left")
			}
			evs := drainEvents(run.rec)
			if len(evs) != 1 || !strings.Contains(evs[0], status.DeletionUnconfirmedReason) || !strings.Contains(evs[0], "1 object(s)") {
				t.Errorf("events = %v, want one DeletionUnconfirmed for the unread entry", evs)
			}
		})

		t.Run("a forbidden delete of an object that is not terminating stalls as before", func(t *testing.T) {
			// The record is present, but this object was never deleted: the
			// record must not release it.
			live := ownedConfigMap("a")
			run := newCleanupRun(t, kind, cleanupFixture{prune: true, sa: "deploy-sa",
				entries:    []releasesv1alpha1.InventoryEntry{configMapEntry("a")},
				conditions: waitRecord(status.DeletionInProgressReason)}, nil, forbidName(""), live)
			run.mustReconcile()
			if run.released() || run.readyReason() != status.ImpersonationFailedReason {
				t.Errorf("released = %t, Ready reason %q; want a stall with ImpersonationFailed", run.released(), run.readyReason())
			}
			if !run.exists(live) {
				t.Error("the object was deleted")
			}
		})

	})
}

// A recheck in which a step failed for a cause that is not a refusal (here a
// server error next to a Forbidden read) is retried, and the record stays.
func TestRecordIsKeptWhenARecheckFailsForATransientCause(t *testing.T) {
	entries := []releasesv1alpha1.InventoryEntry{configMapEntry("a"), configMapEntry("b")}
	forEachCleanupKind(t, func(t *testing.T, kind cleanupKind) {
		run := newCleanupRun(t, kind, cleanupFixture{prune: true, sa: "deploy-sa", entries: entries,
			conditions: waitRecord(status.DeletionInProgressReason)},
			func(obj client.Object) error {
				if obj.GetName() == "a" {
					return forbidden(obj)
				}
				return serverError(obj)
			}, nil, ownedConfigMap("a"), ownedConfigMap("b"))
		if _, err := run.reconcile(); err == nil {
			t.Fatal("a read that failed with a server error must fail the reconcile")
		}
		if run.released() {
			t.Error("the finalizer was removed although a read failed with a server error")
		}
		if got := run.readyReason(); got != status.DeletionInProgressReason {
			t.Errorf("Ready reason = %q, want the record kept", got)
		}
		if evs := drainEvents(run.rec); len(evs) != 0 {
			t.Errorf("events = %v, want none", evs)
		}
	})
}

// The record is read only on an object that is being deleted.
func TestRecordIsIgnoredOnALiveObject(t *testing.T) {
	live := &releasesv1alpha1.ModuleInstance{}
	live.Status.Conditions = waitRecord(status.DeletionInProgressReason)
	if recordedWaitReason(live) != "" {
		t.Error("a live object carries the record")
	}
	deleting := deletingMR("", "")
	if recordedWaitReason(deleting) != "" {
		t.Error("an object without the reason carries the record")
	}
	deleting.Status.Conditions = waitRecord(status.DeletionBlockedReason)
	if recordedWaitReason(deleting) == "" {
		t.Error("a deleting object with the reason does not carry the record")
	}
	deleting.Status.Conditions[0].Status = metav1.ConditionTrue
	if recordedWaitReason(deleting) != "" {
		t.Error("Ready=True counts as the record")
	}
	deleting.Status.Conditions = waitRecord(status.DeletionSAMissingReason)
	if recordedWaitReason(deleting) != "" {
		t.Error("another reason counts as the record")
	}
}

// TestFailedGoneCheckKeepsTheFinalizer: when the read that checks whether a
// deleted object is gone fails with anything but NotFound, it is not known
// whether the object is gone. The finalizer stays, the error is returned for
// a retry, and no wait reason is written: the cleanup did not confirm
// anything, so the next reconcile is a first pass.
func TestFailedGoneCheckKeepsTheFinalizer(t *testing.T) {
	forEachCleanupKind(t, func(t *testing.T, kind cleanupKind) {
		reads := 0
		run := newCleanupRun(t, kind, cleanupFixture{prune: true,
			entries: []releasesv1alpha1.InventoryEntry{claimEntry("data"), configMapEntry("cm")}},
			func(obj client.Object) error {
				if obj.GetName() != "cm" {
					return nil
				}
				// The plan's read succeeds; the gone-check read fails.
				if reads++; reads == 2 {
					return serverError(obj)
				}
				return nil
			}, nil, ownedClaim("data"), ownedConfigMap("cm", holdFinalizer))

		if _, err := run.reconcile(); err == nil {
			t.Fatal("a failed gone-check read must fail the reconcile")
		}
		if reads != 2 {
			t.Fatalf("the ConfigMap was read %d times, want the plan's read and the gone-check", reads)
		}
		if run.released() {
			t.Fatal("the finalizer was removed although the deleted object could not be read")
		}
		if reason := run.readyReason(); reason != "" {
			t.Errorf("Ready reason = %q, want none: nothing was confirmed", reason)
		}
		if evs := drainEvents(run.rec); len(evs) != 0 {
			t.Errorf("events = %v, want none before the gone-check succeeded", evs)
		}
	})
}

// failServiceAccountRead makes the controller's read of the ServiceAccount
// fail with err, as a control plane under load does.
func (r *cleanupRun) failServiceAccountRead(err error) {
	r.env.restConfig = stubRestConfig
	r.env.apiReader = interceptor.NewClient(r.base, interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			if _, ok := obj.(*corev1.ServiceAccount); ok {
				return err
			}
			return c.Get(ctx, key, obj, opts...)
		},
	})
}

// TestRecordHoldsOnATransientIdentityFailure: the record releases only an
// identity that is really gone. A read of the ServiceAccount that fails for
// any other cause (a server error, a timeout, a throttle, a refusal of the
// controller's own read) says nothing about the ServiceAccount: the
// finalizer stays, the record stays, and the reconcile is retried.
func TestRecordHoldsOnATransientIdentityFailure(t *testing.T) {
	failures := map[string]error{
		"server error":      apierrors.NewInternalError(errors.New("injected")),
		"timeout":           context.DeadlineExceeded,
		"server timeout":    apierrors.NewTimeoutError("injected", 1),
		"throttled":         apierrors.NewTooManyRequests("injected", 1),
		"unavailable":       apierrors.NewServiceUnavailable("injected"),
		"connection":        errors.New("dial tcp 10.0.0.1:443: connect: connection refused"),
		"cancelled":         context.Canceled,
		"controller denied": apierrors.NewForbidden(schema.GroupResource{Resource: "serviceaccounts"}, "deploy-sa", errors.New("denied")),
	}
	for name, failure := range failures {
		for _, reason := range []string{status.DeletionInProgressReason, status.DeletionBlockedReason} {
			forEachCleanupKind(t, func(t *testing.T, kind cleanupKind) {
				t.Run(name+"/"+reason, func(t *testing.T) {
					held := ownedConfigMap("cm", holdFinalizer)
					run := newCleanupRun(t, kind, cleanupFixture{prune: true, sa: "deploy-sa",
						entries:    []releasesv1alpha1.InventoryEntry{configMapEntry("cm")},
						conditions: waitRecord(reason)}, nil, nil, held, saFixture(deletionTestNamespace, "deploy-sa"))
					run.failServiceAccountRead(failure)

					if _, err := run.reconcile(); err == nil {
						t.Fatal("the reconcile must return the error, so that it is retried with backoff")
					}
					if run.released() {
						t.Fatal("one failed read of a ServiceAccount removed the cleanup finalizer")
					}
					ready := run.ready()
					if ready == nil || ready.Reason != reason {
						t.Fatalf("Ready = %+v, want the reason %s kept: it is the record", ready, reason)
					}
					if !strings.Contains(ready.Message, "could not be checked") {
						t.Errorf("the message must say that the check failed: %s", ready.Message)
					}
					if len(run.log.reads) != 0 || len(run.log.deletes) != 0 {
						t.Errorf("reads = %v, deletes = %v; want none", run.log.reads, run.log.deletes)
					}
					if evs := drainEvents(run.rec); len(evs) != 0 {
						t.Errorf("events = %v, want none", evs)
					}
				})
			})
		}
	}
}

// A 401 answers the controller's own credential, before impersonation. It
// says nothing about the instance's ServiceAccount, so it is transient: with
// the record the finalizer and the record stay.
func TestRecordHoldsOnUnauthorizedReads(t *testing.T) {
	unauthorized := func(client.Object) error { return apierrors.NewUnauthorized("token rejected") }
	forEachCleanupKind(t, func(t *testing.T, kind cleanupKind) {
		run := newCleanupRun(t, kind, cleanupFixture{prune: true, sa: "deploy-sa",
			entries:    []releasesv1alpha1.InventoryEntry{configMapEntry("a")},
			conditions: waitRecord(status.DeletionBlockedReason)}, unauthorized, nil, ownedConfigMap("a", holdFinalizer))
		if _, err := run.reconcile(); err == nil {
			t.Fatal("an Unauthorized read must fail the reconcile, so that it is retried")
		}
		if run.released() {
			t.Fatal("one Unauthorized answer removed the cleanup finalizer")
		}
		if got := run.readyReason(); got != status.DeletionBlockedReason {
			t.Errorf("Ready reason = %q, want the record kept", got)
		}
		if evs := drainEvents(run.rec); countEventsWithReason(evs, status.DeletionUnconfirmedReason) != 0 {
			t.Errorf("events = %v, want no DeletionUnconfirmed", evs)
		}
	})
}

// impersonationRefused is the 403 the API server sends when the controller
// may not impersonate the ServiceAccount: a plain Forbidden status that names
// the ServiceAccount, whatever object the request was for
// (k8s.io/apiserver, endpoints/filters/impersonation).
func impersonationRefused(client.Object) error {
	return apierrors.NewForbidden(schema.GroupResource{Resource: "serviceaccounts"}, "deploy-sa",
		errors.New("User \"system:serviceaccount:opm-operator-system:manager\" cannot impersonate resource \"serviceaccounts\""))
}

// TestForbiddenReleasesOnlyWhenTheRefusalIsTheInstances: a 403 on a read as
// the ServiceAccount releases a waiting deletion only when the controller can
// show that the refusal is the ServiceAccount's and not its own. It asks the
// API server whether it may still impersonate that ServiceAccount.
func TestForbiddenReleasesOnlyWhenTheRefusalIsTheInstances(t *testing.T) {
	entries := []releasesv1alpha1.InventoryEntry{configMapEntry("a")}
	fixture := cleanupFixture{prune: true, sa: "deploy-sa", entries: entries,
		conditions: waitRecord(status.DeletionInProgressReason)}
	const asked = "impersonate serviceaccounts team-a/deploy-sa"

	forEachCleanupKind(t, func(t *testing.T, kind cleanupKind) {
		t.Run("the controller lost its right to impersonate: holds", func(t *testing.T) {
			// The ServiceAccount exists and keeps its rights.
			run := newCleanupRun(t, kind, fixture, impersonationRefused, nil,
				ownedConfigMap("a", holdFinalizer), saFixture(deletionTestNamespace, "deploy-sa"))
			run.log.denyImpersonate = true
			if _, err := run.reconcile(); err == nil {
				t.Fatal("the reconcile must return an error, so that it is retried")
			}
			if run.released() {
				t.Fatal("a 403 from the controller's own lost right removed the cleanup finalizer")
			}
			ready := run.ready()
			if ready == nil || ready.Reason != status.DeletionInProgressReason {
				t.Fatalf("Ready = %+v, want the record kept", ready)
			}
			if !strings.Contains(ready.Message, "may not impersonate") {
				t.Errorf("the message must say why the deletion is held: %s", ready.Message)
			}
			if len(run.log.reviews) != 1 || run.log.reviews[0] != asked {
				t.Errorf("reviews = %v, want [%s]", run.log.reviews, asked)
			}
			if evs := drainEvents(run.rec); len(evs) != 0 {
				t.Errorf("events = %v, want none", evs)
			}
		})

		t.Run("the review fails: holds", func(t *testing.T) {
			run := newCleanupRun(t, kind, fixture, forbidden, nil, ownedConfigMap("a", holdFinalizer))
			run.log.reviewErr = apierrors.NewServiceUnavailable("injected")
			if _, err := run.reconcile(); err == nil {
				t.Fatal("the reconcile must return an error, so that it is retried")
			}
			if run.released() {
				t.Fatal("the finalizer was removed although the review failed")
			}
			if got := run.readyReason(); got != status.DeletionInProgressReason {
				t.Errorf("Ready reason = %q, want the record kept", got)
			}
		})

		t.Run("the ServiceAccount lost its rights: releases", func(t *testing.T) {
			run := newCleanupRun(t, kind, fixture, forbidden, nil, ownedConfigMap("a", holdFinalizer))
			run.mustReconcile()
			if !run.released() {
				t.Fatalf("the finalizer stays; Ready reason %q", run.readyReason())
			}
			if len(run.log.reviews) != 1 || run.log.reviews[0] != asked {
				t.Errorf("reviews = %v, want [%s]", run.log.reviews, asked)
			}
			if evs := drainEvents(run.rec); len(evs) != 1 || !strings.Contains(evs[0], status.DeletionUnconfirmedReason) {
				t.Errorf("events = %v, want one DeletionUnconfirmed", evs)
			}
		})

		t.Run("the controller deletes as itself and is refused: holds", func(t *testing.T) {
			own := fixture
			own.sa = ""
			run := newCleanupRun(t, kind, own, forbidden, nil, ownedConfigMap("a", holdFinalizer))
			if _, err := run.reconcile(); err == nil {
				t.Fatal("the reconcile must return an error, so that it is retried")
			}
			if run.released() {
				t.Fatal("a refusal of the controller's own read removed the cleanup finalizer")
			}
			if got := run.readyReason(); got != status.DeletionInProgressReason {
				t.Errorf("Ready reason = %q, want the record kept", got)
			}
		})
	})
}

// TestFailingRechecksAreReportedAndBecomeBlocked: a waiting deletion whose
// rechecks keep failing says why in its status at once, and is reported as
// DeletionBlocked after the threshold like any other deletion that is stuck.
// The clock is injected; the test does not sleep.
func TestFailingRechecksAreReportedAndBecomeBlocked(t *testing.T) {
	unavailable := func(client.Object) error { return apierrors.NewServiceUnavailable("injected") }
	causes := map[string]func(t *testing.T, kind cleanupKind) *cleanupRun{
		"a read of the recheck fails": func(t *testing.T, kind cleanupKind) *cleanupRun {
			return newCleanupRun(t, kind, cleanupFixture{prune: true, sa: "deploy-sa",
				entries:    []releasesv1alpha1.InventoryEntry{configMapEntry("a")},
				conditions: waitRecord(status.DeletionInProgressReason)}, unavailable, nil, ownedConfigMap("a", holdFinalizer))
		},
		"the gone-check read fails": func(t *testing.T, kind cleanupKind) *cleanupRun {
			reads := 0
			return newCleanupRun(t, kind, cleanupFixture{prune: true, sa: "deploy-sa",
				entries:    []releasesv1alpha1.InventoryEntry{configMapEntry("a")},
				conditions: waitRecord(status.DeletionInProgressReason)},
				func(obj client.Object) error {
					// Every second read is the gone-check of a reconcile.
					if reads++; reads%2 == 0 {
						return unavailable(obj)
					}
					return nil
				}, nil, ownedConfigMap("a", holdFinalizer))
		},
		"the ServiceAccount lookup fails": func(t *testing.T, kind cleanupKind) *cleanupRun {
			run := newCleanupRun(t, kind, cleanupFixture{prune: true, sa: "deploy-sa",
				entries:    []releasesv1alpha1.InventoryEntry{configMapEntry("a")},
				conditions: waitRecord(status.DeletionInProgressReason)}, nil, nil, ownedConfigMap("a", holdFinalizer))
			run.failServiceAccountRead(apierrors.NewServiceUnavailable("injected"))
			return run
		},
	}
	for name, build := range causes {
		forEachCleanupKind(t, func(t *testing.T, kind cleanupKind) {
			t.Run(name, func(t *testing.T) {
				run := build(t, kind)
				if _, err := run.reconcile(); err == nil {
					t.Fatal("the failed recheck must fail the reconcile")
				}
				ready := run.ready()
				if ready == nil || ready.Reason != status.DeletionInProgressReason {
					t.Fatalf("Ready = %+v, want DeletionInProgress kept", ready)
				}
				if !strings.Contains(ready.Message, "could not be checked") || !strings.Contains(ready.Message, "injected") {
					t.Errorf("the message must say at once that the check failed, and why: %s", ready.Message)
				}
				if evs := drainEvents(run.rec); len(evs) != 0 {
					t.Errorf("events = %v, want none before the threshold", evs)
				}

				run.env.wait.Now = func() time.Time { return time.Now().Add(11 * time.Minute) }
				for range 2 {
					if _, err := run.reconcile(); err == nil {
						t.Fatal("the failed recheck must fail the reconcile")
					}
				}
				ready = run.ready()
				if ready == nil || ready.Reason != status.DeletionBlockedReason {
					t.Fatalf("Ready = %+v, want DeletionBlocked after the threshold", ready)
				}
				for _, want := range []string{"could not be checked", "injected", "spec.prune=false"} {
					if !strings.Contains(ready.Message, want) {
						t.Errorf("the message does not contain %q: %s", want, ready.Message)
					}
				}
				if c := run.condition(status.StalledCondition); c == nil || c.Status != metav1.ConditionTrue {
					t.Errorf("Stalled = %+v, want True", c)
				}
				evs := drainEvents(run.rec)
				if len(evs) != 1 || !strings.HasPrefix(evs[0], "Warning "+status.DeletionBlockedReason) {
					t.Errorf("events = %v, want one Warning DeletionBlocked", evs)
				}
				if run.released() {
					t.Fatal("a blocked deletion gave up its finalizer")
				}
			})
		})
	}
}
