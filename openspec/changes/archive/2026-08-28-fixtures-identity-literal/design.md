## Context

See proposal.md. Fixtures publish to `testing.opmodel.dev/modules/operator/<m>@v0`. PR CI (`hack/fixtures.sh check` + `seed`) refuses a changed fixture whose declared version GHCR already holds and seeds the tree's versions into a job-local registry; render coverage (`modulepackage-kernel-rendering`) loads them through the kernel. Version pins live in three consumer sites per fixture (modulepackage `cue.mod`, `moduleinstance.yaml`, and for `hello` the config sample).

## Goals / Non-Goals

**Goals:** fixtures carry the literal identity form; every consumer follows in the same PR.
**Non-Goals:** rendered-output changes (none beyond the version label); any Go change.

## Decisions

**D1. Form edit first, then `opm module version set <next>` per fixture.** The tool run proves the literal writer on each fixture and produces the exact bytes `deps:pins:fixtures` would. Alternative (hand-edit the literal): same bytes, no proof.

**D2. Use the root `task deps:pins:fixtures` for the pins.** It already enumerates every consumer site (modulepackages, `moduleinstance.yaml`, `config/samples`); its dep-bump half moves the catalog pin to `v2.0.0-alpha.7` (core unchanged at alpha.6), so the diff is the catalog pin plus the version lines. The task also advances each fixture one further patch and edits `cli`'s consumers; those are undone (`opm module version set` back to the specified patch, `task examples:pin`, `cli` tree reverted) so the change keeps the versions in tasks 1.3 and stays in one repo. Hand-editing risks missing the config sample.

**D3. One PR, all four.** Constitution VIII (tiny batches) is satisfied: one pattern applied across the fixture set, identical per file; splitting would run the seed job four times for the same edit.

## Reconcile phase impact

Source: unchanged. Render: the kernel loader now accepts the fixture without the interpolation crutch; rendered manifests identical except `module-instance.opmodel.dev/version`-style labels carrying the new patch. Apply/Prune/Status: unchanged.

## Research & Decisions

### Does the literal form load and render identically?
**Context**: the fixture comment claims interpolation is required by "the registry loader's shape gate".
**Explored**: 2026-08-28 on a fleet module: `Version: "1.0.1"` + `version: id.Version` passes `opm module build` (the same `loader/file` gate), `opm module vet`, `publish --dry-run`; `IsConcrete()` is true on the literal and false on the disjunction (Go probe, `cuelang.org/go v0.17.1`).
**Decision**: plain literal, no interpolation.
**Rationale**: the gate needs a value; the literal is the value. The comment described the defect, not a requirement.

## Risks / Trade-offs

- [A next-patch version already exists on GHCR from an earlier bump] → `hack/fixtures.sh check` refuses; pick the following patch.
- [`test-e2e.yml` pins examples to the published pre-release] → unaffected until merge; after `publish-fixtures.yml` runs, the e2e pin step resolves the new versions.
