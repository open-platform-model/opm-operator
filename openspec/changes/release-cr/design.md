## Context

The controller has two entry points into the OPM render pipeline today:

- **ModuleRelease** — CUE-native OCI resolution. The controller synthesizes a `#ModuleRelease` CUE package, CUE resolves the module from the registry, and the result flows through render → apply. No Flux source-controller dependency. Good for single-module, imperative deployments.
- **BundleRelease** — Has a `sourceRef` field pointing to a Flux OCIRepository. Reconciliation is not yet implemented.

Neither supports GitOps. Teams managing many releases across environments maintain CUE release packages in git (`releases/<env>/<module>/release.cue`), but there is no way to point the controller at a Flux-delivered artifact containing those packages.

The `internal/source/` package already provides `Resolve()` (OCIRepository lookup) and `ArtifactFetcher` (zip extraction with digest verification). The CLI's `pkg/loader/` already provides `LoadReleaseFile()` (CUE evaluation from a directory path) and `DetectReleaseKind()`.

## Goals / Non-Goals

**Goals:**

- New `Release` CRD that bridges Flux source-controller artifacts to the OPM render pipeline.
- One Release CR = one CUE release package at a specific path in the artifact.
- Support both `#ModuleRelease` and `#BundleRelease` CUE definitions, detected at runtime.
- Standard Flux-style fields: `sourceRef`, `interval`, `suspend`, `dependsOn`, `prune`.
- Reuse the existing shared `pkg/` pipeline (loader → module/parse → render → core).
- Reuse `internal/apply/`, `internal/inventory/`, `internal/status/`.
- Tar.gz extraction for GitRepository/Bucket artifacts alongside existing zip for OCIRepository.

**Non-Goals:**

- Multi-package discovery (one Release CR pointing at a directory of many release packages). One path = one release.
- Values in the Release CR. The CUE package is the single source of truth; change values in git, not in the CR.
- Replacing ModuleRelease. Both CRDs coexist — different entry points, same pipeline.
- BundleRelease reconciliation. Separate change.

## Decisions

### 1. No synthesis step — load CUE package directly from artifact

**Decision:** The Release reconciler skips `internal/synthesis/` entirely. The CUE release package already exists in the artifact.

**Rationale:** ModuleRelease needs synthesis because the CR only carries `module.path` + `module.version` — the controller must construct the CUE expression. With Release, the full CUE package (release.cue, values.cue, cue.mod/) is authored by the team and delivered via the artifact. The controller just navigates to the path and evaluates.

**Alternative:** Synthesize anyway, extracting module reference from the CUE package and rebuilding. Rejected — adds complexity for no benefit; the package is already complete.

### 2. Runtime kind detection via CUE `kind` field

**Decision:** After CUE evaluation, inspect the `kind` field to determine `ModuleRelease` vs `BundleRelease`, then dispatch to the appropriate render pipeline branch.

**Rationale:** The CLI already does this via `DetectReleaseKind()`. The release.cue evaluates to a concrete `#ModuleRelease` or `#BundleRelease` value, both of which carry a `kind` field. This makes the Release CRD polymorphic — one CR handles both types.

**Alternative:** Require the kind in the Release CR spec. Rejected — redundant with the CUE definition and creates drift risk.

### 3. Source resolution expanded to GitRepository and Bucket

**Decision:** Extend `internal/source/Resolve()` to handle GitRepository, OCIRepository, and Bucket source kinds. All three follow the same Flux pattern: status.artifact contains URL, revision, digest.

**Rationale:** OCIRepository-only is insufficient for GitOps. GitRepository is the primary use case (release packages in git). Bucket supports S3-compatible sources.

**Alternative:** Only support OCIRepository (push git as OCI). Rejected — adds friction; teams already have GitRepository sources.

### 4. Artifact extraction format detected from source kind

**Decision:** Add `extractTarGz()` alongside `extractZip()` in `internal/source/`. Dispatch based on source kind: GitRepository/Bucket → tar.gz, OCIRepository → zip.

**Rationale:** Flux GitRepository artifacts are tar.gz, CUE OCI artifacts are zip. The fetcher must handle both.

### 5. Status mirrors ModuleRelease

**Decision:** `ReleaseStatus` reuses the same shape: conditions, digests (source, config, render), inventory, history, failure counters, `nextRetryAt`. Add `source` field for resolved artifact metadata.

**Rationale:** Consistency. Operators learn one status shape. The same `internal/status/` helpers work for both.

### 6. DependsOn checks Ready condition of referenced Releases

**Decision:** Before reconciling, the controller checks each `dependsOn` reference. If any referenced Release is not `Ready=True`, requeue with interval and do not proceed.

**Rationale:** Standard Flux pattern (Kustomization `dependsOn`). Enables ordering: infrastructure releases (metallb, cert-manager) before application releases.

### 7. Reconcile triggers: CR change + source revision + interval

**Decision:** The Release controller watches:
- Release CR changes (via `For()`).
- Flux source object changes (via `Watches()` with `handler.EnqueueRequestsForOwner` or custom mapper).
- Interval-based re-reconciliation (via `RequeueAfter: spec.interval`).

**Rationale:** CR changes trigger immediate reconcile. Source revision changes mean new content was pushed (new git commit, new OCI tag). Interval catches drift and retries.

### 8. CUE module validation optional

**Decision:** The existing `ValidateCUEModule()` check (requires `cue.mod/module.cue` at artifact root) is relaxed for Release. The artifact may be a plain git repo; the CUE module structure may only exist at `spec.path`, not at the root.

**Rationale:** A GitRepository artifact is the full repo tree. The CUE module lives at a subdirectory (e.g., `releases/gon1_nas2/minecraft/`). Validating at root would reject valid artifacts. Validation moves to the `spec.path` resolution step instead.

## Risks / Trade-offs

**[Risk] CUE evaluation requires network access to OCI registry** → The artifact from Flux contains only the release package, not transitive CUE module dependencies. CUE's module system resolves them from the OCI registry at evaluation time. If the registry is unreachable, reconciliation fails with `ResolutionFailed`. Mitigation: same as ModuleRelease — `CUE_REGISTRY` is configured at controller startup; registry availability is an operational concern, not a design issue.

**[Risk] Large artifacts increase memory/disk pressure** → GitRepository artifacts can be large (full repo). Mitigation: existing `MaxArtifactSize` (64 MB) limit applies. The controller extracts to a temp directory and cleans up in deferred.

**[Risk] `dependsOn` creates implicit ordering that's hard to debug** → If Release A depends on Release B, and B is stuck, A never reconciles. Mitigation: status conditions clearly show `DependenciesNotReady` reason. Standard Flux pattern operators already understand.

**[Trade-off] No values in CR limits runtime overrides** → Teams cannot override values without pushing to git. This is intentional — GitOps means git is the source of truth. Teams that need imperative overrides should use ModuleRelease directly.

**[Trade-off] Polymorphic CRD (handles both MR and BR) adds complexity** → The reconciler must branch on kind detection. Mitigation: the shared pipeline handles the complexity; the reconciler just dispatches. If BundleRelease rendering is not implemented, that branch returns a clear error.
