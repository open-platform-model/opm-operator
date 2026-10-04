# operator-module-release Specification

## Purpose
Release the operator module `opmodel.dev/modules/opm_operator` from this repository as its own release unit: its version train, release PR and tags, its release gate, its publish with the install manifest, the PR that moves its operator image after each operator release, and its own cascade PR for its core and catalog pins.

## Requirements

### Requirement: The module is a release unit of its own

`release-please-config.json` SHALL declare the module directory `modules/opm_operator` as a second package beside the operator package `"."`. The module package SHALL have its own version in `.release-please-manifest.json`, its own changelog, its own release PR (`separate-pull-requests: true`) and its own release tags of the form `opm_operator-vX.Y.Z`. No operator release tag (`vX.Y.Z`) and no operator release branch (`release/vX.Y`) can take that form. Only release-please SHALL create these tags, and no workflow SHALL move one. The operator package SHALL exclude the module directory, so a commit that changes only the module directory proposes no operator release. A commit that changes no file under the module directory SHALL propose no module release. The module's versions SHALL NOT follow the operator's versions or the cli's versions.

#### Scenario: A module-only change releases only the module

- **WHEN** a `feat(module): ...` PR that changes only files under `modules/opm_operator/` is squash-merged to `main`
- **THEN** release-please opens or updates the module's release PR and leaves the operator's release PR unchanged

#### Scenario: An operator-only change releases only the operator

- **WHEN** a `fix(controller): ...` PR that changes no file under `modules/opm_operator/` is squash-merged
- **THEN** release-please opens or updates the operator's release PR and leaves the module's release PR unchanged

#### Scenario: Module tag shape

- **WHEN** the module's release PR proposing `0.2.0` is merged
- **THEN** release-please creates the tag `opm_operator-v0.2.0` and a draft GitHub Release for it, and no tag starting with `v` is created by that merge

#### Scenario: The operator's tag listing ignores module tags

- **WHEN** the tags `v1.0.0-beta.6` and `opm_operator-v0.2.0` both exist
- **THEN** a listing of `refs/tags/v*`, which the cascade resolver uses for the operator, contains only `v1.0.0-beta.6`

### Requirement: The module version follows the 0.x bump rule on the v0 path

A module release is breaking when its `#config` change is breaking, when the operator release it deploys is a declared breaking change against the one the previous module release deployed, or when its CRDs stop serving a version the previous release's CRDs served. While the module path is `opmodel.dev/modules/opm_operator@v0`, every module version SHALL be `0.y.z`: a breaking release SHALL raise `y` and reset `z`, and every other release SHALL raise `z`, `feat` included. The first module release SHALL be `0.1.0`. The module SHALL move to the `@v1` path only with a `1.0.0` release made no earlier than the operator's first GA release, and the module release gate SHALL refuse `1.0.0` or higher while the operator release the module deploys is a prerelease.

#### Scenario: A breaking change raises the minor

- **WHEN** the module's last release is `0.3.2` and an unreleased squash commit is titled `fix(deps)!: deploy operator v1.0.0-beta.9 from the operator module`
- **THEN** the module's release PR proposes `0.4.0`

#### Scenario: A feature raises the patch

- **WHEN** the module's last release is `0.3.2` and the unreleased commits include `feat(module): add a pod annotations value` and no breaking change
- **THEN** the module's release PR proposes `0.3.3`

#### Scenario: The first release

- **WHEN** the module package has never released and a squash commit titled `feat(module): ...` that changes only `modules/opm_operator/` lands on `main`
- **THEN** the module's release PR proposes `0.1.0`, and the operator's release PR is unchanged

#### Scenario: Hidden module commits open no release

- **WHEN** only `build`, `test`, `docs`, `ci` or `chore` commits have changed `modules/opm_operator/` since the module's last release, or since it was added
- **THEN** release-please opens no module release PR

#### Scenario: 1.0.0 before operator GA is refused

- **WHEN** the module's release PR proposes `1.0.0` and the module names `opm-operator:v1.0.0-beta.9`
- **THEN** the module release gate fails and names the operator prerelease

### Requirement: The module's identity version has one writer

The module's `identity.Version` SHALL be written only by the release workflow's identity-advance step, so the module the publish job pushes is exactly its tagged source with nothing added at publish time. That step runs `opm module version set <version>` on the module's open release PR branch, commits `chore: advance opm_operator identity.Version to <version>`, and pushes to that branch. No `x-release-please-version` annotation or other release-please updater SHALL write the module's identity package, and `opm module publish --version` SHALL only assert the version. The step SHALL be idempotent: when the branch already declares the version it SHALL change nothing. It SHALL run in a job of its own after the release-please job, so its failure never fails the job whose outputs start the release jobs, and it SHALL hold the release App token only for its push.

#### Scenario: Release PR gains the identity commit

- **WHEN** release-please opens the module's release PR proposing `0.2.0` and the module's `identity.Version` reads `0.1.0`
- **THEN** the same workflow run pushes one commit to that PR's branch setting `identity.Version` to `0.2.0`, and the merged release commit declares `0.2.0`

#### Scenario: Re-run converges

- **WHEN** the step runs again for a branch whose `identity.Version` already reads `0.2.0`
- **THEN** it pushes nothing

#### Scenario: A failed identity advance strands no release

- **WHEN** the identity advance fails in a run where release-please created an operator release
- **THEN** the operator's release jobs still run, and "Re-run failed jobs" re-runs only the identity advance

### Requirement: Module release gate

On the module's release PR, the required `Lint` job SHALL run a module release check that fails, naming every failure before it exits, when:

- the module's `cue.mod/module.cue` pins a `-0.dev.` version or the module tree holds a `cue.mod/local-module.cue`;
- the module's operator image reference is not `ghcr.io/open-platform-model/opm-operator:<tag>@<digest>`, where `<tag>` is a published, non-draft operator release and `<digest>` is the manifest-list digest GHCR serves under that tag;
- `identity.Version` differs from the version the release PR proposes, or its major disagrees with the module path's major;
- the proposed version breaks the v0 rules of "The module version follows the 0.x bump rule on the v0 path";
- `<tag>` is older than the repository's minimum operator version, the first operator release that refuses to reconcile the operator's own instance;
- the module's generated CRDs or controller RBAC differ from what the operator source at `<tag>` generates.

The last check exists because a module release must render exactly the CRDs the controller it deploys serves and exactly the cluster permissions that controller declares, and `main` can move ahead of the deployed operator release. Because the module's generated data follow `main`'s `config/` on every pull request, module releases are frozen while `main`'s `config/` differs from the deployed tag's, until the image-bump PR of an operator release carrying that change merges. The check SHALL also run in the module publish job before anything is pushed, so a release merged past a red check still publishes nothing. The checks that need no network (development pin, `local-module.cue`, version and major, the `1.0.0` rule, the minimum operator version, reporting every failure) SHALL be covered by an offline test that the `Lint` job runs on every pull request.

#### Scenario: Image digest does not match GHCR

- **WHEN** the module names `opm-operator:v1.0.0-beta.6@sha256:aaa…` and GHCR serves `sha256:bbb…` under `v1.0.0-beta.6`
- **THEN** the check fails naming both digests, and the publish job, if reached, pushes nothing

#### Scenario: Image tag is a draft release

- **WHEN** the module names an operator tag whose GitHub Release is still a draft
- **THEN** the check fails naming the tag

#### Scenario: CRDs ahead of the deployed operator

- **WHEN** `main` changed a CRD after `v1.0.0-beta.6` and the module still names `v1.0.0-beta.6` but carries the new CRD
- **THEN** the check fails naming the CRD, and the module waits for the image-bump PR of the next operator release

#### Scenario: Operator below the minimum

- **WHEN** the module names `opm-operator:v1.0.0-beta.5` and the minimum operator version is `v1.0.0-beta.6`
- **THEN** the check fails naming both tags

#### Scenario: Development pin

- **WHEN** the module's `cue.mod/module.cue` pins catalog `v4.6.0-0.dev.3`
- **THEN** the check fails naming the pin

#### Scenario: Several failures named at once

- **WHEN** the offline test runs the check over a fixture tree with a development pin and an `identity.Version` that differs from the proposed version
- **THEN** the check exits non-zero and its output names both failures

#### Scenario: Not a release PR

- **WHEN** the `Lint` job runs on any other pull request
- **THEN** the module release check does not run

### Requirement: The module publishes its tagged source once

When release-please creates a module release, the module publish job SHALL check out the module's release tag, install the opm CLI named by `.opm-cli-version`, run the module release check, and publish `modules/opm_operator` with `opm module publish <dir> --version <version>`. The job SHALL run only when the module package created a release. It SHALL never run for an operator-only release. Re-running the job for the same release SHALL succeed without pushing when the registry already holds that version with the content this tag produces. It SHALL fail when the registry holds that version with other content. The job SHALL NOT rely on the registry to refuse an overwrite.

#### Scenario: First publish

- **WHEN** the module release `opm_operator-v0.2.0` is created and GHCR holds no `v0.2.0` of `opmodel.dev/modules/opm_operator`
- **THEN** the job publishes `v0.2.0` from the tagged tree and records its manifest digest

#### Scenario: Re-run after a later step failed

- **WHEN** the publish job is re-run after its publish succeeded and a later step of that job failed
- **THEN** the publish step reports the version as already present with the same content and pushes nothing

#### Scenario: Version held by other content

- **WHEN** GHCR already holds `v0.2.0` with a digest that differs from what the tagged tree publishes
- **THEN** the job fails, the release stays a draft, and the fix is the next module version

### Requirement: Every module release publishes the install manifest rendered from it

The module publish job SHALL render the published module version from the registry at its default values, as the instance `opm-operator` in the namespace `opm-operator-system`, against the platform generated from the module's own pins and with no cluster. It SHALL write the result to `install.yaml`, with the Namespace and the CRDs before every namespaced object, and attach it to the module's draft release. The render and the upload SHALL run in a job apart from the publish, so re-running them never re-enters the publish. The manifest SHALL name the operator image by the tag and digest the module names, and the job SHALL check that before the upload. Applying it with `kubectl apply --server-side` on a cluster with no operator SHALL yield a running operator. A pull request that changes `modules/opm_operator/` or the scripts that render the manifest SHALL prove this on a kind cluster.

#### Scenario: Manifest equals the module's render

- **WHEN** the module release `opm_operator-v0.2.0` completes
- **THEN** its GitHub Release carries `install.yaml`, and rendering `opmodel.dev/modules/opm_operator` version `0.2.0` with no values as `opm-operator` in `opm-operator-system` yields the same objects

#### Scenario: Module debug values are not used

- **WHEN** the module declares non-empty `debugValues`
- **THEN** the manifest is still the render at the `#config` defaults

#### Scenario: Namespace first

- **WHEN** `install.yaml` is read top to bottom
- **THEN** the Namespace `opm-operator-system` and the four CRDs come before every namespaced object

#### Scenario: Upload fails after the publish

- **WHEN** the install-manifest upload fails after the module version was published
- **THEN** "Re-run failed jobs" re-runs the render and the upload, never the publish

#### Scenario: Manifest names another image

- **WHEN** the rendered manifest runs an operator image other than the module's tag and digest
- **THEN** the job fails before the upload and the release stays a draft

#### Scenario: Fresh cluster

- **WHEN** CI applies the manifest rendered from a pull request's module to a kind cluster with no OPM CRDs
- **THEN** the operator's Deployment completes its rollout

### Requirement: Module releases are drafts until every asset is attached

The module package SHALL set `draft: true` and `force-tag-creation: true`. Every upload to a module release SHALL first confirm that exactly one release carries the tag and that it is a draft. A final module publish job SHALL depend on the publish job and SHALL confirm `install.yaml` is attached before it publishes the draft. A module release is not a prerelease, and it SHALL be published with GitHub's "latest" mark withheld (`make_latest=false`), so it never becomes the repository's latest release (0021:D11:R12). Recovery before publication is "Re-run failed jobs". After publication it is the next module version; a published tag is never moved.

#### Scenario: Asset missing

- **WHEN** the final module publish job finds the draft for `opm_operator-v0.2.0` without `install.yaml`
- **THEN** it fails and the release stays a draft

#### Scenario: Never the latest release

- **WHEN** the final module publish job publishes `opm_operator-v0.2.0` and the repository's other releases are operator prereleases
- **THEN** the release is published with `make_latest=false`, and GitHub's latest release of the repository is not `opm_operator-v0.2.0`

#### Scenario: Already published

- **WHEN** the final job runs for a module release that is already published
- **THEN** it succeeds without changing anything

### Requirement: Only this repository's module release publishes under the module path

No workflow, task or script in this repository other than the module publish job SHALL publish to `opmodel.dev/modules/opm_operator` or to its GHCR repository `ghcr.io/open-platform-model/modules/opm_operator`. The module publish job SHALL refuse to run unless the repository is `open-platform-model/opm-operator` and the checked-out ref is a module release tag. Local and test flows SHALL publish the module only to a registry that maps `opmodel.dev` away from GHCR.

#### Scenario: Search for publishers

- **WHEN** `.github/workflows/`, `.github/scripts/`, `Taskfile.yml`, `.tasks/` and `hack/` are searched for a publish of the module directory or of `ghcr.io/open-platform-model/modules/opm_operator`
- **THEN** the only match is the module publish job and the task it calls

#### Scenario: Fork or dispatch on another ref

- **WHEN** the module publish job is started in a fork or on a ref that is not a module release tag
- **THEN** it fails before logging in to the registry

### Requirement: An operator release opens the PR that moves the module's image

Once an operator release has been published, the release workflow SHALL open or update one pull request on the branch `module/operator-image`. The PR SHALL set the operator release the module deploys to that release's tag and the manifest-list digest GHCR serves under it, and SHALL change nothing else: no file outside `modules/opm_operator/`, and not the module's generated CRD and RBAC files, which follow `main`'s `config/`. When the CRDs or RBAC roles under `config/` at that tag differ from `main`'s, the run SHALL fail naming the files and open no PR, because the module release gate would refuse that module. The run SHALL also fail and open no PR when `<tag>` does not descend from the operator tag the module deploys, so a re-run or a dispatch never moves the module back. The PR's title SHALL be `fix(deps): deploy operator <tag> from the operator module`. The title SHALL be `fix(deps)!: ...` instead when the operator CHANGELOG sections between the module's previous operator tag and `<tag>` carry a breaking-change entry, or when a CRD under `config/crd/bases` at `<tag>` stops serving a version it served at the module's previous operator tag. The PR SHALL be created with the release App's token so its CI runs. A `workflow_dispatch` with a `tag` input SHALL do the same for a published operator release, for recovery. While the branch carries only bot commits, a later run SHALL rebuild it from `main`. Once a human commit is on it, the run SHALL add its own commit on top and never rewrite the branch.

#### Scenario: Operator release published

- **WHEN** `v1.0.0-beta.6` is published with manifest-list digest `sha256:abc…`
- **THEN** a PR from `module/operator-image` sets the module's image to `ghcr.io/open-platform-model/opm-operator:v1.0.0-beta.6@sha256:abc…`, changes only `modules/opm_operator/operator/operator.cue`, and is titled `fix(deps): deploy operator v1.0.0-beta.6 from the operator module`

#### Scenario: Breaking operator release

- **WHEN** the operator CHANGELOG section for `v1.0.0-beta.7` lists a breaking change
- **THEN** the PR title carries `!`, and merging it makes the module's next release raise the minor

#### Scenario: Draft operator release

- **WHEN** the dispatch names an operator tag whose release is still a draft
- **THEN** the run fails and opens no PR

#### Scenario: Tag behind main

- **WHEN** the dispatch names `v1.0.0-beta.6` and `main` changed a CRD after that tag
- **THEN** the run fails naming the CRD file and opens no PR

#### Scenario: Older operator tag

- **WHEN** the module deploys `v1.0.0-beta.7` and a re-run or dispatch names `v1.0.0-beta.6`
- **THEN** the run fails and opens no PR

#### Scenario: Image PR never releases the operator

- **WHEN** the image PR is merged
- **THEN** release-please proposes a module release and no operator release
