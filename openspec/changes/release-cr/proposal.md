## Why

The controller supports `ModuleRelease` for single-module deployments via CUE-native OCI resolution, but this requires applying CRD manifests directly. Teams managing many releases across environments need GitOps — release definitions live in git as CUE packages, Flux delivers them. There is no CRD that bridges Flux source-controller artifacts to the OPM render pipeline.

## What Changes

- **New CRD `Release`**: Points to a Flux source (GitRepository, OCIRepository, Bucket) plus a path within the artifact. The controller fetches the artifact, navigates to the path, loads `release.cue`, detects whether it evaluates to `#ModuleRelease` or `#BundleRelease`, and runs the existing render → apply pipeline.
- **New reconciler `ReleaseReconciler`**: Watches Release CRs and Flux source objects. Reconciles on CR change, source artifact revision change, and interval tick.
- **Flux-standard fields**: `interval`, `dependsOn`, `prune`, `suspend`, `serviceAccountName`, `rollout` — following established Flux CRD patterns.
- **No values in CR**: The CUE package in the artifact is the single source of truth for values. GitOps: change values in git, push, Flux delivers.
- **Tar.gz extraction support**: Flux GitRepository/Bucket artifacts use tar.gz format. The existing `internal/source/` package handles zip only. Add tar.gz extraction.
- **Status mirrors `ModuleRelease`**: Same inventory, digests, history, conditions, failure counters shape.

## Capabilities

### New Capabilities

- `release-artifact-loading`: Fetching a Flux source artifact, unpacking it (tar.gz or zip), navigating to `spec.path`, and loading `release.cue` via the CUE evaluator with `CUE_REGISTRY` set.
- `release-kind-detection`: Runtime detection of whether the loaded CUE value is a `#ModuleRelease` or `#BundleRelease`, dispatching to the appropriate render pipeline.
- `release-reconcile-loop`: Full reconcile loop for the Release CRD — source resolution, artifact fetch, CUE load, kind detection, render, apply, prune, status update, with interval-based re-reconciliation and `dependsOn` ordering.
- `release-depends-on`: Cross-Release dependency ordering — a Release with `dependsOn` entries waits until all referenced Releases have `Ready=True` before reconciling.

### Modified Capabilities

- `source-resolution`: Add support for resolving GitRepository and Bucket sources (currently only OCIRepository). Release CRD uses the same `internal/source/` package.
- `artifact-fetch`: Add tar.gz extraction alongside existing zip extraction. GitRepository and Bucket artifacts use tar.gz; OCIRepository artifacts use zip.

## Impact

- **API**: New CRD `Release` in `releases.opmodel.dev/v1alpha1`. No changes to existing `ModuleRelease` or `BundleRelease` CRDs.
- **RBAC**: New RBAC markers for Release CR + expanded source watches (GitRepository, Bucket in addition to OCIRepository).
- **Dependencies**: `github.com/fluxcd/source-controller/api` already imported. GitRepository and Bucket types available.
- **SemVer**: MINOR (additive, v1alpha1).
- **Coexistence**: Release and ModuleRelease are independent entry points into the same render pipeline. No migration required.
