# SSA Ownership and Drift Policy

## Summary

This document defines how the OPM controller applies resources, resolves conflicts, handles ownership, and manages configuration drift.

The controller relies on Kubernetes Server-Side Apply (SSA) for all resource mutations. Because OPM evaluates declarative CUE modules, the controller treats SSA as the execution engine for realizing those CUE definitions in the cluster.

Key policy decisions for v1alpha1:

- **Ownership is authoritative via Inventory**: `status.inventory` is the source of truth for ownership and pruning, not live cluster labels.
- **Labels are supportive**: Runtime labels (like `app.kubernetes.io/managed-by`) are injected for human observability but are not the sole factor in prune decisions.
- **Force-conflicts is opt-in**: The controller fails safely if another field manager owns a field, unless `spec.rollout.forceConflicts` is `true`.
- **Drift is detected, not corrected**: The controller identifies drift but does not automatically revert it.
- **No Namespace/CRD pruning**: Highly destructive global resources are excluded from automatic pruning.

## Server-Side Apply (SSA) Policy

### Field Manager Name

The controller MUST use a consistent field manager name for all SSA patch operations.

- **Value**: `opm-controller`
- **Why**: This distinguishes the controller's mutations from those made by the CLI (`opm-cli`), manual users (`kubectl`), or other automated tools (like `helm` or `kustomize`).

### Conflict Resolution Policy

If a desired change conflicts with fields owned by another manager, the controller must decide whether to force the change (takeover) or fail.

- **Default Behavior**: Do NOT force conflicts (`force: false`).
- **Why**: Failsafe behavior. If a human or another operator modified a field that OPM also wants to manage, silently overwriting it can cause outages or hide configuration errors.
- **Opt-in Override**: Users MAY set `ModuleRelease.spec.rollout.forceConflicts = true` to explicitly instruct the controller to take ownership of conflicting fields.

### Apply Ordering

The controller SHOULD use the Flux `ssa.ResourceManager` staged apply logic (`ApplyAllStaged`).

- **Stage 1**: Apply CustomResourceDefinitions (CRDs) and Namespaces.
- **Stage 2**: Apply all other resources.
- **Why**: Avoids race conditions where a custom resource is applied in the same batch as the CRD that defines it, before the API server has fully registered the new schema.

## Pruning Policy

Pruning is the act of deleting previously owned resources that are no longer part of the desired state.

### Eligibility Rule

A resource is eligible for pruning if and only if ALL of the following are true:

1. `ModuleRelease.spec.prune` is `true`.
2. The resource is present in the release's current `status.inventory`.
3. The resource is NOT present in the newly rendered desired state.

### Safety Exclusions

Even if a resource is eligible by the rule above, the controller MUST NOT prune resources of the following kinds in v1alpha1:

- `Namespace`
  - *Why*: Deleting a namespace cascades to all resources inside it, destroying state that the release may not even own.
- `CustomResourceDefinition` (CRD)
  - *Why*: Deleting a CRD deletes all instances of that CRD globally across the cluster, which is highly destructive.

These resources require explicit manual deletion by an administrator when they are no longer needed.

## Ownership and Marking Policy

### Labels and Annotations

While `status.inventory` is the authoritative store for ownership, the controller MUST ensure live resources are marked for observability.

The following labels MUST be applied to all resources managed by the controller:

- `app.kubernetes.io/managed-by: opm-controller`
- `module-release.opmodel.dev/name: <release-name>`
- `module-release.opmodel.dev/namespace: <release-namespace>`

The runtime-owned labels (`managed-by` and `namespace`) are injected via `#runtimeLabels` in `#TransformerContext` during CUE evaluation. CUE unification enforces that if a module attempts to set these keys to different values, evaluation fails — the conflict is surfaced as an error rather than silently overridden.

### Takeover and Adoption

If a resource already exists in the cluster, but is not in `status.inventory`:

1. The controller attempts an SSA patch.
2. If the patch succeeds (no conflicts, or `forceConflicts=true` allowed it), the controller has effectively adopted the resource.
3. The resource is added to the new desired inventory.
4. Only when the reconcile succeeds does the resource become officially "owned" in `status.inventory`.

Labels alone do NOT grant or transfer ownership.

## Drift Policy

Configuration drift occurs when the live state of a resource diverges from the rendered desired state (for the specific fields managed by `opm-controller`).

### v1alpha1 Behavior: Detection Only

In the initial implementation, the controller MUST detect drift but MUST NOT automatically correct it.

1. **Detection**: During the `PlanActions` phase, the controller MAY use an SSA dry-run to compare the live state against the desired state.
2. **Reporting**: If drift is found, the controller sets a `Drifted=True` condition on the `ModuleRelease` status.
3. **Action**: The reconcile loop completes. It does not re-apply the resources to revert the drift.

### Why Detection Only?

Automatic drift correction is dangerous in early controller iterations. 
- If a mutating webhook (e.g., Istio, Linkerd, or a security policy injector) modifies a field that OPM also manages, automatic correction will trigger an infinite reconcile loop (OPM reverts it -> Webhook changes it back -> OPM reverts it).
- It is safer to alert the human operator via a condition.

### Future State

A future revision of the API will likely introduce a field (e.g., `spec.rollout.driftCorrection: true`) to allow users to opt-in to automatic, continuous enforcement of desired state.

## Immutable Fields Policy

Certain fields in Kubernetes are immutable after creation (e.g., StatefulSet `spec.volumeClaimTemplates`, Job `spec.completions`).

### Handling Immutability Errors

If a desired CUE change modifies an immutable field:

1. The Kubernetes API server will reject the SSA patch with a `422 Unprocessable Entity` (Invalid) error.
2. The controller MUST catch this error.
3. The controller MUST mark the reconcile as a **Stalled Failure** (e.g., `Reason: ApplyFailed`, `Message: field is immutable`).
4. The controller MUST NOT automatically delete and recreate the resource.

### Why Fail Fast?

Automatic delete-and-recreate of resources like StatefulSets destroys attached PersistentVolumeClaims, causing catastrophic data loss. The controller defers to the operator to manually intervene (e.g., deleting the StatefulSet so the controller can safely recreate it on the next reconcile).
