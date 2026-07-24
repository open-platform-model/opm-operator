# Proposal: live-flux-e2e

Test-infrastructure change from the 2026-07-21 workspace fixture audit; closes the workspace's last standing verification blind spot. No controller behavior changes.

## Why

Every Flux-facing path in this repo is validated only against fakes: envtest specs synthesize `OCIRepository` objects with fabricated artifact URLs and stub fetchers, and all four e2e files (`lifecycle_test.go`, `concurrent_test.go`, `prune_test.go`, `finalizer_test.go`) are 100% `Skip()` stubs. Nothing in this repo has ever proven the operator consumes a **real** source-controller `Artifact` (revision, digest, tarball fetch/extract) — the exact surface A1's coupled Flux/k8s bump moved. The only live proof ever produced was enhancement 0006 A5's manual opm-kind-demo bootstrap, and the demo is explicitly not a testing vehicle (user decision, 2026-07-21). The e2e suite already runs a kind cluster, deploys the controller, and publishes example modules in CI (`podinfo_test.go` runs for real there); the missing pieces are a running source-controller and a consumer for the already-existing-but-unconsumed `test/fixtures/modulepackages/podinfo/` fixture.

## What Changes

- **Version-pinned source-controller install for e2e**: the e2e setup installs Flux's source-controller (only — the operator consumes `Artifact`s; kustomize-controller is not needed) at the **flux2 distribution version matching this repo's pinned Flux libraries** (v2.9.0 line per A1/D4). The existing unversioned `flux install` task gains the pin; the pin lives next to the Flux library versions it mirrors so dependabot-style bumps move them together.
- **Live artifact pipeline e2e** (retires the `lifecycle_test.go` "live OCI fetch" stub): local registry up → `flux push artifact` of the podinfo modulepackage fixture → `OCIRepository` reconciled by the real source-controller → operator fetches/extracts the real artifact → `ModulePackage` renders → Deployment Ready. Asserts artifact revision/digest propagate into status.
- **Deployed-controller lifecycle e2e** (retires the remaining lifecycle stub and the live-tier halves of the prune/finalizer stubs): against the running controller — create→Ready, update that removes a resource → stale resource pruned, `prune=false` delete → CR removed, workloads orphaned, finalizer observed added/removed across the cycle.
- **Redundant stub cleanup**: e2e stub specs whose logic is already covered at envtest tier and whose live-tier value is subsumed by the new specs are deleted (not left as permanent Skips); `concurrent_test.go`'s two stubs (parallel instances, controller-kill idempotency) remain as the one recorded future item, with controller-kill as an optional stretch task here.
- **CI wiring**: the e2e workflow gains the flux CLI (pinned) and the source-controller install step.

## Capabilities

### New Capabilities

- `live-flux-e2e`: the live-tier verification contract — pinned source-controller install, real-artifact pipeline assertions, deployed-controller lifecycle/prune/orphan assertions.

### Modified Capabilities

None — product behavior is already specified; this adds the live verification tier (precedent: `test-registry-lifecycle`, `example-test-modules`).

## Impact

- **Packages**: `test/e2e/**` (new specs; stub deletions), `.tasks/flux.yaml` (version pin), `.github/workflows/test-e2e.yml` (flux CLI + install step), possibly `test/utils`.
- **SemVer**: none. Commit types `test:` / `ci:`.
- **Dependencies**: kind + local registry (both already in the e2e path); flux CLI in CI. Depends conceptually on the fixtures shipped by `example-test-modules` — no code dependency on other changes; can land in parallel with `envtest-coverage-batch`.
