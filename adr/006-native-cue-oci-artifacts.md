# ADR-006: Native CUE OCI Artifacts

## Status

Accepted

## Context

CUE modules are distributed through OCI registries using a defined artifact format:

- manifest media type: `application/vnd.oci.image.manifest.v1+json`
- artifact type: `application/vnd.cue.module.v1+json`
- module content layer: `application/zip`
- module file layer: `application/vnd.cue.modulefile.v1`

Flux `source-controller`'s `OCIRepository` content handling path is documented around `tar+gzip` layers. Flux can select layers by media type via `spec.layerSelector` and supports `operation: copy` to preserve original content unaltered, but the primary extraction model assumes tarball-compressed content.

This creates a format mismatch: CUE uses `application/zip`, Flux's happy path assumes `tar+gzip`.

The question is whether OPM should redefine its artifact format around Flux conventions, or continue targeting native CUE module artifacts and absorb any extra integration work.

## Decision

The controller targets native CUE OCI module artifacts as the source format. It does not redefine module distribution around Flux's tarball-centric conventions.

For source resolution via Flux `OCIRepository`:

- The `OCIRepository` must use `spec.layerSelector.mediaType=application/zip` and `spec.layerSelector.operation=copy` to preserve the native CUE zip layer.
- Flux handles OCI reference resolution, digest tracking, authentication, and provenance.
- OPM handles any required zip unpacking and CUE module validation after fetching the artifact.

If the native CUE zip layer cannot be consumed cleanly through Flux, the fallback path (in priority order) is:

1. Keep native CUE modules, add OPM-owned zip handling in the fetch/unpack path.
2. Add an auxiliary publication path while preserving native CUE compatibility.
3. Define an OPM-specific artifact shape only as a last resort.

## Consequences

**Positive:** OPM stays aligned with the CUE ecosystem. Module publication uses the same format as CUE itself, avoiding a second artifact standard. Developers publishing CUE modules do not need a separate publication step for OPM consumption.

**Positive:** The conceptual separation between transport (Flux) and semantics (OPM) is preserved cleanly. Flux resolves the artifact; OPM interprets its content.

**Negative:** The zip/tar+gzip mismatch means the controller may need OPM-owned unpacking logic for the CUE module payload. This adds complexity in the fetch/unpack path that would not exist if the artifact format matched Flux conventions.

**Negative:** The `OCIRepository` must be configured with specific `layerSelector` settings. Misconfigured sources (missing `operation: copy` or wrong media type) will fail. The controller validates this and fails clearly rather than silently.

**Trade-off:** A compatibility spike is recommended before deep implementation to verify that Flux source-controller preserves the native CUE zip layer faithfully. If it does not, OPM absorbs the gap rather than changing the artifact format.

Related: [cue-oci-artifacts-and-flux-source-controller.md](../docs/design/cue-oci-artifacts-and-flux-source-controller.md), [scope-and-non-goals.md](../docs/design/scope-and-non-goals.md)
