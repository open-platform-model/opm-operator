## Why

Library `v1.0.0-beta.6` ships the first packages of the Kubernetes tier (library ADR-011, 0012:D3): `opm/k8s/labels` (the OPM label vocabulary and `IsOPMManagedBy`) and `opm/k8s/object` (the `Resource` wrapper over the kernel's compiled output, its JSON and unstructured conversion, one bulk `Export`, the duplicate-identity check, and the kind-class weight table with `Sort` and `Stages`). The operator still carries its own copy of the first two in its exported `pkg/core` (`labels.go`, `resource.go`, `convert.go`, `compiled_adapter.go`), byte-identical in content to what the library now owns. 0012:D3:R6 says a Kubernetes frontend that adopts a tier package carries no copy of its decisions and no alias from that release on, and its checks refuse a reintroduced copy. The cli deletes its copy in the same library release (its own change, `adopt-kubernetes-object-packages` in the cli).

The operator also does not apply in the library's order. 0012:D4:R3 and 0012:D5:R1 say each frontend submits an instance's objects in the library's kind-class order, and an engine's own staging (Flux's) may refine that order and never contradict it. `internal/apply/apply.go` hands the unsorted render to `fluxssa.ApplyAllStaged`, which sorts every stage with Flux's own table (`fluxcd/pkg/ssa` v0.77.0, `manager_apply.go:207`, `sort.go:41-105`). The two tables disagree: the library applies PersistentVolume and PersistentVolumeClaim (weight 20) before Deployment and StatefulSet (100), and Flux applies them after, because it lists Deployment and StatefulSet early and leaves PV and PVC unlisted. Sorting the set by library weight before handing it to Flux would change nothing, because Flux re-sorts inside every `ApplyAll` call. Submitting one library weight group per `ApplyAll` call keeps Flux as the apply engine and makes its re-sort a refinement: it can only order objects of the same library weight.

## What Changes

- **`pkg/core` is deleted** (all four files and their tests). The seven non-test importers (`cmd/main.go`, `internal/apply/prune.go`, `internal/inventory/entry.go`, `internal/reconcile/moduleinstance.go`, `internal/render/kernel_module_renderer.go`, `internal/render/module.go`, `internal/status/digests.go`), the unit tests under `internal/` and every importer under `test/integration/` switch to `opm/k8s/labels` and `opm/k8s/object`. `core.ResourceFromCompiled` becomes `object.NewResource` (or `object.Resources` for the whole set). Label constants map one to one (`LabelManagedBy` to `labels.ManagedBy`, `LabelManagedByControllerValue` to `labels.ManagedByController`, `LabelComponentName` to `labels.ComponentName`, `LabelModuleInstanceUUID` to `labels.ModuleInstanceUUID`, and so on).
- **One export per resource through the library.** `convertRender` (`internal/reconcile/converted.go`) calls `object.Export` once over the rendered set and takes both the render digest and the unstructured apply copies from its result; `status.RenderDigestJSON` over `[]*core.Resource` is replaced by a digest over the exported set. The digest's bytes do not change (a golden test pins it on the unchanged code first), so no instance re-applies because of the move. Both failure reasons and their messages stay as they are.
- **Apply in library order.** `apply.Apply` cuts the set with `object.Stages` and calls `rm.ApplyAll` once per stage: the cluster-definition stage (CRDs and Namespaces) first, followed by `WaitForSet` on it as `ApplyAllStaged` did, then one call per library weight in ascending order. `object.Stages` works on a sorted copy, so the caller's `applyList` keeps its order for drift and status. The CRD discovery retry wraps the whole staged sequence, as it wraps `ApplyAllStaged` today.
- **Duplicate check: nothing left.** The renderer's duplicate-identity check already uses `object.Duplicates` (opm-operator#248, `kernel_module_renderer.go:235` and `resolution.go:84`).
- **Lint.** `.golangci.yml` enables `depguard` with a rule that refuses `github.com/open-platform-model/opm-operator/pkg/core`, so a reintroduced copy cannot be imported.
- **Docs.** The `internal/apply` package and `NewResourceManager` comments, and ADR-004's staged-ordering bullet (as an "Amended" note), describe the library order.

Out of scope: the operator's delete and prune order (the deletion-protocol change moves both onto the library), the inventory and digest packages of the tier (`opm/k8s/inventory`, its own change), the apply guard (`opm/k8s/ownership`), and removing the library's deprecated `opm/helper/objectset` (a later library release).

## Migration note

Both paragraphs go in the PR body; the squash commit carries only the `feat!:` title, and the CHANGELOG entry links the PR (AGENTS.md, "Beta line").

**BREAKING CHANGE:** the exported package `github.com/open-platform-model/opm-operator/pkg/core` is deleted. No importer outside this repository is known. An importer moves to the library: `Resource`, `ResourceFromCompiled` (now `object.NewResource`), `MarshalJSON` and `ToUnstructured` are in `github.com/open-platform-model/library/opm/k8s/object`; the label constants and `IsOPMManagedBy` are in `github.com/open-platform-model/library/opm/k8s/labels`, with the `Label` prefix and `Value` suffix dropped (`LabelManagedByControllerValue` is `labels.ManagedByController`, `LabelManagedByValue` is `labels.ManagedByCLI`, `LabelManagedByLegacyValue` is `labels.ManagedByLegacy`).

**Behaviour:** the operator now applies a module's objects in the library's kind-class order, one Flux `ApplyAll` per library weight, and Flux orders only objects of equal library weight. Where Flux v0.77.0's table disagreed with the library (the full list is the library's `opm/k8s/object/flux_order_test.go`), the order changes:

- *Webhook configurations before custom resources.* MutatingWebhookConfiguration and ValidatingWebhookConfiguration (weight 500) are now applied before custom resources and every other kind at the default weight (1000); Flux applied them last. A module that ships a webhook with `failurePolicy: Fail` together with custom resources that webhook intercepts (cert-manager does) can now have those resources refused at dry-run until the webhook's Pod serves. The first install then reports `ApplyFailed` and succeeds on a later retry.
- *Class kinds, ResourceQuota and LimitRange after the workloads.* ClusterClass, GatewayClass, IngressClass, PriorityClass, RuntimeClass, VolumeSnapshotClass, ResourceQuota and LimitRange sit at the library's default weight, so they are now applied after the workloads, Jobs, Ingresses, PodDisruptionBudgets and webhook configurations; Flux applied them before. A Pod that names a PriorityClass or RuntimeClass created in the same apply is refused until that class exists, and its controller retries. Pods admitted before a LimitRange in the same apply get none of its defaults or limits and keep running without them until they are recreated; objects created before a ResourceQuota in the same apply are not checked against it at admission.
- *Volumes and StorageClass.* PersistentVolumes and PersistentVolumeClaims (weight 20) are now applied before Services, Deployments, StatefulSets and CronJobs; Flux applied them after. StorageClass (weight 20) is now applied after ClusterRoleBindings, ServiceAccounts, Roles, RoleBindings, Secrets and ConfigMaps; Flux applied it right after ClusterRoles. It still goes before the volumes of its own weight.
- *PodDisruptionBudget* (weight 200) is now applied after DaemonSets, ReplicaSets, Jobs, Ingresses, NetworkPolicies and the volumes; Flux applied it before them.
- *CronJob* (weight 110) is now applied after DaemonSets and ReplicaSets (100).
- ClusterRoles are applied right after the cluster definitions instead of with them, and ClusterRoles and class kinds are no longer waited for before the next stage.

Each `ApplyAll` call dry-runs its own stage before it applies it, so the server-side dry-run now covers one stage at a time, not the whole remainder of the set. A dry-run failure in a later stage leaves the earlier stages applied. On a first install those objects are not yet in the inventory, which is written only after a successful apply, so an instance deleted before any apply succeeds leaves them behind. On an upgrade, the lower-weight objects of the new version are applied although a later stage was refused: a partial rollout that Flux's staged apply refused as a whole at dry-run.

## Release notes (user-visible changes)

The behaviour paragraph of the migration note. No CRD, status field, condition, reason or message changes.

## Classification

**MAJOR** after GA (an exported package is removed); pre-GA it ships as the next `-beta.N` under a `feat!:` title. Complexity (Principle VII): about 400 lines of operator code and tests go; the apply gains one loop over library stages, which replaces one Flux call.

## Capabilities

### New Capabilities

- `kubernetes-tier-adoption`: the operator uses the library's Kubernetes object and label packages, keeps no copy of them, and its lint refuses a reintroduced one.

### Modified Capabilities

- `ssa-apply`: "Staged apply ordering" now names the library's stages and the one-`ApplyAll`-per-stage rule.
- `kernel-module-renderer`: "Adapt compiled output to operator resources" names the library's `object.Resource`.
- `prune-stale-resources`: "Live-state UUID-based ownership guard" names the library's `labels.IsOPMManagedBy`.
- `digest-computation`: "Render digest computation" is computed from the one export, with its bytes pinned.
- `inventory-bridge`: "Component label constant" and "CLI packages copied to `pkg/`" are removed; the key and the types come from the library.

## Impact

- Code: `pkg/core/` (deleted), `cmd/main.go`, `internal/apply/{apply,prune,manager,doc}.go`, `internal/inventory/entry.go`, `internal/reconcile/{converted,moduleinstance}.go`, `internal/render/{kernel_module_renderer,module}.go`, `internal/status/digests.go`, `.golangci.yml`, `adr/004-server-side-apply-as-mutation-primitive.md`.
- Tests: about twenty files under `internal/` and `test/integration/` change their imports; new tests pin the digest bytes and the apply order.
- Dependencies: none. opm-operator#248 already moved to library `v1.0.0-beta.6` and swapped `opm/helper/objectset` for `opm/k8s/object`; this change builds on opm-operator#249, #251 and #252 and changes no pin.
- Downstream: the cli deletes its own `pkg/core` and `pkg/resourceorder` in its adoption change for the same library release. No cli code imports the operator's `pkg/core`.
- Enhancement: `enhancement.yaml` declares 0012 with no decision claimed. 0012:D3:R6, 0012:D4:R3 and 0012:D5:R1 hold only once both frontends have adopted, and the operator's delete order is still its own.
