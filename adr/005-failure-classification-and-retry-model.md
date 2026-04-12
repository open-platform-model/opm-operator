# ADR-005: Failure Classification and Retry Model

## Status

Accepted

## Context

The reconcile loop encounters different failure modes that require different controller responses. A single "retry on error" strategy is insufficient because:

- Some failures will resolve on their own (network timeouts, temporary API unavailability).
- Some failures require the user to change the spec or source (invalid CUE, bad source reference).
- Some situations are not failures at all — the release simply cannot progress yet because upstream state is not ready.

Without explicit classification, the controller risks hot retry loops on permanent failures or unnecessary delays on transient ones. The failure model also determines which status conditions to set and whether `lastApplied*` digests advance.

## Decision

The reconcile loop classifies outcomes into three failure categories plus three success categories:

**Soft-blocked**: the release cannot progress yet, but no real failure has occurred. The source is not ready, or the artifact has not been produced yet. The controller sets `Ready=False` with reason `SourceNotReady`, does not attempt apply or prune, and waits for the next source event or light periodic retry.

**Transient failure**: the operation might succeed later without spec changes. Examples include network failures, temporary Kubernetes API errors, and temporary SSA conflicts. The controller records the failed attempt, preserves the previous successful inventory, and requeues with backoff.

**Stalled failure**: repeating the same reconcile without changing source or spec will not help. Examples include invalid source references, unsupported artifact content, CUE render failures from invalid inputs, and immutable field errors. The controller sets `Stalled=True`, avoids hot retries, and waits for a spec or source change.

The three success categories are:

- **NoOp**: desired state matches last applied state; no action needed.
- **Applied**: resources applied successfully; no prune needed.
- **AppliedAndPruned**: resources applied and stale resources pruned successfully.

Default classification rules per phase:

| Failure | Classification |
| --- | --- |
| Source not ready | Soft-blocked |
| Invalid or missing source reference | Stalled |
| Artifact fetch network error | Transient |
| Artifact content invalid or unsupported | Stalled |
| CUE render failure from invalid input | Stalled |
| Kubernetes API timeout or temporary failure | Transient |
| SSA conflict or patch failure (temporary) | Transient |
| Apply validation or immutable field error | Stalled |
| Prune delete failure from temporary API issue | Transient |
| Prune failure from ownership ambiguity | Stalled |

Key invariants:

- `lastApplied*` only advances after a fully successful reconcile.
- `status.inventory` only updates after a fully successful reconcile.
- Prune never runs after a failed apply in the same reconcile attempt.

## Consequences

**Positive:** The controller avoids hot retry loops on permanent failures, reducing cluster load and log noise. Stalled conditions clearly communicate to operators that human intervention is needed.

**Positive:** Soft-blocked handling prevents noisy failure reporting when the source is simply not ready yet. This is important for GitOps workflows where source artifacts may arrive asynchronously.

**Positive:** The invariant that inventory and `lastApplied*` only advance on full success means the status always represents a coherent, proven state.

**Negative:** Requires careful classification of each failure type per reconcile phase. Misclassifying a transient failure as stalled (or vice versa) degrades the operator experience. The classification rules are explicit defaults that may need tuning as real-world failure modes are encountered.

**Trade-off:** Stalled failures wait indefinitely for spec/source changes. If a transient issue is misclassified as stalled, the controller will not retry until the user acts. This is accepted as safer than the reverse (treating permanent failures as transient and retrying endlessly).

Related: [module-release-reconcile-loop.md](../docs/design/module-release-reconcile-loop.md)
