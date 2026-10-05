## Why

The operator builds on `cuelang.org/go` (`go.mod:6`, `v0.17.1`, a direct require used by the controller tests) and on the library, which drives CUE for every render. The library is where a CUE bump is tested against the kernel: its own pull request runs the closedness canary (`opm/internal/cueregression/closedness_test.go`). A Dependabot pull request in this repo that moves `cuelang.org/go` alone skips that check and builds the operator image against a CUE version no library release was tested with.

The owner decided this in the kernel beta.1 plan walkthrough (decision j4, 2026-10-03): "cuelang.org/go moves only through a library release: Dependabot in cli and operator ignores it, and the rule is documented in RELEASING.md." The rule is now in workspace RELEASING.md, section "Pin classes" (workspace PR 28): cli's and opm-operator's Dependabot `gomod` blocks must ignore `cuelang.org/go` and `cuelabs.dev/go/oci/ociregistry`, the CUE team's OCI module, whose version the library's `go.mod` sets (`cuelang.org/go` sets only a floor), so it moves with library releases. The cli part shipped as cli PR 312. This change is the operator part.

At origin/main (`dd0d798`) the `gomod` block of `.github/dependabot.yml` ignores only `github.com/open-platform-model/*`, so Dependabot still proposes `cuelang.org/go` bumps.

## What Changes

- `.github/dependabot.yml`: the `ignore:` list of the `gomod` update gains `cuelang.org/go` and `cuelabs.dev/go/oci/ociregistry`, under one comment in the style of the `github.com/open-platform-model/*` comment above them. The comment says that CUE moves only through a library release (whose pull request runs the CUE check) and reaches the operator through the cascade's library bump, whose `go get` of the library raises `cuelang.org/go` by minimal version selection (kept by `go mod tidy`); that the library's `go.mod` sets the `ociregistry` version, so it moves with library releases too; and it cites owner decision j4 (2026-10-03) and workspace RELEASING.md, section "Pin classes". The `ociregistry` require is `// indirect` here (`go.mod:29`); it is ignored anyway, because Dependabot security updates also touch indirect requires and RELEASING.md names both modules for both frontends.
- Main spec `release-automation`: one ADDED requirement, "Dependabot leaves cuelang.org/go to library releases", and the MODIFIED requirement "Dependabot leaves OPM Go modules to the release cascade", whose last sentence now excepts the two CUE modules from "Third-party Go modules ... SHALL keep their Dependabot updates" (all three scenarios kept).

Not in this change:

- the CUE CLI pins (the `CUE_VERSION` env in each workflow that installs cue and the `setup-cue` step of `module-image.yml`): they are not Go module requires, Dependabot does not manage them, and RELEASING.md moves them in a separate `ci(deps)` pull request after the cascade pull request that carries a library CUE bump;
- any change to how `task deps:cascade` moves the library pin, or to its `go mod tidy` warning (`warn_deps` in `.tasks/cascade/cascade.sh`);
- the `github-actions` block and the `kubernetes` group.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `release-automation`: Dependabot also leaves `cuelang.org/go` and `cuelabs.dev/go/oci/ociregistry` to library releases.

## Impact

- No API type, controller or reconcile phase changes. Files: `.github/dependabot.yml` and the OpenSpec change only.
- Dependabot security updates for both ignored modules are suppressed too: a CUE security fix reaches the operator through a library release and the cascade, or a hand-made `fix(deps)` library bump.
- Open pull requests: none of the open Dependabot pull requests (opm-operator 126, 149, 151, 152, 180, 181, 195, 218, 239) is titled as a `cuelang.org/go` or `ociregistry` bump. RELEASING.md also says to close or hold one whose `go.mod` diff moves `cuelang.org/go` transitively; checking those diffs is a task of this change.
- Release class: `ci`. No operator or module release is cut. SemVer impact: none, after GA as well.
