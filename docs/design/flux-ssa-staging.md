# Flux SSA Staging and Apply Mechanics

## Summary

The OPM controller delegates resource application to Flux's `ssa.ResourceManager.ApplyAllStaged`. This document describes the built-in staging model, the `CustomStageKinds` extension point, and key behavioral details that differ from raw Kubernetes Server-Side Apply.

This document supplements [ssa-ownership-and-drift-policy.md](ssa-ownership-and-drift-policy.md), which defines the controller's high-level SSA policy.

## ApplyAllStaged Built-in Staging

`ApplyAllStaged` classifies resources into four stages and applies them sequentially. The classification is structural (based on GVK properties), not configurable by the caller except through `CustomStageKinds`.

| Stage | Classification | Readiness Wait | Resources |
|-------|---------------|----------------|-----------|
| 1 — Cluster definitions | `utils.IsClusterDefinition()` | Yes | CRD, Namespace, ClusterRole |
| 2 — Class definitions | `utils.IsClassDefinition()` — kind suffix "Class" | Yes | StorageClass, IngressClass, GatewayClass, VolumeSnapshotClass |
| 3 — Custom | Matches `ApplyOptions.CustomStageKinds` | No | Caller-defined |
| 4 — Default | Everything else | No | All remaining resources |

Stages 1 and 2 include a readiness wait (`WaitForSet`) after apply. This ensures CRDs are established and class definitions are available before dependent resources are applied.

There is no ordering **within** a stage. Resources in the same stage are applied concurrently via `ApplyAll` (controlled by the `concurrency` setting on `ResourceManager`).

### Classification Functions

Flux uses structural checks, not weight tables:

- **`IsClusterDefinition`**: `IsCRD(o) || IsNamespace(o) || IsClusterRole(o)` — checks GVK strings, not annotations or labels.
- **`IsClassDefinition`**: `strings.HasSuffix(o.GetKind(), "Class")` — any kind ending in "Class".
- **`IsCustomStage`**: exact `GroupKind` match against the `CustomStageKinds` map.

## CustomStageKinds Extension Point

`ApplyOptions.CustomStageKinds` is a `map[schema.GroupKind]struct{}` that the caller can populate to promote specific resource types into stage 3. This is the primary mechanism for extending the staging model without reimplementing it.

### When to Use

Custom staging is useful when the controller manages resources with ordering dependencies that Flux's built-in stages do not cover. Examples:

- **RBAC bindings before workloads**: ClusterRoleBindings, Roles, RoleBindings, and ServiceAccounts should exist before Deployments that reference them.
- **Secrets and ConfigMaps before consumers**: Pods and Deployments may fail to start if referenced Secrets or ConfigMaps do not exist yet.
- **Services before Ingress**: An Ingress referencing a Service that does not exist may generate spurious errors from ingress controllers.

### Candidate Resources

The `pkg/resourceorder` package defines a weight table for apply ordering. Resources with weight below 100 (the former `StageOneThreshold`) are candidates for custom staging, minus those Flux already handles in stages 1-2:

| Resource | Weight | Flux Built-in Stage | Custom Stage Candidate |
|----------|--------|--------------------|-----------------------|
| CRD | -100 | 1 (cluster def) | No |
| Namespace | 0 | 1 (cluster def) | No |
| ClusterRole | 5 | 1 (cluster def) | No |
| ClusterRoleBinding | 5 | 4 (default) | Yes |
| ServiceAccount | 10 | 4 (default) | Yes |
| Role | 10 | 4 (default) | Yes |
| RoleBinding | 10 | 4 (default) | Yes |
| Secret | 15 | 4 (default) | Yes |
| ConfigMap | 15 | 4 (default) | Yes |
| StorageClass | 20 | 2 (class def) | No |
| PersistentVolume | 20 | 4 (default) | Yes |
| PersistentVolumeClaim | 20 | 4 (default) | Yes |
| Service | 50 | 4 (default) | Yes |

### Implementation Approach (Deferred)

When custom staging is needed, populate `ApplyOptions.CustomStageKinds` from the `resourceorder` weight table, filtering out kinds already handled by Flux's stages 1-2. This makes the weight table load-bearing without reimplementing Flux's staging logic.

```go
// Example — not yet implemented.
func customStageKinds() map[schema.GroupKind]struct{} {
    candidates := map[schema.GroupKind]struct{}{
        {Group: "rbac.authorization.k8s.io", Kind: "ClusterRoleBinding"}: {},
        {Group: "",                          Kind: "ServiceAccount"}:     {},
        {Group: "rbac.authorization.k8s.io", Kind: "Role"}:              {},
        {Group: "rbac.authorization.k8s.io", Kind: "RoleBinding"}:       {},
        {Group: "",                          Kind: "Secret"}:             {},
        {Group: "",                          Kind: "ConfigMap"}:          {},
        {Group: "",                          Kind: "PersistentVolume"}:   {},
        {Group: "",                          Kind: "PersistentVolumeClaim"}: {},
        {Group: "",                          Kind: "Service"}:            {},
    }
    return candidates
}
```

## ForceOwnership vs Force (Immutable Fields)

Flux's `ResourceManager.apply()` always patches with `client.ForceOwnership`. This means **SSA field-ownership conflicts never occur** through the Flux layer — a different field manager can always overwrite fields owned by another manager.

The `ApplyOptions.Force` flag controls a separate behavior: **immutable field handling**. When `Force` is `true` and the dry-run detects an immutable field change, Flux deletes the existing object and recreates it. When `false`, the apply fails with an immutable field error.

This has implications for the OPM controller:

- `spec.rollout.forceConflicts` maps to `ApplyOptions.Force`, which controls immutable field recreation.
- SSA field-ownership conflicts (as described in the Kubernetes SSA documentation) are not observable through Flux's `ResourceManager`. Flux resolves them implicitly via `ForceOwnership`.
- The policy in [ssa-ownership-and-drift-policy.md](ssa-ownership-and-drift-policy.md) describes the *intended* semantics. The Flux implementation satisfies the apply-succeeds case but does not surface conflict errors when `forceConflicts` is `false`. This is acceptable for v1alpha1 — revisit if fine-grained ownership control is needed.

## Decision Record

- **Rely on Flux's built-in staging**: The controller does not implement its own resource staging. `ApplyAllStaged` handles CRDs, Namespaces, ClusterRoles, and class definitions with readiness waits. This avoids maintaining a parallel staging implementation.
- **Defer custom staging**: The `CustomStageKinds` extension point is available but not yet used. Enable it when ordering failures are observed in production (e.g., Deployments referencing nonexistent Secrets).
- **Accept ForceOwnership semantics**: Flux always forces ownership. This simplifies the apply path at the cost of not surfacing ownership conflicts. Acceptable for v1alpha1.

## References

- Flux SSA source: `github.com/fluxcd/pkg/ssa@v0.69.0`
- `ApplyAllStaged`: `manager_apply.go:256-329`
- `ApplyOptions`: `manager_apply.go:40-97`
- Classification: `utils/is.go:28-68`
- `pkg/resourceorder`: weight table for apply ordering
