package status

import (
	"github.com/fluxcd/pkg/apis/meta"
	"github.com/fluxcd/pkg/runtime/conditions"
)

// Condition types.
// Ready, Reconciling, and Stalled are reexported from Flux meta for consistency.
const (
	ReadyCondition          = meta.ReadyCondition       // "Ready"
	ReconcilingCondition    = meta.ReconcilingCondition // "Reconciling"
	StalledCondition        = meta.StalledCondition     // "Stalled"
	ModuleResolvedCondition = "ModuleResolved"
	DriftedCondition        = "Drifted"

	// ActiveCondition reports whether an accepted TransformerRegistration's
	// provider is serving, the second of the three states enhancement 0015 D3
	// defines. It is a separate condition from Ready because the two axes are
	// independent: Ready carries the acceptance verdict, and a claim can be
	// accepted and not yet active. Once Active=True it never goes False again
	// (see the latch in transformerregistration_controller.go).
	ActiveCondition = "Active"
)

// Condition reasons.
const (
	SuspendedReason        = "Suspended"
	ResolutionFailedReason = "ResolutionFailed"
	RenderFailedReason     = "RenderFailed"
	// SkewRefusedReason: Ready=False, the platform's skew policy is Refuse and
	// the module requires a newer catalog build than the platform pins
	// (enhancement 0019 D7/D18). The fix is a platform pin bump or a module
	// downgrade, so it is distinct from RenderFailed.
	SkewRefusedReason             = "SkewRefused"
	ApplyFailedReason             = "ApplyFailed"
	PruneFailedReason             = "PruneFailed"
	ImpersonationFailedReason     = "ImpersonationFailed"
	DeletionSAMissingReason       = "DeletionSAMissing"
	OrphanedOnDeletionReason      = "OrphanedOnDeletion"
	ReconciliationSucceededReason = "ReconciliationSucceeded"
	DriftDetectedReason           = "DriftDetected"
	ManagedExternallyReason       = "ManagedExternally"

	// Platform-specific reasons (enhancement 0019 D6: the reconciler generates
	// and builds the platform module).
	GeneratedReason      = "Generated"      // Ready=True: the platform module was generated and built.
	GenerateFailedReason = "GenerateFailed" // Ready=False: the module could not be written to disk.
	BuildFailedReason    = "BuildFailed"    // Ready=False: a dependency did not resolve or the module did not build.

	// ModulePackage-specific reasons.
	SourceNotReadyReason = "SourceNotReady"
	FetchFailedReason    = "FetchFailed"
	PathNotFoundReason   = "PathNotFound"
	// Was: ReleaseFileNotFoundReason = "ReleaseFileNotFound"
	InstanceFileNotFoundReason = "InstanceFileNotFound"
	UnsupportedKindReason      = "UnsupportedKind"
	DependenciesNotReadyReason = "DependenciesNotReady"
	PlatformNotReadyReason     = "PlatformNotReady"

	// TransformerRegistration-specific reasons (enhancement 0015 D3: a claim
	// carries a verdict). Every refusal gets a reason of its own: a claimant
	// acts on the reason, and collapsing two causes into one sends them to
	// the wrong fix.
	// AcceptedReason: Ready=True, the claim passed every check. It is not
	// active: acceptance changes what a claim reports, not what it does.
	AcceptedReason = "Accepted"

	// CatalogUnresolvedReason: the claimed coordinate resolves to nothing. A
	// registry or coordinate problem, distinct from CatalogWrongKind, which
	// is an authoring one — collapsing the two sends the claimant to the
	// wrong fix (enhancement 0015 D10).
	CatalogUnresolvedReason = "CatalogUnresolved"
	CatalogWrongKindReason  = "CatalogWrongKind"

	// ProvidesMismatchReason: the contract set re-derived from the catalog is
	// not exactly what the claim lists (enhancement 0015 D11).
	ProvidesMismatchReason = "ProvidesMismatch"

	// ProviderMismatchReason: the claim did not come from the ModuleInstance
	// its providerRef names — the instance is absent, or its inventory
	// settled without this claim (enhancement 0015 D11).
	ProviderMismatchReason = "ProviderMismatch"

	// ProviderInventoryPendingReason: Ready=Unknown, the naming instance has
	// not written an inventory yet. A race, not a verdict.
	ProviderInventoryPendingReason = "ProviderInventoryPending"

	// BuildIncompatibleReason: the claimed catalog requires a shared
	// OPM-namespace path at a version the platform did not resolve to
	// (enhancement 0015 D8). Refused at acceptance rather than at render,
	// where the failure would name an unrelated module instance.
	BuildIncompatibleReason = "BuildIncompatible"

	// ProviderReadyReason: Active=True, the provider ModuleInstance the claim
	// names reports Ready=True, so its CRDs exist and its transformers can be
	// rendered against (enhancement 0015 D3). Latched: the claim keeps this
	// condition through a later provider outage.
	ProviderReadyReason = "ProviderReady"

	// ProviderNotReadyReason: Active=False, the claim is accepted and its
	// provider has not reported Ready yet. An install-ordering state, not a
	// health report — a claim only ever waits here before its first
	// activation.
	ProviderNotReadyReason = "ProviderNotReady"

	// ContractSubscribedReason: the claim provides a contract an enabled
	// Platform.spec.registry subscription's catalog already provides
	// (enhancement 0015 D2). Distinct from ContractClaimed because the fix is
	// a different object: a platform edit, not a module removal.
	ContractSubscribedReason = "ContractSubscribed"

	// ContractClaimedReason: the claim provides a contract another ACTIVE
	// claim already provides. An accepted but inactive claim holds nothing,
	// so it never produces this refusal.
	ContractClaimedReason = "ContractClaimed"

	// DuplicateClaimReason: another claim already holds this provider catalog
	// (enhancement 0015 D12). The message names the holder, so an operator
	// can see which object to remove.
	DuplicateClaimReason = "DuplicateClaim"

	// DependentsRemainReason: the claim's deletion is blocked because
	// instances still demand contracts it provides (enhancement 0015 D3).
	// The message names how many, so the operator's next action is to
	// remove those instances rather than to guess what is holding the
	// object. It is not an acceptance verdict: a blocked claim stays
	// accepted and active, because it is still serving the dependents the
	// block exists to protect.
	DependentsRemainReason = "DependentsRemain"

	// Event-only reasons (no corresponding condition).
	AppliedReason = "Applied"
	PrunedReason  = "Pruned"
	ResumedReason = "Resumed"
	NoOpReason    = "NoOp"
	// RenderWarningReason is the Warning event a successful render's advisory
	// messages (catalog skew under Warn, unhandled optional traits) are
	// emitted under, once per distinct message when the object's warning set
	// changes.
	RenderWarningReason = "RenderWarning"
)

// MarkReconciling sets Reconciling=True, removes Stalled, and sets Ready=Unknown.
func MarkReconciling(obj conditions.Setter, reason, messageFormat string, messageArgs ...any) {
	conditions.MarkReconciling(obj, reason, messageFormat, messageArgs...)
	conditions.MarkUnknown(obj, ReadyCondition, reason, messageFormat, messageArgs...)
}

// MarkStalled sets Stalled=True, removes Reconciling, and sets Ready=False.
func MarkStalled(obj conditions.Setter, reason, messageFormat string, messageArgs ...any) {
	conditions.MarkStalled(obj, reason, messageFormat, messageArgs...)
	conditions.MarkFalse(obj, ReadyCondition, reason, messageFormat, messageArgs...)
}

// MarkReady sets Ready=True and removes Reconciling and Stalled conditions.
func MarkReady(obj conditions.Setter, messageFormat string, messageArgs ...any) {
	MarkReadyWithReason(obj, ReconciliationSucceededReason, messageFormat, messageArgs...)
}

// MarkReadyWithReason sets Ready=True with an explicit reason and removes
// Reconciling and Stalled conditions. Used where the success reason is not the
// generic ReconciliationSucceeded (e.g. the Platform's Generated reason).
func MarkReadyWithReason(obj conditions.Setter, reason, messageFormat string, messageArgs ...any) {
	conditions.Delete(obj, ReconcilingCondition)
	conditions.Delete(obj, StalledCondition)
	conditions.MarkTrue(obj, ReadyCondition, reason, messageFormat, messageArgs...)
}

// MarkSuspended sets Ready=False with reason Suspended and removes Reconciling and Stalled conditions.
func MarkSuspended(obj conditions.Setter) {
	conditions.Delete(obj, ReconcilingCondition)
	conditions.Delete(obj, StalledCondition)
	conditions.MarkFalse(obj, ReadyCondition, SuspendedReason, "Reconciliation is suspended")
}

// MarkManagedExternally sets Ready=Unknown with reason ManagedExternally and
// removes Reconciling and Stalled conditions. Used by the owner-skip gate for
// CLI-owned instances the operator deliberately does not reconcile. The static
// message keeps the write idempotent: re-acknowledging an already-marked
// instance produces an empty patch diff.
func MarkManagedExternally(obj conditions.Setter) {
	conditions.Delete(obj, ReconcilingCondition)
	conditions.Delete(obj, StalledCondition)
	conditions.MarkUnknown(obj, ReadyCondition, ManagedExternallyReason, "ModuleInstance is managed externally by the CLI")
}

// MarkNotReady sets Ready=False with the given reason and message.
func MarkNotReady(obj conditions.Setter, reason, messageFormat string, messageArgs ...any) {
	conditions.MarkFalse(obj, ReadyCondition, reason, messageFormat, messageArgs...)
}

// MarkDrifted sets Drifted=True with a message indicating the number of drifted resources.
// Drift is informational only — does not affect Ready condition.
func MarkDrifted(obj conditions.Setter, count int) {
	conditions.MarkTrue(obj, DriftedCondition, DriftDetectedReason,
		"%d resource(s) drifted from desired state", count)
}

// ClearDrifted removes the Drifted condition (drift resolved by successful apply).
func ClearDrifted(obj conditions.Setter) {
	conditions.Delete(obj, DriftedCondition)
}

// MarkModuleResolved sets ModuleResolved=True indicating the CUE module was
// successfully resolved from the OCI registry.
func MarkModuleResolved(obj conditions.Setter, moduleRef string) {
	conditions.MarkTrue(obj, ModuleResolvedCondition, "ModuleResolved", "module resolved: %s", moduleRef)
}
