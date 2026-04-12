# poc-controller repository guide

## Purpose

- Kubebuilder-based K8s controller, Go.
- Defines/reconciles `ModuleRelease` + `BundleRelease` CRDs in `api/v1alpha1`.
- Preserve controller-runtime patterns, Kubebuilder markers, generated-file boundaries.

## Repository Rules

- Repo-specific agent guidance in `CLAUDE.md` + `CONSTITUTION.md`.

## Entrypoint

- Read these docs first, in order:
- `CLAUDE.md`: repo commands, workflows, style, verification.
- `CONSTITUTION.md`: root engineering principles, change-shaping rules.
- `openspec/config.yaml`: normative OpenSpec constitutional source.
- `Makefile`: authoritative build/generate/lint/test entrypoints.
- `docs/STYLE.md`: documentation prose style rules.

## Repository Layout

```
.
├── adr/            # Architecture Decision Records
├── api/            # CRD schemas (`v1alpha1/`), validation markers, generated DeepCopy (no hand-edit)
├── cmd/            # manager entrypoint (`main.go`), controller registration
├── config/         # Kustomize overlays: generated CRDs + RBAC (no hand-edit), samples, manager, network-policy, prometheus
├── docs/           # design documents
├── enhancements/   # enhancement proposals
├── experiments/    # exploratory prototypes
├── hack/           # helper scripts
├── internal/       # domain packages: apply, controller, inventory, reconcile, render, source, status
├── openspec/       # OpenSpec config + change specs
├── scripts/        # build/dev helper scripts
├── test/           # Kind-backed e2e tests (`e2e` build tag) + test utilities
├── Makefile        # source of truth for generation/build/lint/test/deploy
└── Taskfile.yml    # convenience wrappers delegating to `make`
```

## Architecture Decision Records

ADRs capture significant technical decisions w/ context + consequences.

- Location: `adr/`
- Template: `adr/TEMPLATE.md`
- Naming: `NNN-kebab-case-title.md` (three-digit, zero-padded)

### Creating a new ADR

1. Copy `adr/TEMPLATE.md` → `adr/NNN-title.md`, next available number.
2. Status → `Proposed`.
3. Fill Context, Decision, Consequences.
4. Status → `Accepted` once agreed.

### Updating an ADR

- Never delete ADR — update status.
- Retire: status → `Deprecated`.
- Replace: status → `Superseded by ADR-NNN`, create new ADR.
- One decision per ADR.

## Generated Files And Scaffold Boundaries

- No hand-edit `api/v1alpha1/zz_generated.deepcopy.go`.
- No hand-edit `config/crd/bases/*.yaml` or `config/rbac/role.yaml`.
- No hand-edit `PROJECT`.
- Preserve `// +kubebuilder:scaffold:*` comments + license headers.
- API markers/schema/`*_types.go` changed → run `make manifests generate`.

## Build And Dev Commands

### Core Commands

- `make help`: list targets.
- `make manifests`: regen CRDs, RBAC, webhook manifests w/ `controller-gen`.
- `make generate`: regen DeepCopy methods.
- `make fmt`: `go fmt ./...`.
- `make vet`: `go vet ./...`.
- `make lint-config`: verify golangci-lint config.
- `make lint`: run golangci-lint.
- `make lint-fix`: golangci-lint w/ auto-fixes.
- `make build`: generation + fmt + vet + build `bin/manager`.
- `make run`: run controller locally against current kubeconfig.
- `make test`: non-e2e tests w/ envtest, writes `cover.out`.
- `make setup-test-e2e`: create Kind cluster if missing.
- `make test-e2e`: e2e tests against Kind, then cleanup.
- `make build-installer`: render `dist/install.yaml` from `config/default`.
- `make docker-build IMG=<image>` / `make docker-push IMG=<image>`: build/publish images.
- `make deploy IMG=<image>` / `make undeploy`: install/remove controller from cluster.

### Single Test Commands

- No `make test-one` target; use `go test` directly.
- Package-level: `go test ./internal/controller`.
- Single entrypoint: `go test ./internal/controller -run TestControllers`.
- Ensure envtest binaries: `make setup-envtest`.
- Reuse envtest binaries:
  `KUBEBUILDER_ASSETS="$(./bin/setup-envtest use 1.35.0 --bin-dir ./bin -p path)" go test ./internal/controller -run TestControllers`.
- Focus Ginkgo suite:
  `KUBEBUILDER_ASSETS="$(./bin/setup-envtest use 1.35.0 --bin-dir ./bin -p path)" go test ./internal/controller -run TestControllers -ginkgo.focus="BundleRelease Controller"`.
- Focus single spec:
  `KUBEBUILDER_ASSETS="$(./bin/setup-envtest use 1.35.0 --bin-dir ./bin -p path)" go test ./internal/controller -run TestControllers -ginkgo.focus="should successfully reconcile the resource"`.
- E2E only: `go test -tags=e2e ./test/e2e -v -ginkgo.v`.
- Single e2e:
  `KIND_CLUSTER=poc-controller-test-e2e go test -tags=e2e ./test/e2e -run TestE2E -ginkgo.focus="should run successfully" -v -ginkgo.v`.
- `make test` excludes `/test/e2e`; no Kind tests in default unit path.

## Working Style for Agents

- `api/v1alpha1` edits → `make manifests generate`.
- Go changes `cmd/`/`internal/` → `make fmt vet test` minimum.
- Non-trivial changes → `make lint` or `make lint-fix` before finishing.
- Manifests/RBAC changed → consider `make build-installer` for alignment.
- `task <name>` = shorthand only; `make` = authoritative.

## Go Version And Tooling

- Go `1.25.3`.
- controller-runtime, Kubebuilder APIs, Flux source types, Ginkgo v2, Gomega.
- `golangci-lint` v2 w/ `gofmt` + `goimports` formatters.
- Custom `logcheck` plugin from `.custom-gcl.yml`, enforces K8s logging conventions.
- Envtest binaries installed to `./bin` via Makefile.

## Formatting And Imports

- `gofmt`/`goimports` own layout, spacing, import grouping.
- Standard Go formatting w/ tabs; no manual vertical alignment.
- Import groups: stdlib → third-party → local module.
- Preserve blank lines between import groups.
- Aliases only for clarity or convention: `ctrl`, `logf`, `metav1`, versioned API aliases.
- No unused helpers, no speculative abstractions.

## Naming And API Design

- Exported: `PascalCase`; unexported: `camelCase`.
- Short receiver names; reconcilers use `r`.
- Package names: lowercase, concise.
- Follow K8s patterns: `Spec`, `Status`, `Conditions`, `ObservedGeneration`.
- JSON fields: explicit lowerCamelCase.
- Concrete structs over `map[string]any` in APIs/reconciliation.
- Reuse K8s/Flux reference types where repo already does.
- Status conditions: `[]metav1.Condition`, no custom condition structs.
- API timestamps/durations: K8s types (`metav1.Time`, `metav1.Duration`).
- Maintain `omitempty`/`omitzero` tags consistent w/ existing style.

## Controller And Reconcile Style

- Reconciliation: idempotent, safe to retry.
- Prefer controller-runtime helpers over ad hoc K8s client logic.
- Fetch fresh objects before mutating concurrently-changed state.
- Watches via builder methods: `.For(...)`, `.Owns(...)`.
- `Reconcile` readable; complex logic → `internal/*` packages.
- Explicit `ctrl.Result{}` in non-trivial branches.
- RBAC markers accurate when reconciler touches new resources.

## Error Handling And Logging

- Wrap errors w/ context: `%w`, e.g. `fmt.Errorf("failed to render bundle: %w", err)`.
- Error messages lowercase unless proper noun/identifier.
- No silent error swallowing; return or log best-effort failures clearly.
- Sentinel errors only when callers branch on them.
- Structured controller-runtime logging, balanced key/value pairs.
- K8s log style: capitalized message, no trailing period, meaningful action wording.
- Include identifying keys (name, namespace) in reconciliation logs.

## Testing Style

- Ginkgo v2 + Gomega.
- Envtest controller tests in `internal/controller`, load CRDs from `config/crd/bases`.
- E2E needs Kind; may install CertManager unless `CERT_MANAGER_INSTALL_SKIP=true`.
- Descriptive `Describe`/`Context`/`It` text, `-ginkgo.focus`-friendly.
- `Eventually` for async K8s behavior, no sleeps.
- `Expect(err).NotTo(HaveOccurred())` or `Expect(...).To(Succeed())`.
- Package-local helpers for repeated assertions; keep setup readable.

## Lint Expectations

- Enabled: `errcheck`, `ginkgolinter`, `gocyclo`, `govet`, `misspell`, `modernize`, `revive`, `staticcheck`, `unused`, others.
- `gofmt`/`goimports` enforced via golangci-lint.
- `logcheck` validates structured logging + balanced key/value params.
- `lll`/`dupl` relaxed in `api/*`/`internal/*`; don't rely on exclusions unnecessarily.
- Idiomatic Ginkgo code; `ginkgolinter` flags non-idiomatic patterns.

## Verification Checklist For Agents

- `make manifests generate` after API/marker changes.
- `make fmt vet test` after meaningful Go changes.
- `make lint` or `make lint-fix` for non-trivial edits.
- No manual edits to generated files/scaffold markers.
- Note if e2e skipped due to missing Kind/cluster.
