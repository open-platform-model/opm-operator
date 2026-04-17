package status

import (
	"testing"

	"github.com/stretchr/testify/assert"

	releasesv1alpha1 "github.com/open-platform-model/poc-controller/api/v1alpha1"
)

func TestEnsureCounters_InitializesNil(t *testing.T) {
	status := &releasesv1alpha1.ModuleReleaseStatus{}
	assert.Nil(t, status.FailureCounters)

	counters := EnsureCounters(status)

	assert.NotNil(t, counters)
	assert.Equal(t, int64(0), counters.Reconcile)
	assert.Equal(t, int64(0), counters.Apply)
	assert.Equal(t, int64(0), counters.Prune)
	assert.Equal(t, int64(0), counters.Drift)
}

func TestEnsureCounters_PreservesExisting(t *testing.T) {
	status := &releasesv1alpha1.ModuleReleaseStatus{
		FailureCounters: &releasesv1alpha1.FailureCounters{
			Reconcile: 3,
			Apply:     1,
		},
	}

	counters := EnsureCounters(status)

	assert.Equal(t, int64(3), counters.Reconcile)
	assert.Equal(t, int64(1), counters.Apply)
}

func TestIncrementCounter(t *testing.T) {
	counters := &releasesv1alpha1.FailureCounters{}

	IncrementCounter(counters, CounterReconcile)
	assert.Equal(t, int64(1), counters.Reconcile)

	IncrementCounter(counters, CounterReconcile)
	assert.Equal(t, int64(2), counters.Reconcile)

	IncrementCounter(counters, CounterApply)
	assert.Equal(t, int64(1), counters.Apply)
	assert.Equal(t, int64(2), counters.Reconcile, "other counters unaffected")

	IncrementCounter(counters, CounterPrune)
	assert.Equal(t, int64(1), counters.Prune)

	IncrementCounter(counters, CounterDrift)
	assert.Equal(t, int64(1), counters.Drift)
}

func TestIncrementCounter_UnknownField(t *testing.T) {
	counters := &releasesv1alpha1.FailureCounters{Reconcile: 5}
	IncrementCounter(counters, "unknown")
	assert.Equal(t, int64(5), counters.Reconcile, "no counter changed")
}

func TestResetCounter(t *testing.T) {
	counters := &releasesv1alpha1.FailureCounters{
		Reconcile: 5,
		Apply:     3,
		Prune:     2,
		Drift:     1,
	}

	ResetCounter(counters, CounterApply)
	assert.Equal(t, int64(0), counters.Apply)
	assert.Equal(t, int64(5), counters.Reconcile, "other counters unaffected")
	assert.Equal(t, int64(2), counters.Prune, "other counters unaffected")
	assert.Equal(t, int64(1), counters.Drift, "other counters unaffected")

	ResetCounter(counters, CounterReconcile)
	assert.Equal(t, int64(0), counters.Reconcile)

	ResetCounter(counters, CounterPrune)
	assert.Equal(t, int64(0), counters.Prune)

	ResetCounter(counters, CounterDrift)
	assert.Equal(t, int64(0), counters.Drift)
}

func TestResetCounter_UnknownField(t *testing.T) {
	counters := &releasesv1alpha1.FailureCounters{Reconcile: 5}
	ResetCounter(counters, "unknown")
	assert.Equal(t, int64(5), counters.Reconcile, "no counter changed")
}
