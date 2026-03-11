# BundleRelease API and Inventory Design

## Why

The controller will eventually need a way to manage multiple module releases as one higher-level unit. That is the purpose of `BundleRelease`.

For the POC, `BundleRelease` should be designed as an orchestration API over child `ModuleRelease` objects, not as a second independent rendering/apply engine. That keeps the reconciliation model simple:

- `ModuleRelease` remains the detailed unit of render/apply/prune/inventory
- `BundleRelease` coordinates a set of module releases
- detailed resource ownership stays on each child release

This document proposes a first `BundleRelease` API and shows how inventory and status should work at bundle level.

## Goals

- Define a first controller-facing `BundleRelease` custom resource.
- Define the relationship between `BundleRelease` and child `ModuleRelease` objects.
- Define aggregate bundle status and inventory strategy.
- Keep the POC orchestration model small and explicit.

## Non-goals

- Transactional rollback across all child modules.
- Full dependency graph solving.
- Cross-bundle orchestration.
- Flattening all bundle resources into one direct apply path.
- Final generated CRD schemas and controller code.

## Design Principles

### BundleRelease is orchestration, not a second apply engine

The bundle controller path should evaluate bundle intent and derive desired child `ModuleRelease` objects. Those child releases then reconcile normally.

This avoids duplicating:

- CUE module evaluation semantics
- inventory tracking logic
- SSA apply/prune logic
- per-release history handling

### Child ModuleReleases hold detailed inventory

Detailed resource ownership should live on each `ModuleRelease.status.inventory`.

`BundleRelease` may expose an aggregate inventory summary, but it should avoid duplicating large detailed per-resource inventories for all child modules.

### Bundle status should be operationally useful

Bundle status should tell an operator:

- what source artifact the bundle resolved to
- what module releases were derived
- whether children are ready
- what the last bundle-level attempt did

## Proposed BundleRelease API

### Spec

Recommended initial `spec` fields:

- `suspend`: stops bundle reconciliation when true
- `sourceRef`: Flux-managed bundle source reference
- `values`: bundle-level config inputs
- `prune`: whether removed child modules should be pruned
- `serviceAccountName`: optional service account used for generated children

Example:

```yaml
apiVersion: releases.opmodel.dev/v1alpha1
kind: BundleRelease
metadata:
  name: media-stack
  namespace: apps
  labels:
    app.kubernetes.io/name: media-stack
    app.kubernetes.io/managed-by: opm-controller
spec:
  suspend: false

  sourceRef:
    apiVersion: source.toolkit.fluxcd.io/v1
    kind: OCIRepository
    name: media-stack
    namespace: apps

  values:
    global:
      domain: example.com
      storageClass: fast

  prune: true
  serviceAccountName: opm-controller
```

### Derived child ModuleReleases

Recommended default model:

- the bundle source evaluates to a desired set of modules
- the controller creates or updates one child `ModuleRelease` per desired module
- child names are deterministic
- child objects have owner references back to the bundle

Example generated names:

- `media-stack-jellyfin`
- `media-stack-qbittorrent`
- `media-stack-radarr`

## Relationship to ModuleRelease

Recommended responsibility split:

- `BundleRelease`: bundle source resolution, module derivation, child orchestration, aggregate status
- `ModuleRelease`: source/config evaluation, SSA apply, prune, ownership inventory, detailed history

This means a `BundleRelease` should not directly own the detailed Kubernetes resource inventory of all workloads. It should own child releases, and children should own workload resources.

## Proposed Status Model

Recommended initial `status` areas:

- `observedGeneration`
- `conditions`
- `source`
- `lastAttempted*`
- `lastApplied*`
- `modules[]`
- optional aggregate `inventory`
- `history`

Example:

```yaml
status:
  observedGeneration: 2

  conditions:
    - type: Ready
      status: "True"
      reason: ReconciliationSucceeded
      message: All child ModuleReleases are ready
      observedGeneration: 2
      lastTransitionTime: "2026-03-11T16:10:00Z"
    - type: Reconciling
      status: "False"
      reason: ReconciliationSucceeded
      message: Bundle reconciliation complete
      observedGeneration: 2
      lastTransitionTime: "2026-03-11T16:10:00Z"

  source:
    ref:
      apiVersion: source.toolkit.fluxcd.io/v1
      kind: OCIRepository
      name: media-stack
      namespace: apps
    artifactRevision: "0.4.0@sha256:77dd..."
    artifactDigest: "sha256:77dd..."

  lastAttemptedAction: apply
  lastAttemptedAt: "2026-03-11T16:09:52Z"
  lastAttemptedSourceDigest: "sha256:77dd..."
  lastAttemptedConfigDigest: "sha256:32af..."
  lastAttemptedRenderDigest: "sha256:9ce1..."

  lastAppliedAt: "2026-03-11T16:10:00Z"
  lastAppliedSourceDigest: "sha256:77dd..."
  lastAppliedConfigDigest: "sha256:32af..."
  lastAppliedRenderDigest: "sha256:9ce1..."

  modules:
    - name: jellyfin
      releaseRef:
        name: media-stack-jellyfin
        namespace: apps
      ready: true
      sourceDigest: "sha256:77dd..."
      configDigest: "sha256:a111..."
      renderDigest: "sha256:b111..."
      inventoryCount: 4
    - name: qbittorrent
      releaseRef:
        name: media-stack-qbittorrent
        namespace: apps
      ready: true
      sourceDigest: "sha256:77dd..."
      configDigest: "sha256:a222..."
      renderDigest: "sha256:b222..."
      inventoryCount: 3

  inventory:
    revision: 4
    digest: "sha256:feed..."
    count: 7

  history:
    - sequence: 4
      action: apply
      phase: Succeeded
      startedAt: "2026-03-11T16:09:52Z"
      finishedAt: "2026-03-11T16:10:00Z"
      sourceDigest: "sha256:77dd..."
      configDigest: "sha256:32af..."
      renderDigest: "sha256:9ce1..."
      inventoryCount: 7
      message: Applied bundle with 2 child ModuleReleases
```

## Inventory Strategy

### Child inventory is authoritative

The authoritative detailed inventory for workloads should remain on each child `ModuleRelease.status.inventory`.

That is where the controller and CLI should look for:

- exact owned resource refs
- prune calculations for that module release
- detailed per-resource status scoping

### Bundle inventory is aggregate only

If the bundle exposes `status.inventory`, it should be aggregate and lightweight.

Recommended bundle-level inventory fields:

- `revision`
- `digest`
- `count`

Optional bundle `entries` can be included for small bundles, but the default recommendation is to avoid duplicating all child resource entries at bundle level.

### Why this split helps

This avoids:

- large duplicated status payloads
- two competing authoritative inventories
- extra complexity when child modules reconcile independently

## Reconcile Flow

Recommended bundle reconcile flow:

1. Watch `BundleRelease` and its referenced `OCIRepository`.
2. Resolve and fetch the current source artifact from Flux.
3. Validate that the artifact contains the expected CUE bundle/module layout.
4. Evaluate the bundle intent.
5. Derive the desired set of child `ModuleRelease` specs.
6. Create or update child `ModuleRelease` objects.
7. Prune removed child releases when `spec.prune` is true.
8. Observe child readiness and child inventory summaries.
9. Update bundle status, aggregate inventory summary, history, and conditions.

## Failure and Remediation Model

Recommended POC behavior:

- child reconcile failures surface to bundle conditions
- bundle history records orchestration-level success or failure
- no transactional rollback across all children
- remediation remains per-child through `ModuleRelease`

This keeps the first controller iteration understandable and avoids inventing bundle-wide rollback semantics too early.

## CLI Interop

The CLI should eventually be able to inspect a `BundleRelease` at two levels:

- bundle-level aggregate health and child release summaries
- child `ModuleRelease` details for exact resource ownership and per-module status

That reinforces the design choice that detailed inventory belongs to child releases.

## Open Questions

- Should bundle evaluation derive modules implicitly from the source artifact, or should `spec.modules[]` exist explicitly?
- Should `BundleRelease.status.inventory.entries` ever be populated, or should it remain summary-only?
- Should bundles support explicit ordering between child modules in the POC?
- Should `BundleRelease` own child `ModuleRelease`s via owner references only, or also via labels/selectors for easier querying?
