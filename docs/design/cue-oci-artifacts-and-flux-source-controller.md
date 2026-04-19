# CUE OCI Artifacts and Flux Source Controller

## Summary

This document captures the current understanding of how native CUE OCI modules fit with Flux `source-controller`, and what that means for the OPM proof-of-concept controller.

The central question is:

> Can the OPM controller use Flux `OCIRepository` as the source mechanism for native CUE OCI module artifacts without redefining the artifact format?

The current answer is:

- yes for OCI reference resolution, digest tracking, provenance, and source status
- likely yes for registry compatibility in general
- not yet proven for content extraction in the exact native CUE module format
- likely requires OPM to own the final content interpretation and unpacking path

The most important conclusion is that OPM should continue to optimize for native CUE OCI modules and treat any extra integration work with Flux as OPM controller responsibility, rather than redefining the artifact around Flux-specific conventions.

## Why this matters

The controller architecture has already settled on two principles:

- OPM remains CUE-native end to end
- Flux `source-controller` is used for source acquisition

That means the source story must support native CUE OCI artifacts, not only Flux-style tarball artifacts containing YAML.

If native CUE OCI modules and Flux `OCIRepository` are sufficiently compatible, then the controller can:

- reuse a mature source controller
- keep module publishing aligned with CUE itself
- avoid inventing a custom OPM-specific OCI artifact format too early

If they are not sufficiently compatible, then OPM would need to either:

- add an OPM-owned fetch/unpack bridge while keeping native CUE module artifacts, or
- redesign the published artifact format

The first option is strongly preferred.

## Research findings

### Flux `OCIRepository` is generic at the OCI artifact level

Flux documentation presents `OCIRepository` as a generic OCI source.

The OCI cheatsheet states that Flux can consume OCI artifacts containing arbitrary configuration, not only Kubernetes YAML. The docs explicitly note that non-YAML content is a valid use case, and mention controllers such as `tofu-controller` as consumers of OCI artifacts with non-Kubernetes-specific content.

This means OPM does not need to publish a Flux-specific artifact type merely to be usable by Flux.

Relevant sources:

- `https://fluxcd.io/flux/cheatsheets/oci-artifacts/`
- `https://fluxcd.io/flux/components/source/ocirepositories/`

### Flux `OCIRepository` content handling is layer-format sensitive

Flux documentation for `OCIRepository` includes an important limitation around selected layer handling.

The documented behavior is:

- `spec.layerSelector` can select a layer by media type
- if no `layerSelector` is specified, source-controller extracts the first layer found in the artifact
- the selected layer must be compressed in `tar+gzip` format
- `layerSelector.operation` can be `extract` or `copy`
- `copy` keeps the original tarball content unaltered in storage

The key point is that the documented content path assumes a `tar+gzip` layer.

This is not a generic "any blob format" promise; it is a generic OCI source with a documented tarball-oriented extraction model.

Relevant source:

- `https://fluxcd.io/flux/components/source/ocirepositories/`

### Flux OCI artifacts produced by Flux CLI are not the same as native CUE modules

Flux documents its own `flux push artifact` output format as:

- manifest media type: `application/vnd.oci.image.manifest.v1+json`
- config media type: `application/vnd.cncf.flux.config.v1+json`
- content media type: `application/vnd.cncf.flux.content.v1.tar+gzip`

That is a tarball-based format designed for Flux artifact consumption.

This is useful as a reference point because it highlights the shape Flux is most obviously optimized for, but it is not the same thing as a native CUE module artifact.

Relevant source:

- `https://fluxcd.io/flux/cheatsheets/oci-artifacts/`

### Native CUE modules are distributed through OCI registries

CUE documentation is explicit that modules are downloaded from OCI-compliant registries.

The module system is built around OCI registries, and CUE's registry configuration maps module paths to registry locations.

This confirms that using OCI as the publication/distribution mechanism is native to CUE itself, not something OPM is layering on top.

Relevant sources:

- `https://cuelang.org/docs/concept/modules-packages-instances/`
- `https://cuelang.org/docs/reference/command/cue-help-registryconfig/`

### Native CUE module artifact layout is explicitly defined and uses `zip`

CUE module reference documentation defines the stored OCI artifact shape for modules.

A CUE module is stored in a registry as:

- top-level manifest media type: `application/vnd.oci.image.manifest.v1+json`
- artifact type: `application/vnd.cue.module.v1+json`
- first blob/layer media type: `application/zip`
- second blob/layer media type: `application/vnd.cue.modulefile.v1`

The first blob contains the full module contents.
The second blob contains an exact copy of `cue.mod/module.cue` to allow quick dependency access without downloading the whole module archive.

This is the key compatibility detail for the OPM controller.

Relevant source:

- `https://cuelang.org/docs/reference/modules/`

## Compatibility analysis

### What appears compatible already

The following parts of the controller source flow appear compatible between native CUE module artifacts and Flux `OCIRepository`:

- OCI reference resolution by tag, digest, or semver
- registry authentication
- source verification and provenance tracking
- digest and revision reporting
- source readiness conditions
- artifact metadata projection into Kubernetes status

This means Flux should still be the right control point for saying:

- which source artifact was selected
- what digest it had
- whether it was verified
- whether it was successfully fetched and stored

### What is not yet proven

The unproven area is content extraction and handoff for the actual CUE module payload.

The documented native CUE module archive uses:

- `application/zip` for the full module content

The documented Flux source-controller layer handling path expects:

- `tar+gzip` for layer extraction/copy workflows

This does not prove incompatibility, but it does show a real format mismatch between:

- CUE's documented storage format
- Flux's documented happy path for selected layers

The open question is therefore specific:

> Can Flux `OCIRepository` make a native CUE `application/zip` module layer available in a way the OPM controller can reliably consume, without redefining the module artifact format?

### What this likely means in practice

The most likely outcomes are:

1. `OCIRepository` can still resolve and store the source artifact, but OPM must perform any extra unpacking or interpretation itself.
2. `OCIRepository` may require explicit layer selection if the CUE module layer is not the default first layer Flux should use.
3. The controller may need to consume the resolved artifact as an opaque source artifact and then apply its own zip handling after fetch.

This is acceptable and still consistent with the architecture.

## Design decision

### Decision

The POC controller should continue to target native CUE OCI modules as the source artifact format.

The controller should continue to use Flux `OCIRepository` for:

- source reference and resolution
- source polling
- authentication
- verification
- digest/revision status

The controller should not redefine module distribution around Flux's tarball-centric OCI format unless native CUE modules prove impossible to use in practice.

### Rationale

This keeps OPM aligned with the CUE ecosystem and avoids prematurely inventing a second module artifact standard.

It also preserves the most important conceptual separation:

- Flux owns source acquisition
- OPM owns CUE semantics

Even if OPM has to absorb a little more complexity in the fetch/unpack path, that is a better trade-off than changing the artifact identity away from native CUE modules.

## Implications for the controller

### Responsibilities owned by Flux

Flux should own:

- OCI reference resolution
- source polling and refresh
- source verification
- source authentication
- source artifact digest and revision reporting
- source readiness conditions

### Responsibilities owned by OPM

OPM should own:

- validating that the resolved source artifact is a CUE module
- handling any required zip/module unpacking if Flux does not hand over extracted content directly
- evaluating the CUE module
- computing desired Kubernetes resources
- applying and pruning resources
- tracking ownership inventory
- recording release digests and history in CR status

### Status model remains unchanged

The controller status model proposed elsewhere remains valid regardless of the exact unpacking handoff path.

The controller should still record:

- resolved source artifact revision
- resolved source artifact digest
- config digest
- render digest
- inventory digest and count
- ownership inventory
- bounded history

## Recommended implementation approach

### Phase 1: compatibility spike

Before implementing the full reconcile path, run a focused spike against a real native CUE module artifact.

The spike should:

1. publish a minimal module using the normal CUE module publication flow
2. create a Flux `OCIRepository` pointing at that artifact
3. inspect the resulting source-controller status and artifact behavior
4. determine whether the controller can consume the content directly or whether an extra OPM unpacking step is required

The spike is specifically trying to answer:

- whether Flux accepts the artifact without issue
- what `OCIRepository.status.artifact` contains
- whether the selected content is the CUE `application/zip` module payload
- whether OPM can convert that source artifact into a usable on-disk module tree

### Phase 2: implement OPM-owned unpacking if needed

If Flux source-controller does not provide the extracted module tree in a directly usable form, the controller should:

- still rely on `OCIRepository` for source resolution
- fetch the resolved artifact content
- unpack the module content itself
- validate `cue.mod` and module layout
- continue with normal CUE evaluation

This remains fully consistent with the controller architecture.

### Phase 3: only evaluate alternate artifact publication if strictly necessary

If native CUE module artifacts prove too awkward for source-controller consumption, evaluate fallback options in this order:

1. keep native CUE OCI modules and add OPM-owned handling for the `zip` payload
2. add an auxiliary publication path while preserving native CUE compatibility
3. define an OPM-specific artifact shape only as a last resort

The default position should remain native CUE modules first.

## Risks

### Risk: documented Flux layer handling is stricter than expected

If Flux source-controller assumes `tar+gzip` in more places than currently documented, native CUE modules may require more integration work than expected.

Mitigation:

- validate the real behavior with a minimal module artifact spike before deep implementation work

### Risk: source-controller stores the artifact but not in the ideal consumer form

Even if the source resolves and becomes ready, the OPM controller may still need an additional transformation or unpack step.

Mitigation:

- make unpacking/validation an explicit controller concern rather than assuming Flux will do it for us

### Risk: future bundle/module publication contracts diverge

If bundles and modules end up using different OCI publication shapes, controller complexity increases.

Mitigation:

- define one consistent OPM source-artifact expectation early

## Open questions

- Can Flux `OCIRepository` select or preserve the native CUE `application/zip` layer cleanly enough for OPM to consume it directly?
- Does the stored source-controller artifact contain enough information for OPM to retrieve the original module payload without losing fidelity?
- Should `ModuleRelease.spec.module.path` remain mandatory when the artifact root is itself a single module?
- Should module and bundle artifacts share exactly the same publication contract in the POC?

## Current recommendation

The current recommendation is:

- proceed assuming native CUE OCI modules remain the source artifact contract
- proceed assuming Flux `OCIRepository` remains the source object contract
- explicitly budget a compatibility spike before implementing the real fetch/render path
- bias toward OPM-owned unpacking rather than redefining the artifact format

This is the cleanest path that preserves both CUE-native distribution and Flux-native source management.

## Sources

Online sources:

- Flux OCIRepository docs: `https://fluxcd.io/flux/components/source/ocirepositories/`
- Flux OCI cheatsheet: `https://fluxcd.io/flux/cheatsheets/oci-artifacts/`
- CUE modules, packages, and instances: `https://cuelang.org/docs/concept/modules-packages-instances/`
- CUE registry configuration reference: `https://cuelang.org/docs/reference/command/cue-help-registryconfig/`
- CUE modules reference: `https://cuelang.org/docs/reference/modules/`

Local repository context used during analysis:

- `opm-operator/docs/design/controller-architecture.md`
- `opm-operator/docs/design/controller-tooling.md`
- `opm-operator/docs/design/module-release-api.md`
- `opm-operator/docs/design/flux-gitops-toolkit-research.md`
