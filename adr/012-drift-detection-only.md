# ADR-012: Drift Detection Only in v1alpha1

## Status

Accepted

## Context

Configuration drift occurs when the live state of a resource diverges from the rendered desired state for fields managed by `opm-controller`. This can happen through manual `kubectl` edits, mutating admission webhooks, or other controllers modifying the same resources.

Two approaches to drift exist:

1. **Automatic correction**: the controller detects drift and immediately re-applies desired state to revert the change.

2. **Detection only**: the controller detects drift, reports it through a status condition, and takes no corrective action.

Automatic correction is dangerous in early controller iterations. Mutating webhooks (Istio sidecar injector, Linkerd proxy injector, security policy injectors) routinely modify fields on resources after they are created. If the controller considers those modifications as drift and reverts them, the webhook will re-apply them, creating an infinite reconcile loop. Both the controller and the webhook fight over the same fields indefinitely, generating excessive API server load and constant object churn.

## Decision

In v1alpha1, the controller detects drift but does not automatically correct it.

Detection mechanism: during the `PlanActions` phase, the controller may use an SSA dry-run to compare live state against desired state for fields owned by `opm-controller`.

Reporting: if drift is found, the controller sets a `Drifted=True` condition on the `ModuleRelease` status. The reconcile loop completes normally without re-applying.

No corrective action is taken. The operator must decide whether the drift is intentional (webhook-injected sidecars, manual operational overrides) or accidental (unauthorized changes).

A future API revision will introduce `spec.rollout.driftCorrection: true` as an opt-in mechanism for automatic continuous enforcement of desired state, once the controller has enough operational experience to handle webhook and multi-writer interactions safely.

## Consequences

**Positive:** No risk of infinite reconcile loops with mutating webhooks. The controller and webhooks coexist peacefully because the controller does not fight over fields it detects as drifted.

**Positive:** Operators are informed about drift through the `Drifted` condition and can investigate before deciding whether correction is appropriate.

**Positive:** The reconcile loop structure (Phase 4: Plan Actions) is already designed to accommodate future drift correction as an extension without restructuring.

**Negative:** Drift accumulates until the operator acts. If a critical field is modified (e.g., container image changed by an unauthorized actor), the controller reports it but does not revert it.

**Trade-off:** This is a deliberate limitation for v1alpha1. The controller prioritizes safety and predictability over automatic enforcement. As operational patterns become clear, drift correction can be enabled per-release through the opt-in API field.

Related: [ssa-ownership-and-drift-policy.md](../docs/design/ssa-ownership-and-drift-policy.md), [module-release-reconcile-loop.md](../docs/design/module-release-reconcile-loop.md)
