## Why

`pkg/errors` and `pkg/resourceorder` were copied from the CLI when the controller first embedded the inventory bridge. Nothing in the controller imports either package today: the only importer of `pkg/errors` is its own test file, and `pkg/resourceorder` has none. Both copies have started to drift from the CLI's originals (comment wording, "release" versus "instance" strings), so they cost review attention for code that never runs. Slice 07 of the kernel diet: consumers stop carrying copies they do not use.

## What Changes

- **Removed:** `pkg/errors` (sentinels, `DetailError`, `ValidationError`, `ConfigError`, grouped CUE error helpers) and `pkg/resourceorder` (apply and delete ordering weights), with their tests.
- `pkg/core` stays: `internal/` uses `Resource`, `ResourceFromCompiled` and the label constants in seven files. Keeping it byte-identical to the CLI's copy is a workspace-root lint (like `task fixtures:lint`), outside this change.

**Not in this change:** any change to `pkg/core`; the CLI's own copies (cli changes `vet-validates-through-kernel`, `library-metadata-types`); the shared-labels question (cross-repo, an enhancement if pursued).

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `inventory-bridge`: "CLI packages copied to `pkg/`" now names `core` as the only copied package.

## Impact

**SemVer:** PATCH. No CRD, controller behaviour or reconcile-phase change; no import outside the deleted packages changes.

**API types and controllers affected:** none.

**Complexity justification (Principle VII):** net negative, about 500 lines of unused code deleted.
