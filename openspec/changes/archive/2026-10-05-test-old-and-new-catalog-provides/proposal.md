## Why

Since library v1.0.0-beta.6 (library#195), `catalog.Catalog.Provides` has two paths. A catalog whose committed `cue.mod` pins core `v2.0.0-beta.3` or later carries a `provides` field that core derives, and the library decodes that field. A catalog pinned to an older core carries no such field, and the library falls back to a deprecated Go fold over `#transformers`. Acceptance calls `Provides` through `providesDrift` for every claim. A wrong answer on either path refuses a valid claim `ProvidesMismatch`, or accepts one that lists the wrong contracts.

No operator test covers both paths. Every catalog an operator test resolves today pins core `v2.0.0-beta.1`, so only the fold runs. The `providesDrift` unit specs use hand-built catalogs whose module file requires no core. To the library their core is unknown, and since they author no `provides`, the fold answers them too. The owner asked for an operator test for an old catalog when core gained the per-catalog provider set, because the fold is what keeps published catalogs working until catalog_opm is republished and the fold is removed before GA.

## What Changes

- **One registry-backed integration spec pair in `test/integration/reconcile/backup_fixture_test.go`.** Both specs render `backup_provider`, create its claim and the provider `ModuleInstance` (with an inventory that owns the claim) in envtest, and run the real `TransformerRegistrationReconciler` against a generated platform. Each asserts that the claim is accepted (`accepted: true`, Ready=True with reason `Accepted`), which means it passed the claim check, `providesDrift` included. Each also asserts that `Provides()` returns exactly opm's backup trait.
  - **Old catalog (fold fallback).** The catalog is the published `backup` catalog fixture, acquired from the registry at the claim's coordinate (in PR CI, the registry seeded from this tree). It pins core `v2.0.0-beta.1`. The spec asserts the premises that select the fold: the pin is older than `schema.ProvidesSince` and the evaluated catalog has no `provides` field.
  - **New catalog (decoded field).** The catalog is a copy of the same fixture tree in a temporary directory, with its core pin rewritten to exactly `"v"+schema.ProvidesSince` (core `v2.0.0-beta.3`), acquired with `AcquireCatalogFromDir`. The spec asserts the premises that select the field: the pin compares equal to `ProvidesSince`, the boundary of the library's comparison, and the evaluated catalog carries a `provides` field equal to the result.
- **What the specs can and cannot tell apart.** They pin each path's premises, so a fixture move or a library change to `ProvidesSince` fails loudly, naming the premise. A library that stops folding a catalog without the field fails the old spec; one that picks its path by the field's presence, ignoring the pin, passes it, since that catalog has no field. They cannot tell the fold from the field on a catalog pinned at or after `ProvidesSince`, because on a real core both give the same set. A library that always folded would still pass the new spec; the library's own tests (library#195) cover that direction.
- **A MODIFIED requirement in `example-test-modules`** that keeps the backup catalog's core pin older than `ProvidesSince` while the library keeps the fold, and **a note in `test/fixtures/modules/README.md`**. The `backup` catalog's core pin is also the operator's only old-catalog case. Moving it to `v2.0.0-beta.3` or later turns the old-catalog spec red on purpose. Once the library deletes the fold, that spec is deleted or rewritten.

Out of scope:

- Any change to `internal/controller`. `transformerregistration_controller.go` needs no change: `providesDrift` already calls `Provides()`, and both paths return the same sorted set.
- A second catalog fixture pinned to a newer core (design D1). The new-core catalog is not acquired from the registry; D1 records why.
- Republishing catalog_opm, and removing the library fold.
- The library's own coverage of both paths (library#195 carries it).

## Classification

Test-only. No API type, controller, flag, fixture version or `dist/install.yaml` changes. The PR title is `test(integration): ...`, which cuts no release (`AGENTS.md`, "Commit type decides the release"). After GA it would still cut no release.

## Depends on / gates

- **Satisfied:** `go.mod` pins library `v1.0.0-beta.6`, which carries library#195 (`schema.ProvidesSince`, `schema.CatalogProvides`, the fold fallback). Its default core is `v2.0.0-beta.4`. The `backup` catalog fixture `0.1.0` and core `v2.0.0-beta.3` and `v2.0.0-beta.4` are on GHCR.
- **Gates:** `task dev:fmt dev:vet dev:lint dev:test`. `task dev:test` resolves fixtures from GHCR by default, so the registry-backed specs run. The section also runs the new specs focused with `OPM_TEST_REGISTRY_FORCE=1`, so a skip counts as a failure. No cluster suite is involved; the specs run on envtest.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `registration-acceptance`: a new requirement, "Acceptance holds on both paths of the provider-set derivation".
- `example-test-modules`: "A provider catalog fixture implements a provider-fulfilled contract" gains one sentence on the backup catalog's core pin. Both scenarios are kept unchanged.

## Impact

- Edited: `test/integration/reconcile/backup_fixture_test.go` (one sentence in the header comment; two specs and their helpers appended after the last spec), `test/fixtures/modules/README.md` (one paragraph).
- No enhancement decision is implemented here, so there is no `enhancement.yaml`.
