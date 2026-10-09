package reconcile

import (
	"context"
	"fmt"

	"github.com/fluxcd/pkg/runtime/conditions"
	corev1 "k8s.io/api/core/v1"
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
func runDeletionCleanup(ctx context.Context, env deletionEnv, t deletionTarget) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	log.Info("Running deletion cleanup for " + t.kind)

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
		return actOnLostIdentity(ctx, env, t, identity, verdict, len(claims))
	}

	cleanup, err := apply.RunCleanup(ctx, identity.client, t.identities, t.entries, policy,
		apply.PruneOptions{DeleteData: t.deleteData})
	if err != nil {
		return ctrl.Result{}, err
	}
	verdict := holdVerdict(cleanup.Deletion)
	if !verdict.Release {
		return actOnHold(ctx, env, t, identity, verdict, cleanup)
	}

	log.Info("Deletion cleanup pruned resources",
		"deleted", cleanup.Report.Deleted, "skipped", cleanup.Report.Skipped, "keptClaims", len(cleanup.Report.Kept))
	// Reported before the finalizer goes: afterwards the object, and with it
	// the inventory that named the claims, no longer exists.
	reportKeptClaims(env.recorder, t.obj, "Delete", cleanup.Report.Kept)
	reportLeftBehind(env.recorder, t.obj, "Delete", cleanup.Report.Left)
	return releaseFinalizer(ctx, t)
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

// actOnLostIdentity acts on the verdict of a cleanup whose deleting identity
// is missing or failed. No object is read or deleted, and the controller's
// own client is not used in its place.
func actOnLostIdentity(
	ctx context.Context,
	env deletionEnv,
	t deletionTarget,
	identity identityState,
	verdict lifecycle.HoldVerdict,
	claims int,
) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	namespace := t.obj.GetNamespace()

	switch verdict.Because {
	case lifecycle.HoldInventoryEmpty:
		// Only kept claims are left: the cleanup would delete nothing, so
		// the lost identity has nothing to do. The claims cannot be read
		// without it, so no ClaimsKept event would be true.
		log.Info("Only kept PersistentVolumeClaims are left and the deleting identity is unavailable; releasing without a read",
			"serviceAccount", identity.name, "serviceAccountSource", identity.source, "claims", claims)
		env.recorder.Eventf(t.obj, nil, corev1.EventTypeWarning, status.DeletionUnconfirmedReason, "Delete",
			"%s", status.ClaimsUnreadNote(claims, identity.namespaced(namespace), identity.unread()))
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
