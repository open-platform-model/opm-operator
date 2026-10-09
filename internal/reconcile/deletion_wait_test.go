package reconcile

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/fluxcd/pkg/runtime/conditions"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	if w.BlockedAfter != 10*time.Minute || w.MinRecheck != 5*time.Second || w.MaxRecheck != 60*time.Second || w.Now == nil {
		t.Fatalf("defaults = %+v", w)
	}
	for age, want := range map[time.Duration]time.Duration{
		0:                5 * time.Second,
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
			if result.RequeueAfter != 5*time.Second {
				t.Fatalf("reconcile %d: RequeueAfter = %s, want 5s", i, result.RequeueAfter)
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

		if result := run.mustReconcile(); result.RequeueAfter != 5*time.Second {
			t.Fatalf("RequeueAfter = %s, want 5s", result.RequeueAfter)
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
				!strings.Contains(evs[0], "2 object(s)") || !strings.Contains(evs[0], `"team-a/deploy-sa" is forbidden to read them`) {
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
			if result := run.mustReconcile(); result.RequeueAfter != 5*time.Second {
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

		t.Run("a failure that is not Forbidden holds and is retried as before", func(t *testing.T) {
			run := newCleanupRun(t, kind, cleanupFixture{prune: true, sa: "deploy-sa", entries: entries,
				conditions: waitRecord(status.DeletionInProgressReason)},
				func(obj client.Object) error {
					if obj.GetName() == "a" {
						return forbidden(obj)
					}
					return serverError(obj)
				}, nil, ownedConfigMap("a"), ownedConfigMap("b"))
			if _, err := run.reconcile(); err != nil {
				t.Fatalf("reconcile: %v", err)
			}
			if run.released() {
				t.Error("the finalizer was removed although a read failed with a server error")
			}
		})
	})
}

// The record is read only on an object that is being deleted.
func TestRecordIsIgnoredOnALiveObject(t *testing.T) {
	live := &releasesv1alpha1.ModuleInstance{}
	live.Status.Conditions = waitRecord(status.DeletionInProgressReason)
	if everyDeleteWasSent(live) {
		t.Error("a live object carries the record")
	}
	deleting := deletingMR("", "")
	if everyDeleteWasSent(deleting) {
		t.Error("an object without the reason carries the record")
	}
	deleting.Status.Conditions = waitRecord(status.DeletionBlockedReason)
	if !everyDeleteWasSent(deleting) {
		t.Error("a deleting object with the reason does not carry the record")
	}
	deleting.Status.Conditions[0].Status = metav1.ConditionTrue
	if everyDeleteWasSent(deleting) {
		t.Error("Ready=True counts as the record")
	}
	deleting.Status.Conditions = waitRecord(status.DeletionSAMissingReason)
	if everyDeleteWasSent(deleting) {
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
