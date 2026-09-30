## Why

A platform that enables two majors of one catalog sharing contract keys (for example `opmodel.dev/catalogs/opm@v4` and `opmodel.dev/catalogs/opm@v5`, both listing `.../resources/container@v1`) fails to evaluate on every core up to `2.0.0-alpha.12`: core's `#Platform.#contracts` fold keys `defined` and `definedBy` by contract FQN, so two enabled definers conflict. The operator reports that today as `BuildFailed` with a CUE conflict on `#contracts.definedBy`, which names neither the cause nor the fix.

The five-change set this belongs to (`orchestration.md`) turns that failure into a report. Core (change A, `fold-colliding-contract-keys`) folds only keys with exactly one enabled definer, reports the rest as `collisions` and `collidingEntries`, and reads `routable` false while any exist. The library (change B, `refuse-colliding-contracts`) decodes both as `ContractInventory.Collisions` and `CollidingEntries`, refuses such a platform at render with a typed cause, and moves `DefaultSchemaModule` to A's core, so the platform module this operator generates pins it. This change is C: the Platform reconciler's generation gate names the collision.

Without it, the gate still refuses a colliding platform (`Routable` is false), but it words the refusal wrongly: `inventoryRefusal` (`internal/controller/platform_inventory.go:64-80`) prints the over-subscription finding whenever `Routable` is false, so a collision-only inventory reports reason `OverSubscribedContracts` with "platform is not routable: 0 over-subscribed contracts" and no row. An admin is told to disable a competing provider that does not exist.

## What Changes

- **A new Ready reason, `ContractCollisions`** (`Ready=False`, Stalled): the built platform's inventory reports contract keys that more than one enabled registry entry defines. The message names each colliding key and the registry entries (path with major) defining it, read from `CollidingEntries` and never counted by the operator, and says a package cannot be generated until all but one of those entries is disabled.
- **The routing refusal words only what the inventory reports.** The over-subscription finding is printed only when `OverSubscribed` is non-empty. A collision is reported whenever `Collisions` is non-empty. An inventory that reads `Routable: false` with neither list populated still refuses (fail closed) under `OverSubscribedContracts`, with a finding saying the inventory names no over-subscribed or colliding contract.
- **Precedence `ContractCollisions` > `OverSubscribedContracts` > `ComparablePredicates`.** A collision hides its keys from `definedBy`, `requiredBy` and `comparable`, so the other findings may be incomplete while one exists, and its fix (disable a major) comes first. Every finding stays in the one message, as today.
- **Library pin to change B.** `github.com/open-platform-model/library` moves to B's pushed head (a Go pseudo-version) for development, then to B's release in the last section. The generated platform module follows `schema.DefaultSchemaModule` to A's core with no operator constant involved.
- **Docs and API comment.** `PlatformStatus.Conditions` lists the new reason (regenerating the CRD description and `dist/install.yaml`), `internal/status/conditions.go` defines and documents it, and `docs/RENDERING.md` and `docs/site/diagnostics/operator-conditions.md` describe it and its precedence.

## Classification

**PATCH** (pre-GA, released as `fix`). One condition reason is added; no API type or field changes, and only a CRD description string moves. The behaviour change is confined to platforms that could not be built before: a colliding platform moves from `BuildFailed` (a CUE conflict) to `ContractCollisions` (a named verdict), and is still never recorded. A routable platform, and an over-subscribed or undiscriminated one without a collision, read exactly as today. Complexity (Principle VII): one reason constant and one wording function; the gate stays a pure function of the inventory, and the operator still counts nothing.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `platform-inventory-gate`: a new requirement refuses a platform whose enabled entries share contract keys with reason `ContractCollisions`, naming each key and its defining entries; the over-subscription requirement yields its reason to a collision and fails closed on an unexplained unroutable verdict; the discrimination requirement's joint-refusal rule names the new precedence; the re-evaluation requirement gains a recovery scenario.
- `platform-reconciler`: "Surface materialize outcome on status" lists `ContractCollisions` among the refusal reasons.

## Impact

- `go.mod`, `go.sum` (library pin).
- `internal/status/conditions.go` (the reason constant, the `OverSubscribedContracts` precedence comment).
- `internal/controller/platform_inventory.go` (`inventoryRefusal`, a collision finding), `platform_controller.go` (the reconciler doc's reason list).
- Tests: `internal/controller/platform_inventory_test.go`, `platform_failure_test.go`.
- `api/v1alpha1/platform_types.go` doc comment, regenerated `config/crd/bases/opmodel.dev_platforms.yaml` and `dist/install.yaml`.
- `docs/RENDERING.md`, `docs/site/diagnostics/operator-conditions.md`.
- Downstream: none. The render path and TransformerRegistration acceptance never see a colliding package, because the gate refuses it before it is recorded.
- Waits on change B's pushed head to start and B's release to merge (`orchestration.md`). No 0026 decision is fully delivered by the set, so no `enhancement.yaml`.
