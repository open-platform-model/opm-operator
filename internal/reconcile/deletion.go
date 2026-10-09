package reconcile

import (
	"context"
	"errors"
	"fmt"
	"strings"
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
	// deletion. Defaults 1 and 60 seconds.
	MinRecheck time.Duration
	MaxRecheck time.Duration
	// Now is the controller's clock. Default time.Now.
	Now func() time.Time
}

// The defaults of DeletionWait. A judgment, not a measurement. The API server
// sets the foregroundDeletion finalizer on every object a Foreground delete
// reaches, so also a ConfigMap is still there for a moment: the first recheck
// comes after one second, so that a deletion of objects that go at once is
// not held longer. A Pod's default grace period is 30 seconds; the interval
// has grown to about 8 seconds by then.
const (
	defaultDeletionBlockedAfter = 10 * time.Minute
	defaultDeletionMinRecheck   = time.Second
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
	// gone says that err is the API server's NotFound for exactly this
	// ServiceAccount (apply.IsServiceAccountGone), read by the controller
	// itself and not as an impersonated identity.
	gone bool
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
// Ready reason DeletionInProgress or DeletionBlocked (recordedWaitReason).
// It decides one thing: with the record, the finalizer is released when the
// instance's ServiceAccount is gone (DeletionUnconfirmed). Gone means exactly
// one thing: the controller's own read of that ServiceAccount is answered
// NotFound for that ServiceAccount (apply.IsServiceAccountGone, the typed
// status, never message text). A NotFound comes from storage; it cannot come
// from an authorizer that is loading or refusing for a moment.
//
// Every other answer, on any read or delete, by anyone, holds: Forbidden,
// Unauthorized, a server error, a throttle, a timeout, a cancelled context,
// also a Forbidden answer on one object next to readable ones. The finalizer
// and the record stay, the Ready message says at once what could not be
// checked, the reconcile is retried with backoff, and once the object has
// been deleting for BlockedAfter the deletion is reported as DeletionBlocked
// with the way out (holdRecheck). The record never causes a read or a delete.
func runDeletionCleanup(ctx context.Context, env deletionEnv, t deletionTarget) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	log.Info("Running deletion cleanup for " + t.kind)
	env.wait = env.wait.withDefaults()

	// Read before any Mark* call of this reconcile.
	recorded := recordedWaitReason(t.obj)

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
		if recorded != "" {
			// Every delete was sent before; this recheck could not be
			// done. Whatever failed, it does not show that the identity
			// is gone, so nothing is released.
			return holdRecheck(ctx, env, t, recorded, failedSteps(cleanup.Deletion), nil)
		}
		return actOnHold(ctx, env, t, identity, verdict, cleanup)
	}

	log.Info("Deletion cleanup pruned resources",
		"deleted", cleanup.Report.Deleted, "skipped", cleanup.Report.Skipped, "keptClaims", len(cleanup.Report.Kept))
	check := goneCheck{steps: cleanup.Deletion.Deleted(), recorded: recorded}
	if recorded == "" {
		// The first reconcile of this deletion that reaches a release
		// verdict reports; a recheck does not report again.
		check.report = cleanup.Report
	}
	return awaitGone(ctx, env, t, identity, check)
}

// recordedWaitReason reads the record that an earlier reconcile of this
// deletion reached a release verdict: Ready is False with one of the two wait
// reasons. It returns that reason, or the empty string without a record. It
// is read from the object as the reconcile loaded it, and counts only on an
// object that is being deleted: on a live object the reasons mean nothing,
// whoever wrote them.
//
// The record lives in status, so whoever may write the status subresource can
// write it. Its effect is bounded to this: when the deleting identity is also
// gone, the finalizer of a deleting object is released early. That leaves
// objects in the cluster and never deletes one; spec.prune=false does the
// same for anyone who may edit the spec. The roles the operator ships give
// users no write verb on a status subresource (test/rbac).
func recordedWaitReason(obj conditions.Getter) string {
	if obj.GetDeletionTimestamp().IsZero() {
		return ""
	}
	ready := apimeta.FindStatusCondition(obj.GetConditions(), status.ReadyCondition)
	if ready == nil || ready.Status != metav1.ConditionFalse {
		return ""
	}
	if ready.Reason == status.DeletionInProgressReason || ready.Reason == status.DeletionBlockedReason {
		return ready.Reason
	}
	return ""
}

// The bounds of the Ready message of a held recheck. A condition message may
// have 32768 characters and an inventory can hold hundreds of entries, so the
// message names the first failed steps and counts the rest.
const (
	heldMaxSteps = 3
	heldMaxCause = 240
)

// cutCause shortens the text of an error for a status message.
func cutCause(text string) string {
	runes := []rune(text)
	if len(runes) <= heldMaxCause {
		return text
	}
	return string(runes[:heldMaxCause]) + "..."
}

// failedSteps words the failed steps of a recheck for a status message: how
// many failed, the first heldMaxSteps of them as whole entries (the request,
// the object and the cause, cut), and the number of the rest. It carries the
// API server's answer and names of objects, and no object content.
func failedSteps(deletion apply.Deletion) string {
	var named []string
	failed := 0
	for _, run := range deletion.Runs {
		for _, step := range run.Steps {
			if step.Outcome.Result != lifecycle.ResultFailed {
				continue
			}
			failed++
			if len(named) == heldMaxSteps {
				continue
			}
			verb := "get"
			if step.Failed == lifecycle.ActionDelete {
				verb = "delete"
			}
			cause := step.Outcome.Message
			if step.Err != nil {
				cause = step.Err.Error()
			}
			entry := step.Entry
			named = append(named, fmt.Sprintf("%s %s %s/%s: %s", verb, entry.Kind, entry.Namespace, entry.Name, cutCause(cause)))
		}
	}
	text := fmt.Sprintf("%d step(s) of the recheck failed: %s", failed, strings.Join(named, "; "))
	if more := failed - len(named); more > 0 {
		text += fmt.Sprintf("; and %d more", more)
	}
	return text
}

// holdRecheck keeps the finalizer and the record of a waiting deletion whose
// recheck could not be done, and makes the cause visible: the Ready message
// says at once what could not be checked. The wait reason stays, because it
// is the record: a stall reason would erase it. Once the object has been
// deleting for BlockedAfter by the controller's clock a failed recheck is
// reported as DeletionBlocked, with one Warning event and the way out, like a
// deletion whose objects do not go. The age is the deleting object's own: no
// deleted object could be read. The returned error retries the reconcile
// with backoff; it carries a failed status patch too, so that a status that
// could not be written is not left as it was.
func holdRecheck(
	ctx context.Context, env deletionEnv, t deletionTarget, recorded, what string, cause error,
) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	if cause != nil {
		what = fmt.Sprintf("%s: %s", what, cutCause(cause.Error()))
	}
	failure := fmt.Errorf("rechecking the waiting deletion: %s", what)
	if cause != nil {
		failure = fmt.Errorf("rechecking the waiting deletion: %w", cause)
	}
	log.Error(failure, "A recheck of the waiting deletion could not be done, retaining finalizer", "what", what)

	age := env.wait.Now().Sub(t.obj.GetDeletionTimestamp().Time)
	if recorded == status.DeletionBlockedReason || age >= env.wait.BlockedAfter {
		msg := status.DeletionCheckBlockedNote(what, env.wait.BlockedAfter)
		if !readyAlreadyStalledWith(t.obj.GetConditions(), status.DeletionBlockedReason) {
			env.recorder.Eventf(t.obj, nil, corev1.EventTypeWarning, status.DeletionBlockedReason, "Delete",
				"%s", status.EventNote(msg))
		}
		status.MarkDeletionBlocked(t.obj, msg)
	} else {
		status.MarkDeletionInProgress(t.obj, status.DeletionCheckFailedNote(what))
	}
	if err := t.patchStatus(ctx); err != nil {
		return ctrl.Result{}, errors.Join(failure, fmt.Errorf("recording the failed recheck: %w", err))
	}
	return ctrl.Result{}, failure
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

// goneCheck is what a cleanup waits for after every delete was sent.
type goneCheck struct {
	// steps are the deleted objects to read again.
	steps []apply.StepResult
	// report is set by the first reconcile that reaches a release verdict:
	// its kept claims and left-behind objects are reported once.
	report *apply.PruneResult
	// recorded is the wait reason the object carried when the reconcile
	// started; empty on a first pass.
	recorded string
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
		if check.recorded != "" {
			return holdRecheck(ctx, env, t, check.recorded, "the read that checks whether a deleted object is gone failed", err)
		}
		// A first pass: nothing is recorded, and the next reconcile judges
		// every entry again.
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
		identity.gone = apply.IsServiceAccountGone(err, name)
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
	// recorded is the wait reason an earlier reconcile wrote when it had
	// sent every delete; empty without that record.
	recorded string
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

	if counts.recorded != "" && !identity.gone {
		// The ServiceAccount could not be looked up, or the answer was not a
		// NotFound of that ServiceAccount. Neither says that it is gone, and
		// the record releases only a ServiceAccount that is gone.
		return holdRecheck(ctx, env, t, counts.recorded,
			fmt.Sprintf("ServiceAccount %q could not be looked up", identity.namespaced(namespace)), identity.err)
	}

	if counts.recorded != "" {
		// The ServiceAccount is gone: the controller's own read of it, by
		// name and in the namespace of the deleting object, was answered
		// NotFound for that ServiceAccount. Every delete was sent by an
		// earlier reconcile of this deletion, so it has nothing left to
		// delete. The objects cannot be read without it: the event says so.
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
