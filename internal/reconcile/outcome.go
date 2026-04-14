package reconcile

// Outcome classifies the result of a reconcile attempt.
// Drives requeue behavior and condition setting.
type Outcome int

const (
	// SoftBlocked — source exists but not ready. Ready=Unknown, Reconciling=True.
	// Requeue: wait for source event or light retry.
	SoftBlocked Outcome = iota

	// NoOp — all four digests match last applied. Ready=True, Reconciling=False.
	// Requeue: none (watch-driven only).
	NoOp

	// Applied — resources applied successfully (no prune needed or prune disabled).
	// Ready=True, Reconciling=False. Requeue: none.
	Applied

	// AppliedAndPruned — resources applied and stale resources pruned.
	// Ready=True, Reconciling=False. Requeue: none.
	AppliedAndPruned

	// FailedTransient — temporary failure (network, API server).
	// Ready=False, Reconciling=True. Requeue: exponential backoff.
	FailedTransient

	// FailedStalled — permanent failure (invalid config, invalid artifact).
	// Ready=False, Stalled=True. Requeue: none (wait for spec/source change).
	FailedStalled
)

// MetricLabel returns the snake_case label value for Prometheus metrics.
func (o Outcome) MetricLabel() string {
	switch o {
	case SoftBlocked:
		return "soft_blocked"
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
	case SoftBlocked:
		return "SoftBlocked"
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
