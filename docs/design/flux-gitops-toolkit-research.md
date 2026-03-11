# Flux and GitOps Toolkit Research Notes

## Purpose

This document records the research that informed the OPM controller architecture and tooling choices.

It combines:

- local inspection of `github.com/fluxcd/pkg` checked out at `/var/home/emil/Dev/open-platform-model/.external_repos/pkg`
- Flux documentation research online
- prior discussion of how Flux controllers such as `helm-controller` structure state and reconciliation

The goal is not to copy Flux controllers exactly, but to use Flux components and packages where they fit OPM well.

## High-level findings

### Flux is explicitly designed as reusable GitOps Toolkit components

Flux documents itself as a set of:

- specialized controllers
- composable APIs
- reusable Go packages

for building continuous delivery systems on Kubernetes.

This is important because it means using Flux APIs and packages in a custom OPM controller is not a hack; it is an intended extension path.

Source:

- `https://fluxcd.io/flux/components/`

### `github.com/fluxcd/pkg` is the shared GitOps Toolkit Go SDK

The local `pkg` repository and upstream README describe it as the shared Go SDK for:

- metadata APIs
- controller runtime helpers
- artifact management
- OCI helpers
- SSA-based resource management

This is exactly the layer we want for an OPM controller that should feel Flux-native without being forced into Helm/Kustomize semantics.

Sources:

- `/var/home/emil/Dev/open-platform-model/.external_repos/pkg/README.md`
- `https://github.com/fluxcd/pkg`

### Flux runtime packages are meant to be used together with controller-runtime

The Flux runtime README states that the packages build on:

- controller-runtime
- Kubernetes API conventions
- standard `metav1.Condition`
- kstatus conventions

It also highlights conditions, patching, events, metrics, reconcile helpers, predicates, probes, and testenv support.

That strongly supports the choice to use controller-runtime as the substrate and Flux runtime packages as the conventions layer.

Sources:

- `/var/home/emil/Dev/open-platform-model/.external_repos/pkg/runtime/README.md`
- `https://pkg.go.dev/github.com/fluxcd/pkg/runtime`

## Local package inspection findings

### 1. `pkg/runtime/conditions`

Flux runtime provides helpers to:

- get and set conditions
- mark conditions true/false/unknown/reconciling/stalled
- summarize multiple conditions into `Ready`

Why this matters for OPM:

- `ModuleRelease` and `BundleRelease` should use standard condition handling
- `Ready`, `Reconciling`, and `Stalled` should follow Flux-style semantics where practical

Relevant docs:

- `/var/home/emil/Dev/open-platform-model/.external_repos/pkg/runtime/README.md`

### 2. `pkg/runtime/patch`

Flux provides helpers for safe patching and a `SerialPatcher` implementation.

The local code shows `SerialPatcher` keeps a previous object copy and patches serially against new object state.

Why this matters for OPM:

- controller status updates should be safe and conflict-aware
- status patching should avoid ad hoc update logic

Relevant code:

- `/var/home/emil/Dev/open-platform-model/.external_repos/pkg/runtime/patch/serial.go`

### 3. `pkg/runtime/reconcile`

Flux provides `ResultFinalizer` and `ProgressiveStatus` helpers.

The local code shows `ResultFinalizer`:

- aligns status with kstatus expectations
- manages `Ready`, `Reconciling`, and `Stalled`
- integrates observed generation patch options

Why this matters for OPM:

- OPM should not invent bespoke status semantics if a good standard already exists
- a Flux-style result finalization path makes controller behavior easier to reason about

Relevant code:

- `/var/home/emil/Dev/open-platform-model/.external_repos/pkg/runtime/reconcile/result.go`

### 4. `pkg/runtime/controller`

Local inspection shows Flux runtime controller helpers include:

- metrics embedding
- wrapped builders
- queue/requeue helpers
- rate limiter helpers

The builder wrapper and reconciler wrapper are especially useful because they align watches and queue semantics with Flux conventions.

Relevant code:

- `/var/home/emil/Dev/open-platform-model/.external_repos/pkg/runtime/controller/doc.go`
- `/var/home/emil/Dev/open-platform-model/.external_repos/pkg/runtime/controller/builder.go`
- `/var/home/emil/Dev/open-platform-model/.external_repos/pkg/runtime/controller/reconciler.go`
- `/var/home/emil/Dev/open-platform-model/.external_repos/pkg/runtime/controller/rate_limiter.go`
- `/var/home/emil/Dev/open-platform-model/.external_repos/pkg/runtime/controller/metrics.go`

### 5. `pkg/ssa`

This is one of the strongest fits for OPM.

Local inspection shows `ssa.ResourceManager` supports:

- server-side apply
- dry-run drift checks
- immutable field recreation logic
- staged apply for CRDs and cluster definitions
- delete and wait flows
- ownership labels

This is very close to the operational behavior OPM needs for rendered Kubernetes resources.

Relevant code:

- `/var/home/emil/Dev/open-platform-model/.external_repos/pkg/ssa/manager.go`
- `/var/home/emil/Dev/open-platform-model/.external_repos/pkg/ssa/manager_apply.go`

### 6. `pkg/http/fetch` and `pkg/tar`

Flux source artifacts are exposed as downloadable archives. Flux’s own source-watcher example uses `fetch.ArchiveFetcher` and tar extraction utilities.

Why this matters for OPM:

- it gives us a standard way to consume artifacts produced by source-controller
- it fits the requirement that OPM should use Flux for source acquisition but do its own evaluation after fetch

Relevant docs:

- `https://fluxcd.io/flux/gitops-toolkit/source-watcher/`
- `/var/home/emil/Dev/open-platform-model/.external_repos/pkg/README.md`

### 7. `pkg/runtime/testenv`

Flux provides envtest helpers for local Kubernetes API server based tests.

Why this matters for OPM:

- the controller should have strong controller-level and API-level tests early
- these helpers can reduce friction when setting up test environments

Relevant code:

- `/var/home/emil/Dev/open-platform-model/.external_repos/pkg/runtime/testenv/doc.go`

## Online research findings

### Flux source-controller is the right source integration point

Flux documentation makes clear that source-controller is the component responsible for producing artifacts from source definitions like `GitRepository`, `Bucket`, and `OCIRepository`.

For OPM, this means:

- OPM should consume source-controller artifacts
- OPM should not duplicate source-controller responsibilities
- `OCIRepository` is the most relevant Flux source type for OCI-based CUE modules

Sources:

- `https://fluxcd.io/flux/components/source/`
- `https://fluxcd.io/flux/components/source/ocirepositories/`

### Flux recommends Kubebuilder for source-aware custom controllers

The Flux source-watcher guide explicitly walks through building a custom controller with Kubebuilder that watches Flux source objects and fetches source-controller artifacts.

That is a very strong signal for the OPM POC stack:

- use Kubebuilder
- use controller-runtime
- use Flux source APIs
- use Flux fetch helpers

Source:

- `https://fluxcd.io/flux/gitops-toolkit/source-watcher/`

### Flux API types are intended to be imported and consumed from Go

Flux documents how to `go get` and import its API packages for:

- source-controller
- helm-controller
- kustomize-controller
- notification-controller

This confirms that referencing `OCIRepository` types directly from Go is part of the intended developer workflow.

Source:

- `https://fluxcd.io/flux/gitops-toolkit/packages/`

## Research on Flux controller patterns

### Helm controller pattern: two layers of state

From prior research and Flux docs, `helm-controller` keeps state in two places:

- Helm storage backend for actual release state and manifest history
- `HelmRelease.status` as the controller ledger of observations, digests, counters, and summary inventory

The important lesson for OPM is not to copy Helm storage literally, but to copy the separation of concerns:

- one place tracks applied/owned runtime state
- another place tracks reconcile metadata and history

For OPM, the better mapping is:

- `status.inventory` = ownership state
- digest fields + `status.history` = controller ledger

### Kustomize controller pattern: source-controller + SSA + status conditions

Flux documentation presents kustomize-controller as:

- source-controller consumer
- manifest generator/validator
- SSA-style apply/prune operator
- status/health reporter

The architectural analogy for OPM is strong, even though OPM uses CUE instead of Kustomize:

- consume artifacts from source-controller
- generate desired resources
- apply and prune with SSA
- publish conditions and operational status

Source:

- `https://fluxcd.io/flux/components/kustomize/`

## Conclusions from the research

### What OPM should reuse directly

Strong candidates for direct reuse:

- controller-runtime base manager/reconciler model
- Flux source APIs
- Flux runtime condition helpers
- Flux patch helpers
- Flux reconcile finalizer helpers
- Flux metrics/events/rate-limiter helpers
- Flux SSA resource manager
- Flux artifact fetch helpers

### What OPM should not outsource to Flux

OPM should not outsource:

- CUE module validation
- CUE evaluation
- OPM-specific release status schema
- OPM ownership inventory semantics
- OPM module/bundle domain semantics

### Why this hybrid is the best fit

This approach gives OPM:

- mature source integration
- mature controller runtime conventions
- mature SSA apply/prune machinery
- freedom to stay CUE-native and OPM-native at the domain layer

It is the best balance between:

- reusing proven GitOps building blocks
- not forcing OPM into Helm/Kustomize-shaped abstractions

## Sources

Online sources referenced in this research:

- Flux components overview: `https://fluxcd.io/flux/components/`
- Flux Helm controller docs: `https://fluxcd.io/flux/components/helm/`
- Flux Kustomize controller docs: `https://fluxcd.io/flux/components/kustomize/`
- GitOps Toolkit packages guide: `https://fluxcd.io/flux/gitops-toolkit/packages/`
- Flux source-watcher guide: `https://fluxcd.io/flux/gitops-toolkit/source-watcher/`
- `fluxcd/pkg` repository: `https://github.com/fluxcd/pkg`

Local sources inspected:

- `/var/home/emil/Dev/open-platform-model/.external_repos/pkg/README.md`
- `/var/home/emil/Dev/open-platform-model/.external_repos/pkg/runtime/README.md`
- `/var/home/emil/Dev/open-platform-model/.external_repos/pkg/runtime/controller/doc.go`
- `/var/home/emil/Dev/open-platform-model/.external_repos/pkg/runtime/controller/builder.go`
- `/var/home/emil/Dev/open-platform-model/.external_repos/pkg/runtime/controller/reconciler.go`
- `/var/home/emil/Dev/open-platform-model/.external_repos/pkg/runtime/controller/rate_limiter.go`
- `/var/home/emil/Dev/open-platform-model/.external_repos/pkg/runtime/controller/metrics.go`
- `/var/home/emil/Dev/open-platform-model/.external_repos/pkg/runtime/reconcile/result.go`
- `/var/home/emil/Dev/open-platform-model/.external_repos/pkg/runtime/patch/serial.go`
- `/var/home/emil/Dev/open-platform-model/.external_repos/pkg/ssa/manager.go`
- `/var/home/emil/Dev/open-platform-model/.external_repos/pkg/ssa/manager_apply.go`
- `/var/home/emil/Dev/open-platform-model/.external_repos/pkg/runtime/testenv/doc.go`
