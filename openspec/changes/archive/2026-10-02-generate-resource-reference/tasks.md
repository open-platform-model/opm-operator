# Tasks: generate-resource-reference

One PR, title `docs(site): generate the operator resource reference`. The archive (section 4) rides the same PR. Design.md "Research & Decisions" records the sample and summary findings; no assumption is unverified, so there is no spike section.

## 1. Kind doc comments (api/v1alpha1, CRDs, dist)

- [x] 1.1 Open the doc comments of `ModuleInstance`, `ModulePackage`, `Platform` and `TransformerRegistration` with a sentence saying what the kind does, from facts the existing comments state (design.md, "Kind summaries"); move the rest to following paragraphs.
- [x] 1.2 Drop the kubebuilder scaffold condition list from `ModuleInstanceStatus.Conditions`; write the `RequiredContracts` citation as `0015:D3/D16`.
- [x] 1.3 `task dev:manifests dev:generate` and `task operator:installer`; the CRD and `dist/install.yaml` diffs are descriptions only.
- [x] 1.4 `task dev:fmt dev:vet dev:lint dev:test` green, then commit `docs(api): open each kind's doc comment with what it does`.

## 2. Generator, sample proof and the page (hack/crdref, test/integration, docs/site, .tasks, AGENTS.md)

- [x] 2.1 `hack/crdref` per design.md D1 to D4, with unit tests for the summary split, escaping and citation links, the splice, the type names, determinism and a current page.
- [x] 2.2 `test/integration/crdvalidation/samples_test.go` per design.md D5. Verify: it passes on the tree, and fails with an unknown-field error when `spec.bogusField: 1` is added to the Platform sample (revert afterwards).
- [x] 2.3 Replace the page's planning comment with its one authored sentence and the two marker lines; keep the front matter and the "See also" section.
- [x] 2.4 `task dev:docs:reference` and `task dev:docs:reference:check` in `.tasks/dev.yaml`; run the first and commit its output.
- [x] 2.5 `AGENTS.md`: the two tasks, the marker rule, and that types, samples or controller registration edits regenerate the page.
- [x] 2.6 Site proof from the workspace root: `OPM_VERSIONS=v1.0=/src OPM_SRC_OPM_OPERATOR=<worktree> task -d opmodel.dev lint:sources` and `... task -d opmodel.dev build` pass; read `site/public/v1.0/docs/reference/operator-resources/index.html` (escaped `<namespace>`, `<key>` and `#Platform` render as text, the decision links resolve).
- [x] 2.7 `task dev:fmt dev:vet dev:lint dev:test` green and `openspec validate generate-resource-reference --strict` passes, then commit `docs(site): generate the operator resource reference`.

## 3. Check in CI (lint.yml)

- [x] 3.1 `.github/workflows/lint.yml`: a step "Generated resource reference is current" after "Run linter", `run: task dev:docs:reference:check`. Verify: `go run github.com/rhysd/actionlint/cmd/actionlint@latest .github/workflows/lint.yml` is clean; `task dev:docs:reference:check` fails after a description edit to a CRD and passes again after `task dev:docs:reference`.
- [x] 3.2 `task dev:fmt dev:vet dev:lint dev:test` green, then commit `ci(lint): fail on a stale operator resource reference`.

## 4. Archive

- [x] 4.1 `openspec archive generate-resource-reference --yes`; replace the placeholder `## Purpose` of the new `openspec/specs/operator-resource-reference/spec.md` with one sentence. Verify: `openspec validate operator-resource-reference --type spec --strict --no-interactive` passes, and `openspec validate --all --strict --no-interactive` fails no spec that passed before.
- [x] 4.2 Commit `chore(openspec): archive generate-resource-reference`.
