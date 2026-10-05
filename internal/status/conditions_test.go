package status

import (
	"testing"

	"github.com/fluxcd/pkg/runtime/conditions"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
)

// Compile-time interface compliance checks.
var _ conditions.Getter = (*releasesv1alpha1.ModuleInstance)(nil)
var _ conditions.Setter = (*releasesv1alpha1.ModuleInstance)(nil)

func newModuleInstance() *releasesv1alpha1.ModuleInstance {
	return &releasesv1alpha1.ModuleInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test",
			Namespace:  "default",
			Generation: 1,
		},
	}
}

func TestMarkReconciling(t *testing.T) {
	obj := newModuleInstance()
	MarkReconciling(obj, SuspendedReason, "starting reconciliation")

	assert.True(t, conditions.IsTrue(obj, ReconcilingCondition))
	assert.True(t, conditions.IsUnknown(obj, ReadyCondition))
	assert.False(t, conditions.Has(obj, StalledCondition))
}

func TestMarkReconciling_RemovesStalled(t *testing.T) {
	obj := newModuleInstance()
	MarkStalled(obj, RenderFailedReason, "render error")
	assert.True(t, conditions.Has(obj, StalledCondition))

	MarkReconciling(obj, SuspendedReason, "retrying")
	assert.False(t, conditions.Has(obj, StalledCondition))
	assert.True(t, conditions.IsTrue(obj, ReconcilingCondition))
}

func TestMarkStalled(t *testing.T) {
	obj := newModuleInstance()
	MarkStalled(obj, RenderFailedReason, "render error")

	assert.True(t, conditions.IsTrue(obj, StalledCondition))
	assert.True(t, conditions.IsFalse(obj, ReadyCondition))
	assert.False(t, conditions.Has(obj, ReconcilingCondition))
}

func TestMarkStalled_RemovesReconciling(t *testing.T) {
	obj := newModuleInstance()
	MarkReconciling(obj, SuspendedReason, "working")
	assert.True(t, conditions.Has(obj, ReconcilingCondition))

	MarkStalled(obj, ApplyFailedReason, "apply error")
	assert.False(t, conditions.Has(obj, ReconcilingCondition))
	assert.True(t, conditions.IsTrue(obj, StalledCondition))
}

func TestMarkReady(t *testing.T) {
	obj := newModuleInstance()
	// Set up pre-existing conditions that should be cleared.
	MarkReconciling(obj, SuspendedReason, "working")

	MarkReady(obj, "all resources applied")

	assert.True(t, conditions.IsTrue(obj, ReadyCondition))
	assert.False(t, conditions.Has(obj, ReconcilingCondition))
	assert.False(t, conditions.Has(obj, StalledCondition))
	assert.Equal(t, ReconciliationSucceededReason, conditions.GetReason(obj, ReadyCondition))
}

func TestMarkSuspended(t *testing.T) {
	obj := newModuleInstance()
	// Set up pre-existing conditions that should be cleared.
	MarkReconciling(obj, "Progressing", "working")
	MarkStalled(obj, RenderFailedReason, "render error")

	MarkSuspended(obj)

	assert.True(t, conditions.IsFalse(obj, ReadyCondition))
	assert.Equal(t, SuspendedReason, conditions.GetReason(obj, ReadyCondition))
	assert.Equal(t, "Reconciliation is suspended", conditions.GetMessage(obj, ReadyCondition))
	assert.False(t, conditions.Has(obj, ReconcilingCondition))
	assert.False(t, conditions.Has(obj, StalledCondition))
}

func TestMarkSelfManagementRefused(t *testing.T) {
	obj := newModuleInstance()
	// Conditions an earlier adoption may have left behind.
	MarkModuleResolved(obj, "opmodel.dev/modules/opm_operator@v0")
	MarkDrifted(obj, 2)
	MarkReconciling(obj, "Progressing", "working")

	MarkSelfManagementRefused(obj, "this ModuleInstance deploys the operator")

	assert.True(t, conditions.IsFalse(obj, ReadyCondition))
	assert.Equal(t, SelfManagementRefusedReason, conditions.GetReason(obj, ReadyCondition))
	assert.Equal(t, "this ModuleInstance deploys the operator", conditions.GetMessage(obj, ReadyCondition))
	assert.True(t, conditions.IsTrue(obj, StalledCondition))
	assert.Equal(t, SelfManagementRefusedReason, conditions.GetReason(obj, StalledCondition))
	assert.False(t, conditions.Has(obj, ReconcilingCondition))
	assert.False(t, conditions.Has(obj, ModuleResolvedCondition))
	assert.False(t, conditions.Has(obj, DriftedCondition))
}

func TestMarkNotReady(t *testing.T) {
	obj := newModuleInstance()
	MarkNotReady(obj, RenderFailedReason, "render failed: invalid values")

	assert.True(t, conditions.IsFalse(obj, ReadyCondition))
	assert.Equal(t, RenderFailedReason, conditions.GetReason(obj, ReadyCondition))
	assert.Equal(t, "render failed: invalid values", conditions.GetMessage(obj, ReadyCondition))
}

func TestMarkReconcilePanic(t *testing.T) {
	obj := newModuleInstance()
	// Ready=True and Stalled=True together: the worst state a panic can
	// start from, both of which the helper must replace.
	MarkReady(obj, "Reconciliation succeeded")
	conditions.MarkStalled(obj, RenderFailedReason, "a stall left behind")
	assert.True(t, conditions.IsTrue(obj, ReadyCondition))
	assert.True(t, conditions.Has(obj, StalledCondition))

	MarkReconcilePanic(obj, "reconcile panicked: %v", "boom")

	assert.True(t, conditions.IsFalse(obj, ReadyCondition))
	assert.Equal(t, ReconcilePanicReason, conditions.GetReason(obj, ReadyCondition))
	assert.Equal(t, "reconcile panicked: boom", conditions.GetMessage(obj, ReadyCondition))
	assert.True(t, conditions.IsTrue(obj, ReconcilingCondition))
	assert.Equal(t, ReconcilePanicReason, conditions.GetReason(obj, ReconcilingCondition))
	assert.False(t, conditions.Has(obj, StalledCondition), "a panic is not stalled")
}

func TestMarkRenderTimedOut(t *testing.T) {
	obj := newModuleInstance()
	MarkReady(obj, "Reconciliation succeeded")
	conditions.MarkStalled(obj, RenderFailedReason, "a stall left behind")

	MarkRenderTimedOut(obj, "render did not finish within %s", "10m0s")

	assert.True(t, conditions.IsFalse(obj, ReadyCondition))
	assert.Equal(t, RenderTimedOutReason, conditions.GetReason(obj, ReadyCondition))
	assert.Equal(t, "render did not finish within 10m0s", conditions.GetMessage(obj, ReadyCondition))
	assert.True(t, conditions.IsTrue(obj, ReconcilingCondition))
	assert.Equal(t, RenderTimedOutReason, conditions.GetReason(obj, ReconcilingCondition))
	assert.False(t, conditions.Has(obj, StalledCondition), "a render timeout is not stalled")
}

func TestMarkModuleResolved(t *testing.T) {
	obj := newModuleInstance()
	MarkModuleResolved(obj, "opmodel.dev/modules/hello@v0@v0.1.0")

	assert.True(t, conditions.IsTrue(obj, ModuleResolvedCondition))
	assert.Contains(t, conditions.GetMessage(obj, ModuleResolvedCondition), "opmodel.dev/modules/hello@v0@v0.1.0")
}

func TestMarkModuleResolved_Overwrite(t *testing.T) {
	obj := newModuleInstance()
	// Manually set a False condition to verify overwrite.
	conditions.MarkFalse(obj, ModuleResolvedCondition, "Failed", "initial failure")
	assert.True(t, conditions.IsFalse(obj, ModuleResolvedCondition))

	MarkModuleResolved(obj, "opmodel.dev/test@v0@v0.2.0")
	assert.True(t, conditions.IsTrue(obj, ModuleResolvedCondition))
}

func TestConditionConstants(t *testing.T) {
	assert.Equal(t, "Ready", ReadyCondition)
	assert.Equal(t, "Reconciling", ReconcilingCondition)
	assert.Equal(t, "Stalled", StalledCondition)
	assert.Equal(t, "ModuleResolved", ModuleResolvedCondition)
}

func TestReasonConstants(t *testing.T) {
	reasons := []string{
		SuspendedReason,
		ResolutionFailedReason,
		RenderFailedReason,
		ApplyFailedReason,
		PruneFailedReason,
		ReconciliationSucceededReason,
		SelfManagementRefusedReason,
		ReconcilePanicReason,
		RenderTimedOutReason,
	}
	for _, r := range reasons {
		assert.NotEmpty(t, r, "reason constant should not be empty")
	}
}

func TestHealthyConstants(t *testing.T) {
	assert.Equal(t, "Healthy", HealthyCondition)
	assert.Equal(t, "RolledOut", RolledOutReason)
	assert.Equal(t, "NotRolledOut", NotRolledOutReason)
	assert.Equal(t, "ProgressDeadlineExceeded", ProgressDeadlineExceededReason)
	assert.Equal(t, "HealthUnknown", HealthUnknownReason)
}

func TestMarkHealthy_SetsOnlyHealthy(t *testing.T) {
	for _, s := range []metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionFalse, metav1.ConditionUnknown} {
		obj := newModuleInstance()
		MarkReady(obj, "applied")
		ClearDrifted(obj)
		MarkDrifted(obj, 1)
		before := conditions.Get(obj, ReadyCondition).DeepCopy()

		MarkHealthy(obj, s, RolledOutReason, "%d/%d objects ready", 1, 1)

		got := conditions.Get(obj, HealthyCondition)
		if assert.NotNil(t, got) {
			assert.Equal(t, s, got.Status)
			assert.Equal(t, RolledOutReason, got.Reason)
			assert.Equal(t, "1/1 objects ready", got.Message)
		}
		assert.Equal(t, before, conditions.Get(obj, ReadyCondition))
		assert.True(t, conditions.IsTrue(obj, DriftedCondition))
		assert.Len(t, obj.GetConditions(), 3)
	}
}

func TestReadyHelpers_LeaveHealthyAlone(t *testing.T) {
	marks := map[string]func(*releasesv1alpha1.ModuleInstance){
		"MarkReady":       func(o *releasesv1alpha1.ModuleInstance) { MarkReady(o, "applied") },
		"MarkReconciling": func(o *releasesv1alpha1.ModuleInstance) { MarkReconciling(o, "Progressing", "working") },
		"MarkStalled":     func(o *releasesv1alpha1.ModuleInstance) { MarkStalled(o, RenderFailedReason, "bad") },
		"MarkSuspended":   func(o *releasesv1alpha1.ModuleInstance) { MarkSuspended(o) },
		"MarkNotReady":    func(o *releasesv1alpha1.ModuleInstance) { MarkNotReady(o, ApplyFailedReason, "bad") },
	}
	for name, mark := range marks {
		t.Run(name, func(t *testing.T) {
			obj := newModuleInstance()
			MarkHealthy(obj, metav1.ConditionFalse, NotRolledOutReason, "0/1 objects ready")
			before := conditions.Get(obj, HealthyCondition).DeepCopy()
			mark(obj)
			assert.Equal(t, before, conditions.Get(obj, HealthyCondition))
		})
	}
}

func TestOwnerHelpers_RemoveHealthy(t *testing.T) {
	obj := newModuleInstance()
	MarkHealthy(obj, metav1.ConditionTrue, RolledOutReason, "1/1 objects ready")
	MarkManagedExternally(obj)
	assert.False(t, conditions.Has(obj, HealthyCondition))

	obj = newModuleInstance()
	MarkHealthy(obj, metav1.ConditionTrue, RolledOutReason, "1/1 objects ready")
	MarkSelfManagementRefused(obj, "this ModuleInstance deploys the operator")
	assert.False(t, conditions.Has(obj, HealthyCondition))
}
