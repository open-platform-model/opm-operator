# Why TransformerRegistration activation latches

Status: Current (2026-09-15). Implemented in
`internal/controller/transformerregistration_controller.go` (`gateActivation`).

A `TransformerRegistration` reports three states, from 0015:D3: not
yet judged, accepted but inactive, and active. It becomes active when the
`ModuleInstance` its `spec.providerRef` names reports `Ready=True`. It never
becomes inactive again. A claim leaves the active state by deletion, and by
nothing else.

This document exists because that rule reads like an oversight. The obvious
implementation mirrors the provider's `Ready` condition onto `status.active`
every reconcile, which is fewer lines, and someone will eventually read the
one-way gate as a bug and "fix" it. It is not a bug. This is what the fix would
cost.

## The gate is about install ordering, not health

A subscription in `Platform.spec.registry` is live the moment it is written.
A registration claim cannot be: the transformers a provider catalog ships render
against custom resource definitions that exist only once the provider itself is
installed and running. The readiness gate is what expresses *yet* — the
provider's definitions do not exist **yet** — and the wait ends the first time
the provider comes up.

Steady-state health is a different question, and the gate does not answer it.
Custom resource definitions are cluster-scoped objects that outlive the pods
that installed them. A provider whose deployment is scaled to zero, is
mid-rollout, or is failing its probes has not withdrawn the definitions its
transformers render against, so a claim that stays active through the outage is
reporting the truth.

## What live-tracking would cost

The set of active claims is an input to platform-package regeneration
(0015:D13): the generated platform carries the catalog of every
active claim, and every module instance in the fleet renders against that
package.

Mirroring provider health onto `status.active` therefore wires pod-level
liveness to fleet-wide rendering:

1. A provider's pods restart, so its `ModuleInstance` reports `Ready=False`
1. The claim deactivates, so the active set changes
1. The platform package is regenerated without the provider's catalog
1. Every module instance whose components use that catalog's contracts fails to
   render, because the transformers it needs are no longer on the platform
1. The provider recovers seconds later, the set changes back, and the platform
   is regenerated again

The blast radius is the whole fleet, the trigger is transient, and the failures
name the wrong objects: the render errors land on unrelated tenants' instances
rather than on the provider that flapped. None of that is visible from the two
lines of code that would cause it.

## How the rule is enforced

`gateActivation` reads `status.active` first and returns before the readiness
check when it is already true. No path through the function assigns `false`.
That is deliberate: the latch is a property of the control flow rather than a
convention a later reader has to know about, so an edit to the readiness check
cannot reintroduce flapping by accident.

The claim's `Active` condition records the transition and is left alone on the
same early return, so a provider going unready and recovering produces no
condition churn either.

## Related

- Acceptance and its refusals: the same file's `Reconcile`, and the
  `registration-acceptance` capability
- Regeneration keyed on the active set: 0015:D13, delivered
  separately
- Removal of an active claim: 0015:D16, delivered separately
