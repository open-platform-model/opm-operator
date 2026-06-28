package reconcile

// Outcome classifies the result of a reconcile attempt.
// Drives requeue behavior and condition setting.
type Outcome int

const (
	// NoOp — all four digests match last applied. Ready=True, Reconciling=False.
	// Requeue: watch-driven, plus the controller's periodic interval where one
	// applies (ModulePackage requeues on spec.interval; ModuleInstance and
	// Platform are watch-only on the happy path today).
	NoOp Outcome = iota

	// Applied — resources applied successfully (no prune needed or prune disabled).
	// Ready=True, Reconciling=False. Requeue: same as NoOp (interval where the
	// controller defines one, otherwise watch-driven).
	Applied

	// AppliedAndPruned — resources applied and stale resources pruned.
	// Ready=True, Reconciling=False. Requeue: same as Applied.
	AppliedAndPruned

	// FailedTransient — temporary failure (network, API server).
	// Ready=False, Reconciling=True. Requeue: exponential backoff (ComputeBackoff).
	FailedTransient

	// FailedStalled — needs a spec or source change to resolve (invalid config,
	// invalid module). Ready=False, Stalled=True. Requeue: StalledRecheckInterval
	// (a long safety recheck guarding against misclassification), not none.
	FailedStalled
)

// MetricLabel returns the snake_case label value for Prometheus metrics.
func (o Outcome) MetricLabel() string {
	switch o {
	case NoOp:
		return "no_op"
	case Applied:
		return "applied"
	case AppliedAndPruned:
		return "applied_and_pruned"
	case FailedTransient:
		return "failed_transient"
	case FailedStalled:
		return "failed_stalled"
	default:
		return "unknown"
	}
}

// String returns a human-readable name for the outcome.
func (o Outcome) String() string {
	switch o {
	case NoOp:
		return "NoOp"
	case Applied:
		return "Applied"
	case AppliedAndPruned:
		return "AppliedAndPruned"
	case FailedTransient:
		return "FailedTransient"
	case FailedStalled:
		return "FailedStalled"
	default:
		return "Unknown"
	}
}
