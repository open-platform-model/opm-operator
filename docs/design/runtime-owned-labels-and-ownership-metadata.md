# Runtime-owned Labels and Ownership Metadata

## Summary

This document defines how rendered Kubernetes resources should be marked with labels and annotations in the OPM controller, and which parts of that metadata are owned by:

- the catalog and transformation model
- the release author
- the runtime actor (`opm-cli` or `opm-controller`)

The main decision is:

> `app.kubernetes.io/managed-by` is runtime-owned metadata and must be supplied by the actor performing the reconciliation.

For the initial model:

- `opm-cli` sets `app.kubernetes.io/managed-by=opm-cli`
- `opm-controller` sets `app.kubernetes.io/managed-by=opm-controller`

This keeps operational metadata trustworthy while preserving the catalog's ability to inject module and component metadata through `#TransformerContext`.

## Background

The catalog injects standard labels through `#TransformerContext` in `catalog/v1alpha1/core/transformer/transformer.cue`.

Previously, `controllerLabels` hardcoded `app.kubernetes.io/managed-by` to `open-platform-model`. That was workable when OPM effectively had one execution actor, but it became too coarse once OPM gained two distinct runtimes:

- the CLI (`opm-cli`)
- the Kubernetes controller (`opm-controller`)

Those actors must be distinguishable in live cluster metadata.

`#TransformerContext` now uses a hidden input field `#runtimeLabels` that the executing actor must supply. The previous `controllerLabels` field has been renamed to `identityLabels` and no longer contains `managed-by`.

## Problem statement

Rendered resources carry a mix of concerns:

- application and component identity
- release identity
- transformation metadata
- operational runtime identity

If all of these are allowed to flow through the same unconstrained label pipeline, then the cluster metadata becomes less trustworthy.

In particular, `app.kubernetes.io/managed-by` becomes ambiguous if:

- it is hardcoded statically in the catalog
- it is freely overridable by module or release authors
- it does not distinguish `opm-cli` from `opm-controller`

For debugging, interoperability, and future automation, the actor that last reconciled a resource should control this label.

## Design goals

- Keep application and release metadata flowing naturally through the catalog and transformation model.
- Make operational actor metadata trustworthy.
- Keep the controller and CLI aligned on which labels are authoritative vs supportive.
- Avoid treating labels alone as the source of truth for prune ownership.
- Preserve future flexibility for additional actor-controlled metadata.

## Non-goals

- Making labels the authoritative ownership store.
- Replacing inventory with selectors.
- Finalizing every possible annotation key the project may ever need.
- Solving multi-writer shared-resource ownership in v1alpha1.

## Ownership model for metadata

The metadata model should distinguish three classes of labels and annotations.

### 1. Domain metadata

This metadata describes the module, release, and component that produced the resource.

Examples:

- `app.kubernetes.io/name`
- `app.kubernetes.io/instance`
- `module-release.opmodel.dev/name`
- `module-release.opmodel.dev/namespace`
- `component.opmodel.dev/name`

This metadata may originate from:

- module metadata
- component metadata
- release metadata in `#TransformerContext`

### 2. Runtime-owned metadata

This metadata describes the actor that reconciled or emitted the resource.

Initial examples:

- `app.kubernetes.io/managed-by`

Future examples may include:

- stable release UID annotations
- runtime/controller instance annotations
- ownership-generation annotations

This metadata must be controlled by the runtime actor, not by the module or release author.

### 3. Reserved metadata

Reserved metadata is metadata that OPM treats as operationally significant and therefore should not be overridden by arbitrary module-level labels.

Initial reserved keys should include at least:

- `app.kubernetes.io/managed-by`
- `module-release.opmodel.dev/name`
- `module-release.opmodel.dev/namespace`

Future reserved metadata may include a stable release UID annotation.

## Primary decision

### `app.kubernetes.io/managed-by` is runtime-owned

This label should no longer be treated as a static catalog constant.

Instead, the rendering runtime should set it explicitly.

Initial values:

- CLI: `opm-cli`
- controller: `opm-controller`

### Why this is the right boundary

`app.kubernetes.io/managed-by` is operational metadata, not domain metadata.

It answers:

> Which runtime actor currently manages this resource through OPM?

That makes it different from labels like:

- component name
- release name
- module-related labels

Those describe the object's domain identity.

`managed-by` describes the execution actor.

## Labels are supportive, not authoritative ownership

Even with runtime-owned labels, resource ownership is still authoritative in inventory/status, not in labels alone.

For the controller:

- `status.inventory` is the authoritative ownership record
- labels and annotations are supportive live metadata

This distinction matters because labels can be:

- edited manually
- mutated by other tools
- absent on broken or partially migrated objects

The controller should therefore use labels for:

- observability
- debugging
- convenience filtering

but not as the sole source of truth for prune eligibility.

## Runtime behavior

### CLI behavior

When the CLI renders and applies a release, it should inject runtime-owned metadata indicating:

- `app.kubernetes.io/managed-by=opm-cli`

### Controller behavior

When the controller renders and applies a release, it should inject runtime-owned metadata indicating:

- `app.kubernetes.io/managed-by=opm-controller`

### Adoption and transition

If a resource previously managed by the CLI is later reconciled successfully by the controller, then the controller should be allowed to update the runtime-owned metadata accordingly.

That means `managed-by` is not a permanent object birthmark. It reflects the current OPM actor responsible for reconciliation.

## Rendering contract

`#TransformerContext` in `catalog/v1alpha1/core/transformer/transformer.cue` defines four named label sets that are merged into a final `labels` field:

| Set | Source | Contents |
|---|---|---|
| `moduleLabels` | `#moduleReleaseMetadata.labels` | Forwarded from the release file's module metadata |
| `componentLabels` | `#componentMetadata` | `app.kubernetes.io/name`, `module-release.opmodel.dev/name`, plus component labels (excluding `transformer.opmodel.dev/` prefixed keys) |
| `identityLabels` | `#componentMetadata.name` | `app.kubernetes.io/name`, `app.kubernetes.io/instance` |
| `#runtimeLabels` | Runtime actor (hidden input) | `app.kubernetes.io/managed-by`, `module-release.opmodel.dev/namespace`, plus any future runtime-owned keys |

### Precedence

From lower to higher precedence:

1. `moduleLabels` — module metadata labels
2. `componentLabels` — component metadata labels
3. `identityLabels` — standard Kubernetes resource identity labels
4. `#runtimeLabels` — runtime-owned labels (highest precedence)

### Enforcement via CUE unification

Because CUE uses unification rather than override semantics, conflicting values between label sets produce an evaluation error. If a module or component label sets a key that is also present in `#runtimeLabels` with a different value, CUE evaluation fails. This is the enforcement mechanism for reserved labels — no post-render filtering or stripping is needed.

If a module sets the same key to the same value as `#runtimeLabels`, unification succeeds (the values agree). This is harmless and intentional.

## Initial key set

### Runtime-owned keys (in `#runtimeLabels`)

- `app.kubernetes.io/managed-by` — identifies the runtime actor
- `module-release.opmodel.dev/namespace` — the release's target namespace

### Identity keys (in `identityLabels`)

- `app.kubernetes.io/name` — component name
- `app.kubernetes.io/instance` — component name

### Domain keys (in `componentLabels`)

- `module-release.opmodel.dev/name` — release name
- `component.opmodel.dev/name` — set by the module catalog, not by `#TransformerContext`

### Future candidate annotation

Deferred for now:

- `module-release.opmodel.dev/uid` as an annotation rather than a label

Rationale:

- it is useful for stronger identity
- it should not be relied on for broad selector use
- annotations are a better fit for stable opaque identity values

## Why not leave `managed-by` open to module influence

If module or release authors can arbitrarily set `app.kubernetes.io/managed-by`, then:

- the cluster can contain untrustworthy operational metadata
- debugging becomes harder
- CLI-managed and controller-managed objects become indistinguishable
- future tooling built around runtime actor identity becomes unreliable

That would defeat the purpose of having a runtime-meaningful label.

## Implications for controller implementation

The controller should assume:

- runtime-owned metadata is injected by the controller path
- inventory remains the authoritative ownership source
- label mismatches do not automatically redefine ownership
- runtime-owned labels are still valuable for live cluster introspection

This should influence both:

- the render/input contract into shared OPM helpers
- the SSA apply pipeline that mutates final resource metadata

## Implications for CLI/controller interoperability

Using different `managed-by` values is desirable, not a problem.

It allows operators to distinguish:

- objects currently managed by the CLI
- objects currently managed by the controller

At the same time, both runtimes can still share:

- inventory contracts
- release identity labels
- component labels
- module/release metadata conventions

## Resolved questions

### How are runtime-owned labels injected?

**Decision**: via `#runtimeLabels`, a hidden input field on `#TransformerContext`.

The runtime actor (CLI or controller) supplies `#runtimeLabels` during CUE evaluation. The CUE schema merges these at highest precedence in the `labels` field. CUE unification enforces that no other label set can conflict with runtime-owned keys.

This was chosen over post-render normalization because it keeps the CUE output correct without requiring a second pass, and leverages CUE's type system for enforcement.

### Should `module-release.opmodel.dev/namespace` be a label?

**Decision**: yes, included in `#runtimeLabels`.

Previously the release namespace was only used for `metadata.namespace` on rendered resources. Adding it as a label enables filtering resources by release namespace in live clusters.

### Should `module-release.opmodel.dev/uid` be introduced immediately?

**Decision**: deferred. Not introduced in this change.

### Should `app.kubernetes.io/instance` remain component-name-oriented?

**Decision**: yes, no change. It remains set to the component name in `identityLabels`.

## Open questions

- Should `module-release.opmodel.dev/name` move from `componentLabels` to `#runtimeLabels` in the future?
- Should `component.opmodel.dev/name` be added to `#TransformerContext` directly or remain a module-catalog responsibility?

## Decisions

- `app.kubernetes.io/managed-by` is runtime-owned, injected via `#runtimeLabels`
- `opm-cli` and `opm-controller` are the initial actor values
- The legacy value `open-platform-model` is recognized for backward compatibility but no longer stamped on new resources
- `controllerLabels` in `#TransformerContext` has been renamed to `identityLabels` and no longer contains `managed-by`
- `module-release.opmodel.dev/namespace` is added to `#runtimeLabels`
- Inventory/status remains the authoritative ownership record
- CUE unification is the enforcement mechanism for reserved labels
- The catalog and transformer context continue to provide domain metadata for rendered resources
