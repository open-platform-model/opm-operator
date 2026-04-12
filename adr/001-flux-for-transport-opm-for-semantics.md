# ADR-001: Flux for Transport, OPM for Semantics

## Status

Accepted

## Context

The OPM controller needs a source acquisition mechanism for CUE OCI module artifacts. Two broad approaches exist: build a custom OCI artifact polling and storage loop inside the controller, or delegate source acquisition to an existing tool.

Flux `source-controller` already provides a mature, proven path for OCI artifact resolution, authentication, verification, digest tracking, and provenance. It is generic enough to reference non-YAML OCI artifacts, and other projects (such as `tofu-controller`) already consume Flux sources for non-Kubernetes content.

At the same time, Flux should not be asked to understand OPM's CUE module semantics. Flux does not know how to validate a CUE module layout, evaluate CUE, or compute desired Kubernetes resources from CUE definitions. Mixing source transport with semantic evaluation would couple two fundamentally different concerns.

The question is where to draw the boundary between what Flux owns and what OPM owns.

## Decision

Source acquisition is delegated to Flux `source-controller`, specifically `OCIRepository`. OPM retains full responsibility for CUE semantic evaluation.

Flux owns:

- OCI reference resolution (tag, digest, semver)
- source polling and refresh
- registry authentication
- artifact verification and provenance tracking
- digest and revision reporting
- source readiness conditions

OPM owns:

- validating that the resolved artifact contains a CUE module
- any required unpacking or content recovery from the Flux artifact handoff
- CUE module evaluation
- computing desired Kubernetes resources
- SSA apply and prune
- ownership inventory and release status

The controller watches its own release CRs and the referenced Flux source objects, consuming the resolved artifact from `OCIRepository.status.artifact`. The controller does not implement its own OCI polling loop.

This is not a thin wrapper around Flux `Kustomization` or `HelmRelease`. Those CRDs assume YAML or Helm semantics. OPM's source is CUE-native, and the release semantics remain OPM-managed.

## Consequences

**Positive:** The controller inherits a mature source acquisition path with existing support for authentication, verification, and digest tracking. The controller codebase stays focused on OPM-specific concerns rather than reimplementing source infrastructure. The conceptual boundary between transport and semantics is clean and testable.

**Positive:** Flux's `OCIRepository` status model provides structured artifact metadata that the controller can project directly into `ModuleRelease.status.source`.

**Negative:** If Flux source-controller's content handling path (documented as tar+gzip oriented) does not cleanly preserve native CUE `application/zip` layers, the OPM controller must absorb additional unpacking complexity. This is an acceptable trade-off because the alternative (redefining the artifact format around Flux conventions) would compromise CUE-native distribution.

**Trade-off:** The controller depends on Flux source-controller being installed in the cluster. This is a deliberate coupling: Flux is the most mature GitOps Toolkit source implementation, and OPM benefits more from reusing it than from building a competing source loop.

Related: [controller-architecture.md](../docs/design/controller-architecture.md), [cue-oci-artifacts-and-flux-source-controller.md](../docs/design/cue-oci-artifacts-and-flux-source-controller.md)
