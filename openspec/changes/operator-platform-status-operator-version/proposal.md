# Proposal: operator-platform-status-operator-version

> Slice A6 of enhancement `0006` (CLI CR Inventory, Library Kernel Adoption, and Operator Handoff). Implements the operator half of decision D24.

## Why

Enhancement 0006's version-skew contract (D24) has the CLI refuse the unsafe CLI-older-than-operator direction by reading an operator-version ceiling from `Platform.status.operatorVersion` — with absence meaning "solo cluster, check skipped". The operator never publishes that field today; in fact the binary has no version identity at all (no version package, no ldflags, no Dockerfile build-arg). Until it self-publishes, the CLI-side gate (slice C1) would silently disable itself on operator-managed clusters. This slice is the only thing C1 still waits on.

## What Changes

- New `internal/version` package carrying the operator's version **burned into source**: a constant annotated with `x-release-please-version`, bumped automatically by release-please via `extra-files` — so every build of a tagged commit (image, local, `go install`) reports the correct version with zero build-arg plumbing. Dev builds report the last released version, best-effort suffixed with VCS revision/dirty from `debug.ReadBuildInfo()` when built from a git checkout.
- `PlatformStatus` gains `operatorVersion` (optional string) plus an `Operator` printcolumn; CRD/DeepCopy/`dist/install.yaml` regenerated.
- `PlatformReconciler` stamps `status.operatorVersion` on **every** status patch — success and materialize-failure paths alike (the field means "the operator version running against this Platform", not "materialization succeeded").
- The manager logs its version at startup (the operator currently cannot say what version it is).
- `release-please-config.json` gains `extra-files: ["internal/version/version.go"]`.

Additive and backward-compatible (new optional status field within `v1alpha1`). No behavior change for any existing consumer.

## Capabilities

### New Capabilities

- `operator-version-identity`: the operator's build-time version identity — source-burned constant maintained by release automation, exposed at startup and via `Platform.status.operatorVersion`.

### Modified Capabilities

- `platform-crd`: the `PlatformStatus carries conditions and observedGeneration` requirement extends with the `operatorVersion` field and printcolumn.
- `platform-reconciler`: new requirement — the reconciler stamps `status.operatorVersion` on every status patch.
- `release-automation`: new requirement — release PRs bump the annotated version constant via `extra-files`.

## Impact

- **New:** `internal/version/` (constant + `Full()` helper + test).
- **Modified:** `api/v1alpha1/platform_types.go` (+regenerated `zz_generated.deepcopy.go`, `config/crd/bases/opmodel.dev_platforms.yaml`, `dist/install.yaml`), `internal/controller/platform_controller.go` (+tests), `cmd/main.go`, `release-please-config.json`.
- **Consumers:** enhancement 0006 slice C1 (`cli-cr-inventory-backend`) reads the field; value contract documented in design.md (always `v`-prefixed semver, possibly `+…` build-metadata on dev builds — C1 must tolerate the suffix). The next operator release carrying this field becomes the CLI's new embedded pin (`task operator:sync` in `cli/`).
- **Residual risk:** the release-please generic updater's bump of the constant is only provable on the next Release PR — verify there; fallback is the JSON-typed extra-file config.
