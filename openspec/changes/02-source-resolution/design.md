## Context

The ModuleRelease CRD has `spec.sourceRef` (a `NamespacedObjectKindReference`) pointing to a Flux `OCIRepository`. The controller must resolve this reference to get the artifact URL, revision, and digest before it can fetch and render. The existing `internal/source` package has an `ArtifactRef` wrapper and a `Fetcher` interface but no resolution logic.

Flux source-controller sets `status.artifact` on `OCIRepository` when the artifact is available, and reports readiness via standard conditions.

## Goals / Non-Goals

**Goals:**
- Look up the referenced `OCIRepository` by namespace/name from the controller-runtime client.
- Validate the source is ready (`Ready=True` condition).
- Extract `status.artifact` fields (URL, revision, digest) into a typed `ArtifactRef`.
- Set up OCIRepository watches so artifact changes trigger ModuleRelease reconciliation.

**Non-Goals:**
- Fetching or unpacking the artifact (that's change 3).
- Cross-namespace source references (ModuleRelease and OCIRepository must be in the same namespace for now).
- Supporting source types other than OCIRepository.

## Decisions

### 1. Source resolution returns a structured result, not raw Flux types

`Resolve` returns an `*ArtifactRef` with extracted fields rather than exposing `sourcev1.OCIRepository` to callers. This keeps the source package as the sole Flux integration point.

**Alternative considered:** Returning the full `OCIRepository` object. Rejected because downstream consumers only need artifact metadata, not the full Flux object.

### 2. Cross-namespace references deferred

For v1alpha1, the OCIRepository must be in the same namespace as the ModuleRelease. The `sourceRef.namespace` field is respected if set, but no RBAC or policy validation for cross-namespace access is implemented.

### 3. Watch via handler.EnqueueRequestsFromMapFunc

The controller watches OCIRepository objects and maps changes back to ModuleRelease objects that reference them using `handler.EnqueueRequestsFromMapFunc`. This follows the Flux controller pattern.

## Risks / Trade-offs

- **[Risk] Source not found vs not ready** — These are different failure modes (stalled vs soft-blocked). The resolver must distinguish them for correct condition reporting. Mitigation: return typed errors (`ErrSourceNotFound`, `ErrSourceNotReady`).
- **[Risk] Race between source update and reconcile** — The OCIRepository may update between resolution and fetch. Mitigation: the digest in `ArtifactRef` is verified during fetch (change 3).
