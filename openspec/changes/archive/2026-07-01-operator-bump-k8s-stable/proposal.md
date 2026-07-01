## Why

`opm-operator` and the CLI are drifting onto different Kubernetes API lines: the operator pins `k8s.io/*` v0.35.2 and `sigs.k8s.io/controller-runtime` v0.23.3, while the CLI is already on `k8s.io/*` v0.36.0. Enhancement [0006](../../../../enhancements/0006/) is converging the two repos onto a shared runtime contract (kernel, CR-backed inventory), and while D13/D31 narrowed how much code the two actors actually share, keeping them on the same Kubernetes API line is still the cheapest way to avoid MVS (minimum version selection) conflicts the next time a dependency edge is added between them, and to keep client behavior (informer/cache semantics, SSA field-manager handling) consistent across both actors. This is slice A1 in `enhancements/0006/planned-changes.md` — independent prep work, blocking nothing today but worth doing before the CLI/operator dependency surface grows further.

**Widened during implementation:** `k8s.io/api` v0.36.x deletes the `scheduling/v1alpha1` and `autoscaling/v2beta2` API-group packages outright. `opm-operator`'s currently-pinned Flux libraries (`fluxcd/cli-utils` v0.37.2-flux.1 via `pkg/kstatus/polling`, `fluxcd/pkg/ssa` v0.69.0 via `ssa/normalize`) still reference those deleted packages, so the k8s.io bump cannot land without also bumping Flux's own dependencies. This is a one-directional coupling — there is no sequencing that lands the k8s.io bump without the Flux bump — so both move together in this one change rather than as originally scoped.

## What Changes

- Bump `k8s.io/api`, `k8s.io/apiextensions-apiserver`, `k8s.io/apimachinery`, `k8s.io/client-go` (and their indirect siblings: `k8s.io/apiserver`, `k8s.io/cli-runtime`, `k8s.io/component-base`, `k8s.io/klog/v2`, `k8s.io/kube-openapi`, `k8s.io/kubectl`, `k8s.io/utils`) from v0.35.2 to v0.36.0.
- Bump `sigs.k8s.io/controller-runtime` from v0.23.3 to v0.24.1 — the version that pins `k8s.io/* v0.36.0` directly in its own `go.mod`, confirming this is the intended pairing rather than an arbitrary newer tag.
- Bump Flux's own packages to the coordinated set the `flux2` v2.9.0 distribution itself ships against (see `design.md` D4 for why this specific combination, not hand-picked minimums):
  - `github.com/fluxcd/cli-utils` v0.37.2-flux.1 → v1.2.2
  - `github.com/fluxcd/pkg/apis/meta` v1.26.0 → v1.30.1
  - `github.com/fluxcd/pkg/runtime` v0.103.0 → v0.110.1
  - `github.com/fluxcd/pkg/ssa` v0.69.0 → v0.76.1
  - `github.com/fluxcd/source-controller/api` v1.8.0 → v1.9.1
- Regenerate `go.sum` and re-vendor indirect dependencies that shift underneath this bump (`sigs.k8s.io/apiserver-network-proxy/konnectivity-client`, `sigs.k8s.io/kustomize/api`, `sigs.k8s.io/kustomize/kyaml`, `sigs.k8s.io/structured-merge-diff/v6`, etc.).
- No `go` directive change — `opm-operator`'s current directive (1.26.2) already exceeds controller-runtime v0.24.1's requirement (1.26.0) and the CLI's own directive (1.26.0). Confirmed during exploration; do not bump it as part of this change.
- Regenerate CRDs/RBAC/DeepCopy (`task dev:manifests dev:generate`) as a verification step, even though no API types change — controller-runtime/Kubebuilder tooling versions can shift generated output formatting.
- Minimal production code changes. controller-runtime v0.24.0's one documented breaking change (removal of the deprecated webhook custom-path builder function) does not apply — `opm-operator` ships no webhooks (confirmed by exploration: no `webhook.CustomDefaulter`, no `ctrl.NewWebhookManagedBy`, no `*webhook*` files in the repo). `cli-utils` v1.0.0's one documented breaking change (project scope reduced to `kstatus` only) does not apply either — `opm-operator` imports exactly `fluxcd/cli-utils/pkg/kstatus/polling` and nothing else from that module. **One small fix was needed, discovered by `task dev:lint`, not anticipated in research:** controller-runtime v0.24.0 also deprecated `pkg/scheme.Builder` (flagged by `staticcheck`), requiring a 4-file, behavior-preserving migration to the apimachinery-native `runtime.NewSchemeBuilder` equivalent — see `design.md` D8.

## Capabilities

### New Capabilities

None. This is a dependency-version change with no new controller-observable behavior.

### Modified Capabilities

None. No CRD, reconcile-phase, or API-visible behavior changes — this is confirmed by the design goal (Kubernetes and controller-runtime client libraries only; no `opm-operator` package changes its public contract). If the full `k8s.io/client-go`/`apimachinery` diff surfaces an unexpected behavior change during implementation, that would need to be captured as a modified capability at that point — not anticipated here.

## Impact

- **Affected files:** `go.mod`, `go.sum` only, plus regenerated artifacts if `task dev:manifests dev:generate` produces a diff (`config/crd/bases/*.yaml`, `config/rbac/role.yaml`, `api/v1alpha1/zz_generated.deepcopy.go`).
- **Affected controllers/API types:** none directly — this is a transitive dependency bump underneath `internal/controller/`, `internal/apply/` (uses `fluxcd/pkg/ssa` and `fluxcd/cli-utils/pkg/kstatus/polling`), `internal/render/`, and every package importing `k8s.io/*`, `sigs.k8s.io/controller-runtime`, or the bumped `fluxcd/*` packages (`pkg/apis/meta`, `pkg/runtime/conditions`, `pkg/runtime/patch`, `source-controller/api/v1`), but their source is not expected to change.
- **SemVer:** PATCH. No public API or CRD change; a pure dependency-version bump with (expected) no behavior change.
- **Cross-repo:** aligns `opm-operator` onto the CLI's current k8s line (`v0.36.0`) and onto Flux's own current released distribution (`flux2` v2.9.0's dependency set). Does not by itself unblock any other enhancement 0006 slice — A1 has no dependents in `planned-changes.md`.
- **Verification surface:** `task dev:fmt dev:vet dev:lint dev:test` must pass unchanged; `task dev:e2e` if Kind is available. The actual "does this compile and behave the same" question is answered empirically here, since neither the Kubernetes/controller-runtime nor the Flux release notes give a complete deprecation/removal list for every touched subpackage — only the ones this change specifically checked (see `design.md`).
