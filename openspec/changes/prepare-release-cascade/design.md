## Context

The release cascade is designed in the workspace repo (workspace RELEASING.md). opm-operator is a
tier-2 repo there: it receives library releases (shipped Go pin, `go.mod:14`
`github.com/open-platform-model/library v1.0.0-beta.1`) and cli releases (release-tool pin), and
the cli receives its published `install.yaml`. This change is the operator's Phase 1 preparation
(workspace RELEASING.md, section "Rollout and changes"): it adds the gate, the Dependabot ignore,
the changelog tweak and the pin file that the later cascade changes build on. It writes no
cascade automation.

Current state, verified on `origin/main` at `f31a013`:

| Item | Where | Today |
|---|---|---|
| Release PR CI | `release.yml:38-52` mints the release App token, so release-please PRs trigger `pull_request` CI normally; head branch `release-please--branches--main` | no pin check |
| Cheapest always-run PR job | `lint.yml:9-29`, job `lint` ("Run on Ubuntu"), checkout + setup-go + Task | no pin check |
| `replace` directives | `go mod edit -json` → `"Replace": null` | none |
| OPM Go pins | `go list -m all` → `github.com/open-platform-model/library v1.0.0-beta.1` (tag exists, `02344e5`) | clean |
| Published fixture cue.mods | `test/fixtures/modules/{hello,hello_web,podinfo,redis}`, `test/fixtures/modulepackages/{same four}` (`.tasks/examples.yaml:21-22`; published by `release.yml` `publish-examples`, bundled by `examples:bundle`) | no `-0.dev.` pin |
| Tracked `local-module.cue` | `git ls-files '*local-module.cue'` | none |
| Dependabot `gomod` | `.github/dependabot.yml:9-21` | no ignore; prefix `build` |
| `docs` section | `release-please-config.json:23` `"hidden": false` | releases |
| opm CLI pin | `test.yml:59`, `test-e2e.yml:98`, `publish-fixtures.yml:66`, `release.yml:274` | `@v1.0.0-beta.2` literal ×4 |
| Local opm use | `.tasks/examples.yaml:30`, `.tasks/module.yaml:31` (`OPM` default `opm` on PATH), `hack/fixtures.sh:94,103` (`OPM_BIN`) | no version pinned anywhere outside workflows |

## Goals / Non-Goals

**Goals:**
- A release PR cannot go green with an unreproducible OPM pin (G1).
- OPM Go bumps come only from the cascade.
- A doc-only commit cuts no release.
- The opm CLI pin lives in a file the cascade App can edit without the Workflows permission.

**Non-Goals:**
- The cascade receiver (`task deps:cascade`), labels, `.cascade-hold`, `.cascade-frozen`, G2/G3
  statuses: later changes `add-deps-cascade-task` and `join-release-cascade`.
- Making the `Lint` check required: an owner ruleset action (workspace RELEASING.md, section
  "Owner settings").
- Checking `.opm-cli-version` itself in G1. It is a release-tool pin and cuts no release; a bad
  value fails `go install` in the same PR's CI.
- Any controller, API, CRD or reconcile change. Reconcile phases (Source, Render, Apply, Prune,
  Status) are unaffected.

## Decisions

### G1 lives in a script plus a task, called from the `lint` job

`hack/release-pin-check.sh` holds the logic; `.tasks/deps.yaml` exposes it as
`task deps:release-check` (included from `Taskfile.yml` as `deps`), so it runs the same way
locally and in CI. The later `add-deps-cascade-task` change adds the receiver to the same
`deps` namespace. The CI step goes into `lint.yml`'s `lint` job: it runs on every PR, needs only
Go and git, and finishes in about a minute, so a gate failure shows early. `test.yml`'s `test`
job would work too but starts a registry service and seeds fixtures first.

```yaml
# .github/workflows/lint.yml, job lint, after "Install Task"
      - name: Release-pin gate (G1, release PRs only)
        if: startsWith(github.head_ref || github.ref_name, 'release-please--')
        run: task deps:release-check
```

The `github.ref_name` fallback is unused here (operator release PRs always arrive as
`pull_request`), but it keeps the condition identical across repos, where core and catalog_opm
dispatch release-PR CI with `workflow_dispatch` (workspace RELEASING.md, section "Gates").

### The four checks

```bash
#!/usr/bin/env bash
# hack/release-pin-check.sh: fail when a release would ship an unreproducible OPM pin.
set -euo pipefail
fail=0; bad() { printf 'release-pin-check: %s\n' "$*" >&2; fail=1; }

# 1. No replace directives.
go mod edit -json | jq -e '.Replace == null' >/dev/null \
  || bad "go.mod has replace directives: $(go mod edit -json | jq -c '.Replace')"

# 2. OPM Go pins: no pseudo-versions, and the version is a tag of the module's repo.
pseudo='([-.]0\.|-)[0-9]{14}-[0-9a-f]{12}$'
while read -r path ver; do
  if [[ $ver =~ $pseudo ]]; then bad "$path $ver is a pseudo-version"; continue; fi
  repo="https://$(cut -d/ -f1-3 <<<"$path")"   # github.com/open-platform-model/<repo>
  git ls-remote --exit-code --tags "$repo" "refs/tags/$ver" >/dev/null \
    || bad "$path $ver is not a tag of $repo"
done < <(go mod edit -json | jq -r '.Require[]?
          | select(.Path | startswith("github.com/open-platform-model/"))
          | "\(.Path) \(.Version)"')

# 3. No -0.dev. pins in published fixtures.
for f in test/fixtures/modules/*/cue.mod/module.cue test/fixtures/modulepackages/*/cue.mod/module.cue; do
  while IFS= read -r l; do bad "dev pin $f:$l"; done < <(grep -nE 'v: "[^"]*-0\.dev\.' "$f" || true)
done

# 4. No tracked local-module.cue anywhere.
while IFS= read -r f; do bad "tracked $f"; done < <(git ls-files '*cue.mod/local-module.cue')

exit $fail
```

The loops read through process substitution so `bad` runs in the main shell and `fail` survives.
The script MUST print every failure before exiting, not stop at the first. A nested OPM module
whose tags carry a path prefix would need its own tag rule; none exists today.

- **Pins are read from `go.mod` itself (`go mod edit -json`), not `go list -m all`.** `go mod edit`
  needs no network and no module resolution, so an unresolvable pin (the very case the gate
  exists for) cannot make the listing fail silently inside a process substitution. Since Go
  1.17 `go.mod` lists indirect requirements too, so nothing is missed.
- **Tag existence uses `git ls-remote`, not the Go proxy.** The proxy serves any commit as a
  pseudo-version and caches tags; `ls-remote` asks the repo itself. All OPM repos are public, so
  no token is needed. `library` is the only OPM Go pin today; the loop covers any future one.
- **The pseudo-version regex** is the one in workspace RELEASING.md, section "Gates"; it matches
  all three Go pseudo-version shapes (`vX.0.0-…`, `vX.Y.Z-pre.0.…`, `vX.Y.(Z+1)-0.…`).
- **Dev CUE pins are checked only in published fixtures.** `internal/source/testdata/minimal-module`
  is test-only and never published; flagging it would block a release for nothing.
- **`local-module.cue` is checked repo-wide**, because it is CUE's replace mechanism and has no
  legitimate tracked use.
- `PinnedOperatorVersion` versus `install.yaml` is a cli-only G1 check and does not apply here.

### Dependabot ignore

```yaml
  - package-ecosystem: "gomod"
    directory: "/"
    ...
    ignore:
      # OPM modules move only through the release cascade, which titles a
      # shipped bump fix(deps) so it releases (workspace RELEASING.md).
      - dependency-name: "github.com/open-platform-model/*"
```

The `build` prefix and the Kubernetes group stay as they are; third-party bumps are unaffected.

### Hide `docs`, keep `refactor`

`release-please-config.json:23` becomes `{ "type": "docs", "section": "Documentation", "hidden": true }`.
release-please already treats hidden types as non-releasable here: the spec's "Only
non-releasable commits" scenario relies on it for `chore`/`test`/`ci`/`build`, and those have
never opened a Release PR. Past CHANGELOG entries are not rewritten. `refactor` stays visible
because library rewrites must keep integrating early (workspace RELEASING.md, section "Bump rule").

### `.opm-cli-version` and how workflows read it

The file is one line, `v1.0.0-beta.4`, with a trailing newline. Each workflow reads it in the
install step itself, so no step depends on an env var set elsewhere:

```yaml
      - name: Install opm
        run: |
          v=$(tr -d '[:space:]' < .opm-cli-version)
          [[ $v =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]] \
            || { echo "::error file=.opm-cli-version::not a cli tag: '$v'"; exit 1; }
          go install "github.com/open-platform-model/cli/cmd/opm@${v}"
```

- **No literal `cli/cmd/opm@v` remains.** The current workspace `.tasks/deps/opm-cli.sh` finds the
  operator pin by grepping `cli/cmd/opm@v` in workflow files; after this change that grep matches
  nothing, so the workspace script must write `.opm-cli-version` instead (dependency on workspace
  `docs/release-cascade`).
- **`release.yml` `publish-examples`** checks out the release tag (`release.yml:247-252`), so it
  installs the CLI pinned at that tag. That is the right version: the one CI tested the tag with.
- **The four-line block is repeated, not factored.** A composite action or shared script would
  add a file the cascade must not touch and save twelve lines. Repetition is cheaper (Principle
  VII).
- **Local tasks are unchanged.** `.tasks/examples.yaml:30` and `.tasks/module.yaml:31` use `opm` on
  PATH and pin no version. `hack/fixtures.sh` must stay byte-identical to the cli copy
  (`.tasks/examples.yaml:10-11`), so its "install the pinned cli release" hint is left as is.

### One section per concern, CLI move first

Moving the CLI from beta.2 to beta.4 is the only step that can change CI behaviour (the fixture
publish gates run through `opm module publish --dry-run`, `hack/fixtures.sh:232`). It goes first
so a surprise surfaces before the gate work. The other three sections are independent config
and script edits.

## Research & Decisions

### Is cli v1.0.0-beta.4 installable and gate-compatible?
**Context**: `go install pkg@version` refuses a module whose `go.mod` has `replace`; a newer CLI
could add a publish gate the fixtures fail.
**Explored**: `git show v1.0.0-beta.4:go.mod` in cli: no `replace`, `go 1.26.0`, library
`v1.0.0-beta.1`. `git log v1.0.0-beta.2..v1.0.0-beta.4` has no commit touching publish gates or
`module version`. Tags `v1.0.0-beta.1..4` all exist (`git ls-remote`).
**Decision**: Move straight to beta.4 in section 1, verified by running `task examples:check`
locally with beta.4 before the commit.
**Rationale**: Low risk, and the section's own gates catch a surprise; no separate spike needed.

### Where G1 runs
**Context**: G1 must be a step in a required job (workspace RELEASING.md, section "Gates").
**Explored**: `lint.yml` (`lint` job, Go + Task, no services) and `test.yml` (`test` job,
registry service, fixture seed, envtest).
**Decision**: `lint`.
**Rationale**: Same inputs, faster signal; both jobs are candidates for the ruleset's required
checks.

### Hiding `docs` and the spec
**Context**: The main spec's "Release PR creation on push to main" has a scenario "Docs-only
commits cut a release". OpenSpec 1.12 refuses a MODIFIED block that drops a main-spec scenario.
**Decision**: REMOVED that requirement and ADDED "Release PR opens for releasable commits on push
to main" with the surviving scenarios plus two new ones; MODIFIED "Changelog generation" and
"Version bump determination per release line" in place (their scenario names still fit).

## Risks / Trade-offs

- [G1 is advisory until the ruleset requires `Lint`] → The owner settings step in workspace
  RELEASING.md, section "Owner settings" makes it binding; the gate still shows red on the PR.
- [`git ls-remote` needs network from the runner] → GitHub-hosted runners have it; a transient
  failure fails the gate loudly, never silently passes.
- [A docs-only fix to user-facing docs no longer releases] → Intended. A doc fix that must ship
  rides the next releasable commit, or uses a `Release-As:` footer.
- [Old workspace script skips the operator after merge] → Merge workspace `docs/release-cascade`
  first (proposal, "Depends on / gates").

## Migration Plan

Merge after workspace `docs/release-cascade`. No rollback beyond reverting the PR; nothing is
published by this change.

## Open Questions

- None blocking. The owner may later prefer the gate in both `lint` and `test`; that is a
  one-line addition.
