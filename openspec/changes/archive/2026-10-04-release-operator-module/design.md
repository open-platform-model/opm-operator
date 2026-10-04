## Context

See proposal.md, "Why". This change touches only release automation. No reconcile phase changes: Source, Render, Apply, Prune and Status behave the same. No API type or CRD changes.

### What the repository has today

Read 2026-10-04 at `origin/main` `40a2345`:

- **One release-please package.** `release-please-config.json` has the single package `"."`: release type `go`, `package-name: opm-operator`, `draft: true`, `force-tag-creation: true`, `versioning: prerelease`, beta, and `extra-files` for `internal/version/version.go` and the install page. At the top level it sets `"bump-minor-pre-major": false`, `"bump-patch-for-minor-pre-major": false` and `"include-component-in-tag": false`. `.release-please-manifest.json` is `{".": "1.0.0-beta.5"}`.
- **Every release job gates on `releases_created`.** `.github/workflows/release.yml` keys `image-release` (line 58), `publish-examples` (238), `publish-docs` (326) and `publish-release` (343) on `needs.release-please.outputs.releases_created == 'true'`, and reads `tag_name` (lines 35, 69 and others). release-please-action v5 (`release.yml:49`) sets `releases_created` when any package released. It sets `release_created` and `tag_name` for the root package and `<path>--release_created` and `<path>--tag_name` for any other package. With a second package, a module-only release would start `image-release` with an empty tag. The unmerged `join-release-cascade` adds `notify-downstream` with the same `if`.
- **The previous-tag lookup is unanchored.** `publish-examples` finds its previous tag with `git describe --tags --abbrev=0 "${TAG}^"` (`release.yml:290`). Once a module tag exists, it would return that tag and skip fixtures that changed since the last operator release.
- **The image job owns the install manifest.** `image-release` renders `dist/install.yaml` from `config/default` with the image digest (`release.yml:216-220`) and uploads it to the draft (224-231). `release-guard.sh publish` refuses a draft without `install.yaml` or `opm-examples.tar.gz` (`.github/scripts/release-guard.sh:15-18`). This change leaves that in place; `stop-operator-install-manifest` removes it once the cli no longer reads it.
- **The release-pin check runs only on release PRs.** The `Lint` job runs `task deps:release-check` when the head ref starts with `release-please--` (`lint.yml:29-31`). That runs `hack/release-pin-check.sh`, which covers `go.mod` and the fixtures only. The job checks out at the default depth with no tags (`lint.yml:16`) and sets up Go and Task, not CUE.
- **Hidden commits open no release PR.** The `release-automation` requirement "Release PR opens for releasable commits on push to main" makes `docs`, `chore`, `test`, `ci` and `build` hidden, and a `release-as` value does not make them releasable. Every commit `add-operator-module` lands under `modules/opm_operator/` is `build`, `test` or `docs`.
- **Precedents.**
  - catalog_opm releases a module from a sub-path with `include-component-in-tag`, tags `opm-vX.Y.Z` and `separate-pull-requests: true`. Its release workflow pushes the identity-advance commit onto `release-please--branches--main--components--opm` with `opm catalog version set` (catalog_opm `.github/workflows/release.yml`, "Advance identity.Version on the release PR"). It publishes with `opm catalog publish ./src --version`, which asserts the version. It signs nothing: no CUE artifact in the workspace carries a cosign signature or attestation.
  - The image job's guard reuses a version tag only when its revision label names the release commit (`.github/scripts/image-tag-guard.sh`).
- **The resolver lists only v-tags.** The cascade resolver lists operator releases from `refs/tags/v*` and counts one as published when `install.yaml` downloads (`.github` `.github/scripts/cascade/lib/release.sh:19`, `lib/query.sh:88-90`).
- **The resolver keys pins by a free string.** `pins.sh` rows are keyed by a pin key the resolver requires to be non-empty and unique within one report (`.github` `.github/scripts/cascade/lib/prtext.sh:44`). `.cascade-hold` and `.cascade-frozen` name pin keys, and the operator's `cascade.sh` looks holds and frozen paths up by module path (`opmodel.dev/catalogs/opm@v4`, `opmodel.dev/core@v2`).
- **The cascade receiver runs one task on one branch.** The unmerged `join-release-cascade` calls the shared `cascade-receive.yml`, which runs `task -x deps:cascade` and pushes `deps/cascade`.
- **The cli downloads the operator's manifest.** It fetches `releases/download/<operator tag>/install.yaml` for `opm operator install --version` (cli `internal/operator/fetch.go:13` holds the base URL, line 23 builds the asset URL) and for `task operator:sync` (cli `Taskfile.yml:497`). This is the behaviour archived `0006:D35` set: one embedded `install.yaml`, `--version` fetching the release asset.
- **The pinned cli has the commands this needs.** `.opm-cli-version` is `v1.0.0-beta.7`. That cli has `opm module version set` (exit 0 when set or already set) and `opm module publish --version`, which only asserts a declared version. Its first-party publish gate admits `opmodel.dev/modules/<leaf>` (cli `internal/publish/gates.go:23` at `v1.0.0-beta.7`). A second publish of a held version is refused with "<path> already holds <tag>" (cli `internal/publish/registry.go:56` at `v1.0.0-beta.7`).

### Interface with add-operator-module

That change merged as opm-operator PR #216 (`81a640d`) and is archived. Its proposal and design (read 2026-10-04 at `008fec3`, and at `9f09ac2` for the minimum operator version) named what this change calls; "Reconciled with the merged tree" below records what landed:

| What | Name in add-operator-module |
| --- | --- |
| Module directory | `modules/opm_operator/`, `cue.mod` declares `opmodel.dev/modules/opm_operator@v0` |
| Identity | `modules/opm_operator/identity/identity.cue`: `ModulePath`, `Version` (tooling writes these two only) |
| Deployed operator | `modules/opm_operator/operator/operator.cue`: `Version` (bare, e.g. `1.0.0-beta.5`) and `Image: {repository, tag, digest}`, with `tag: "v\(Version)"`, so tag and version cannot disagree. The image file below means this file. |
| Generated data | `zz_generated_crds.cue` (`#crdSource`) and `zz_generated_rbac.cue` (`#rbacSource`), from `config/crd/bases` and eight `config/rbac` role files |
| Generator | `hack/operator-module/generate.sh`, task `operator-module:generate` |
| Drift check | `hack/operator-module/drift-check.sh`, task `operator-module:drift`; on every pull request it compares the module's data with `config/` at `HEAD`; `--ref <tag>` compares with `config/` extracted from that tag with `git archive` |
| Minimum operator version | `hack/operator-module/min-operator-version`: one line, the tag of the first operator release containing `refuse-own-instance` (opm-operator PR #211); the module never names an older operator, and its render test enforces that |
| Instance coordinates | the module renders only for instance `opm-operator` in `opm-operator-system` and refuses any other |
| `#config` | image repository, registry mapping, default service account, resources, replicas, extra arguments; `image.tag`, `image.digest` and `image.pullPolicy` are refused, so the image moves only through `operator/operator.cue` |

Task G-dep confirms each name against the merged tree and substitutes any that changed. The drift check's `--ref` mode is the one name this change depends on in a specific way: the release gate must compare with the deployed tag, not `HEAD`. The generator needs no such mode (see "The module's generated data follow `main`").

### Reconciled with the merged tree

Read 2026-10-04 at `origin/main` `81a640d` (add-operator-module #216, join-release-cascade #217 and the `v1.0.0-beta.6` release merged since `40a2345`). Every name in the table above holds as written, with these facts added:

- **Names.** `identity/identity.cue` declares `Version: "0.1.0"` already, so the first identity advance is a no-op. `operator/operator.cue` names `Version: "1.0.0-beta.6"` and the `sha256:7871a5dd…825e` digest GHCR serves under `v1.0.0-beta.6`. The drift check also runs as `task operator-module:drift REF=<tag>`, which first runs `cue fmt --check`; the release gate calls the script directly. `.tasks/operator-module.yaml` is the include. The module declares `debugValues: {}`.
- **Minimum operator version.** `hack/operator-module/min-operator-version` reads `v1.0.0-beta.6`, a published release; PR #211's squash commit `8d34b6b` is an ancestor of it. `config/crd/bases` and `config/rbac` are unchanged between `v1.0.0-beta.6` and `main`, and `drift-check.sh --ref v1.0.0-beta.6` passes.
- **Render order.** The pinned cli renders the module as the four CRDs, the Namespace, the cluster-scoped roles and bindings, then the namespaced objects. The manifest check therefore only verifies the order; it never reorders.
- **Release state.** `.release-please-manifest.json` is `{".": "1.0.0-beta.6"}`. Read again at review (2026-10-04): the operator's release PR #222 merged before this change as `4eebece`, and `v1.0.0-beta.7` is published (prerelease). Its release run predates `module-image-pr`, so no image PR exists for it; the module still deploys `v1.0.0-beta.6`. The only commit under `modules/` since `v1.0.0-beta.6` is `build(module)` (#216), hidden, so merging this change opens no module release PR. The first module release opens either from the README carrier (`0.1.0` deploying beta.6) or from `module-image.yml` dispatched with `tag=v1.0.0-beta.7`, whose `fix(deps)` image PR, once merged, opens `0.1.0` deploying beta.7, makes the carrier redundant, and is the first real run of `module-image.yml`. The owner picks which.
- **The cascade is wired.** `join-release-cascade` merged. `notify-downstream` is a key-holding job whose one step is the SHA-pinned `cascade-notify` action, not a reusable `cascade-notify.yml`. `task cascade:wiring:check` (the "Verify the cascade wiring" step of `Lint`) pins that job's `if` to `releases_created`, admits exactly one job in the `cascade` Environment per workflow, and lists every `.github` reference. Rekeying `notify-downstream` therefore edits the wiring check too, and a second notify job would change the Phase 3 wiring contract's key-holder set.
- **`deps:cascade` already leaves the module alone.** Its `MODULE_DIRS` loop reads only `test/fixtures/modules/*` (opm-operator issue #219 asks to add the module there). This change answers #219 with `deps:cascade:module` instead.
- **The enhancement amendment merged.** enhancements PR #94 (2026-10-04) holds the amended `0021:D4` and the new `0021:D11`, "The operator's install artifact is a registry module on its own version train" (G-enh). Its R12 keeps every module release off GitHub's "latest" mark, which this change implements; `enhancement.yaml` claims D11 for its release half (R3, R4, R5, R12) and leaves D4 to `stop-operator-install-manifest`.
- **The cli has moved on.** cli `v1.0.0-beta.8` is released, but `.opm-cli-version` still names `v1.0.0-beta.7`, whose `module build` takes a published path with `--version` and `-f`, and `module publish --version` only asserts a declared version.

### Evidence from the prototype experiments

Two prototypes were run on 2026-10-04 while this design was worked out, against operator `v1.0.0-beta.5`, core `v2.0.0-beta.2`, catalog opm `4.5.2` and a cli built from `main` at release `1.0.0-beta.7`. Their results that bear on releasing the module:

- **The render needs no cluster.** `opm module build` with `KUBECONFIG=/nonexistent` rendered all 19 objects of the operator's install against a platform generated from the module's own dependency pins. Rendering the module after publishing it to a local registry produced the same output as rendering the local tree. This is what the publish job's install-manifest step does.
- **Render cost.** 5.66 s wall time and 507 MB peak memory cold (fresh CUE cache, core, catalog and Kubernetes schemas fetched), 2.0 to 3.9 s and about 500 MB warm. The module was 1,977 lines of CUE, 1,713 of them generated. A release job absorbs this easily.
- **The CRDs round-trip.** All four CRD specs rendered equal to the controller-gen YAML, including CEL validations, preserve-unknown-fields, list-map keys, the status subresource and printer columns.
- **The drift check catches the drift that matters.** The regenerate-and-diff check passed against the operator's own `config/` and failed, with a readable diff, on a copy with one added RBAC verb (`list` on serviceaccounts) and one added CRD short name. That is the failure the release gate turns into a refused module release.
- **A module upgrade is a re-apply that relabels everything.** Installing `0.1.0` and then `0.2.0` from the registry on kind applied `15 configured, 4 unchanged`: every non-CRD object changed because its `module.opmodel.dev/version` label changed. A reinstall of the same version changed no object (same uid and resourceVersion for all 19, same render digest). So every module release touches every object on every cluster that takes it, even a release that only moves a pin. That is why the operator package and the module package exclude each other's commits, and why the image-bump PR is one PR per operator release, not one per commit.
- **The manifest must carry the Namespace first.** The install applied the Namespace before the other 18 objects. Creating the namespace separately (`--create-namespace`) left a Namespace without OPM labels that the module's own Namespace then collided with. The kubectl manifest therefore orders the Namespace and the CRDs before every namespaced object, and nothing else creates the Namespace.
- **Install time.** Three fresh kind installs from the registry-pulled module took 28.6 to 31.8 s, of which render plus CRDs was 2.3 to 3.5 s; readiness (image pull, probes) dominates. The PR-time kind proof in section 2 budgets for that.

Not measured: whether GHCR or a local registry refuses a re-push of a module version. The prototype only avoided re-pushing. The publish job therefore relies on the cli's own refusal ("already holds") and checks content itself (U7), whatever the registry does.

## Goals / Non-Goals

**Goals:**

- Two release units in one repository that never trigger each other's release, except through the image-bump PR.
- The module published from its tagged tree byte for byte, with the install manifest beside it.
- Each step of the cascade (operator to module to cli) automated, and every version written by one writer.

**Non-Goals:**

- The cli's install, pins, release-pin check and docs pins, workspace `RELEASING.md` and the `.github` resolver. Those are their own changes.
- Removing `install.yaml` from operator releases. That is `stop-operator-install-manifest`, gated on the cli.
- Transferring ownership of the operator's own instance. Another change owns it; the instance `opm-operator` in `opm-operator-system` stays CLI-owned and the operator never reconciles it.
- Release branches for either unit. None exist during beta (root `AGENTS.md`, "Release branches").
- A docs-kit bundle for the module, and a signature, provenance or SBOM on the module artifact (see "Deferred").

## Decisions

### Tag shape `opm_operator-vX.Y.Z`

The module package sets `"component": "opm_operator"`, `"include-component-in-tag": true` and the default separator `-`. Tags are `opm_operator-v0.1.0`, and a future release branch would be `release/opm_operator-v0.1`.

Why this shape:

- An operator tag always starts with `v`, and an operator release branch with `release/v`, so neither can take this shape. The workspace allows only release-please to create tags, and the operator's tags carry no component today, so the module's tags need one to be told apart.
- The resolver's `refs/tags/v*` listing never sees module tags.
- It mirrors catalog_opm's `opm-vX.Y.Z`: the leaf of the module path, then the version.

**Alternatives:**

- `module-vX.Y.Z` does not name the module.
- `opm_operator/vX.Y.Z` (Go-style) reads as a Go submodule path, and this repository is a Go module.
- `opm-operator-module-vX.Y.Z` is long, and the leaf name is what users type.

### Version rule: `@v0`, 0.x bumps, `1.0.0` only after operator GA

A module version is breaking when its `#config` change is breaking, when the operator release it deploys is a declared break against the one the previous module release deployed, or when its CRDs stop serving a version the previous release's CRDs served. Those are the three things a consumer of a module version receives.

The path starts at `@v0`. While the deployed operator is on its beta line, its controller arguments and so `#config` still change; a `@v1` start would move the path to a new major, an import change for every consumer, on each narrowing. On `@v0` a break raises the minor and every other release raises the patch. The module moves to `@v1` only with a `1.0.0` release made no earlier than the operator's first GA release.

A `1.0.0-beta.N` line like the operator's was not chosen: it would look coupled to the operator's `1.0.0-beta.N` when it is not.

These are supervisor defaults the owner has not yet confirmed (gate G-owner).

### Separate release PRs and excluded paths

Top level: `separate-pull-requests: true`. Package `"."` gains `"exclude-paths": ["modules/opm_operator"]`. Package `"modules/opm_operator"` is release type `simple` with:

- `"component": "opm_operator"`, `"include-component-in-tag": true`, `"package-name": "opm_operator"`;
- `"bump-minor-pre-major": true` and `"bump-patch-for-minor-pre-major": true`, which override the top-level `false`;
- `"versioning": "default"`, `"prerelease": false`, `"initial-version": "0.1.0"`;
- `"draft": true`, `"force-tag-creation": true`;
- `"changelog-path": "CHANGELOG.md"` inside the module, so the changelog ships with it;
- `extra-files: [{type: generic, path: "RELEASE"}]`, catalog_opm's pattern, so `modules/opm_operator/RELEASE` carries the version and `x-release-please-version`;
- the root package's changelog sections, `deps` included.

The two pre-major flags are exactly the 0.x rule above: a breaking change in 0.x raises the minor, and `feat` raises the patch.

`exclude-paths` keeps the image-bump PR from cutting an operator release. Without it, an operator release would trigger an image PR, which would trigger another operator release, with no end. The exclusion works only for a squash commit that touches nothing outside `modules/opm_operator/`. A PR that changes the module and any other path counts for both packages, which is why every automated module PR (the image bump, the module's cascade PR, the carrier) is confined to the module's directory.

Separate PRs give the module its own release PR, and one merge never releases both units. The root package's release PR branch is then `release-please--branches--main--components--opm-operator` (U3).

**Alternative:** one combined release PR (the default). Not chosen: one merge would release both units at once, and the module's release could not wait for the operator it names to be published.

### A visible carrier opens the first module release

Hidden commits open no release PR, and `add-operator-module` lands only hidden ones. So, once section 2's config is on `main`, section 3 lands one PR titled `feat(module): publish the operator module on its own release train`. It changes only `modules/opm_operator/README.md`: a section saying how a module version maps to the operator version it deploys and where the install manifest is published. That PR is the carrier: release-please opens the module's release PR proposing `0.1.0`, and the operator package sees nothing. A `feat` on `@v0` would raise the patch, but the first release takes `initial-version`. U5 proves both halves in the sandbox: hidden module commits alone open nothing, and the carrier opens `0.1.0`.

**Alternative:** title section 2's PR `feat`. Not chosen: that squash commit changes `.github/` and the release config, which are the operator package's paths, so it would also cut an operator `-beta.N` with no binary change.

### Existing jobs key on the root package

`image-release`, `publish-examples`, `publish-docs`, `publish-release` and `notify-downstream` change `if:` to `needs.release-please.outputs.release_created == 'true'` (`notify-downstream` keeps its `&& vars.CASCADE_NOTIFY != 'off'`). `task cascade:wiring:check` pins `notify-downstream`'s `if`, so its expected value moves in the same commit. That differs from the `if` the Phase 3 wiring contract (version 3.1) §4.6 prints, which predates a second package; without it a module-only release would run the notify action with an empty tag. They keep `tag_name`, which is the root package's. The `release-please` job exports `module_release_created: ${{ steps.release.outputs['modules/opm_operator--release_created'] }}`, `module_tag_name` and `module_version`. `publish-examples` uses `git describe --tags --abbrev=0 --match 'v[0-9]*' "${TAG}^"`.

### Identity advance, the only writer of the module's version

The published module must be its tagged source with nothing stamped at publish time, so the version must be in the tree before the tag exists. A job of its own, `module-identity-advance`, after the `release-please` job, does the following:

1. Look the branch `release-please--branches--main--components--opm_operator` up with `GITHUB_TOKEN`. If it does not exist, exit 0.
2. Check that branch out with no persisted credentials, and install the opm CLI from its `.opm-cli-version`, the same check as the other install steps.
3. Read `VERSION=$(jq -r '.["modules/opm_operator"]' .release-please-manifest.json)` from that branch. release-please writes the proposed version into the manifest on the release branch, the first release included, which is what catalog_opm reads too.
4. Run `opm module version set "$VERSION" ./modules/opm_operator`. If the diff is empty, exit 0.
5. Commit `chore: advance opm_operator identity.Version to ${VERSION}`, then mint the release App token and push to the branch with it, so the release PR's CI runs.

It is a job, not steps of the `release-please` job (review of PR #221): every release job reads that job's outputs, so a failed identity advance there skipped them all and stranded the draft release-please had just created, and a re-run re-ran release-please, which no longer reports the release. The advance runs whenever the module's release PR is open, which the `config/` freeze keeps true across operator releases, so the two often meet in one run. As its own job, a failure leaves the release jobs running and "Re-run failed jobs" re-runs only the advance. The App token is minted only after the cli is installed and run, so no module-proxy code runs while it exists.

The step never relies on `opm module publish --version` to fill the version. release-please writes only `CHANGELOG.md` and `RELEASE` in the module. That `RELEASE` file is release-please's own record and is not read as the identity, the same split catalog_opm uses.

### Module release gate inside `Lint`

The new `hack/operator-module/release-check.sh` runs as `task operator-module:release-check VERSION=<v>`. It runs in the `Lint` job when the head ref is the module's release branch, with the proposed version read from the branch's `.release-please-manifest.json`. Running inside the existing required job matters: a skipped job reports as passing, a failing step does not (workspace `RELEASING.md`, "G1 placement"). The job keeps its name. The publish job runs the same script from the tag before pushing. The operator's release-pin check (G1) is skipped on the module's release branch: it checks `go.mod` and the fixtures, none of which ship in the module, so a pseudo-version Go pin on `main` would otherwise block a module release.

The script runs from the module directory, which holds the `cue.mod`; the repository root has none:

```bash
# hack/operator-module/release-check.sh <proposed-version>
M=modules/opm_operator
ex() { (cd "$M" && cue export "$@" --out text); }
tag=$(ex ./operator -e Image.tag)
img=$(ex ./operator -e '"\(Image.repository):\(Image.tag)@\(Image.digest)"')
[[ $img =~ ^ghcr\.io/open-platform-model/opm-operator:(v[^@]+)@(sha256:[0-9a-f]{64})$ ]] || bad "image reference $img"
want=${BASH_REMATCH[2]}
"$RELEASE_GUARD" assert-published "$tag"   || bad "$tag is not a published release"
got=$("$IMAGE_TAG_GUARD" digest "ghcr.io/open-platform-model/opm-operator:$tag")
[ "$got" = "$want" ]                       || bad "$tag: module names $want, GHCR serves $got"
grep -nE 'v: *"[^"]*-0\.dev\.' $M/cue.mod/module.cue && bad "development pin"
[ -e $M/cue.mod/local-module.cue ]         && bad "local-module.cue present"
[ "$(ex ./identity -e Version)" = "$proposed" ] || bad "identity.Version"
# v0 rules: major 0 on the @v0 path; 1.0.0 or higher refused while $tag carries a prerelease suffix
min=$(cat "$MIN_OPERATOR_VERSION_FILE")
if ! git rev-parse --verify --quiet "$min^{commit}" "$tag^{commit}" >/dev/null; then
  bad "cannot resolve $min or $tag (shallow checkout or missing tag?)"
elif ! git merge-base --is-ancestor "$min" "$tag"; then
  bad "$tag is older than the minimum operator version $min (refuse-own-instance)"
fi
"$DRIFT_CHECK" --ref "$tag" || bad "CRDs or RBAC differ from $tag"
```

Every `bad` records the failure and the script exits non-zero after the last check, so one run names every failure. `RELEASE_GUARD` and `IMAGE_TAG_GUARD` default to `.github/scripts/release-guard.sh` and `image-tag-guard.sh`, `DRIFT_CHECK` to `hack/operator-module/drift-check.sh`, and `MIN_OPERATOR_VERSION_FILE` to `hack/operator-module/min-operator-version`. Git calls use the working directory's repository. The offline test `hack/operator-module/test-release-check.sh` sets the two guards and `DRIFT_CHECK` to passing stubs and runs the script once per case inside a scratch git repository it builds: the case's fixture tree from `hack/testdata/operator-module-release-check/` copied to `modules/opm_operator/`, a min file naming the first of two tags made in order, and the fixture's operator tag on the second unless the case is the minimum case. Every case therefore has the file and both tags, so the dev pin, `local-module.cue`, version and major mismatch, the `1.0.0` rule, the minimum operator version, an unresolvable tag and the reporting of several failures at once each fail for their own reason, and a clean tree passes. All of this runs in `Lint` on every pull request, without network.

The minimum check compares by ancestry, not by parsing versions: operator tags are cut from `main` in order, so a tag at or above the minimum is one that contains it. The same file is what add-operator-module's render test compares with `golang.org/x/mod/semver`; the release check repeats it because a release is where a hand-edited downgrade would ship.

What the `Lint` job needs for the check: `fetch-depth: 0`, so the drift check's `git archive` and the minimum check's `git merge-base` find the operator tags; `cue-lang/setup-cue` at the `CUE_VERSION` `test.yml` names; `GH_TOKEN: ${{ github.token }}` and `GH_REPO: ${{ github.repository }}` on the step, for `release-guard.sh`. `docker buildx imagetools` is on the hosted runner already. These additions change nothing for the job's other steps.

### The module's generated data follow `main`

add-operator-module's drift check runs on every pull request against `config/` at `HEAD`, so the module's generated CRD and RBAC files always equal `main`'s `config/`. The release gate compares them with `config/` at the deployed operator tag. Both hold only while `main`'s `config/` equals the deployed tag's. So:

- **Module releases are frozen while `main`'s `config/` differs from the deployed tag.** A pull request that changes a CRD or an RBAC marker regenerates the module's data, and from then on the gate refuses every module release until an operator release carrying that change exists and the image-bump PR names it. A module-only fix in that window waits for the next operator release. The gate names the differing files, so the freeze is visible on the release PR.
- **The image-bump PR regenerates nothing.** It writes only the deployed version and digest. Since the module's data already equal `main`'s `config/`, they equal the new tag's when nothing moved after the tag.
- **An image bump for a tag behind `main` is refused.** `hack/operator-module/image.sh` compares `config/crd/bases` and the RBAC role files at the tag with `main` and fails, naming the files, when they differ. Such a PR could never release: the gate would refuse it. This covers the dispatch of an old tag and the case where a CRD change merged between the release and the job.

**Alternative:** regenerate the image PR's data from the tag with a generator `--ref` mode. Not chosen: the per-PR drift check would fail that PR whenever `main` had moved, and when `main` had not moved, regenerating changes nothing.

### Publish job

`module-publish` needs `release-please` and runs only when `module_release_created == 'true'`. It holds `contents: read` and `packages: write`. Steps 1 to 5 are its own; steps 6 and 7 are `module-manifest`, which needs `module-publish`, holds `contents: write` and `packages: read`, checks the tag out, installs opm and logs in to GHCR. Split at review of PR #221: with one job, a failed render or upload sent "Re-run failed jobs" back through the publish, whose reuse path rests on U7 (an unproven byte-reproducible publish); as two jobs a re-run never re-enters the publish. Steps:

1. Refuse unless `github.repository == 'open-platform-model/opm-operator'` and `MODULE_TAG` matches `^opm_operator-v[0-9]+\.[0-9]+\.[0-9]+$`.
2. Check out the module tag with full history (the gate needs the operator tag).
3. Set up CUE and Go, install opm from `.opm-cli-version` at the tag, log in to GHCR.
4. Run `task operator-module:release-check VERSION=<v>`.
5. Run `opm module publish ./modules/opm_operator --version <v>`. If it refuses only with "already holds", resolve the held digest and compare it with a dry publish into a job-local registry (U7). Equal is a reuse; anything else fails.
6. Run `opm module build opmodel.dev/modules/opm_operator --version <v> --name opm-operator -n opm-operator-system -f hack/operator-module/defaults.cue > install.yaml`, with empty values (U6). Then run `hack/operator-module/manifest-order.sh install.yaml`, which fails unless the Namespace and every CRD come before every namespaced object (the pinned cli already renders them first; the script checks, never reorders). Then `hack/operator-module/manifest-image.sh install.yaml` fails unless every Deployment container runs `<Image.repository>:v<Version>@<Image.digest>` from the tag's `operator/operator.cue`. The kind install job (`operator-module.yml`) runs both checks on every PR that touches the module, its scripts or `.opm-cli-version`.
7. Run `release-guard.sh assert-draft "$MODULE_TAG"`, then `gh release upload "$MODULE_TAG" install.yaml --repo "$GH_REPO" --clobber`.

`module-publish-release` needs `[release-please, module-manifest]` and runs `release-guard.sh publish "$MODULE_TAG"`. `release-guard.sh` picks its required assets by tag shape: `install.yaml` for `opm_operator-v*`; `install.yaml` and `opm-examples.tar.gz` for `v*`, unchanged.

A module notify job is not part of this change. Notify is now the `cascade-notify` action in a key-holding job, the wiring contract admits one such job in `release.yml`, the `.github` resolver lists operator releases from `v*` tags only, and no cli consumes the module yet. A follow-up adds `module-notify` once the contract and the cli's receiver know module tags (see "Deferred").

Both units' releases now attach an asset named `install.yaml`. Anything that lists this repository's releases and reads that asset must filter by tag shape. The cli change `migrate-manifest-installed-operator` has a tool, `hack/operator-legacy`, that downloads `install.yaml` from every opm-operator release that has it; it must read only `v*` releases, or it takes module renders in as legacy operator manifests. That is reported to the cli change, not edited here. The same name is kept because both are the same kind of thing, an install manifest. A module release never takes the repository's "latest" mark (`0021:D11:R12`, and `0021:D8:R6` for a `0.x` release): `release-guard.sh publish` publishes an `opm_operator-v*` draft with `make_latest=false`, so `releases/latest/download/install.yaml` is never the module's manifest and the install page names a module release by tag.

### Image-bump PR

`module-image-pr` needs `[release-please, publish-release]` and runs only when `release_created == 'true'`. It mints the release App token, the same App as the release PR, so the PR's CI starts on its own. Then it runs `hack/operator-module/image.sh set "$TAG"`:

```bash
# hack/operator-module/image.sh set <operator-tag>
M=modules/opm_operator
release-guard.sh assert-published "$tag"        # new read-only mode: one release, draft == false
digest=$(image-tag-guard.sh digest "ghcr.io/open-platform-model/opm-operator:$tag")
prev=$(cd "$M" && cue export ./operator -e Image.tag --out text)
git diff --quiet "$tag" origin/main -- config/crd/bases config/rbac || fail "main's config/ moved after $tag"
write $M/operator/operator.cue Version=${tag#v} Image.digest=$digest   # tag follows Version
breaking=0
awk "/^## \[${tag#v}\]/,/^## \[${prev#v}\]/" CHANGELOG.md | grep -q '⚠ BREAKING CHANGES' && breaking=1
served-versions(config/crd/bases at $prev) vs (at $tag): a version dropped -> breaking=1
```

The workflow then checks `module/operator-image` out from `origin/main`. If the branch carries only commits by the App, it is rebuilt. If a human commit is on it, the workflow merges `origin/main` and adds a commit. It pushes with `--force-with-lease` and creates or edits the PR with the computed title. A `workflow_dispatch` input `tag` on a small `module-image.yml` workflow runs the same script for recovery.

The work is split in two jobs, as the cascade receiver splits it, and lives in `module-image.yml`, which `release.yml`'s `module-image-pr` calls (`workflow_call`) and a person dispatches (`workflow_dispatch`). Its `compute` job runs `image.sh` with no App token (only `GITHUB_TOKEN` with `contents: read`, for `release-guard.sh`) and hands the changed module file and the PR body on as an artifact. Its `publish` job mints the App token, checks out `main`, unpacks the change and runs `hack/operator-module/bot-pr.sh`, which owns the rebuild-or-extend rule, the lease push and `gh pr create|edit`, and refuses any path outside `modules/opm_operator/`. So no CHANGELOG parse or registry read runs while the App token exists, and the release run and the recovery dispatch run the same jobs. `image.sh` checks the tag against `main` with the drift check's `--ref` mode, the release gate's own verdict, and lists the differing `config/` files when it fails. `module-deps.yml` uses the same split and the same `bot-pr.sh`. Both workflows run `bash -eo pipefail` (`defaults.run.shell: bash`), so `image.sh ... | tee -a $GITHUB_OUTPUT` fails its step on a refusal; before review it ran without pipefail and every refusal left a green run with no PR. The publish job refuses a handed-over tar member outside `modules/opm_operator/` before extracting, and `module-image.yml` opens its PR only from `main`. `image.sh` refuses a tag that does not descend from the operator tag the module deploys, so a re-run of an older release's job or a dispatch with an old tag never moves the module back.

The cascade receiver could have moved this pin as a shipped pin. That is not chosen: the cascade is still dry, and an operator release is this repository's own event. A dedicated job keeps the image moving whether the cascade is live or not.

### The module's core and catalog pins move in their own PR

Every user who installs the module receives its core and catalog pins, so a held pin would ship a catalog no release was tested against. They are shipped pins, and the operator module, not the operator binary, becomes the downstream of core and the catalog.

They cannot ride the repository's cascade PR. That PR also moves the fixtures (`test/`) and possibly library (`go.mod`), which belong to the operator package. Under the squash merge its one commit would then count for both packages: a `fix(deps)` title would propose an operator `-beta.N` with no binary change, whose image-bump PR would cut a second module release and notify the cli twice. So:

- `task deps:cascade` never touches `modules/opm_operator/`. Its PR keeps today's title rules: `test(fixtures)` when only fixtures moved.
- `task deps:cascade:module` runs the same script with the module as its only target. It moves the module's catalog to the newest published catalog `K` and its core to the core `K` pins, under the same holds and frozen paths, keyed by the same module paths (`opmodel.dev/catalogs/opm@v4`, `opmodel.dev/core@v2`): a hold on the catalog holds the module too, and a frozen entry for `modules/opm_operator` freezes only the module. It never touches the image file, `identity.Version`, the generated files, `CHANGELOG.md` or `RELEASE`.
- Its title and body come from the resolver, with `.tasks/cascade/module-pins.sh` and `.tasks/cascade/module-classes` (every path under the module shipped). Its report has two rows keyed by the module paths; being its own report, the keys are unique. The title is `fix(deps): bump the operator module's opm catalog to vX and core to vY`.
- `.github/workflows/module-deps.yml` runs the task on `repository_dispatch` `upstream-released` (the same dispatch the receiver takes, so both run) and on `workflow_dispatch`. It checks the resolver out from `open-platform-model/.github` at the one pinned SHA with `# .github main`, so `task cascade:wiring:check` lists it as a sixth `.github` reference. It pushes `module/deps` with the release App token and opens or updates its PR, like the image-bump PR. While `CASCADE_DRY_RUN` is not exactly `false` it writes the diff to the job summary and pushes nothing, as the receiver does.

**Alternative:** a scope input on the shared `cascade-receive.yml`. Not chosen: it is another repository's workflow, and this repository already opens the image-bump PR the same way.

### Release-flow sandbox unknowns

Each unknown is proven in `open-platform-model/release-flow-sandbox` with this change's config shape, as the App, before this change's PR merges (the config lands in that PR). The results are recorded on the PR; "G-sandbox evidence" holds what was proven without the sandbox.

| # | Unknown | Pass condition |
| --- | --- | --- |
| U1 | A component-less root package `"."` beside a component package: release-please keeps finding the root's last release from `v*` tags, and the module's from `opm_operator-v*` | root proposes the next `beta.N`; module proposes `0.1.0`, then `0.1.1` |
| U2 | `exclude-paths: ["modules/opm_operator"]` on `"."` | a module-only commit opens no root release PR; a commit touching the module and `test/` opens both |
| U3 | `separate-pull-requests: true` | both branch names as stated; an already open combined release PR is not merged into either (owner closes it) |
| U4 | per-package `bump-minor-pre-major` and `bump-patch-for-minor-pre-major` override the top-level `false` | `fix!` raises 0.y; `feat` raises 0.y.z |
| U5 | first release: hidden commits under the module, then a visible carrier, with `initial-version: "0.1.0"` and no manifest entry | hidden commits alone open no module release PR; the carrier opens one proposing `0.1.0`; the branch's manifest names `0.1.0`; the identity commit lands on that PR |
| U6 | `opm module build <published path> --version <v> -f <empty values>` renders the `#config` defaults, not `debugValues`, and orders Namespace and CRDs first | render matches the defaults render; order checked. The prototype rendered a published version from a registry with no cluster, but its `debugValues` was empty, so the defaults-versus-debugValues half is still open |
| U7 | re-run of `opm module publish` on a held version: identify "same content" (digest of a dry publish to a local registry equals the held digest) | equal on a true re-run, unequal after a source change |
| U8 | a module release published with `make_latest=false` stays off GitHub's "latest" mark (`0021:D11:R12`), and an operator release published later is unaffected | after the module release, `gh api repos/<sandbox>/releases/latest` does not return the module tag |

If any unknown fails, the PR does not merge. design.md and the spec deltas are revised, through a follow-up change once this one is archived.

## Risks / Trade-offs

- [The release-please multi-package behaviour differs from the docs] → U1 to U5 are proven in the sandbox before the config merges. A failure holds the PR.
- [A module release merged before the operator it names has been published] → The gate refuses a draft operator tag on the release PR. The publish job reruns the gate before pushing. A stranded module tag rolls forward to the next version, as for every tag.
- [A CRD or RBAC change on `main` freezes module releases until the next operator release] → Intended: a module release must render exactly the CRDs and RBAC of the operator it deploys. The gate names the files. The image-bump PR of the next operator release unblocks it. There is no other way out: the per-PR drift check keeps the module's data at `main`.
- [Every module release re-applies every object on every cluster] → Measured in the prototype (see "Evidence"). Mutual path exclusion, one image PR per operator release and one module cascade PR keep releases to the ones that carry a change.
- [A hand-made PR touches the module and other paths] → It releases both units. `AGENTS.md` says to keep module changes in PRs of their own; automated PRs are confined by their scripts.
- [A CRD or RBAC PR must touch the module] → `task operator-module:drift` (`test.yml`) forces it to regenerate `modules/opm_operator/zz_generated_*` in the same PR, so its squash commit lands in both changelogs and opens a module release PR that the gate holds until the image PR of an operator release carrying that `config/` merges. `AGENTS.md` names this exception. Tooling under `hack/operator-module/` belongs to the operator package and is committed as `ci` or `build`.
- [The `.github` cascade gates treat the module's release PR as the operator's] → `gates-eval.sh` in `.github` takes every `release-please--*` PR: G2 (freshness) runs the operator's `task -x deps:cascade` on the module's release head, and the module's own pins are never freshness-gated; G3 (settled, `lib.sh`) flags an opm-operator `autorelease: pending` PR whose body holds `**deps:**`, which every module release PR after an image or `module/deps` PR carries, so the cli's release PRs are flagged until the module releases although the cli does not consume it. Both gates warn only today. A `.github` follow-up scopes opm-operator's G2 and G3 by head branch (`--components--opm-operator`) and runs `deps:cascade:module` for `--components--opm_operator`, before either gate enforces.
- [The App token's PR on `module/operator-image` or `module/deps` rewrites a branch] → It rewrites only while every commit on the branch is the App's, and once a human commits it only adds commits. The lease refuses if the branch moved since it was fetched.
- [The two minimum checks can disagree on a release branch] → add-operator-module's render test orders by semver; the release check orders by git ancestry. For a tag cut off a release branch they can disagree: a higher semver tag without PR #211 passes the render test, and a tag carrying a cherry-pick of #211 fails the release check. No `release/*` branch exists during beta (Non-Goals), so both agree today. A release-branch change revisits the release check, for example by testing for #211's change itself rather than ancestry.
- [Two release PRs confuse the owner] → `AGENTS.md` names both. The module's release PR also carries the identity-advance commit, so wait for it before merging, as in catalog_opm.
- [GHCR package created with the wrong link or visibility on first publish] → G-owner runs after the first module release: link the package to this repository only, give Actions access only to it, and make it public. Until that is done, `opm` cannot pull it anonymously, so the cli's work waits.
- [This repository's wiring check departs from the wiring contract's printed `if` for `notify-downstream`] → The contract's `releases_created` predates a second package; keeping it would notify the cli with an empty tag on every module release. `.github` should take the same edit into contract §4.6; until then the in-repo check is the source of truth for this repository.
- [A module release takes GitHub's "latest" mark] → `0021:D11:R12` forbids it, and GitHub marks a published non-prerelease as latest by default. `release-guard.sh publish` sends `make_latest=false` for every `opm_operator-v*` tag; U8 proves it in the sandbox. `stop-operator-install-manifest` still plans a kubectl path through `releases/latest/download/install.yaml`, which this rule rules out; that change is revised before it is implemented (reported to the supervisor).

## Migration Plan

Two PRs (revised 2026-10-04 when the change was implemented; the planned one-PR-per-section delivery would have left half-wired release jobs on `main` between PRs):

- **This change's PR** carries sections 1, 2, 4 and 5 and the archive. Every commit is `ci`, `docs` or `chore`, so its squash commit releases neither unit, even though it adds the `modules/opm_operator/RELEASE` seed (task 2.1): a hidden type opens no release PR for either package. The reviewer runs the sandbox proof (G-sandbox) and the owner answers G-owner (a) before it merges.
- **The carrier PR** (section 3) changes only `modules/opm_operator/README.md` and is opened only after this change's PR is on `main`: under today's single-package config its `feat` would cut an operator release. Its merge opens the module's `0.1.0` release PR. The owner merges that after G-owner. Afterwards the owner applies the GHCR package settings, and the cli's wave can start.

Rollback: revert the section's PR. Tags and published module versions stay, as for every release.

## Deferred

- **A docs-kit bundle for the module's `#config`.** No owner decision asks for it, and no site reads a module bundle. The module's README documents `#config`. A bundle under `modules/opm_operator/` would also ship inside the published module, because `opm module publish` zips the directory as it is on disk. A later change adds it with its own directory outside the module.
- **A module notify job.** The cli does not consume the module until its `install-operator-from-module` change ships, the `.github` resolver lists operator releases from `v*` tags only, and notify is a key-holding job the Phase 3 wiring contract admits once per `release.yml`. A follow-up adds `module-notify` with the contract change. Until then the cli learns of a module release by its own sweep or by hand. `stop-operator-install-manifest` assumes this job exists, so it now waits for that follow-up too.
- **A cosign signature and provenance attestation on the module.** No CUE artifact in the workspace is signed (catalog_opm and opm-modules sign nothing), and signing would add `sha256-*.sig` and `.att` tags to the module's repository that every resolver and `cue mod` lookup would have to ignore. A later change adds it across all CUE artifacts at once if the owner wants it.

## Open Questions

1. The tag shape, the `0.1.0` start and the 0.x rule are supervisor defaults the owner has not confirmed. G-owner asks before section 2 merges.
2. Settled by `0021:D11:R12` (merged after this design was written): a module release never takes the "latest" mark.

## Research & Decisions

### release-please outputs with two packages
**Context**: Every release job reads `releases_created`, which a second package also sets.
**Explored**: release-please-action v5 `outputReleases` (root outputs unprefixed, other paths `<path>--<key>`), `release.yml:34-35,58,238,326,343`, `join-release-cascade` notify job.
**Decision**: Root jobs use `release_created`; module jobs use `modules/opm_operator--release_created`, exported as `module_release_created`.
**Rationale**: Without it, a module-only release starts the image job with an empty tag.

### Where the module's version is written
**Context**: The published module must be its tagged source with nothing added at publish time; `opm module publish --version` can fill an open version.
**Explored**: catalog_opm's identity-advance step and its `RELEASE` extra file; cli `module version set` and `publish --version` help (`v1.0.0-beta.7`).
**Decision**: Release-workflow identity advance on the module's release PR; `--version` only asserts.
**Rationale**: One writer, the version is in the tagged tree, and the precedent is proven in production.

### How the first module release is opened
**Context**: Every module commit so far is a hidden type, and hidden commits open no release PR, even with `release-as`.
**Explored**: `release-automation` "Release PR opens for releasable commits on push to main"; titling section 2 `feat`; a `release-as` value.
**Decision**: A carrier PR `feat(module)` confined to `modules/opm_operator/README.md`.
**Rationale**: Only a visible commit confined to the module's directory releases the module without releasing the operator.

### How the image reference moves
**Context**: Each operator release must be followed by a module change that names its image by tag and digest.
**Explored**: the cascade receiver (`join-release-cascade`, dry for now), a release.yml job, Renovate-style digest pinning, regenerating the module's data from the tag.
**Decision**: A release.yml job after `publish-release` plus a dispatch workflow, both calling `hack/operator-module/image.sh`, which writes only the version and digest and refuses a tag whose `config/` differs from `main`.
**Rationale**: Independent of the cascade's state, one PR per operator release, and consistent with the per-PR drift check that keeps the module's data at `main`.

### How the module's core and catalog move
**Context**: A cascade PR that moves the module and the fixtures together releases both units.
**Explored**: one cascade PR with a module row (the squash commit counts for both packages), a scope input on the shared receive workflow, a module-only task and workflow in this repository.
**Decision**: `task deps:cascade:module` and `module-deps.yml`, opening `module/deps`.
**Rationale**: Confined to the module's directory, so it releases only the module; no change to another repository's workflow.

### Publish idempotence
**Context**: "Re-run failed jobs" must not fail after a successful publish, and must not overwrite.
**Explored**: the cli refuses a held version with "already holds" (cli `internal/publish/registry.go:56` at `v1.0.0-beta.7`); whether GHCR or a local registry refuses an overwrite was not measured.
**Decision**: Publish through the cli; on "already holds" compare the held digest with a dry publish into a job-local registry (U7).
**Rationale**: The cli's refusal is the known guard, and a blind skip on "already holds" would accept other content under the tag.

## G-sandbox evidence

The sandbox run is not recorded here: the writer may not run anything in `open-platform-model/release-flow-sandbox`, so U1 to U5 and U7, U8 are the reviewer's pre-merge step, with the exact steps in the PR body ("What the reviewer must do"). What was proven without the sandbox, 2026-10-04:

- **U2 (static).** release-please 17.11.2, the version `release-please-action` v5.0.0 resolves (`^17.6.0`), drops a commit from a package when every file it touches under that package lies under an `exclude-paths` entry (`build/src/util/commit-exclude.js`, `shouldInclude` and `isRelevant`, paths normalized without slashes and matched as `<path>/` prefixes). The root package `"."` is assigned every commit (`CommitSplit` skips `"."`), so `exclude-paths: ["modules/opm_operator"]` is what keeps module-only commits out of it. Behaviour on GitHub is still for the sandbox.
- **U6 (local directory half).** With the pinned cli `v1.0.0-beta.7` and `KUBECONFIG=/nonexistent`, a copy of the module with `debugValues: {replicas: 3}` rendered `replicas: 3` with no `-f`, and `replicas: 1` (the `#config` default) with `-f` naming a file holding `{}` or `values: {}`. The unchanged module rendered the same bytes with and without that file, in 2.1 s, ordered CRDs, Namespace, cluster-scoped roles and bindings, then namespaced objects. The published-path half waits for the first module release.
- **Release config.** `release-please-config.json` validates against release-please's published `schemas/config.json` (python `jsonschema` 4.23); the schema is open, so every key was also matched against the keys release-please 17.11.2's `manifest.js` reads.
- **Identity advance.** The step's script, run twice in a scratch clone against a bare origin holding the module's release branch at `0.2.0`, pushed one `chore: advance opm_operator identity.Version to 0.2.0` commit, then reported `identity.Version already declares 0.2.0` and pushed nothing.
- **Release gate.** `task operator-module:release-check VERSION=0.1.0` passes on the merged module with its committed image, and fails naming both digests when the module names a wrong one.
