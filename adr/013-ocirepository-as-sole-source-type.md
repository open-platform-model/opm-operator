# ADR-013: OCIRepository as Sole Source Type in v1alpha1

## Status

Accepted

## Context

Flux `source-controller` supports multiple source types: `OCIRepository`, `GitRepository`, `Bucket`, and `HelmRepository`. The controller's `spec.sourceRef` could be designed to accept any of these, providing flexibility in how CUE modules are sourced.

However, each source type has different artifact formats, status models, and content handling paths. Supporting multiple sources in the initial implementation would:

- increase the surface area of source resolution, validation, and content extraction code
- require testing each source type's interaction with native CUE module artifacts
- introduce ambiguity about which source types preserve the CUE `application/zip` layer correctly
- distract from the primary goal of proving the core reconcile loop

The proof-of-concept has enough uncertainty in the CUE OCI artifact handoff through Flux without adding multi-source complexity.

## Decision

Only Flux `source.toolkit.fluxcd.io/v1 OCIRepository` is supported as the source type in v1alpha1. No `GitRepository`, `Bucket`, `HelmRepository`, or direct OCI references are supported.

The `spec.sourceRef` must reference an `OCIRepository` that preserves the native CUE `application/zip` layer (via `spec.layerSelector.mediaType=application/zip` and `spec.layerSelector.operation=copy`).

The controller validates the source reference at reconcile time. If the referenced source is not an `OCIRepository` or does not satisfy the expected contract, the reconcile is classified as a stalled failure.

## Consequences

**Positive:** The source contract is narrow and well-defined. One source type means one validation path, one extraction path, and one set of compatibility requirements to test and maintain.

**Positive:** Aligns with OPM's native module distribution story. CUE modules are published to OCI registries, so `OCIRepository` is the natural Flux source type for consuming them.

**Positive:** Supporting one good source contract is more valuable for the POC than abstracting several incomplete ones. The core reconcile loop can be proven without source-type polymorphism.

**Negative:** Users who prefer Git-based workflows cannot use `GitRepository` as a source. They must publish their CUE modules to an OCI registry and reference them via `OCIRepository`.

**Trade-off:** Multi-source support is deferred, not rejected. Once the core loop is proven with `OCIRepository`, broader source support can be evaluated based on real demand and compatibility testing.

Related: [scope-and-non-goals.md](../docs/design/scope-and-non-goals.md), [cue-oci-artifacts-and-flux-source-controller.md](../docs/design/cue-oci-artifacts-and-flux-source-controller.md), [module-release-api.md](../docs/design/module-release-api.md)
