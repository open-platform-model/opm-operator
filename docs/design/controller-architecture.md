# Controller Architecture

## Summary

This document captures the current architectural decisions for the OPM proof-of-concept Kubernetes controller/operator.

The controller is intentionally designed around the following constraints:

- OPM remains CUE-native end to end.
- Flux `source-controller` is used for source acquisition only.
- The controller must process CUE OCI artifacts, not YAML manifests.
- Shared public APIs and helpers should come from the CLI as it is published and versioned with Goreleaser.
- Inventory is ownership-only and shared conceptually between the CLI and the controller.

This is an experiment, but the architecture should still be coherent enough that successful parts can graduate into a production controller later.

## Core decisions

### 1. Source acquisition is delegated to Flux `source-controller`

The controller will not implement its own first-class OCI artifact polling and storage loop.

Instead:

- users create or reference a Flux `OCIRepository`
- Flux resolves, fetches, verifies, and stores the artifact
- the OPM controller watches its own release CRs and the referenced Flux source objects
- the OPM controller consumes the resolved artifact from `OCIRepository.status.artifact`

This gives OPM:

- a mature source acquisition path
- digest and revision tracking from Flux
- reuse of GitOps Toolkit APIs and conventions
- less duplicated controller complexity

This also keeps the controller focused on OPM-specific work:

- validating CUE module layout
- evaluating CUE
- computing desired resources
- applying with SSA
- tracking ownership inventory and release status

### 2. OPM remains responsible for CUE evaluation

Flux should not be asked to understand or render OPM artifacts semantically.

The controller assumes:

- the artifact fetched through Flux contains a CUE module
- OPM knows how to validate and evaluate that module
- the same CUE-native logic used by the CLI should be reused in the controller

This is the main reason the controller should not be implemented as a thin wrapper around Flux `Kustomization` or `HelmRelease`.

The source is Flux-managed, but the release semantics remain OPM-managed.

### 3. `ModuleRelease` is the primary detailed reconciliation unit

The first serious controller should revolve around `ModuleRelease`.

That means:

- `ModuleRelease` is the object that points at a Flux `OCIRepository`
- `ModuleRelease` is responsible for rendering desired resources
- `ModuleRelease` owns the detailed resource inventory
- `ModuleRelease` owns per-release digests, conditions, and bounded history

This keeps the main reconciliation contract small and concrete.

### 4. `BundleRelease` is an orchestrator over child `ModuleRelease`s

`BundleRelease` should not be a second direct apply engine in the POC.

Instead:

- `BundleRelease` evaluates bundle intent
- `BundleRelease` creates or updates child `ModuleRelease`s
- child `ModuleRelease`s do detailed render/apply/prune work
- bundle status aggregates child state

This avoids duplicating:

- SSA logic
- ownership inventory logic
- detailed per-resource status handling
- reconciliation history handling

### 5. Inventory is ownership-only

The controller should adopt the same ownership-only inventory direction being prepared in the CLI.

Inventory should answer only:

> What resources does this release currently own?

Inventory should contain:

- current resource entries
- optional inventory revision
- optional inventory digest
- optional inventory count

Inventory should not contain:

- raw values
- source path/version metadata
- remediation counters
- full reconcile history
- rendered manifests

### 6. Reconciliation state lives in CR status

The controller should persist operational state in CR `status`, not in inventory.

Recommended top-level status fields:

- `status.source`
- `status.lastAttemptedSourceDigest`
- `status.lastAttemptedConfigDigest`
- `status.lastAttemptedRenderDigest`
- `status.lastAppliedSourceDigest`
- `status.lastAppliedConfigDigest`
- `status.lastAppliedRenderDigest`
- `status.conditions`
- `status.history`
- `status.inventory`

This gives a much cleaner split:

- `spec` = desired intent
- `status.inventory` = what is owned
- `status.history` and digest fields = what happened

### 7. Shared APIs and helpers should come from the CLI release

The user has decided that the CLI will be published with Goreleaser so the controller can consume public APIs and helpers from it.

This means the architecture should assume:

- shared reusable code is promoted into stable public packages in the CLI
- the controller imports those packages instead of copying logic
- inventory, render helpers, and related contracts should converge in the CLI first

This is important for avoiding duplicate implementations of:

- inventory identity logic
- release/source helper contracts
- CUE loading and evaluation helpers

## High-level controller shape

Recommended repo/package structure for the controller:

```text
cmd/
  manager/
    main.go

api/
  releases/
    v1alpha1/
      modulerelease_types.go
      bundlerelease_types.go
      groupversion_info.go

controllers/
  modulerelease_controller.go
  bundlerelease_controller.go
  predicates.go

internal/
  source/
    artifact.go
    fetch.go
    validate.go
  render/
    module.go
    bundle.go
  apply/
    manager.go
    prune.go
  inventory/
    digest.go
    stale.go
  status/
    conditions.go
    history.go
    digests.go
  reconcile/
    modulerelease.go
    bundlerelease.go
```

This split keeps the top-level controller files small while isolating major concerns.

## Reconcile model

### ModuleRelease reconcile flow

Recommended flow:

1. Fetch `ModuleRelease`.
2. Exit early if suspended.
3. Create a patch helper / serial patcher.
4. Mark reconciling conditions.
5. Resolve the referenced `OCIRepository`.
6. Read `status.artifact` from the source object.
7. Fetch and extract the artifact.
8. Validate the artifact contains a CUE module.
9. Evaluate the module using shared OPM helpers.
10. Compute digests:
    - source digest
    - config digest
    - render digest
    - inventory digest
11. Convert desired resources into ownership inventory entries.
12. Apply desired resources with SSA.
13. Compute stale owned resources from previous vs current inventory.
14. Prune stale resources if enabled.
15. Update:
    - `status.source`
    - `status.lastAttempted*`
    - `status.lastApplied*`
    - `status.inventory`
    - `status.history`
    - `status.conditions`
16. Finalize reconcile result.

### BundleRelease reconcile flow

Recommended flow:

1. Fetch `BundleRelease`.
2. Exit early if suspended.
3. Resolve the referenced source artifact.
4. Fetch and validate the bundle artifact.
5. Evaluate desired bundle intent.
6. Derive child `ModuleRelease` specs.
7. Create/update child `ModuleRelease`s.
8. Prune removed child releases if enabled.
9. Aggregate child status into bundle status.
10. Update bundle-level history, digests, conditions, and summary inventory.

## Ownership and pruning

The architecture assumes SSA plus ownership inventory.

The model is:

- desired state is recomputed from deterministic CUE inputs
- previous ownership is recorded in `status.inventory`
- stale set is computed as:
  - `previous owned resources - current desired owned resources`
- stale resources are deleted only if they are currently owned by the release

This gives OPM a simpler and more direct model than Helm-style persisted manifest storage.

## Status and history strategy

### Status is the controller ledger

The controller should use CR status as the primary operational record.

Status should capture:

- source artifact identity
- desired input identity
- rendered output identity
- whether reconcile succeeded or failed
- what is currently owned

### History is bounded and metadata-only

History should stay in `status.history` and remain bounded.

Each history entry should store compact metadata such as:

- action
- phase
- timestamps
- source/config/render digests
- inventory digest/count
- short message

It should not store full rendered manifests.

## Failure model

Recommended POC behavior:

- source fetch or validation failures set `Ready=False`
- render failures set `Ready=False`
- SSA/apply failures set `Ready=False`
- prune failures set `Ready=False`
- bounded failure counters can be recorded in status
- no complex rollback is required for the first POC

Remediation should initially mean:

- retry on the next reconcile
- optionally drift detection and correction later

## Relationship to the CLI

The controller should not invent a separate domain model where reuse is practical.

The CLI is the first place where public OPM helpers are being prepared. The controller should consume those public packages for:

- inventory contracts and helpers
- CUE loading/rendering helpers where appropriate
- release-related shared types when stabilized

This keeps the ecosystem aligned:

- the CLI and controller reason about ownership the same way
- the CLI can inspect controller-managed releases later
- controller behavior stays close to existing OPM semantics

## Phasing recommendation

Recommended implementation order:

1. `ModuleRelease` API and controller.
2. Shared public helper consumption from the CLI.
3. Ownership inventory and SSA apply/prune.
4. Status digests and bounded history.
5. `BundleRelease` orchestration over child `ModuleRelease`s.

This keeps the first milestone small while building toward the full design.
