package reconcile

// Outcome classifies the result of a reconcile attempt.
// Drives requeue behavior and condition setting.
type Outcome int

const (
	// NoOp — all four digests match last applied. Ready=True, Reconciling=False.
	// Requeue: none (watch-driven only).
	NoOp Outcome = iota

	// Applied — resources applied successfully (no prune needed or prune disabled).
	// Ready=True, Reconciling=False. Requeue: none.
	Applied

	// AppliedAndPruned — resources applied and stale resources pruned.
	// Ready=True, Reconciling=False. Requeue: none.
	AppliedAndPruned

	// FailedTransient — temporary failure (network, API server).
	// Ready=False, Reconciling=True. Requeue: exponential backoff.
	FailedTransient

	// FailedStalled — permanent failure (invalid config, invalid module).
	// Ready=False, Stalled=True. Requeue: none (wait for spec change).
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
