# Controller Tooling and Framework Decisions

## Summary

This document captures the framework and tooling decisions for the OPM proof-of-concept controller.

The conclusion is:

- use `controller-runtime` as the base controller framework
- use Flux GitOps Toolkit packages as the controller conventions and runtime toolset
- use Flux source APIs for source integration
- use Kubebuilder for project scaffolding and CRD/controller layout
- consume shared OPM helper APIs from the CLI as they are published

## Decision summary

### Base framework

Use:

- `sigs.k8s.io/controller-runtime`

Why:

- Flux itself builds on controller-runtime
- Flux runtime helpers are designed to compose with controller-runtime
- controller-runtime is the most standard substrate for Kubernetes controllers in Go
- it keeps the controller close to Kubernetes API conventions and ecosystem expectations

### Flux/GitOps Toolkit integration

Use:

- `github.com/fluxcd/pkg/runtime/conditions`
- `github.com/fluxcd/pkg/runtime/patch`
- `github.com/fluxcd/pkg/runtime/reconcile`
- `github.com/fluxcd/pkg/runtime/controller`
- `github.com/fluxcd/pkg/runtime/predicates`
- `github.com/fluxcd/pkg/ssa`
- `github.com/fluxcd/pkg/http/fetch`
- `github.com/fluxcd/pkg/tar`

Why:

- these packages codify Flux controller conventions
- they reduce boilerplate around conditions, patching, metrics, and result finalization
- they give the controller a Flux-native operational style without surrendering OPM-specific semantics

### Flux API usage

Use:

- `github.com/fluxcd/source-controller/api/v1`

Why:

- the controller must reference `OCIRepository`
- Flux explicitly supports consuming its API types through Go modules
- source-controller is the upstream source of truth for fetched artifacts

### Scaffolding

Use:

- `kubebuilder`

Why:

- it gives a standard controller-runtime project layout
- Flux’s own source-watcher guide recommends Kubebuilder when building a source-aware controller
- it keeps API generation, RBAC markers, and CRD generation straightforward

### Shared OPM code consumption

Use:

- public APIs and helpers from the CLI release artifacts / published module

Why:

- avoids duplicating inventory logic and related helpers
- aligns controller and CLI behavior
- fits the user’s plan to publish the CLI with Goreleaser

## Detailed tooling choices

### 1. Reconciler runtime

The reconciler should use controller-runtime manager/client/cache primitives directly.

Recommended usage:

- manager setup in `main.go`
- typed reconcilers for `ModuleRelease` and `BundleRelease`
- watches on release CRs and Flux source objects
- health/readiness endpoints from controller-runtime and Flux runtime helpers

### 2. Conditions and status patching

Use Flux runtime helpers instead of hand-rolling condition mutation logic.

Recommended packages:

- `github.com/fluxcd/pkg/runtime/conditions`
- `github.com/fluxcd/pkg/runtime/patch`
- `github.com/fluxcd/pkg/runtime/reconcile`

What they provide:

- kstatus-aligned condition handling
- safe serial patching with owned conditions
- status finalization and observed generation handling

Recommended pattern:

- create a patcher near the start of reconcile
- mutate status in-memory throughout reconcile
- finalize conditions using `ResultFinalizer`
- patch once at the end or at controlled reconciliation boundaries

### 3. Metrics, events, and rate limiting

Use Flux controller helpers for common operational behavior.

Recommended packages:

- `github.com/fluxcd/pkg/runtime/controller`

Useful pieces:

- metrics helper embedding
- event helper embedding
- wrapped builder support
- default rate limiter support

This keeps the controller closer to how Flux controllers expose metrics and runtime behavior.

### 4. Server-side apply engine

Use Flux `pkg/ssa` instead of a minimal hand-written apply loop.

Recommended package:

- `github.com/fluxcd/pkg/ssa`

Why:

- it already supports apply, diff, delete, wait, and staged apply
- it already implements drift-oriented dry-run comparisons
- it already handles immutable field recreation logic and metadata cleanup options
- it is much closer to what a GitOps-style controller needs than raw client patch calls alone

Recommended usage for OPM:

- convert rendered resources to `*unstructured.Unstructured`
- create one `ssa.ResourceManager`
- use `ApplyAllStaged` for CRDs / cluster-scoped resources / main resources
- use `DeleteAll` for stale owned resources
- optionally use `Diff` later for richer drift reporting

### 5. Artifact fetching

Use Flux artifact fetching helpers.

Recommended packages:

- `github.com/fluxcd/pkg/http/fetch`
- `github.com/fluxcd/pkg/tar`

Why:

- Flux source artifacts are exposed as fetchable tarballs
- Flux’s own source-watcher example uses `fetch.ArchiveFetcher`
- secure extraction helpers already exist in the toolkit

Recommended pattern:

- get artifact URL and digest from `OCIRepository.status.artifact`
- fetch into a temporary directory
- validate extracted layout for CUE module structure

### 6. Testing stack

Recommended layers:

- unit tests for inventory, digest, status, and conversion helpers
- controller env tests using controller-runtime envtest
- Flux `runtime/testenv` helpers where useful
- later: kind-based integration tests with real Flux source-controller installed

Recommended packages:

- `github.com/fluxcd/pkg/runtime/testenv`
- controller-runtime envtest
- `testing` + testify if desired

### 7. Release and dependency strategy

Because shared OPM helpers should come from the CLI, the controller should depend on:

- versioned public Go packages exposed by the CLI

This means:

- the CLI must publish stable public modules/helpers
- the controller should avoid importing CLI internal packages
- helper extraction in the CLI is part of the controller-enablement path

## What not to use as the primary abstraction

### Operator SDK

Operator SDK is not necessary as the primary framework.

Why not:

- it adds an extra abstraction layer above controller-runtime
- Flux tooling and examples already assume controller-runtime directly
- the POC benefits more from being close to Flux and Kubernetes primitives than from operator packaging abstractions

### Flux `HelmRelease` / `Kustomization` as the execution engine

Do not model OPM as a thin adapter to Flux apply controllers.

Why not:

- OPM must evaluate CUE natively
- OPM needs its own status and inventory semantics
- `HelmRelease` and `Kustomization` solve adjacent but different problems

They are useful research references, but not the execution substrate for OPM reconciliation.

## Recommended initial dependency set

At a high level, the controller should expect to use packages equivalent to:

```text
sigs.k8s.io/controller-runtime
github.com/fluxcd/source-controller/api/v1
github.com/fluxcd/pkg/runtime/conditions
github.com/fluxcd/pkg/runtime/patch
github.com/fluxcd/pkg/runtime/reconcile
github.com/fluxcd/pkg/runtime/controller
github.com/fluxcd/pkg/runtime/predicates
github.com/fluxcd/pkg/http/fetch
github.com/fluxcd/pkg/tar
github.com/fluxcd/pkg/ssa
```

And public OPM packages from the CLI for shared helpers as they are published.

## Practical implementation pattern

Recommended controller implementation style:

1. Scaffold with Kubebuilder.
2. Register OPM APIs and Flux source APIs in the scheme.
3. Build controllers with controller-runtime.
4. Use Flux runtime helpers for conditions, patching, metrics, and reconcile finalization.
5. Use Flux `ssa.ResourceManager` for apply/delete/wait.
6. Use Flux fetch helpers to consume source-controller artifacts.
7. Reuse published CLI helper packages for inventory and CUE-related logic.

## Decision checkpoint

The current recommendation is intentionally conservative:

- stay close to Kubernetes/controller-runtime norms
- stay close to Flux runtime conventions
- keep OPM-specific logic in OPM packages
- reuse the CLI where public APIs already exist or are being prepared

This gives the POC a solid foundation without over-designing a custom operator framework.
