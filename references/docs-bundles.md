# Docs bundle, resource reference and docs-kit pins

Covers the `opm-operator` docs-kit bundle: what it holds, how it builds and publishes, how to fix a released page, the `reconciledBy` rule, the docs-kit pin, and the generated-file rules that feed the resource reference. Site page prose follows the workspace `STYLE.md` and `VOICE.md`.

## Contents

- What the bundle holds
- Build, publish and recover
- Fix a released page
- `reconciledBy`
- The docs-kit pin
- Generated files that feed the reference
- Pitfalls
- Check before you finish

## What the bundle holds

`docs-kit.cue` declares one bundle, `opm-operator` (docs-kit contracts C6, C15, C18). It has two parts:

- the authored pages under `docs/site/`;
- the resource reference, which docs-kit's `crd` source generates from `config/crd/bases` and `config/samples`.

The reference is the section `reference/operator/`, one page per kind. The doc comments on the types in `api/v1alpha1` are the pages. In `docs/site/reference/operator/_index.md` only the front matter and intro are authored. Write type doc comments for a reader of the site.

## Build, publish and recover

- `Docs / check` builds and lints the bundle on every pull request.
- A push to `main` publishes it as `edge`.
- Every release publishes it from the `publish-docs` job of `release.yml`, after `image-release` (`publish-release` does not wait for it), to `ghcr.io/open-platform-model/docs/opm-operator`.
- Preview: `task docs:bundle` writes `out/opm-operator/`. `opm-docs serve` works once `.opm-docs-version` names a docs-kit release that has it.
- Recover a release with no bundle, or backfill one cut before adoption (from `v1.0.0-beta.4`): `gh workflow run docs.yml --ref main -f mode=release -f tag=vX.Y.Z`.

## Fix a released page

Use `mode=revision` with `fix=<40-hex sha>`. The commit must be Markdown-only or comment-only, on `main`. Revisions are dispatched by hand (opm-operator#188).

Once the site reads the bundle, an authored fix reaches a released version only by a release or a revision. A CRD description fix reaches it only by a release.

## `reconciledBy`

`reconciledBy` in `docs-kit.cue` names the controller of each kind: the name the controller registers itself under.

A test in `internal/controller/` (part of `task dev:test`) keeps it true. It scans the controllers and fails naming the kind when a name differs. A renamed controller, or a new kind, therefore needs its `reconciledBy` entry; `task dev:test` fails until it has one.

## The docs-kit pin

The docs-kit release is pinned twice: in `.opm-docs-version` and in every `publish.yml@vX.Y.Z` ref. The ref is a tag, never a SHA (docs-kit C5, C9). Move both in one PR. `task docs:pins:check` (a `Lint` step) refuses a mismatch.

## Generated files that feed the reference

- Generated files are never hand-edited (the list is in `AGENTS.md`). `task operator-module:drift`, a CI step, fails when the module data or `config/` are stale.
- After a change to API markers, schema, type files or an RBAC marker, run `task dev:manifests dev:generate docs:bundle:check`. The doc comments become the CRD descriptions and the public resource reference.
- After a change to `config/samples` or controller registration, run `task docs:bundle:check`. `test/integration/crdvalidation` proves every sample is admitted by the CRDs.

## Pitfalls

- Moving `.opm-docs-version` without every `publish.yml` ref (or the reverse) fails `task docs:pins:check`.
- Pinning `publish.yml` to a SHA breaks docs-kit C5.
- Expecting a CRD description fix to reach a released version through a revision: it needs a release.
- Renaming a controller without updating `reconciledBy` fails `task dev:test`.
- An operator version on a site page outside the release-please markers goes stale at the next release.

## Check before you finish

- [ ] `task docs:bundle:check` passes after an edit to `docs/site/`, `api/v1alpha1`, `config/samples`, `docs-kit.cue` or a docs-kit pin.
- [ ] A new or renamed controller has its `reconciledBy` entry.
- [ ] No generated file was edited by hand.
- [ ] `.opm-docs-version` and every `publish.yml` ref name the same release.
