## 1. docs-kit v0.7.0

- [x] 1.1 `.opm-docs-version` and every `open-platform-model/docs-kit/.github/workflows/publish.yml@` ref under `.github/workflows/` name `v0.7.0` (design.md D4). Verify: `task docs:pins:check` passes.
- [x] 1.2 Remove `.bin/opm-docs`, then `task docs:bundle:check` green on the unchanged page layout, then commit `ci(docs): move docs-kit to v0.7.0`.

## 2. The reference becomes a section

- [ ] 2.1 `docs-kit.cue`: `owns: ["reference/operator/"]`; the `crd` source takes `section: "reference/operator/"` (no `page`), `title: "Operator Reference"`, description "One generated page per operator resource kind: ModuleInstance, ModulePackage, Platform and TransformerRegistration.", the rest unchanged; comments name the new path.
- [ ] 2.2 `git mv docs/site/reference/operator-resources.md docs/site/reference/operator/_index.md`; front matter per design.md D2; intro for a section of one page per kind, no `## Kinds`.
- [ ] 2.3 Every link to `/docs/reference/operator-resources/` under `docs/site/` per design.md D3; title mentions "Operator resources" in page comments and link text become "Operator Reference"; `AGENTS.md` and `.tasks/` comments name the new path. Verify: `grep -rn 'operator-resources\|Operator resources' docs/site AGENTS.md docs-kit.cue .tasks Taskfile.yml .github` finds nothing.
- [ ] 2.4 `task docs:bundle`. Verify: `out/opm-operator/content/reference/operator/` holds `_index.md` (title "Operator Reference", the authored intro, then `## Kinds`), `moduleinstance.md`, `modulepackage.md`, `platform.md`, `transformerregistration.md`; `reference/operator-resources.md` is gone.
- [ ] 2.5 `task dev:fmt dev:vet dev:lint dev:test` and `task docs:bundle:check` green, then commit `docs(reference): publish the operator reference as a section`.

## 3. Specs

- [ ] 3.1 `openspec validate publish-operator-reference-section --strict` passes; `openspec archive publish-operator-reference-section --yes`; the `operator-resource-reference` Purpose names the new URL. Verify: `openspec validate --specs --strict` passes for `operator-resource-reference`, `docs-bundle` and `deps-cascade`. Commit `docs(openspec): archive publish-operator-reference-section`.
