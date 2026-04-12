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

## Testing Strategy

### Unit tests for `Resolve` (fake client, no cluster)

Use `controller-runtime/pkg/client/fake` to build a client with `sourcev1.AddToScheme`. Fabricate `OCIRepository` objects with specific `status.conditions` and `status.artifact` values. Each spec scenario maps 1:1 to a test case.

Tests must be behavioral — assert exact field mapping, not just absence of error:

- **Source found and ready**: Assert `ArtifactRef.URL == status.artifact.url`, `ArtifactRef.Revision == status.artifact.revision`, `ArtifactRef.Digest == status.artifact.checksum`. All three fields must be populated.
- **Source not found**: Assert `errors.Is(err, ErrSourceNotFound)` returns true. Assert the wrapped error includes the source name for debuggability.
- **Source not ready** (`Ready=False` or `Ready=Unknown`): Assert `errors.Is(err, ErrSourceNotReady)` returns true.
- **Source ready but nil artifact**: Assert `errors.Is(err, ErrSourceNotReady)` returns true. This distinguishes "source not yet reconciled" from "source broken."
- **Namespace resolution**: Assert same-namespace default when `sourceRef.Namespace` is empty. Assert override when `sourceRef.Namespace` is set.

### Envtest integration for controller watch

Requires the Flux OCIRepository CRD loaded into envtest. Vendor the CRD YAML into `internal/controller/testdata/crds/` and add it to `CRDDirectoryPaths` in `suite_test.go`. Register `sourcev1.AddToScheme` in the test scheme.

- **Watch triggers reconciliation**: Create an OCIRepository and a ModuleRelease referencing it. Update the OCIRepository's `status.artifact`. Assert the ModuleRelease is enqueued for reconciliation.
- **Map function correctness**: Create multiple ModuleReleases, only some referencing a given OCIRepository. Update that OCIRepository. Assert only the referencing ModuleReleases are enqueued.

### CRD vendoring for envtest

The `source-controller/api` Go module does not ship CRD YAML. Obtain `source.toolkit.fluxcd.io_ocirepositories.yaml` from the `fluxcd/source-controller` repo's `config/crd/bases/`. Place it in `internal/controller/testdata/crds/`. This CRD will also be needed by subsequent changes (03+), so it is shared infrastructure.

## Risks / Trade-offs

- **[Risk] Source not found vs not ready** — These are different failure modes (stalled vs soft-blocked). The resolver must distinguish them for correct condition reporting. Mitigation: return typed errors (`ErrSourceNotFound`, `ErrSourceNotReady`).
- **[Risk] Race between source update and reconcile** — The OCIRepository may update between resolution and fetch. Mitigation: the digest in `ArtifactRef` is verified during fetch (change 3).
