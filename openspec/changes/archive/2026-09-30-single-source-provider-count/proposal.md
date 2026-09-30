## Why

The single-provider rule (a contract declared `fulfilment: "provider"` has exactly one provider on a platform; 0010:D37, 0015:D2/D18) is counted in three places today, and they disagree. Core's `#Platform.#contracts` counts only contracts an enabled catalog defines and keys a provider by the transformer's major-free stamp. The library render build counts every enabled registry entry by registry key (path plus major). And this operator's `TransformerRegistration` acceptance keeps a third count of its own (`subscriptionProviders`, `internal/controller/transformerregistration_contracts.go:69`), folded off `#composedTransformers` and keyed by the stamped `metadata.modulePath`.

That third count is wrong on real platforms. Core stamps every transformer's `metadata.modulePath` as `"<registryPath>/transformers"` (core `src/catalog.cue:162`), major-free and suffixed, while a claim's `spec.catalog` and the registry key it becomes (`platform_controller.go:555`) are the major-suffixed module path. So the check's self-exclusion (`held == ownCatalog`, `transformerregistration_contracts.go:169`) never matches: an ACTIVE claim re-judged after its catalog joined the platform is refused for providing its own contract, the oscillation the exclusion exists to prevent. The refusal also names the stamp (`opmodel.dev/catalogs/velero/transformers`) instead of the catalog a platform admin can act on. The unit fixture (`transformerregistration_contracts_test.go:57`) stamps the major-suffixed path, which is why the suite never saw it.

The four-change set this belongs to (orchestration.md) makes the render build's count the only count: core computes it once as `#contracts.providedBy` (change A, core `2.0.0-alpha.12`), the library reads it and exposes `ContractInventory.ProvidedBy` (change B). This change is C: the operator stops counting and reads `ProvidedBy`, and its over-subscription refusal names the providing registry entries from it.

## What Changes

- **Library pin to change B.** `github.com/open-platform-model/library` moves to B's pushed head (a Go pseudo-version) for development, then to B's release in the last section. The generated platform module follows `schema.DefaultSchemaVersion()` to core `2.0.0-alpha.12` with no operator constant involved.
- **Acceptance reads the platform's provider count.** The one-provider-per-contract check against enabled registry entries reads `ContractInventory.ProvidedBy` off the built platform (`Contracts()`), keyed by registry key. `subscriptionProviders`, `collectProvided` and the two CUE paths they read are deleted. The claim's own entry is excused by registry key, which now matches `spec.catalog`, so an active claim is no longer refused against itself, and another major of the claim's own catalog is another provider. The refusal names the registry key (`opmodel.dev/catalogs/velero@v2`). Red first: the self-refusal is reproduced with a core-stamped fixture before the switch.
- **The over-subscription refusal names the providers.** The `OverSubscribedContracts` Ready message lists, for each contract, its defining catalog when one is enabled and the registry entries that provide it (`provided by <ProvidedBy>`), replacing the requiring transformers. A contract whose defining catalog is disabled or absent (Bug 2) is now worded with its providers instead of an empty list.
- **Two platform shapes the gate passed are now refused at generation.** Two majors of one provider catalog enabled together (Bug 1), and two providers of a contract whose defining catalog is disabled or absent (Bug 2), read `Routable: false` from B's inventory, so the Platform reports `Ready=False` with reason `OverSubscribedContracts` and records no package, where it used to report `Generated` and every render then failed with `RenderFailed`. No operator code decides this; the specs and docs say it.
- **Docs and API comment.** `PlatformStatus.Conditions` (regenerating the CRD description and `dist/install.yaml`), the `OverSubscribedContractsReason` comment, `docs/RENDERING.md` and `docs/site/diagnostics/operator-conditions.md` say what the refusal names and that the count is per registry entry.

## Classification

**PATCH** (pre-GA, released as `fix`). No API type, field or reason is added or removed; only a condition message's list and a CRD description change. Two behaviours move, both toward the render's existing verdict: Bug 1 and Bug 2 platforms are refused at generation instead of on every render, and an active claim is no longer refused against its own catalog. Complexity is net negative (Principle VII): one CUE fold and its two path constants are deleted, and nothing is added in their place.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `registration-acceptance`: "A contract has one provider" reads the providers from the built platform's provider count, keyed by registry entry (path plus major); the claim's own entry is its `spec.catalog`; another major of its own catalog is another provider; the refusal names the registry key.
- `platform-inventory-gate`: "An over-subscribed platform is refused at generation" counts providers per registry entry whether or not the defining catalog is enabled, names the providing entries, and gains the two-majors and definer-disabled scenarios.
- `platform-reconciler`: "Surface materialize outcome on status" says the `OverSubscribedContracts` message names the providing registry entries rather than the requiring transformers.

## Impact

- `go.mod`, `go.sum` (library pin).
- `internal/controller/transformerregistration_contracts.go` (own count deleted), `transformerregistration_controller.go` (the check reads `Contracts()`), `platform_inventory.go` (refusal wording, `definedBy` comment).
- Tests: `transformerregistration_contracts_test.go` (core-shaped platform fixture), `transformerregistration_loop_test.go`, `platform_inventory_test.go`, `platform_failure_test.go`, `platform_controller_test.go`.
- `api/v1alpha1/platform_types.go` doc comment, regenerated `config/crd/bases/opmodel.dev_platforms.yaml` and `dist/install.yaml`; `internal/status/conditions.go` comment.
- `docs/RENDERING.md`, `docs/site/diagnostics/operator-conditions.md`.
- Waits on change B's release (orchestration.md); merges after it. No enhancement entry backs the set, so no `enhancement.yaml`.
