package reconcile

import (
	"context"
	"fmt"
	"time"

	"github.com/fluxcd/pkg/runtime/conditions"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	k8sinventory "github.com/open-platform-model/library/opm/k8s/inventory"
	"github.com/open-platform-model/library/opm/k8s/lifecycle"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	"github.com/open-platform-model/opm-operator/internal/apply"
	"github.com/open-platform-model/opm-operator/internal/status"
)

// deletionEnv is what a deletion cleanup needs from the reconciler params of
// either kind.
type deletionEnv struct {
	client                client.Client
	apiReader             client.Reader
	restConfig            *rest.Config
	recorder              events.EventRecorder
	defaultServiceAccount string
	wait                  DeletionWait
}

// DeletionWait tunes the wait of a deletion cleanup for its deleted objects.
// Each zero field is replaced by its default; cmd/main.go sets nothing. It is
// a seam for tests and not a flag: a test moves Now past BlockedAfter instead
// of sleeping.
type DeletionWait struct {
	// BlockedAfter is how long a deleted object may stay terminating before
	// the deletion is reported as DeletionBlocked. Default 10 minutes.
	BlockedAfter time.Duration
	// MinRecheck and MaxRecheck bound the requeue interval of a waiting
	// deletion. Defaults 5 and 60 seconds.
	MinRecheck time.Duration
	MaxRecheck time.Duration
	// Now is the controller's clock. Default time.Now.
	Now func() time.Time
}

// The defaults of DeletionWait. A judgment, not a measurement: a Pod's
// default grace period is 30 seconds, so most deletions end within the first
// few rechecks.
const (
	defaultDeletionBlockedAfter = 10 * time.Minute
	defaultDeletionMinRecheck   = 5 * time.Second
	defaultDeletionMaxRecheck   = 60 * time.Second
)

func (w DeletionWait) withDefaults() DeletionWait {
	if w.BlockedAfter == 0 {
		w.BlockedAfter = defaultDeletionBlockedAfter
	}
	if w.MinRecheck == 0 {
		w.MinRecheck = defaultDeletionMinRecheck
	}
	if w.MaxRecheck == 0 {
		w.MaxRecheck = defaultDeletionMaxRecheck
	}
	if w.Now == nil {
		w.Now = time.Now
	}
	return w
}

// recheckAfter is the requeue interval of a waiting deletion: a quarter of
// the age of the oldest terminating object, at least MinRecheck and at most
// MaxRecheck. It needs no stored counter and backs off on its own.
func (w DeletionWait) recheckAfter(age time.Duration) time.Duration {
	return min(max(age/4, w.MinRecheck), w.MaxRecheck)
}

// deletionTarget is the deleting ModuleInstance or ModulePackage, as the one
// cleanup of both kinds sees it.
type deletionTarget struct {
	obj conditions.Setter
	// kind names the object's kind in log lines.
	kind string
	// prune, deleteData and serviceAccount are spec.prune, whether
	// spec.dataPolicy deletes claims, and spec.serviceAccountName.
	prune          bool
	deleteData     bool
	serviceAccount string
	// entries is status.inventory.entries; identities are the recorded
	// instance identities, most recent first.
	entries    []releasesv1alpha1.InventoryEntry
	identities []string

	clearInventory  func()
	patchStatus     func(context.Context) error
	removeFinalizer func(context.Context) error
}

// identityState is the deleting identity of one cleanup reconcile.
type identityState struct {
	// client deletes and reads; nil when the identity is unavailable. The
	// controller's own client is never a fallback for an identity that
	// could not be obtained.
	client client.Client
	// held is the verdict input: available, missing or failed.
	held lifecycle.Identity
	// err is the impersonation error of an unavailable identity.
	err error
	// name is the effective ServiceAccount name, empty when the controller
	// deletes as itself; source says where the name came from.
	name, source string
}

// namespaced is the ServiceAccount as events name it; empty without one.
func (i identityState) namespaced(namespace string) string {
	if i.name == "" {
		return ""
	}
	return namespace + "/" + i.name
}

// unread says why an unavailable identity could not read.
func (i identityState) unread() status.Unread {
	if i.held == lifecycle.IdentityMissing {
		return status.UnreadIdentityMissing
	}
	return status.UnreadIdentityFailed
}

// runDeletionCleanup is the deletion cleanup of a ModuleInstance and of a
// ModulePackage. The cleanup finalizer comes off only on a release verdict of
// the library's hold verdict (lifecycle.MayReleaseHold, 0012:D4:R1), asked
// for every plan of the cleanup, with the plans as built: without the
// PersistentVolumeClaims spec.dataPolicy keeps.
//
//   - prune-disabled and inventory-empty release without a read. An inventory
//     of kept claims only is empty for the plan: with the identity available
//     the claims are read for the ClaimsKept report; with the identity
//     missing or failed they are left unread and a DeletionUnconfirmed event
//     says so.
//   - With a missing or failed identity no plan runs: the verdict is asked
//     with the zero State and answers on the identity. force-orphan releases
//     as OrphanedOnDeletion; otherwise the deletion stalls with
//     DeletionSAMissing or ImpersonationFailed.
//   - Otherwise the plans run. cleanup-forbidden stalls with
//     ImpersonationFailed under impersonation; any other hold returns the
//     error for a retry with backoff.
//   - After a release verdict the finalizer stays until every object this
//     cleanup deleted is gone (awaitGone). The reconcile never blocks: it
//     reads, marks DeletionInProgress or DeletionBlocked, and requeues. Each
//     recheck starts fresh plans from the zero State and judges every entry
//     again from the cluster; no deletion state is stored.
//
// One fact is carried across reconciles: that a reconcile of this deletion
// reached a release verdict, so every delete was sent. The record is the
// Ready reason DeletionInProgress or DeletionBlocked (everyDeleteWasSent).
// It decides one thing: with the record, a deleting identity that is lost
// during the wait, or reads that are refused as Forbidden, no longer hold the
// finalizer (DeletionUnconfirmed). It never causes a read or a delete.
func runDeletionCleanup(ctx context.Context, env deletionEnv, t deletionTarget) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	log.Info("Running deletion cleanup for " + t.kind)
	env.wait = env.wait.withDefaults()

	// Read before any Mark* call of this reconcile.
	recorded := everyDeleteWasSent(t.obj)

	policy := lifecycle.Policy{
		Prune:       t.prune,
		ForceOrphan: t.obj.GetAnnotations()[releasesv1alpha1.AnnotationForceDeleteOrphan] == "true",
	}
	planned, claims := apply.SplitKeptClaims(t.entries, t.deleteData)
	// firstPlan is the first plan as RunDeletion builds it. It is asked the
	// verdicts that need no run; a later plan exists only after a run.
	firstPlan := lifecycle.NewDeletionPlan(libraryEntries(planned), policy, firstIdentity(t.identities))

	// Asked as an available identity: these two verdicts come before the
	// identity, so nothing is impersonated for them.
	early := lifecycle.MayReleaseHold(firstPlan, lifecycle.State{}, lifecycle.HoldInput{})
	if early.Because == lifecycle.HoldPruneDisabled ||
		(early.Because == lifecycle.HoldInventoryEmpty && len(claims) == 0) {
		if !t.prune {
			log.Info("Prune disabled, orphaning managed resources on deletion")
		}
		return releaseFinalizer(ctx, t)
	}

	identity := resolveDeletingIdentity(ctx, env, t)
	if identity.client == nil {
		verdict := lifecycle.MayReleaseHold(firstPlan, lifecycle.State{}, lifecycle.HoldInput{Identity: identity.held})
		return actOnLostIdentity(ctx, env, t, identity, verdict, lostCounts{
			claims:   len(claims),
			planned:  unreadSteps(firstPlan),
			recorded: recorded,
		})
	}

	cleanup, err := apply.RunCleanup(ctx, identity.client, t.identities, t.entries, policy,
		apply.PruneOptions{DeleteData: t.deleteData})
	if err != nil {
		return ctrl.Result{}, err
	}
	verdict := holdVerdict(cleanup.Deletion)
	if !verdict.Release {
		if recorded && verdict.Because == lifecycle.HoldCleanupForbidden {
			if waitFor, unconfirmed, onlyLostRights := classifyRecheck(cleanup.Deletion); onlyLostRights {
				return awaitGone(ctx, env, t, identity, goneCheck{steps: waitFor, unconfirmed: unconfirmed})
			}
		}
		return actOnHold(ctx, env, t, identity, verdict, cleanup)
	}

	log.Info("Deletion cleanup pruned resources",
		"deleted", cleanup.Report.Deleted, "skipped", cleanup.Report.Skipped, "keptClaims", len(cleanup.Report.Kept))
	check := goneCheck{steps: cleanup.Deletion.Deleted()}
	if !recorded {
		// The first reconcile of this deletion that reaches a release
		// verdict reports; a recheck does not report again.
		check.report = cleanup.Report
	}
	return awaitGone(ctx, env, t, identity, check)
}

// everyDeleteWasSent reads the record that an earlier reconcile of this
// deletion reached a release verdict: Ready is False with one of the two wait
// reasons. It is read from the object as the reconcile loaded it, and counts
// only on an object that is being deleted: on a live object the reasons mean
// nothing, whoever wrote them.
//
// The record lives in status, so whoever may write the status subresource can
// write it. Its effect is bounded to this: when the deleting identity is also
// lost, the finalizer of a deleting object is released early. That leaves
// objects in the cluster and never deletes one; spec.prune=false does the
// same for anyone who may edit the spec. The roles the operator ships give
// users no write verb on a status subresource (test/rbac).
func everyDeleteWasSent(obj conditions.Getter) bool {
	if obj.GetDeletionTimestamp().IsZero() {
		return false
	}
	ready := apimeta.FindStatusCondition(obj.GetConditions(), status.ReadyCondition)
	return ready != nil && ready.Status == metav1.ConditionFalse &&
		(ready.Reason == status.DeletionInProgressReason || ready.Reason == status.DeletionBlockedReason)
}

// unreadSteps counts the steps of a plan that a cleanup would read: all but
// the kinds it leaves in place up front.
func unreadSteps(plan lifecycle.DeletionPlan) int {
	n := 0
	for _, step := range plan.Steps() {
		if step.Skip == "" {
			n++
		}
	}
	return n
}

// classifyRecheck sorts the failed steps of a recheck that holds as
// cleanup-forbidden, for a deletion that already sent every delete:
//
//   - A read refused as Forbidden cannot be confirmed and does not hold.
//   - A delete refused as Forbidden of an object that was read and is already
//     terminating is a repeated delete: the object is waited for, as a
//     deleted one.
//   - Any other failure (a failure that is not Forbidden, or a Forbidden
//     delete of an object that is not terminating) is not a lost right
//     during the wait: onlyLostRights is false and the hold stands.
//
// waitFor also holds the steps whose delete was accepted.
func classifyRecheck(deletion apply.Deletion) (waitFor []apply.StepResult, unconfirmed int, onlyLostRights bool) {
	for _, run := range deletion.Runs {
		for _, step := range run.Steps {
			switch step.Outcome.Result {
			case lifecycle.ResultDeleted:
				waitFor = append(waitFor, step)
			case lifecycle.ResultFailed:
				forbidden := step.Outcome.Failure == lifecycle.FailureForbidden
				switch {
				case forbidden && step.Failed == lifecycle.ActionRead:
					unconfirmed++
				case forbidden && step.Failed == lifecycle.ActionDelete &&
					step.Live != nil && !step.Live.GetDeletionTimestamp().IsZero():
					waitFor = append(waitFor, step)
				default:
					return nil, 0, false
				}
			case lifecycle.ResultSkipped:
			}
		}
	}
	return waitFor, unconfirmed, true
}

// goneCheck is what a cleanup waits for after every delete was sent.
type goneCheck struct {
	// steps are the deleted objects to read again.
	steps []apply.StepResult
	// unconfirmed counts the entries whose read was refused as Forbidden.
	unconfirmed int
	// report is set by the first reconcile that reaches a release verdict:
	// its kept claims and left-behind objects are reported once.
	report *apply.PruneResult
}

// awaitGone keeps the cleanup finalizer until every deleted object is gone:
// its read returns NotFound, or another object holds its name. A kept claim,
// an object left behind and an object that was already absent are not waited
// for: they are no deleted step.
//
// With objects left it marks DeletionInProgress and requeues after
// recheckAfter, or marks DeletionBlocked once the oldest deletionTimestamp
// is BlockedAfter old by the controller's clock, and rechecks every
// MaxRecheck. It never gives up. Each event is emitted once: not while Ready
// already carries the reason.
func awaitGone(
	ctx context.Context, env deletionEnv, t deletionTarget, identity identityState, check goneCheck,
) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	left, err := apply.StillThere(ctx, identity.client, check.steps)
	if err != nil {
		log.Error(err, "Could not check that the deleted objects are gone, retaining finalizer")
		return ctrl.Result{}, err
	}
	if check.report != nil {
		// Reported before the finalizer goes: afterwards the object, and
		// with it the inventory that named the claims, no longer exists.
		reportKeptClaims(env.recorder, t.obj, "Delete", check.report.Kept)
		reportLeftBehind(env.recorder, t.obj, "Delete", check.report.Left)
	}
	if len(left) == 0 {
		if check.unconfirmed > 0 {
			log.Info("Every readable deleted object is gone; releasing without confirming the objects that are forbidden to read",
				"serviceAccount", identity.name, "unconfirmed", check.unconfirmed)
			env.recorder.Eventf(t.obj, nil, corev1.EventTypeWarning, status.DeletionUnconfirmedReason, "Delete",
				"%s", status.DeletionUnconfirmedNote(check.unconfirmed, identity.namespaced(t.obj.GetNamespace()), status.UnreadForbidden))
		}
		return releaseFinalizer(ctx, t)
	}

	age := env.wait.Now().Sub(oldestDeletion(left, env.wait.Now()))
	waiting := waitingObjects(left)
	if age < env.wait.BlockedAfter {
		msg := status.DeletionInProgressNote(waiting)
		log.Info("Waiting for deleted objects to be gone", "remaining", len(left), "oldest", age.String())
		if !readyAlreadyStalledWith(t.obj.GetConditions(), status.DeletionInProgressReason) {
			env.recorder.Eventf(t.obj, nil, corev1.EventTypeNormal, status.DeletionInProgressReason, "Delete",
				"%s", status.EventNote(msg))
		}
		status.MarkDeletionInProgress(t.obj, msg)
		if err := t.patchStatus(ctx); err != nil {
			return ctrl.Result{}, fmt.Errorf("recording the deletion wait: %w", err)
		}
		return ctrl.Result{RequeueAfter: env.wait.recheckAfter(age)}, nil
	}

	msg := status.DeletionBlockedNote(waiting, env.wait.BlockedAfter)
	log.Info("Deleted objects do not go; deletion is blocked", "remaining", len(left), "oldest", age.String())
	if !readyAlreadyStalledWith(t.obj.GetConditions(), status.DeletionBlockedReason) {
		env.recorder.Eventf(t.obj, nil, corev1.EventTypeWarning, status.DeletionBlockedReason, "Delete",
			"%s", status.EventNote(msg))
	}
	status.MarkDeletionBlocked(t.obj, msg)
	if err := t.patchStatus(ctx); err != nil {
		return ctrl.Result{}, fmt.Errorf("recording the blocked deletion: %w", err)
	}
	return ctrl.Result{RequeueAfter: env.wait.MaxRecheck}, nil
}

// oldestDeletion is the earliest deletionTimestamp of the objects, as the
// gone-check read them. An object without one counts as deleted now.
func oldestDeletion(objects []*unstructured.Unstructured, now time.Time) time.Time {
	oldest := now
	for _, obj := range objects {
		if ts := obj.GetDeletionTimestamp(); !ts.IsZero() && ts.Time.Before(oldest) {
			oldest = ts.Time
		}
	}
	return oldest
}

// waitingObjects names the objects for a status message.
func waitingObjects(objects []*unstructured.Unstructured) []status.WaitingObject {
	out := make([]status.WaitingObject, len(objects))
	for i, obj := range objects {
		out[i] = status.WaitingObject{
			Kind: obj.GetKind(), Namespace: obj.GetNamespace(), Name: obj.GetName(), Finalizers: obj.GetFinalizers(),
		}
	}
	return out
}

// libraryEntries converts entries to the library's type.
func libraryEntries(entries []releasesv1alpha1.InventoryEntry) []k8sinventory.Entry {
	out := make([]k8sinventory.Entry, len(entries))
	for i, entry := range entries {
		out[i] = k8sinventory.Entry(entry)
	}
	return out
}

// firstIdentity is the identity of the first plan: empty with none recorded.
func firstIdentity(identities []string) string {
	if len(identities) == 0 {
		return ""
	}
	return identities[0]
}

// resolveDeletingIdentity builds the client the cleanup deletes with: the
// impersonated ServiceAccount when one is in effect, the controller's own
// client otherwise.
func resolveDeletingIdentity(ctx context.Context, env deletionEnv, t deletionTarget) identityState {
	name, source := resolveEffectiveSA(t.serviceAccount, env.defaultServiceAccount)
	identity := identityState{client: env.client, name: name, source: source}
	if name == "" || env.restConfig == nil {
		return identity
	}
	impersonated, err := apply.NewImpersonatedClient(ctx, env.restConfig, env.apiReader, env.client.Scheme(),
		t.obj.GetNamespace(), name)
	switch {
	case err == nil:
		identity.client = impersonated
	case apply.IsServiceAccountNotFound(err):
		identity.client, identity.held, identity.err = nil, lifecycle.IdentityMissing, err
	default:
		identity.client, identity.held, identity.err = nil, lifecycle.IdentityFailed, err
	}
	return identity
}

// holdVerdict asks the hold verdict for every plan of a cleanup that ran, as
// the identity that ran it. It releases only when every plan releases;
// otherwise it returns the first Forbidden hold, or else the first hold.
func holdVerdict(deletion apply.Deletion) lifecycle.HoldVerdict {
	var held *lifecycle.HoldVerdict
	var last lifecycle.HoldVerdict
	for _, run := range deletion.Runs {
		last = lifecycle.MayReleaseHold(run.Plan, run.State, lifecycle.HoldInput{})
		if last.Release {
			continue
		}
		if held == nil || (held.Because != lifecycle.HoldCleanupForbidden && last.Because == lifecycle.HoldCleanupForbidden) {
			v := last
			held = &v
		}
	}
	if held != nil {
		return *held
	}
	return last
}

// releaseFinalizer removes the cleanup finalizer.
func releaseFinalizer(ctx context.Context, t deletionTarget) (ctrl.Result, error) {
	if err := t.removeFinalizer(ctx); err != nil {
		return ctrl.Result{}, fmt.Errorf("removing finalizer: %w", err)
	}
	logf.FromContext(ctx).Info("Finalizer removed, deletion can proceed")
	return ctrl.Result{}, nil
}

// lostCounts is what a cleanup without its deleting identity knows.
type lostCounts struct {
	// claims is the number of kept claims; planned the number of entries a
	// plan would read.
	claims, planned int
	// recorded says that an earlier reconcile sent every delete.
	recorded bool
}

// actOnLostIdentity acts on the verdict of a cleanup whose deleting identity
// is missing or failed. No object is read or deleted, and the controller's
// own client is not used in its place.
func actOnLostIdentity(
	ctx context.Context,
	env deletionEnv,
	t deletionTarget,
	identity identityState,
	verdict lifecycle.HoldVerdict,
	counts lostCounts,
) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	namespace := t.obj.GetNamespace()

	switch verdict.Because {
	case lifecycle.HoldInventoryEmpty:
		// Only kept claims are left: the cleanup would delete nothing, so
		// the lost identity has nothing to do. The claims cannot be read
		// without it, so no ClaimsKept event would be true.
		log.Info("Only kept PersistentVolumeClaims are left and the deleting identity is unavailable; releasing without a read",
			"serviceAccount", identity.name, "serviceAccountSource", identity.source, "claims", counts.claims)
		env.recorder.Eventf(t.obj, nil, corev1.EventTypeWarning, status.DeletionUnconfirmedReason, "Delete",
			"%s", status.ClaimsUnreadNote(counts.claims, identity.namespaced(namespace), identity.unread()))
		return releaseFinalizer(ctx, t)

	case lifecycle.HoldForceOrphan:
		orphanCount := int64(len(t.entries))
		log.Info("Orphaning inventory and removing finalizer at operator request",
			"serviceAccount", identity.name,
			"serviceAccountSource", identity.source,
			"inventoryCount", orphanCount)
		env.recorder.Eventf(t.obj, nil, corev1.EventTypeWarning,
			status.OrphanedOnDeletionReason, "Delete",
			"Orphaned %d managed resources; ServiceAccount %q missing and %s annotation set",
			orphanCount, identity.name, releasesv1alpha1.AnnotationForceDeleteOrphan)
		t.clearInventory()
		if err := t.patchStatus(ctx); err != nil {
			log.Error(err, "Failed to patch "+t.kind+" status on orphan-exit")
		}
		return releaseFinalizer(ctx, t)
	}

	if counts.recorded {
		// Every delete was sent by an earlier reconcile of this deletion, so
		// the lost identity has nothing left to delete. The objects cannot
		// be read without it: the event says so.
		log.Info("The deleting identity was lost while the deletion waited; releasing without a read",
			"serviceAccount", identity.name, "serviceAccountSource", identity.source, "unconfirmed", counts.planned)
		env.recorder.Eventf(t.obj, nil, corev1.EventTypeWarning, status.DeletionUnconfirmedReason, "Delete",
			"%s", status.DeletionUnconfirmedNote(counts.planned, identity.namespaced(namespace), identity.unread()))
		return releaseFinalizer(ctx, t)
	}

	if identity.held == lifecycle.IdentityMissing {
		log.Error(identity.err, "Impersonation ServiceAccount missing during deletion; "+t.kind+" stalled pending operator action",
			"serviceAccount", identity.name,
			"serviceAccountSource", identity.source,
			"annotation", releasesv1alpha1.AnnotationForceDeleteOrphan)
		return stallDeletion(ctx, env, t, status.DeletionSAMissingReason,
			deletionSAMissingMessage(namespace, identity.name), "DeletionSAMissing stall")
	}

	log.Error(identity.err, "Impersonation failed during deletion cleanup",
		"serviceAccount", identity.name,
		"serviceAccountSource", identity.source)
	return stallDeletion(ctx, env, t, status.ImpersonationFailedReason, identity.err.Error(), "ImpersonationFailed stall")
}

// actOnHold acts on a hold verdict of a cleanup that ran its plans.
func actOnHold(
	ctx context.Context,
	env deletionEnv,
	t deletionTarget,
	identity identityState,
	verdict lifecycle.HoldVerdict,
	cleanup apply.Cleanup,
) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	err := cleanup.Failed
	if err == nil {
		// A hold without a failed step: the plan did not finish.
		err = fmt.Errorf("deletion cleanup is held: %s", verdict.Message)
	}
	if verdict.Because == lifecycle.HoldCleanupForbidden && identity.name != "" {
		log.Error(err, "Impersonation denied during deletion cleanup",
			"serviceAccount", identity.name,
			"serviceAccountSource", identity.source)
		return stallDeletion(ctx, env, t, status.ImpersonationFailedReason, err.Error(), "Forbidden deletion prune")
	}
	log.Error(err, "Partial failure during deletion cleanup, retaining finalizer")
	return ctrl.Result{}, err
}

// stallDeletion marks the deleting object stalled with reason, emits the
// Warning event unless Ready already carried the reason, and asks for the
// stalled recheck. The finalizer stays.
func stallDeletion(
	ctx context.Context, env deletionEnv, t deletionTarget, reason, msg, what string,
) (ctrl.Result, error) {
	emit := !readyAlreadyStalledWith(t.obj.GetConditions(), reason)
	status.MarkStalled(t.obj, reason, "%s", msg)
	if emit {
		env.recorder.Eventf(t.obj, nil, corev1.EventTypeWarning, reason, "Delete", "%s", msg)
	}
	if err := t.patchStatus(ctx); err != nil {
		logf.FromContext(ctx).Error(err, "Failed to patch "+t.kind+" status on "+what)
	}
	return ctrl.Result{RequeueAfter: StalledRecheckInterval}, nil
}
