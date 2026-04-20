package reconcile

import (
	"testing"

	"github.com/stretchr/testify/assert"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
)

func TestUpdateFailureCounters_IndependentReset(t *testing.T) {
	mrStatus := &releasesv1alpha1.ModuleReleaseStatus{
		FailureCounters: &releasesv1alpha1.FailureCounters{
			Apply: 3,
			Prune: 2,
		},
	}

	// Apply succeeded, prune failed.
	phases := phaseOutcomes{
		applyRan:    true,
		applyFailed: false,
		pruneRan:    true,
		pruneFailed: true,
	}

	updateFailureCounters(mrStatus, FailedTransient, phases)

	assert.Equal(t, int64(0), mrStatus.FailureCounters.Apply, "apply counter should reset on success")
	assert.Equal(t, int64(3), mrStatus.FailureCounters.Prune, "prune counter should increment on failure")
	assert.Equal(t, int64(1), mrStatus.FailureCounters.Reconcile, "reconcile counter should increment on failure")
}

func TestUpdateFailureCounters_SuccessResetsAll(t *testing.T) {
	mrStatus := &releasesv1alpha1.ModuleReleaseStatus{
		FailureCounters: &releasesv1alpha1.FailureCounters{
			Reconcile: 5,
			Apply:     3,
			Prune:     2,
			Drift:     1,
		},
	}

	phases := phaseOutcomes{
		driftRan: true,
		applyRan: true,
		pruneRan: true,
	}

	updateFailureCounters(mrStatus, Applied, phases)

	assert.Equal(t, int64(0), mrStatus.FailureCounters.Reconcile)
	assert.Equal(t, int64(0), mrStatus.FailureCounters.Apply)
	assert.Equal(t, int64(0), mrStatus.FailureCounters.Prune)
	assert.Equal(t, int64(0), mrStatus.FailureCounters.Drift)
}

func TestUpdateFailureCounters_NoPhaseRan(t *testing.T) {
	mrStatus := &releasesv1alpha1.ModuleReleaseStatus{
		FailureCounters: &releasesv1alpha1.FailureCounters{
			Apply: 3,
			Prune: 2,
			Drift: 1,
		},
	}

	// Early failure before any phase ran (e.g., source not found).
	phases := phaseOutcomes{}

	updateFailureCounters(mrStatus, FailedStalled, phases)

	// Only reconcile counter should change.
	assert.Equal(t, int64(1), mrStatus.FailureCounters.Reconcile, "reconcile incremented")
	assert.Equal(t, int64(3), mrStatus.FailureCounters.Apply, "apply untouched")
	assert.Equal(t, int64(2), mrStatus.FailureCounters.Prune, "prune untouched")
	assert.Equal(t, int64(1), mrStatus.FailureCounters.Drift, "drift untouched")
}

func TestUpdateFailureCounters_NilCountersInitialized(t *testing.T) {
	mrStatus := &releasesv1alpha1.ModuleReleaseStatus{}

	phases := phaseOutcomes{driftRan: true}

	updateFailureCounters(mrStatus, NoOp, phases)

	assert.NotNil(t, mrStatus.FailureCounters)
	assert.Equal(t, int64(0), mrStatus.FailureCounters.Reconcile)
	assert.Equal(t, int64(0), mrStatus.FailureCounters.Drift)
}
