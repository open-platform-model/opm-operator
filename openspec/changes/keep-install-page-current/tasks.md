Delivery: one PR, two sections (design.md D4). No gate: independent of `publish-crd-bundle`.

## 1. The page names this release

- [ ] 1.1 `docs/site/start/install-the-operator.md`: the four marker blocks of design.md D1 around the passages naming the operator version, each version in them set to the current `Version` (`1.0.0-beta.5` at planning); the edits of design.md D2. Nothing else in the file and no other file changes. Verify: `git diff --stat` lists only the page; `grep -n '4\.4\.4\|2\.0\.0-beta\|1\.0\.0-beta\.4' docs/site/start/install-the-operator.md` finds nothing; `task docs:bundle:check` passes.
- [ ] 1.2 `task dev:fmt dev:vet dev:lint dev:test` and `task docs:bundle:check` green, then commit `docs(site): name the current operator release on the install page`.

## 2. Release-please keeps it current

- [ ] 2.1 `release-please-config.json`: add `docs/site/start/install-the-operator.md` to the root package's `extra-files`, after `internal/version/version.go`.
- [ ] 2.2 `internal/version/version_test.go`: `TestInstallPageNamesThisRelease` per design.md D3. Verify: it passes; it fails naming the version when a block's version is changed locally to `1.0.0-beta.4`, and naming the page when a start marker is removed.
- [ ] 2.3 `AGENTS.md`, beside the release-please and version-identity rules: operator versions on `docs/site/` pages sit inside `x-release-please-start-version` blocks (outside code fences) and move with each Release PR; a page names no core, catalog or cli version, it links where to find the current one.
- [ ] 2.4 `openspec validate keep-install-page-current --strict` passes; `task dev:fmt dev:vet dev:lint dev:test` green, then commit `ci(release): rewrite the install page's operator version in the Release PR`.
- [ ] 2.5 `openspec archive keep-install-page-current --yes` (the archive rides this PR). Verify: `openspec validate --specs --strict` passes for `release-automation`. Commit `docs(openspec): archive keep-install-page-current`.
