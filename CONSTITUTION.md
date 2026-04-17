# Open Platform Model Controller Constitution

## Purpose

This document is the reader-friendly reference for the principles that shape controller design, implementation, validation, and change management. The controller is governed by the normative constitutional source in `openspec/config.yaml`.

## Design Principles

| # | Principle | Summary |
| ---- | --------- | ------- |
| **I** | [Flux for Transport, OPM for Semantics](#i-flux-for-transport-opm-for-semantics) | Flux handles source transport and provenance; OPM owns semantic evaluation |
| **II** | [Separation of Concerns](#ii-separation-of-concerns) | Reconcile, source, render, apply, and inventory responsibilities stay clearly split |
| **III** | [Authoritative Inventory](#iii-authoritative-inventory) | `status.inventory` is the source of truth for ownership and prune decisions |
| **IV** | [Status as the Operational Ledger](#iv-status-as-the-operational-ledger) | Status records reconcile outcomes, digests, and bounded history |
| **V** | [Declarative Intent via Server-Side Apply](#v-declarative-intent-via-server-side-apply) | The controller declares desired state and relies on SSA for mutation |
| **VI** | [Semantic Versioning and Commit Discipline](#vi-semantic-versioning-and-commit-discipline) | Releases use SemVer and commits follow Conventional Commits |
| **VII** | [Simplicity & YAGNI](#vii-simplicity--yagni) | Complexity must be justified; start with the simplest design that works |
| **VIII** | [Small Batch Sizes](#viii-small-batch-sizes) | Changes must stay tiny, incremental, and independently verifiable |

---

### I. CUE-Native Module Resolution

> **Note:** This principle was updated from "Flux for Transport, OPM for Semantics".
> CUE module delivery now uses CUE's native OCI module resolution instead of Flux
> source-controller. See change `cue-native-module-release` for context.

For CUE module acquisition, the controller MUST use CUE's native module system to resolve modules from OCI registries. The controller synthesizes a `#ModuleRelease` CUE package at reconcile time that imports the target module via standard CUE import paths. CUE handles OCI resolution, dependency resolution, and caching.

- CUE's module system handles OCI transport and dependency resolution
- The controller synthesizes release packages, not custom transport
- OPM owns semantic evaluation and rendering
- The `internal/source/` package is retained for potential future use
- BundleRelease continues to use Flux source-controller (separate concern)

---

### II. Separation of Concerns

The codebase MUST preserve clear package boundaries:

- `internal/controller/` handles reconcile orchestration, watches, and event handling
- `internal/synthesis/` handles CUE release package generation for module resolution
- `internal/source/` handles Flux artifact interaction (retained, used by BundleRelease)
- `internal/render/` handles CUE evaluation and object generation
- `internal/apply/` handles server-side apply and prune behavior
- `internal/inventory/` handles ownership and previously-applied resource tracking

Domain logic should live in focused internal packages rather than accumulating in reconcilers. Clear boundaries keep code easier to test, reason about, and evolve.

```text
controller -> source -> render -> apply -> inventory -> status
     |            |         |         |            |
 orchestration  fetch    evaluate   mutate     track ownership
```

---

### III. Authoritative Inventory

Resource ownership MUST be tracked explicitly in `status.inventory`.

- Prune decisions MUST be based on inventory, not labels alone
- Labels such as `app.kubernetes.io/managed-by` are helpful hints, not the source of truth
- Apply MUST succeed before prune is attempted
- Ownership data should explain what the controller believes it manages

This keeps pruning explicit, safe, and explainable across retries and upgrades.

---

### IV. Status as the Operational Ledger

CRD `status` is the operational source of truth for reconcile progress.

- Reconcile outcomes belong in `status`
- Source, config, and render digests belong in `status`
- Reconcile history SHOULD stay bounded and compact
- Status SHOULD explain what was attempted, what succeeded, and what stalled

Status is the durable ledger for controller behavior. It should allow a reader to understand the last meaningful reconcile result without reconstructing the entire event stream from logs.

---

### V. Declarative Intent via Server-Side Apply

All managed object mutation MUST use Server-Side Apply.

- The controller declares desired state
- The Kubernetes API server resolves field ownership and conflicts
- Transient API failures SHOULD requeue
- Permanent semantic failures SHOULD surface as stalled status rather than endless retries

This keeps reconciliation declarative, retry-safe, and aligned with Kubernetes ownership semantics.

---

### VI. Semantic Versioning and Commit Discipline

Controller releases MUST follow SemVer 2.0.0. Commits SHOULD follow Conventional Commits v1 in the form `type(scope): description`.

Recommended commit types:

- `feat`
- `fix`
- `refactor`
- `docs`
- `test`
- `chore`

Recommended scopes include `api`, `controller`, `source`, `render`, `apply`, and `inventory`.

Versioning and commit structure are how the repository communicates compatibility, change risk, and implementation intent.

---

### VII. Simplicity & YAGNI

Start with the simplest implementation that satisfies the current requirement. New complexity MUST be justified by a concrete need.

- Prefer direct solutions over broad abstraction layers
- Prefer explicit flow over hidden magic
- Defer rollback machinery until real requirements justify it
- Defer cross-release dependency graphs such as `dependsOn` until basic execution is proven

If complexity is introduced, it should solve a real controller problem, not a speculative future one.

---

### VIII. Small Batch Sizes

All work MUST be delivered in tiny, independently verifiable steps.

- A request spanning multiple major concerns SHOULD be split before implementation
- A single commit SHOULD ideally address one concern
- Large features SHOULD be delivered as sequential increments
- Small changes keep validation, review, and rollback simpler

This principle applies to both planning and implementation. Large bundled changes hide risk, slow review, and weaken validation.

#### Execution Gate

Before beginning implementation, an agent MUST evaluate whether the requested change is small enough for a safe iteration.

If the request is too large, the required response is:

> "🛑 **Scope Warning**: This request is too large for a single safe iteration. I suggest we split it into the following smaller steps: [list 2-3 logical, tiny steps]. Should we start with step 1?"

---

## Technology Standards

For detailed repository mechanics, see `AGENTS.md`.

- Framework: `controller-runtime` with `kubebuilder`
- GitOps toolkit: Flux packages, especially `github.com/fluxcd/pkg`
- Reconcile flow: source -> render -> apply -> prune -> status

## Code Style Expectations

The controller code SHOULD follow these defaults:

- Accept interfaces where useful, return concrete structs when practical
- Propagate `context.Context` through async and API-facing operations
- Wrap errors with context, for example `fmt.Errorf("fetching artifact: %w", err)`
- Prefer concrete types over `map[string]any`
- Do not hand-edit generated files such as `api/v1alpha1/zz_generated.deepcopy.go` or `config/crd/bases/*`

### Logging

- Use structured logging through controller-runtime logging helpers
- Capitalize log messages
- Do not end log messages with periods
- Include identifying keys when useful, such as name and namespace

### Imports

Keep imports in this order:

1. standard library
2. external dependencies, including Flux and Kubernetes packages
3. local module imports

Let `gofmt` and `goimports` control formatting and grouping.

## Quality Gates

Before merge, the following checks SHOULD pass:

1. `make manifests generate`
2. `make fmt vet`
3. `make lint`
4. `make test`

When API types or markers change, `make manifests generate` is mandatory.

---

## OpenSpec Artifact Rules

These principles also shape how OpenSpec artifacts should be written.

### Proposal

- Focus on WHY the change is needed and WHAT is in or out of scope
- Update the proposal when scope changes, intent clarifies, or the approach fundamentally shifts
- Identify affected API types and controllers
- State whether the change is MAJOR, MINOR, or PATCH under SemVer
- Any added complexity MUST include explicit justification
- Scope MUST remain small enough for a short implementation session

### Design

- Focus on HOW the change will be implemented
- Update the design when implementation reveals a better approach or constraints change
- Use RFC 2119 language: MUST, SHALL, SHOULD, MAY
- Include a `Research & Decisions` section whenever exploration was required
- Include Go pseudocode or Kubernetes manifest examples where they clarify intent
- Explain reconcile phase impact across Source, Render, Apply, Prune, and Status

Recommended `Research & Decisions` shape:

```md
## Research & Decisions

### [Topic]
**Context**: [Why this decision was needed]
**Explored**: [What was investigated]
**Decision**: [Chosen option]
**Rationale**: [Why this option was selected]
```

### Specs

- Focus on WHAT behavior changes, not HOW it is implemented
- Update specs when requirements change or new observable behavior is introduced
- Use RFC 2119 language: MUST, SHALL, SHOULD, MAY
- Describe observable behavior such as status changes, rendered resources, and logs
- Use `ADDED`, `MODIFIED`, and `REMOVED` sections for deltas
- Include scenarios such as transient error versus stalled reconcile behavior

### Tasks

- Focus on implementation steps
- Update tasks as work completes, blockers appear, or new work is discovered
- Break tasks into tiny chunks, ideally no more than 1-2 hours each
- If the list grows beyond roughly 10 items or spans multiple features, split it into another OpenSpec change
- Group tasks by component such as API, internal packages, and controller
- Include validation gates as final tasks, especially `make fmt vet lint test`

---

## How Principles Work Together

These principles reinforce each other:

- CUE-native resolution keeps module loading simple and composable
- Separation of concerns keeps reconcile flow understandable and testable
- Inventory and status make operations explicit and auditable
- SSA keeps mutation declarative and retry-safe
- Small batch sizes keep change quality high and validation practical

When principles appear to conflict, treat that as a design smell and document the trade-off explicitly.

## Further Reading

- `openspec/config.yaml` — normative constitutional source
- `AGENTS.md` — repository mechanics, commands, and coding guidance
