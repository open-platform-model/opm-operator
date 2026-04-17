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
	SuspendedReason               = "Suspended"
	ResolutionFailedReason        = "ResolutionFailed"
	RenderFailedReason            = "RenderFailed"
	ApplyFailedReason             = "ApplyFailed"
	PruneFailedReason             = "PruneFailed"
	ImpersonationFailedReason     = "ImpersonationFailed"
	ReconciliationSucceededReason = "ReconciliationSucceeded"
	DriftDetectedReason           = "DriftDetected"

	// Event-only reasons (no corresponding condition).
	AppliedReason = "Applied"
	PrunedReason  = "Pruned"
	ResumedReason = "Resumed"
	NoOpReason    = "NoOp"
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
	conditions.Delete(obj, ReconcilingCondition)
	conditions.Delete(obj, StalledCondition)
	conditions.MarkTrue(obj, ReadyCondition, ReconciliationSucceededReason, messageFormat, messageArgs...)
}

// MarkSuspended sets Ready=False with reason Suspended and removes Reconciling and Stalled conditions.
func MarkSuspended(obj conditions.Setter) {
	conditions.Delete(obj, ReconcilingCondition)
	conditions.Delete(obj, StalledCondition)
	conditions.MarkFalse(obj, ReadyCondition, SuspendedReason, "Reconciliation is suspended")
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
