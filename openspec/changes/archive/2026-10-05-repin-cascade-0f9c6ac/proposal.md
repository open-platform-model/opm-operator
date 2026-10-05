## Why

`.github` main is now `0f9c6ac2c9b752a79f4874f637ef9955bcf00c13`. Since this repo's pin `6938f8e` it carries `.github` PR 16 (a single code owner), PR 17 (owner decision 37: which repos move when, in the README's rollout steps) and PR 19: `cascade-publish` refuses a push or recreate when a receiver's mirrored `.tasks/cascade/` files (`mirror_sources` in `wiring/lib.sh`) differ on its `origin/main`, the daily `cascade-mirror-drift.yml` runs the same comparison, the publish deny-list gains this repo's `modules/**`, and the resolver's `newest` checks this repo's `opm_operator-v*` module tags are on its `main`. This repo still pins `6938f8e`, so its cascade runs neither the mirror refusal nor the updated resolver.

PR 19 changes `wiring/lib.sh`, which both `cascade-notify` and `cascade-publish` run, so the README's "Both actions" rule applied: the library moved first as the canary (library PR 204). Its first live publish (library PR 206) and its first live notify (the library `v1.0.0-beta.5` release run 37294102028, `Notify downstream` green) on the new pin have succeeded, so the other repos may now move.

## What Changes

- All six `open-platform-model/.github` references (`cascade-notify` in `release.yml`, `cascade-receive.yml` and `cascade-publish` in `deps-cascade.yml`, the `cascade-gates.yml` call, and the resolver checkout `ref:` in `cascade-task.yml` and `module-deps.yml`) move to `0f9c6ac2c9b752a79f4874f637ef9955bcf00c13 # .github main`.
- `.tasks/cascade/wiring-check.sh` stays the canonical file at the new SHA, byte for byte. It is unchanged between `6938f8e` and `0f9c6ac`, so the copy is already identical; `--pin-on-main` compares it against the new pin.
- Nothing else. The README's config row for opm-operator at `0f9c6ac` matches `.tasks/cascade/wiring-check.yaml` (`env-allow`, the seven `publish-workflows`, `lint.yml` job `lint`, notify `needs`, `labels-managed: false`), and no caller shape changed, so neither the config nor `lint.yml` changes.
- The two `cascade-receiver` requirements that cite the `.github` README at `6938f8e` cite it at `0f9c6ac`.

Mirror check: this repo's `pins.sh`, `lib.sh`, `classes` and `cascade.sh` on `origin/main` (`bcfa722`) hash to the four values `mirror_sources opm-operator` records at `0f9c6ac`, so publish will not refuse this repo for mirror drift.

## Capabilities

### Modified Capabilities

- `cascade-receiver`: the publish-job and pin requirements cite the `.github` README at the new pin; behavior is unchanged.

## Impact

CI only (`ci`/`chore` commits; no operator or module release). Affected files: `.github/workflows/{release,deps-cascade,cascade-gates,cascade-task,module-deps}.yml`. `CASCADE_DRY_RUN` stays as it is.
