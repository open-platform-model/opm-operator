## Why

A claim with a bare SemVer `spec.version` was refused `CatalogUnresolved` by every released operator up to v1.0.0-beta.5 (opm-operator#210), and no automated test caught it. The integration spec `test/integration/reconcile/backup_fixture_test.go` calls the same library verb acceptance calls, but it never runs the `TransformerRegistrationReconciler` or the `PlatformReconciler`. The cluster proof of the archived change `2026-10-04-add-active-provider-fixture` (design D4) was a manual run on a throwaway cluster. That change's design D5 deferred the e2e spec until the library bump landed.

The owner's walkthrough decision i1 (2026-10-02/03) asks for that test: "after library release, operator e2e test drives a real claim through acceptance AND platform build." The library release has landed. The operator pins library v1.0.0-beta.4, which carries library#170. The registry verbs accept a bare and a `v`-prefixed version, and `platformmodule.Generate` stamps the version without its `v`. This change adds the e2e spec, for both spellings.

## What Changes

- **A new e2e spec, `test/e2e/registration_test.go`.** It deploys the controller built from the branch, applies the sample Platform and waits for Ready, then applies the `backup_provider` fixture's `moduleinstance.yaml`. That fixture is published and renders a real `TransformerRegistration` naming the `backup` catalog fixture at `0.1.0`.
  - **Bare spelling (`0.1.0`).** The spec waits for the claim `default.backup-provider` to report `accepted: true` and `active: true`. It then waits for the Platform to rebuild with the backup catalog in `status.registry` (`source: Registration`, version `0.1.0`), under a new `status.packageIdentity`, and to report Ready.
  - **`v`-prefixed spelling (`v0.1.0`).** opm's `#VersionType` refuses a `v`, so no module can render this spelling. It reaches a cluster only when someone edits the claim by hand, which the CRD admits (`MinLength=1` only). The spec suspends the provider instance first, so the controller does not re-apply the rendered claim over the edit. It then sets the live claim's `spec.version` to `v0.1.0`. The claim must be re-judged and accepted at the new generation, with Ready=True/`Accepted` naming `v0.1.0`. Only then must the Platform list the same build in `status.registry`, under another new package identity, and report Ready (design D2).
  - **Removal** is the last ordered step: deleting the provider instance prunes the claim and releases its removal guard. Teardown then removes any leftover claim, the applier RBAC and the Platform, and undeploys, as the podinfo spec does.
- **A note in `test/fixtures/modules/README.md`**: a `backup` catalog bump now needs two pull requests (design Risks).
- **The N4 consumer check.** Research item N4 asks whether consumers absorbed library f1d9908 (beta.2, `feat(kernel)!`). Since that commit, `AcquireInstanceFromDir` refuses an instance package whose own values carry an undeclared key, a value of the wrong type, or a broken constraint. The check found nothing to fix in this repo (design D4). Section 1 confirms it by running the registry-backed ModulePackage specs on beta.4.

Out of scope:

- The `TransformerRegistration` CRD doc comment ("a bare SemVer string"). opm-operator#210 is closed and the doc is accurate for what opm renders.
- A `backup_consumer` render in e2e (design D3).
- Moving `backup_provider` to `#PreBoundRegistration`.
- Any change outside `opm-operator`. The N4 findings for other repos go in the supervisor report.

## Classification

Test-only. Nothing changes in an API type, controller, flag, fixture or `dist/install.yaml`. The PR title is `test(e2e): ...`, which cuts no release (`AGENTS.md`, "Commit type decides the release"). After GA it would still cut no release.

## Depends on / gates

- **Satisfied:** library v1.0.0-beta.4 in `go.mod`, which contains library#170 (64799d5). The `backup` catalog, `backup_provider` and `backup_consumer` at `0.1.0` are on GHCR and readable without credentials (checked 2026-10-04).
- **Gates:** `task dev:fmt dev:vet dev:lint dev:test`. `go vet -tags=e2e ./test/e2e/` and `golangci-lint run --build-tags=e2e ./test/e2e/...` cover the e2e package, which the default lint and vet builds leave out (`.golangci.yml` sets no build tags). The new spec runs under the workspace cluster lock (design D5).

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `live-flux-e2e`: a new requirement, "Deployed-controller registration claim proof".

## Impact

- New: `test/e2e/registration_test.go`.
- Edited: `test/fixtures/modules/README.md` (one paragraph).
- CI: `test-e2e.yml` runs the new spec with the rest of the suite. It needs no new step, because `task examples:pin` already re-pins `backup_provider/moduleinstance.yaml` to the per-commit pre-release tag.
- No enhancement decision is implemented here (i1 is a walkthrough decision, not an enhancement decision), so there is no `enhancement.yaml`.
