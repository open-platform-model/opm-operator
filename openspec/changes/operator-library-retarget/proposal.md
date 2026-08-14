# Proposal: operator-library-retarget

> Slice `operator-library-retarget` of enhancement `0010` (Module and Catalog Identity). Retargets the operator to the core-v2 library line and retires the Platform CRD's subscription filter — the API-surface half of the recorded 0010 deviation. The fixture-fleet half is the companion change `examples-fleet-core-v2`.

## Why

The operator pins `github.com/open-platform-model/library v1.0.0-alpha.8`. The library has since crossed to core v2 (alpha.12) and landed the 0010/0011 library slices (alpha.13): subscriptions are a required scalar `version` (`#SubscriptionFilter` no longer exists — `synth.FilterSpec` is deleted from the Go surface), materialize pulls exactly the named build and verifies artifact identity (`oerrors.IdentityError`), and unresolved demands fail `Compile` (`*oerrors.UnresolvedDemandsError`). Until the operator crosses, it renders against a retired schema line and blocks the whole downstream chain: the CLI's `cli-coordinate-adoption` re-vendors this repo's released `install.yaml` (`task operator:sync`) and cannot exercise its Platform-CR paths until a release ships a CRD whose `spec.registry.*` carries `version`.

The 0010 design said this slice needs "no feature code" — that was recorded as contradicted when the library's acquire slice landed: `Subscription.Filter` is a versioned CRD field feeding a synth input that no longer exists. The field is not merely stale, it is unmappable; the CRD change and the library bump are one atomic crossing (the controller cannot compile against alpha.13 while mapping `Filter`, and cannot wire `Version` while on alpha.8).

**Definition of done includes the release**: the next operator release (expected `v1.0.0-alpha.9`) must ship `install.yaml` with the v2 CRD — that release asset is the artifact the CLI consumes.

## What Changes

- **`go.mod`: library `v1.0.0-alpha.8` → `v1.0.0-alpha.13`.** The zero-value schema loader then resolves `opmodel.dev/core@v2`; `verifyCoreSchema` at startup fails fast if it is unreachable.
- **Platform CRD (`api/v1alpha1`)**: `SubscriptionFilter` deleted; `Subscription` loses `Filter` and gains `Version string` (`json:"version"`, `MinLength=1`). Whether `Version` is CRD-`required` or CRD-optional is decided by an envtest verification of validation ratcheting against a stored CR lacking the field (the `cluster` singleton is status-patched on every reconcile and its spec is never rewritten by the operator) — see design. In either posture a versionless subscription is always rejected before anything materializes; the measurement selects only whether the rejection happens at admission or at reconcile. Regenerate CRD bases, DeepCopy, `dist/install.yaml`.
- **`internal/controller/platform_controller.go`**: `platformInput` maps `spec.Registry[k].Version` → `synth.SubscriptionSpec.Version`; the `FilterSpec` branch (the only production compile break) is deleted.
- **`config/samples/opmodel.dev_v1alpha1_platform.yaml`**: the e2e-load-bearing sample moves to `registry: {"opmodel.dev/catalogs/opm@v2": {version: "2.0.0-alpha.3"}}` — key carries the mandatory `@vN`, value is the pinned catalog build (kept aligned with the CLI's `DefaultPlatformTemplate` seed; the pin is load-bearing and bumped as an ordinary fixture update).
- **Tests updated for the new shapes and semantics**: the three `synth.FilterSpec` sites in `test/integration/reconcile/`, the `SubscriptionFilter` literal in `internal/controller/platform_test.go`, the two versionless `Subscription{}` literals that now trip `synth.ErrSubscriptionMissingVersion`, the `OPM_TEST_CATALOG_RANGE` knob becoming an exact-version knob, and `acquire_test`'s `ModulePath` assertion moving to the v2 full-address identity (interim: still against the v1-authored published fixture, which the library's tolerant fallback accepts — the assertion flips fully in `examples-fleet-core-v2`).
- **Digest-formula freeze made checked**: a pin test on `status.ModuleSourceDigest` asserting the exact `sha256(path + "@" + version)` golden, with a comment naming `cli/internal/workflow/apply`'s `sourceDigest` as the byte-identical peer — the cross-repo invariant nothing currently tests.
- **Deliberately NOT changed (charter: no feature code)**: no new condition reasons and no typed routing for `oerrors.IdentityError` / `*oerrors.UnresolvedDemandsError` — both land on `RenderFailedReason` via the existing classifier; the existing `*oerrors.MaterializeError` routing already surfaces catalog-side identity errors adequately. Recorded as a follow-up candidate in design, not done here.

## Capabilities

### New Capabilities

<!-- none -->

### Modified Capabilities

- `platform-crd`: `Subscription` carries a scalar `version` naming one catalog build; the filter vocabulary is removed; subscription keys are major-suffixed module paths.
- `platform-reconciler`: the reconciler maps `version` into platform synthesis; new failure modes (missing version, version-not-published, identity mismatch) surface through the existing `MaterializeFailed` routing.
- `library-kernel-runtime`: the embedded kernel resolves `opmodel.dev/core@v2`; compile fails on unresolved demands (fleet-visible readiness change: renders that silently under-delivered now stall as `RenderFailed`).
- `digest-computation`: the source-digest formula is pinned as a cross-repo contract.

## Impact

- **SemVer: MAJOR-shaped within the alpha line** (`feat!`): a versioned CRD field is removed and platform specs must be re-authored (`filter` → `version`, keys gain `@vN`). Ships with a `BREAKING CHANGE` footer; release-please cuts the next alpha.
- **API types**: `api/v1alpha1.Subscription` (field swap), `api/v1alpha1.SubscriptionFilter` (deleted) + regenerated `zz_generated.deepcopy.go`, `config/crd/bases/opmodel.dev_platforms.yaml`, `dist/install.yaml`.
- **Controllers**: `PlatformReconciler` (input mapping only). `ModuleInstance`/`ModulePackage` reconcilers: no code change; behavior change via the library (unresolved-demand failures; one-time fleet `RenderDigest` movement when D36's label removal reaches rendered output).
- **Existing CRs**: stored `filter` persists in etcd until the next spec write (structural pruning is write-time); the singleton's status patches must keep succeeding — verified, not assumed (design).
- **Downstream**: the CLI re-vendors the released `install.yaml` and bumps `PinnedOperatorVersion` (currently `v1.0.0-alpha.4` — a 5-release jump); `opm-kind-demo` pins the operator release and carries its own Platform document to migrate. Both move on their own schedules after the release.
- **Ordering**: `examples-fleet-core-v2` follows this change; between the two, the registry-backed integration specs and e2e fixture specs are expected-red locally (they already skip in CI — no PR-CI impact). The release is cut only after both land.
- **Concurrent change**: `add-cross-namespace-source-grants` (0/23) — no semantic overlap; mechanical regeneration conflicts on `config/crd/bases/` + `dist/install.yaml`; whichever lands second re-runs `task dev:manifests dev:generate`.
