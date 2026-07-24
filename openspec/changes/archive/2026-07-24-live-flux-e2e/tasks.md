# Tasks: live-flux-e2e

## 1. Install plumbing

- [x] 1.1 Pin source-controller install: single version variable in `.tasks/flux.yaml` (flux2 distribution matching go.mod's Flux library line, A1/D4), `--components=source-controller --version=<pin>`; co-located comment tying the pin to the library bump workflow
- [x] 1.2 e2e setup wires the install (suite setup or task dependency) with the suite's gating idiom: CI marker ⇒ hard-fail when absent, local ⇒ skip with notice
- [x] 1.3 CI (`test-e2e.yml`): install flux CLI at the same pin; run the source-controller install step; echo both versions

## 2. Live artifact pipeline spec (replaces lifecycle 5.2 stub)

- [x] 2.1 Helper: `flux push artifact` of `test/fixtures/modulepackages/podinfo/` to the local registry (reuse `.tasks/registry.yaml` wiring) with a deterministic tag
- [x] 2.2 Spec: apply fixture OCIRepository + ModulePackage → Eventually Artifact (revision+digest) present → ModulePackage Ready → rendered Deployment Ready → artifact revision in ModulePackage status
- [x] 2.3 Negative sanity: assert the spec fails meaningfully (clear message) when source-controller is absent under the CI marker

## 3. Deployed-controller lifecycle spec (replaces lifecycle 5.1 stub; subsumes prune/finalizer live halves)

- [x] 3.1 Extend the podinfo e2e flow: create → Ready → finalizer present
- [x] 3.2 Update values so the render drops a resource → assert live prune by the deployed controller + inventory shrink
- [x] 3.3 Recreate; `spec.prune: false` + delete → CR gone (finalizer released), workloads remain (orphan), namespace intact

## 4. Stub disposition

- [x] 4.1 Delete `test/e2e/prune_test.go` and `test/e2e/finalizer_test.go` stub files; pointer comment in the lifecycle spec names the envtest coverage + the LD3 assertions that subsume them
- [x] 4.2 Keep `concurrent_test.go` recorded; OPTIONAL stretch: controller-pod-kill idempotency spec (delete manager pod mid-reconcile, assert convergence) — drop without guilt if flaky

## 5. Verification

- [x] 5.1 Full e2e green locally (kind) with the new specs executing (not skipping) — 2026-07-24: `task dev:e2e:local` on a fresh kind cluster, 11 passed / 0 failed / 2 skipped (only the recorded concurrent stubs); artifact pipeline, redis prune/orphan, and finalizer specs all executed. Negative gating verified both ways (broken `flux` shim): `OPM_E2E_FLUX=1` ⇒ hard BeforeAll failure with explicit message, unset ⇒ skip with notice
- [x] 5.2 CI run green with hard-fail gating active — 2026-07-24: PR #54 e2e green (11 passed / 0 failed / 2 skipped, flux CLI v2.9.0 at the pin); the first run also proved the gate live — a broken CLI install hard-failed BeforeAll with the explicit `OPM_E2E_FLUX` message instead of skipping
- [x] 5.3 Stub census: only concurrent scenarios remain recorded
- [x] 5.4 Sync/archive per openspec flow — 2026-07-24: delta spec synced to `openspec/specs/live-flux-e2e/`, change archived (re-landed on main via cherry-pick after the #54/#55 merge-order miss)
