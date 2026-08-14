# Tasks — operator-library-retarget

> Staged like the library's crossing: greens before, one flip commit, greens after. Group 1 is measurement that decides a marker in group 2. Companion change `examples-fleet-core-v2` follows; the release cut waits for both. Commit subjects must respect the bare-`@name` ban (write `opmodel.dev/core@v2` glued, or "the v2 line") and carry `feat!` + `BREAKING CHANGE` on the flip.

## 1. Pre-flip measurements (green, no behavior change)

- [x] 1.1 Envtest ratcheting verification: store a Platform with `filter` and no `version` under the current CRD; install the reshaped CRD (hand-built for the test); patch status. Record whether the patch succeeds. Outcome selects task 2.2's marker (`+required` if patch survives; optional otherwise).
- [x] 1.2 Digest golden pin in `internal/status/digests_test.go` with the peer-file comment (formula freeze — passes before and after the flip by construction).

## 2. The flip (one commit)

- [x] 2.1 `go.mod`: library → `v1.0.0-alpha.13`; `go mod tidy`.
- [x] 2.2 `api/v1alpha1/platform_types.go`: delete `SubscriptionFilter` and `Subscription.Filter`; add `Version string` with `MinLength=1` and the marker chosen by 1.1; doc comments state the D14 contract (one published build; key carries the major).
- [x] 2.3 `internal/controller/platform_controller.go`: `platformInput` maps `Version`; `FilterSpec` branch deleted.
- [x] 2.4 `task dev:manifests dev:generate`; `task operator:installer` to refresh `dist/install.yaml`.
- [x] 2.5 `config/samples/opmodel.dev_v1alpha1_platform.yaml`: key `opmodel.dev/catalogs/opm@v2`, `version: "2.0.0-alpha.3"`, load-bearing-pin comment; rewrite the v1-era explanation comment.
- [x] 2.6 Compile-fix the test fleet (design § Test inventory): three `synth.FilterSpec` sites, `platform_test.go` literal, two versionless subscriptions, `OPM_TEST_CATALOG_VERSION` knob.
- [x] 2.7 `task dev:fmt dev:vet dev:lint dev:test` — unit + envtest green (registry-backed integration specs skip as today).

## 3. Post-flip greens

- [x] 3.1 New test: versionless subscription surfaces as `MaterializeFailed` naming the subscription path (via `ErrSubscriptionMissingVersion` through `failMaterialize`).
- [x] 3.2 `internal/render/module.go:21` doc comment: warnings are effectively-optional traits only.
- [x] 3.3 `opm-operator/CLAUDE.md`: fix the stale "fixtures are deliberately not on GHCR" note (the release/e2e workflows publish them there; measured in 0010's migration inventory).
- [x] 3.4 Spec deltas (this change's `specs/`): `platform-crd`, `platform-reconciler`, `library-kernel-runtime`, `digest-computation`.

## 4. Validation gates

- [x] 4.1 `task dev:manifests dev:generate` (no drift), `task dev:fmt dev:vet`, `task dev:lint`, `task dev:test`.
- [x] 4.2 Scan every commit message and the PR body for bare `@` tokens; confirm no attribution footers beyond the plain co-author trailer (optional).

## 5. Record & release coordination

- [x] 5.1 Record in `enhancements/0010/`: history event noting the CRD work absorbed into this slice per the recorded deviation (the "no feature code" concern text corrected); `plan.yaml` slice stays `in-progress` until `examples-fleet-core-v2` also lands (one slice, two OpenSpec changes — `openspec_ref` cites both).
- [ ] 5.2 After BOTH changes merge: cut the release (release-please PR → `v1.0.0-alpha.9`); verify the release asset `install.yaml` carries `spec.registry.*.version`; notify the CLI track (`task operator:sync` target) — this is the slice's graduation gate.
- [x] 5.3 Follow-up candidates recorded, not done: typed routing of `IdentityError`/`UnresolvedDemandsError` onto `ResolutionFailedReason`; wiring `OPM_TEST_REGISTRY_FORCE` (or a GHCR-tolerant helper) so the registry-backed integration tier stops silently skipping in CI.
