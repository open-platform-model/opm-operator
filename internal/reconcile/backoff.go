package reconcile

import (
	"math"
	"time"
)

const (
	// BackoffBaseDelay is the initial retry delay after the first transient failure.
	BackoffBaseDelay = 5 * time.Second

	// BackoffMaxDelay is the maximum retry delay (cap for exponential growth).
	BackoffMaxDelay = 5 * time.Minute

	// StalledRecheckInterval is the periodic safety recheck for stalled failures,
	// guarding against misclassification.
	StalledRecheckInterval = 30 * time.Minute
)

// ComputeBackoff returns the exponential backoff delay for a given failure count.
// Formula: min(baseDelay * 2^(failures-1), maxDelay).
// Returns baseDelay when failureCount is 0 or 1.
func ComputeBackoff(failureCount int64) time.Duration {
	if failureCount <= 1 {
		return BackoffBaseDelay
	}
	delay := float64(BackoffBaseDelay) * math.Pow(2, float64(failureCount-1))
	if delay > float64(BackoffMaxDelay) {
		return BackoffMaxDelay
	}
	return time.Duration(delay)
}
