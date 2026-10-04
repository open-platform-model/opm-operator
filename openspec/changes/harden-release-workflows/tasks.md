Every section runs `actionlint` on the workflows it changes, `task cascade:wiring:check`, and the repo gates `task dev:fmt dev:vet dev:lint`. No Go code changes, so `task dev:test` runs once, in section 5. Every commit is `ci` or `docs` typed, so no section cuts an operator release.

## 1. Proposal

- [x] 1.1 Write proposal.md, design.md, the `workflow-hardening` and `container-image-publish` deltas and this file.
- [x] 1.2 `openspec validate harden-release-workflows --strict`, then commit `docs(openspec): propose harden-release-workflows`

## 2. Release key Environment and explicit permissions

- [x] 2.1 `release.yml`: workflow-level `permissions: {}`; `release-please` gets `environment: release` and `permissions: {}` (design D1, D2).
- [x] 2.2 `lint.yml`, `test.yml`, `test-e2e.yml`: top-level `permissions: contents: read` (the e2e job keeps its own grant until section 4).
- [x] 2.3 Every file under `.github/workflows/` has a top-level `permissions:` of `{}` or `contents: read`. Only `release-please` declares `release`, and only it reads `RELEASE_APP_PRIVATE_KEY`.
- [x] 2.4 Gates green, then commit `ci: declare least-privilege permissions and the release environment`

## 3. No Actions cache in publishing jobs

- [x] 3.1 `release.yml` `image-release`: drop `cache-from`/`cache-to`; `publish-examples`: setup-go `cache: false` (design D3).
- [x] 3.2 `publish-fixtures.yml`: setup-go `cache: false`; `image-pr.yml`: drop `cache-from`/`cache-to`.
- [x] 3.3 `grep -rn 'type=gha\|actions/cache' .github/workflows` finds nothing.
- [x] 3.4 Gates green, then commit `ci: build and publish without the Actions cache`

## 4. Bot heads hold no write token

- [ ] 4.1 `image-pr.yml`: skip `deps/cascade` and `release-please--*` heads (design D5).
- [ ] 4.2 `test-e2e.yml`: split into `publish-fixtures` (`packages: write`, trusted events only, setup-go `cache: false`) and `test-e2e` (`contents: read`, `packages: read`, pins and credentials only after a successful publish) (design D4).
- [ ] 4.3 Gates green, then commit `ci: keep write tokens away from cascade and release heads`

## 5. Code owners and docs

- [ ] 5.1 Add `.github/CODEOWNERS` (design D6).
- [ ] 5.2 `AGENTS.md`: record the release Environment, explicit permissions, the no-cache rule, the bot-head rule and the e2e split.
- [ ] 5.3 `task dev:test`, gates green, `openspec validate harden-release-workflows --strict`, then commit `ci: add code owners for the release machinery`

## 6. Verify and archive

- [ ] 6.1 Run openspec verify for `harden-release-workflows` and resolve its findings.
- [ ] 6.2 Archive on this branch; check `openspec/specs/workflow-hardening/spec.md` has a real Purpose and `openspec validate workflow-hardening --strict` passes. Commit `chore(openspec): archive harden-release-workflows`
