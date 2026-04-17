package status

import (
	releasesv1alpha1 "github.com/open-platform-model/poc-controller/api/v1alpha1"
)

// Counter field names used by IncrementCounter and ResetCounter.
const (
	CounterReconcile = "reconcile"
	CounterApply     = "apply"
	CounterPrune     = "prune"
	CounterDrift     = "drift"
)

// EnsureCounters initializes status.FailureCounters if nil and returns the pointer.
// Callers can safely use the returned pointer without nil checks.
func EnsureCounters(status *releasesv1alpha1.ModuleReleaseStatus) *releasesv1alpha1.FailureCounters {
	return ensureCountersField(&status.FailureCounters)
}

// EnsureReleaseCounters is the Release equivalent of EnsureCounters.
func EnsureReleaseCounters(status *releasesv1alpha1.ReleaseStatus) *releasesv1alpha1.FailureCounters {
	return ensureCountersField(&status.FailureCounters)
}

func ensureCountersField(counters **releasesv1alpha1.FailureCounters) *releasesv1alpha1.FailureCounters {
	if *counters == nil {
		*counters = &releasesv1alpha1.FailureCounters{}
	}
	return *counters
}

// IncrementCounter increments the named failure counter by one.
// Unknown field names are silently ignored.
func IncrementCounter(counters *releasesv1alpha1.FailureCounters, field string) {
	switch field {
	case CounterReconcile:
		counters.Reconcile++
	case CounterApply:
		counters.Apply++
	case CounterPrune:
		counters.Prune++
	case CounterDrift:
		counters.Drift++
	}
}

// ResetCounter sets the named failure counter to zero.
// Unknown field names are silently ignored.
func ResetCounter(counters *releasesv1alpha1.FailureCounters, field string) {
	switch field {
	case CounterReconcile:
		counters.Reconcile = 0
	case CounterApply:
		counters.Apply = 0
	case CounterPrune:
		counters.Prune = 0
	case CounterDrift:
		counters.Drift = 0
	}
}
