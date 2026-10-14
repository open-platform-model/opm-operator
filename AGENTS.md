# opm-operator repository guide

The OPM Kubernetes controller, Go, built with Kubebuilder. It reconciles the CRDs in `api/v1alpha1` (`ModuleInstance`, `ModulePackage`, `Platform`, `TransformerRegistration`) by calling the library Kernel to render and then applying with server-side apply. It also ships its own OPM module, `modules/opm_operator`, as a second release unit. Preserve controller-runtime patterns, Kubebuilder markers and generated-file boundaries.

## Branch model

| Branch | Purpose |
| --- | --- |
| `main` | All work. It lands by PR. release-please cuts releases from it (operator `1.0.0-beta.N`, module `0.x`). |

Authority, in order: this file, `CONSTITUTION.md`, `openspec/config.yaml` (the normative OpenSpec source). `Taskfile.yml` and `.tasks/` win on how a command runs.

## Repository rules

- **Do not hand-edit generated files.** They are `zz_generated*` in `api/v1alpha1/` and `modules/opm_operator/`, everything in `config/crd/bases/` and `config/rbac/`, and `PROJECT`. `task dev:manifests` writes the module's CUE data from `config/`, and `task operator-module:drift` fails in CI when that data or `config/` is stale.
- **Keep `// +kubebuilder:scaffold:*` comments and license headers.**
- **Type doc comments are public text.** They become the CRD descriptions and the resource reference on the site, so write them for a site reader.
- **A PR that changes `modules/opm_operator` changes nothing else.** Otherwise its squash commit releases both units. The one forced exception is a CRD or RBAC change, which regenerates the module's `zz_generated_*` in the same PR.
- **Commit type decides the release.** `docs`, `chore`, `test`, `ci` and `build` release nothing. A pin bump in `config/samples`, `hack`, `test/fixtures` or `internal/source/testdata` is `test(fixtures)`. Tooling under `hack/operator-module/` is `ci` or `build`, never `fix` or `feat`.
- **A breaking change is `feat!` in the PR title** (beta rule), and it never moves the module path.
- **Never hold a lock while calling the Kernel.** The process has one library Kernel and no gate. Every kernel verb builds in its own context and is safe to call concurrently (library ADR-007). Concurrency is bounded by one render slot pool (`--max-concurrent-renders`, default 1) and by the Platform reconciler building one generation at a time. `--render-timeout` (default 10m) lets a render outlive its reconcile: the slot pool returns a timeout error while the render keeps its slot. A caller copies its inputs before the call, never reads what the render writes after that error, and lets the render own the files it reads. Detail: the rendering note in `docs/`.
- **`reconciledBy` in `docs-kit.cue` names the controller of each kind.** A renamed controller or a new kind needs its entry. `task dev:test` fails until it has one.
- **The opm CLI pin lives only in `.opm-cli-version`.** No workflow carries a version literal, so the release cascade moves it without editing workflows.
- **Run `task deps:cascade` as `task -x deps:cascade`.** Without `-x`, go-task turns exit 3 (nothing to do) into 201.
- **A fixture never has an `opmodel.dev/*` path.** CUE routes by longest prefix. Fixtures live under `testing.opmodel.dev`.
- **Registry precedence on the binary:** `--registry`, then `OPM_REGISTRY`, then the built-in default. `CUE_REGISTRY` as pod env is ignored.
- **Add new automation as a task in `.tasks/`, not in the `Makefile`.** `Taskfile.yml` is the source of truth. The `Makefile` is the Kubebuilder scaffold and still serves some local flows.
- **Enhancement references in comments:** default is none; the form is in the workspace enhancements reference. Repo extras: never in scaffold templates, generated files, fixtures a user copies or CLI output strings; in a CUE file the reference goes in a `// WHY` block, one blank line below the doc comment, because `cue lsp` hover and `cue def` replay a doc comment verbatim; no `Was:` rename history. `task cite-lint` checks this.

## Layout

```text
adr/            Architecture Decision Records
api/            CRD types (v1alpha1), validation markers, generated DeepCopy
cmd/            manager entrypoint and controller registration
config/         Kustomize: generated CRDs and RBAC, samples, manager, network-policy, prometheus
docs/           rendering note, design/ notes, site/ pages the docs bundle ships
hack/           helper scripts and operator-module/ tooling
internal/       apply, controller, inventory, metrics, moduleacquire, platform, reconcile, render, shrink, source, status, version
modules/        opm_operator/, the operator's own OPM module (its own release unit)
openspec/       OpenSpec config, specs, changes
references/     detail files that CONTEXT.md routes to
test/           e2e (build tag e2e), integration, fixtures, rbac, collector, utils
.tasks/         Taskfile includes (dev, operator, operator-module, docker, kind, registry, flux, release, module, examples, deps, cascade, tools)
.github/        workflows, scripts, golangci-lint schema, CODEOWNERS
```

## Commands

| Command | What it does | When |
| --- | --- | --- |
| `task` | List tasks | Orientation |
| `task dev:manifests` | CRDs, RBAC and webhook manifests with controller-gen, then the operator module's data (needs `cue` at the `CUE_VERSION` that CI sets) | After API markers, type files or an RBAC marker changes |
| `task dev:generate` | DeepCopy methods | Same |
| `task dev:fmt dev:vet dev:test` | Format, vet, unit and integration tests with envtest | Minimum after a Go change in `cmd/` or `internal/` |
| `task dev:lint` / `dev:lint:fix` | golangci-lint | Before you finish a non-trivial change |
| `task dev:lint:config` | Check the lint config, schema and version agree | After a linter version move |
| `task docs:bundle:check` | Pins check, then build and lint the docs bundle | After `docs/site/`, `api/v1alpha1`, `config/samples`, `docs-kit.cue` or a docs-kit pin |
| `task docs:bundle` | Build the bundle into `out/opm-operator/` | Preview |
| `task cascade:wiring:check` | Offline check of the cascade workflows | After any edit under `.github/workflows/` |
| `task cite-lint` | Comment rule on Go and CUE files | Before a PR that adds comments |
| `task operator:binary` / `operator:run` | Build `bin/manager` / run against the current kubeconfig | Local run |
| `task operator:installer` | Render the install manifest into `dist/` from `config/default` | After manifest or RBAC changes |
| `task dev:e2e` | e2e on Kind, then cleanup | Needs Kind; say so when skipped |
| `task operator-module:drift` | Fail on stale `config/` or module data; `REF=<tag>` checks the data against that tag's `config/` | After a CRD or RBAC change |
| `task operator-module:generate` | Regenerate the module's CRD and RBAC data from `config/` (`dev:manifests` runs it) | After a CRD or RBAC change |
| `task operator-module:release-check VERSION=<X.Y.Z>` | The module's release gate. Needs `GH_REPO`, `gh`, `docker buildx` and the operator tags | On the module's release PR |
| `task operator-module:release-check:test` / `operator-module:publish:test` | Offline tests of the release gate / the module publish script | After editing those scripts |
| `task kind:setup` | Create the Kind cluster if missing | Before e2e by hand |
| `task docker:build IMG=<image>` / `docker:push IMG=<image>` | Build / publish the manager image | Image work |
| `task operator:controller:install IMG=<image>` / `operator:controller:uninstall` | Install / remove the controller on the current cluster | Manual cluster test |
| `task examples:pin` / `examples:check` | Re-pin fixture consumers / run the publish gates | After a fixture bump |
| `task -x deps:cascade` | Move upstream pins in the working tree | Cascade PRs |

Go version: `go.mod`. Dependencies: controller-runtime, Kubebuilder APIs, Flux source types, Ginkgo v2, Gomega. Envtest binaries install into `./bin` via `.tasks/`. `task dev:lint*` needs the network.

## Style

- `gofmt` and `goimports` own layout and import grouping (stdlib, third-party, local). Aliases only for clarity or convention: `ctrl`, `logf`, `metav1`, versioned API aliases.
- Exported `PascalCase`, unexported `camelCase`, short receivers (`r` for reconcilers), lowercase package names.
- Follow Kubernetes API patterns: `Spec`, `Status`, `Conditions`, `ObservedGeneration`. Conditions are `[]metav1.Condition`. Use `metav1.Time` and `metav1.Duration`. JSON fields are explicit lowerCamelCase. Prefer concrete structs over `map[string]any`. Keep `omitempty` and `omitzero` consistent.
- Reconcile is idempotent and safe to retry. Prefer controller-runtime helpers. Fetch fresh objects before mutating changed state. Watch with `.For(...)` and `.Owns(...)`. Move complex logic to `internal/*`. Return explicit `ctrl.Result{}` in non-trivial branches. Keep RBAC markers accurate.
- Wrap errors with `%w` and lowercase messages. No silent swallowing. Sentinel errors only when callers branch on them.
- Logs are structured with balanced key/value pairs, a capitalized message, no trailing period, and identifying keys (name, namespace).

@CONTEXT.md
