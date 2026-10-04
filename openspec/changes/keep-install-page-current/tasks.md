Delivery: two PRs (design.md D4). PR A is section 1 alone and merges first; PR B is section 2 with the archive. No gate: independent of `publish-crd-bundle`.

## 1. The page names this release

- [x] 1.1 `docs/site/start/install-the-operator.md`: the four marker blocks of design.md D1 around the passages naming the operator version, each version in them set to the current `Version` (`1.0.0-beta.5` at planning); the edits of design.md D2. Nothing else in the file and no other file changes. Verify: `git diff --stat` lists only the page; every marker inside step 1 is indented three spaces; `task docs:bundle`, then in an opmodel.dev checkout `OPM_BUNDLES_LOCAL="opm-operator@v1.0=<this worktree>/out/opm-operator" task build:edge` and `grep -c 'start="' site/public/v1.0/docs/start/install-the-operator/index.html` prints 0 (one ordered list); `grep -n '4\.4\.4\|2\.0\.0-beta\|1\.0\.0-beta\.4' docs/site/start/install-the-operator.md` finds nothing; `task docs:bundle:check` passes.
- [x] 1.2 `task dev:fmt dev:vet dev:lint dev:test` and `task docs:bundle:check` green, then commit `docs(site): name the current operator release on the install page` and open it as PR A, alone. (Shipped as `docs(start): keep the install page's versions current`, opm-operator#200; its Lint needed opm-operator#201 first, design.md D4.)
- [x] 1.3 After PR A merges: record its squash SHA in design.md D4 (in PR B) as the commit a `1.0.0-beta.5` revision passes as `fix=`. (Recorded: `e268cbd`, revision run 37177763682.)

## 2. Release-please keeps it current

- [x] 2.1 `release-please-config.json`: add `docs/site/start/install-the-operator.md` to the root package's `extra-files`, after `internal/version/version.go`.
- [x] 2.2 `internal/version/version_test.go`: `TestInstallPageNamesThisRelease` per design.md D3. Verify: it passes; it fails naming the version when a block's version is changed locally to `1.0.0-beta.4`, and naming the page when a start marker is removed.
- [x] 2.3 `AGENTS.md`, beside the release-please and version-identity rules: operator versions on `docs/site/` pages sit inside `x-release-please-start-version` blocks (outside code fences) and move with each Release PR; a page names no core, catalog or cli version, it links where to find the current one.
- [ ] 2.4 `openspec validate keep-install-page-current --strict` passes; `task dev:fmt dev:vet dev:lint dev:test` green, then commit `ci(release): rewrite the install page's operator version in the Release PR`.
- [ ] 2.5 `openspec archive keep-install-page-current --yes` (the archive rides this PR). Verify: `openspec validate --specs --strict` passes for `release-automation`. Commit `docs(openspec): archive keep-install-page-current`.
