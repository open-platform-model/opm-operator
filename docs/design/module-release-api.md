# ModuleRelease API and Inventory Design

## Why

The POC controller needs a Kubernetes-native release API that stays aligned with how OPM already works today:

- source acquisition comes from Flux `source-controller`
- artifacts must contain native CUE modules
- OPM owns CUE evaluation and Kubernetes apply logic
- the same ownership inventory shape should be reusable by both the CLI and the future controller

Because OPM rendering is deterministic for the same source artifact and config inputs, the controller does not need to persist full rendered manifests as release state. Instead, it should persist:

- what source artifact was used
- what config was used
- what rendered output digest was applied
- what Kubernetes resources are currently owned

This document proposes the first `ModuleRelease` API for the POC controller and defines how inventory should work in that API.

## Goals

- Define a first controller-facing `ModuleRelease` custom resource.
- Define a small ownership-only `status.inventory` shape.
- Define where source, config, render, and reconciliation state live.
- Keep the POC API small and explicit.

## Non-goals

- Final generated CRD schemas and controller code.
- Full rollback implementation.
- Final `BundleRelease` orchestration semantics.
- Cross-release dependency resolution.
- Persisting full rendered manifests.
- Rich drift reporting or remediation UX.
- Complex values composition from multiple external sources.

This document follows the narrower experiment boundary defined in `experimental-scope-and-non-goals.md`.

## Design Principles

### Flux provides transport, not CUE semantics

The controller should rely on Flux `source-controller` for fetching and tracking OCI artifacts, but Flux should not be responsible for understanding OPM CUE modules.

The contract is:

- Flux resolves and fetches the OCI artifact.
- The artifact must contain a CUE module.
- OPM validates and evaluates that module.

### Inventory tracks ownership only

Inventory should answer one question:

> What Kubernetes resources are currently owned by this release?

Inventory should not be used to store source details, values history, remediation counters, or rendered manifests.

### Reconcile state lives in status

The controller should record operational state in top-level status fields:

- resolved source artifact metadata
- last attempted digests
- last applied digests
- conditions
- bounded history

### Deterministic CUE rendering reduces storage needs

For the same source artifact digest and config digest, OPM should produce the same rendered desired state. That means the controller can recompute desired resources and use inventory only for ownership/prune semantics.

## Source Model

`ModuleRelease.spec.sourceRef` should reference a Flux `OCIRepository`.

Example:

```yaml
sourceRef:
  apiVersion: source.toolkit.fluxcd.io/v1
  kind: OCIRepository
  name: jellyfin-module
  namespace: apps
```

The controller should use `OCIRepository.status.artifact` as the resolved source artifact.

The artifact requirements for the POC are:

- the artifact is fetched by Flux
- the referenced `OCIRepository` must preserve the native CUE `application/zip` layer
- for the current experiment that means `spec.layerSelector.mediaType=application/zip` and `spec.layerSelector.operation=copy`
- the artifact contains a CUE module
- the artifact contains the module content OPM needs to evaluate
- the controller validates the expected layout after extraction, including `cue.mod`

If the referenced `OCIRepository` does not satisfy that contract, the controller should fail clearly rather than attempting to infer alternate source behavior.

## Proposed ModuleRelease API

### Spec

Recommended initial `spec` fields:

- `suspend`: stops reconciliation when true
- `sourceRef`: Flux-managed source reference
- `module.path`: module path to evaluate from the source artifact
- `values`: inline user-supplied configuration values for evaluation
- `prune`: enables deletion of previously owned stale resources
- `serviceAccountName`: service account used for apply operations
- `rollout`: optional low-level apply behavior knobs only

Example:

```yaml
apiVersion: releases.opmodel.dev/v1alpha1
kind: ModuleRelease
metadata:
  name: jellyfin
  namespace: apps
  labels:
    app.kubernetes.io/name: jellyfin
    app.kubernetes.io/managed-by: opm-controller
spec:
  suspend: false

  sourceRef:
    apiVersion: source.toolkit.fluxcd.io/v1
    kind: OCIRepository
    name: jellyfin-module
    namespace: apps

  module:
    path: opmodel.dev/modules/jellyfin

  values:
    namespace: apps
    ingress:
      enabled: true
      host: jellyfin.example.com

  prune: true
  serviceAccountName: opm-controller

  rollout:
    forceConflicts: true
```

### Status

Recommended initial `status` areas:

- `observedGeneration`
- `conditions`
- `source`
- `lastAttempted*`
- `lastApplied*`
- `failureCounters`
- `inventory`
- `history`

For the experiment, status should stay focused on source, digests, ownership, and recent operational results. It should not become a rich health or drift reporting surface.

Full example:

```yaml
status:
  observedGeneration: 4

  conditions:
    - type: Ready
      status: "True"
      reason: ReconciliationSucceeded
      message: Applied desired state successfully
      observedGeneration: 4
      lastTransitionTime: "2026-03-11T15:02:10Z"
    - type: Reconciling
      status: "False"
      reason: ReconciliationSucceeded
      message: Reconciliation complete
      observedGeneration: 4
      lastTransitionTime: "2026-03-11T15:02:10Z"
    - type: SourceReady
      status: "True"
      reason: ArtifactAvailable
      message: Source artifact fetched successfully
      observedGeneration: 4
      lastTransitionTime: "2026-03-11T15:01:50Z"

  source:
    ref:
      apiVersion: source.toolkit.fluxcd.io/v1
      kind: OCIRepository
      name: jellyfin-module
      namespace: apps
    artifactRevision: "1.2.3@sha256:4b2cf7d3d7d2b0e6..."
    artifactDigest: "sha256:4b2cf7d3d7d2b0e6..."
    artifactURL: "http://source-controller.flux-system.svc/artifact.tar.gz"

  lastAttemptedAction: apply
  lastAttemptedAt: "2026-03-11T15:02:07Z"
  lastAttemptedDuration: "2.8s"
  lastAttemptedSourceDigest: "sha256:4b2cf7d3d7d2b0e6..."
  lastAttemptedConfigDigest: "sha256:18a1d5f8ab0b69d0..."
  lastAttemptedRenderDigest: "sha256:b91e6e54b27b11f7..."

  lastAppliedAt: "2026-03-11T15:02:10Z"
  lastAppliedSourceDigest: "sha256:4b2cf7d3d7d2b0e6..."
  lastAppliedConfigDigest: "sha256:18a1d5f8ab0b69d0..."
  lastAppliedRenderDigest: "sha256:b91e6e54b27b11f7..."

  failureCounters:
    reconcile: 0
    apply: 0
    prune: 0

  inventory:
    revision: 9
    digest: "sha256:aa22d4a6d8d0c7a6a4e8a6c9b52d0d3b7c1b5c56d1e1f9b622f0d7288f2e6abc"
    count: 4
    entries:
      - group: apps
        kind: Deployment
        namespace: apps
        name: jellyfin
        v: v1
        component: server
      - group: ""
        kind: Service
        namespace: apps
        name: jellyfin
        v: v1
        component: server
      - group: networking.k8s.io
        kind: Ingress
        namespace: apps
        name: jellyfin
        v: v1
        component: ingress
      - group: ""
        kind: PersistentVolumeClaim
        namespace: apps
        name: jellyfin-data
        v: v1
        component: storage

  history:
    - sequence: 9
      action: apply
      phase: Succeeded
      startedAt: "2026-03-11T15:02:07Z"
      finishedAt: "2026-03-11T15:02:10Z"
      sourceDigest: "sha256:4b2cf7d3d7d2b0e6..."
      configDigest: "sha256:18a1d5f8ab0b69d0..."
      renderDigest: "sha256:b91e6e54b27b11f7..."
      inventoryDigest: "sha256:aa22d4a6d8d0c7a6a4e8a6c9b52d0d3b7c1b5c56d1e1f9b622f0d7288f2e6abc"
      inventoryCount: 4
      message: Applied 4 resources successfully
```

## Inventory Model

### Ownership-only shape

The inventory shared by the controller and CLI should look like this:

```yaml
inventory:
  revision: 9
  digest: sha256:aa22d4a6d8d0c7a6a4e8a6c9b52d0d3b7c1b5c56d1e1f9b622f0d7288f2e6abc
  count: 4
  entries:
    - group: apps
      kind: Deployment
      namespace: apps
      name: jellyfin
      v: v1
      component: server
```

### Identity semantics

Recommended semantics:

- ownership identity: `group`, `kind`, `namespace`, `name`, `component`
- Kubernetes identity: `group`, `kind`, `namespace`, `name`
- `version` is informative and excluded from ownership equality

This allows the controller to:

- prune by current ownership
- detect component ownership changes separately from Kubernetes identity changes
- tolerate API version migrations without creating false stale resources

### What inventory should not store

Inventory should not store:

- raw values
- source path/version metadata
- last attempted/applied digests
- remediation counters
- full rendered manifests
- full reconcile history

Those live in top-level status instead.

## Digests

Three digests should be treated as first-class:

### Source digest

The exact content digest of the fetched OCI artifact.

Used for:

- tying applied state to a specific source artifact
- proving what source revision was reconciled
- deciding whether source content changed

### Config digest

A digest of the normalized release configuration inputs.

Used for:

- detecting whether user intent changed
- avoiding persistence of large or sensitive resolved values in inventory

### Render digest

A digest of the deterministic rendered desired resource set.

Used for:

- deciding whether desired applied output changed
- recognizing no-op reconciles
- tying history to the exact rendered desired state

## Reconcile Flow

Recommended flow for the controller:

1. Watch `ModuleRelease` and the referenced `OCIRepository`.
2. Resolve the current source artifact from Flux status.
3. Validate that the source preserves the native CUE zip payload expected by OPM.
4. Fetch and recover the artifact payload.
5. Validate that the recovered content contains a CUE module.
6. Evaluate the desired module using `spec.module.path` and `spec.values`.
7. Compute source, config, render, and inventory digests.
8. Compare current desired inventory against previously recorded ownership inventory.
9. Apply desired resources with server-side apply.
10. Prune stale previously owned resources when `spec.prune` is true.
11. Update status, inventory, history, and conditions.

## CLI Interop

This design intentionally keeps `status.inventory` usable by the CLI later.

The CLI should be able to use controller-managed release status for:

- owned resource discovery
- prune visibility
- status views scoped to owned resources

The CLI should not expect inventory to provide:

- source version display metadata
- values history
- reconcile counters

Those should come from top-level controller status fields.

## Open Questions

- Should `spec.module.path` remain required if the OCI artifact root is already the only module?
- Should `component` remain part of ownership identity long-term, or eventually become informational only?
- How much bounded `status.history` should the POC retain?
- Should `failureCounters` remain in the first experimental API, or is bounded history enough?
