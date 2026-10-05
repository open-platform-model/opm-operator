## Why

`.github` merged `bound-cascade-publish` and the resolver and wiring-check follow-ups (PRs 11, 12, 14 and 15; squash `6938f8e0247e019cb0c2db13fff5b7b558a6b67d`). core, catalog_opm, library and cli already pin that SHA (core 122, catalog_opm 153, library 190, cli 315). This repo still pins `2376ffa`, so its receiver runs the old resolver, its publish job calls a `cascade-publish` that now requires a `gates-only` input, and its wiring check is an older copy that reads no config and never compares itself with `.github`.

## What Changes

- Move all six `.github` references (`cascade-notify`, `cascade-publish`, `cascade-receive.yml`, `cascade-gates.yml`, and the resolver checkout `ref:` in `cascade-task.yml` and `module-deps.yml`) to `6938f8e0247e019cb0c2db13fff5b7b558a6b67d # .github main`.
- Replace `.tasks/cascade/wiring-check.sh` with the canonical file at that SHA (byte-identical, `cmp`).
- Add `.tasks/cascade/wiring-check.yaml` with this repo's values. `publish-workflows` names every workflow that publishes (`release.yml`, `publish-fixtures.yml`, `docs.yml`, `image-pr.yml`, `test-e2e.yml`, `module-image.yml`, `module-deps.yml`), and `extra-references` declares the `module-deps.yml` resolver checkout.
- `deps-cascade.yml` `publish`: the `if:` gains `inputs.gates_only != true`, and the `cascade-publish` step passes `gates-only: ${{ inputs.gates_only == true }}`.
- `lint.yml`: "Verify the cascade wiring" runs `bash .tasks/cascade/wiring-check.sh --pin-on-main` with `GH_TOKEN`, after only SHA-pinned actions.
- Doc follow-ups to `harden-release-workflows` (opm-operator PR 225; the wording is chosen in this change, not taken from a review): the `module-image.yml` header, the `AGENTS.md` `module-image-pr` note (the key is in the Environment; only the org-secret deletion is pending), and the `CODEOWNERS` header (GitHub requests the review; the ruleset decides whether it is required).

## Capabilities

### Modified Capabilities

- `cascade-receiver`: the publish job's gates-only clause and input, and the pin and check requirement (canonical copy, config file, `--pin-on-main` CI step, the new rules).

## Impact

CI and docs only (`ci`/`docs` commits; no operator or module release). All receivers are dry-run (`CASCADE_DRY_RUN=true`), so the publish edits do not run live until the Phase 4 canary.

The diff from `2376ffa` touches `cascade-publish` (the required `gates-only` input), so the canary rule in the `.github` README (step 2 of moving the pin) and workspace `RELEASING.md` applies: the other receivers stay on the old pin until one canary's first live publish succeeds. No live publish has run yet. This change, the last of five, moves every receiver to `6938f8e` while all are dry-run, under a supervisor decision; the first live publish then happens in one repo only (Phase 4). That exception is not yet recorded by the owner: before this merges, the owner either amends the README and `RELEASING.md` to allow it or signs it off on the pull request.

The doc follow-ups ride in this pin change although the README says a pin PR changes nothing else. They are comment and doc text only, nothing releases from them, and they keep `AGENTS.md` and `CODEOWNERS` true to what this change makes the check do.
