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
