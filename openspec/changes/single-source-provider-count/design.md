## Context

See proposal.md (Why) for the three counts and the self-refusal. This change is C of the four-change set in `orchestration.md`; A (core) computes `#contracts.providedBy`, B (library) decodes it as `ContractInventory.ProvidedBy`, floors core at `2.0.0-alpha.12` with `errors.PlatformCoreTooOldError`, and moves `schema.DefaultSchemaModule` to `opmodel.dev/core@v2.0.0-alpha.12`. The interface C codes against is in `orchestration.md` § Interface B → C, D; B may rename only by reporting it under `surface`.

State on `origin/main` (5d2a4de, library `v1.0.0-alpha.33`, core pinned through `schema.DefaultSchemaVersion()`), verified 2026-09-29:

- **The operator's own count.** `internal/controller/transformerregistration_contracts.go:32-149`: `composedTransformers` (`#composedTransformers`), `transformerModulePath` (`metadata.modulePath`), `subscriptionProviders` (line 69) folding every composed transformer's required demands with `fulfilment: "provider"` into `contract FQN -> sorted stamped modulePaths`, and `collectProvided` (line 117). Its doc comment (lines 56-60) records why it exists: the inventory "does not carry this map". B adds exactly that map.
- **Its one caller.** `transformerregistration_controller.go:212-229`: `subscriptionProviders(generated.Platform)`, an unreadable platform defers the verdict (`PlatformNotReady`), then `subscribedContract(claim.Spec.Provides, providers, claim.Spec.Catalog)` (`transformerregistration_contracts.go:166`) refuses with `ContractSubscribed` on the first provider that is not `ownCatalog` (line 169).
- **The key mismatch.** Core stamps a transformer's `metadata.modulePath` as `"\(M._ref.registryPath)/transformers"` (core `src/catalog.cue:162`), which carries no major. A claim's `spec.catalog` is the major-suffixed module path (`api/v1alpha1/transformerregistration_types.go:34-41`), and it is the registry key of the entry the claim contributes (`platform_controller.go:555`, `Path: claim.Spec.Catalog`). So on a real platform `held == ownCatalog` never holds: an active claim re-judged against a platform carrying its own catalog is refused, naming `<path-without-major>/transformers`.
- **Why the suite passes.** `platformProviding` (`transformerregistration_contracts_test.go:49-71`) stamps `metadata: modulePath: <catalogPath>` with the major (line 57), a shape core never produces. `transformerregistration_loop_test.go:142-169` ("stays accepted") therefore passes.
- **The generation gate.** `platform_controller.go:271` reads `p.Contracts()`, a read error is `BuildFailed`, and `inventoryRefusal(inv)` (line 281, `platform_inventory.go:58`) refuses on `!inv.Routable`. `overSubscribedFinding` (`platform_inventory.go:79-90`) prints `<contract> (defined by X) required by <transformer FQNs>` from `DefinedBy` and `RequiredBy`. Both are keyed by defined contracts only, so a Bug 2 contract prints `<contract> required by ` with an empty list; `definedBy` (`platform_inventory.go:149-158`) calls a missing key "a core defect", which B makes false.
- **Render classification.** A render refused by the kernel reaches `renderFailureReason` (`internal/reconcile/resolution.go`), which maps `*oerrors.OverSubscribedContractsError` and every untyped refusal to `RenderFailed` with the kernel's message verbatim.

## Goals / Non-Goals

**Goals:**

- The operator reads the provider count and never computes one.
- An active claim is never refused against its own registry entry on a core-stamped platform, proven red before the switch.
- The over-subscription refusal names what an admin disables: registry entries.

**Non-Goals:**

- The counting rule (core's, change A) and the render guard (library's, change B).
- The claim-versus-claim check (`activeContractHolder`), which compares `spec.provides` lists of claims and never counted transformers.
- Operator-side wording for `PlatformCoreTooOldError` (Decision 4).
- A registry-backed Bug 1 or Bug 2 platform spec: no provider catalog with two majors, and no definer-disabled pair, is published on the testing domain. The shapes are pinned by hand-built inventories here and by the library's parity test (B) against real CUE.
- Re-pinning the operator's published test fixtures (`test/fixtures/modules/*` core pins) to the new core; that is the supervisor's workspace `task deps:pins:fixtures`.

## Decisions

### 1. Acceptance reads `ProvidedBy`; the operator's count is deleted

```go
// transformerregistration_controller.go, the D2 check against enabled entries
inv, err := generated.Platform.Contracts()
if err != nil {
	// Not a refusal, for the same reason an absent platform is not one.
	return r.deferVerdict(ctx, patcher, &claim, status.PlatformNotReadyReason,
		fmt.Sprintf("The generated platform's contract providers could not be read: %v", err))
}
if contract, entry := subscribedContract(claim.Spec.Provides, inv.ProvidedBy, claim.Spec.Catalog); contract != "" {
	return r.refuse(ctx, patcher, &claim, status.ContractSubscribedReason, fmt.Sprintf(
		"Contract %s is already provided by subscribed catalog %s; a contract has exactly one provider, "+
			"so disable that subscription or withdraw this claim",
		contract, entry))
}
```

`subscribedContract` keeps its signature and its sorted walk; its comment says the providers are registry keys, so `ownCatalog` (`spec.catalog`) matches the claim's own entry and nothing else, and another major of the same path is another provider. `subscriptionProviders`, `collectProvided`, `composedTransformers` and `transformerModulePath` are deleted, with the `cue` and `schema` imports they need. The reason string, the message text and the deferral message are unchanged; only the value `%s` prints moves from the stamp to the registry key.

**Alternatives.** Fixing the key inside the operator's fold (strip `/transformers`, re-add the major from the registry): a second copy of core's rule, which is the drift the set removes, and the major is not recoverable from the stamp at all. Reading `ProvidedBy` straight off `Package` by CUE path: duplicates the library's decode and its core floor; `Contracts()` is the library's one read of `#contracts`.

**Cost.** `Contracts()` decodes nine fields where the fold walked `#composedTransformers`; both run once per claim reconcile against a platform already evaluated at acquisition. No measurement is planned beyond the suite's wall time.

### 2. Red first, with a core-shaped fixture

`platformProviding` is rewritten to build what core builds: `#composedTransformers` stamped `"<path without major>/transformers"` exactly as `src/catalog.cue:162` does, plus a complete `#contracts` (all nine fields B's `Contracts()` reads; `providedBy` keyed by registry key and sorted, `overSubscribed` its keys with two or more entries, `routable` its emptiness, `definedBy`/`requiredBy`/`comparable` empty, `unfulfilled` empty, `fulfilled` and `discriminated` true). Its input stays `map[registryKey][]contract`. The stamped fold is kept after the switch on purpose: a regression that reads `#composedTransformers` again fails on the stamp instead of passing on a fixture that flatters it.

Section 1 confirms the stamp formula on a real built platform (the registry-backed test platform, `opmodel.dev/catalogs/opm@v4`) before section 2 relies on it.

Section 2 then runs the suite on the unchanged acceptance code with the new fixture. Expected red, four for the key mismatch and one because the old code never reads the inventory:

| Spec | Old outcome |
| --- | --- |
| loop: "stays accepted: a claim's own contracts are not already-provided" | refused `ContractSubscribed`, naming its own stamp (the self-refusal) |
| loop: "still refuses a claim whose contract another subscribed catalog provides" | refused, but the message names a stamp, not `opmodel.dev/catalogs/velero@v2` |
| contracts: "refuses the claim, naming the contract and the subscribed catalog" | refused, message names `opmodel.dev/catalogs/velero/transformers` |
| contracts (new): "refuses a claim whose contract another major of its own catalog provides" | refused, message names the major-free stamp, not the other major's entry |
| contracts (new): "defers the verdict when the provider count cannot be read" | accepted: the old code reads the fold, never the inventory |

Every other acceptance spec stays green. No red commit lands: the switch is in the same section.

### 3. The refusal names `ProvidedBy`

```go
for _, contract := range contracts {
	providers := slices.Sorted(slices.Values(inv.ProvidedBy[contract]))
	fmt.Fprintf(&b, "\n  %s%s provided by %s", contract, definedBy(inv, contract), strings.Join(providers, ", "))
}
```

The header line ("platform is not routable: N over-subscribed contract(s); a platform package cannot be generated until one competing catalog is disabled or its claim removed:") is unchanged. Each row replaces `required by <transformer FQNs>` with `provided by <registry keys>`. `definedBy`'s comment says a missing key is a contract no enabled catalog defines (Bug 2), not a defect. Every list is still sorted before printing, so the message stays order-independent for `failReconcile`'s event gate.

**Alternatives.** Appending `provided by` beside `required by`: two lists for one fact, and `RequiredBy` is empty on exactly the Bug 2 rows that most need naming. The registry key is what the remedy in the header acts on (`spec.registry` keys, a claim's `spec.catalog`); a transformer FQN is not.

### 4. `PlatformCoreTooOldError` gets no operator wording

B's proposal lists wording it "as a re-pin hint" among C's and D's work. For the operator it would be advice nobody can take: the operator generates the platform module and pins core from `schema.DefaultSchemaVersion()`, and B floors core at that same release, so a generated package cannot predate the floor. The typed error can reach the operator only through a library defect (a `DefaultSchemaModule` below its own floor). Both paths already surface it verbatim: `Contracts()` in the Platform reconciler as `BuildFailed` ("reading the platform's contract inventory: ..."), and a render as `RenderFailed` through `renderFailureReason`'s default. The kernel's message names the field and the release. This change pins that contract instead of wording it: `TestInventoryUnreadableIsAnErrorNotAnEmptyInventory` also asserts `errors.As` to `*oerrors.PlatformCoreTooOldError`. Reported under `deviations`.

### 5. Sections

1. **Library pin to B's head, spike** (`fix(deps)`): the pseudo-version, the two measurements written into this file, the typed-error pin. Behaviour unchanged except the generated core pin.
2. **Acceptance reads the count** (`fix(controller)`): fixture, red run, switch, deletion.
3. **The refusal names the providers** (`fix(controller)`): wording, tests, API comment and regenerated CRD and installer, status comment, docs.
4. **Library pin to B's release** (`fix(deps)`): stop and report if not released; cross-cutting e2e.

Each ends green under `task dev:fmt dev:vet dev:lint dev:test` (after `task dev:manifests dev:generate` in section 3).

### Reconcile phase impact

- **Source / Apply / Prune:** none.
- **Render:** none in operator code. Bug 1 and Bug 2 platforms are no longer recorded, so instances on a fresh cluster wait at `PlatformNotReady` with the cause on the Platform, instead of reaching `RenderFailed` on every render; on a cluster holding a last good package, renders keep consuming it.
- **Status:** Platform `Ready` message rows name providers; Bug 1 and Bug 2 platforms move from `Generated` to `OverSubscribedContracts`. TransformerRegistration `ContractSubscribed` names registry keys, and an active claim stays `Accepted` on re-judge.

## Research & Decisions

### Where the self-refusal comes from

**Context**: The brief says the operator's registration check refuses a claim against itself on a real platform; the suite is green.
**Explored**: Core `src/catalog.cue:162` (stamp `"<registryPath>/transformers"`); `platformEntries` (`platform_controller.go:536-563`, registry key = `spec.catalog`); `subscribedContract`'s exclusion (`transformerregistration_contracts.go:169`); the fixture stamp (`transformerregistration_contracts_test.go:57`).
**Decision**: Treat it as a key mismatch that a core-shaped fixture reproduces, and fix it by reading registry keys from `ProvidedBy`.
**Rationale**: The stamp can never equal a major-suffixed path, so the exclusion is dead code on every real platform; only the fixture made it live.

### Whether the operator keeps any count

**Context**: The operator folded `#composedTransformers` because the inventory lacked the provider map.
**Explored**: B's surface (`ContractInventory.ProvidedBy`, sorted registry keys per contract, the same value the render guard reads).
**Decision**: Delete the fold; read `ProvidedBy`.
**Rationale**: With the map exported, a second fold can only disagree with the render, which is the defect this change set removes.

## Risks / Trade-offs

- [B renames `ProvidedBy`, the typed error or its fields] → code against B's reported `surface`; a rename is a mechanical edit in sections 1 and 2, reported under `deviations`.
- [B's head moves after section 1 pins it] → the pseudo-version is only a development pin; section 4 re-pins B's release and reruns every gate.
- [The stamp formula differs on the released core] → section 1 measures it on a built platform before section 2 depends on it; a different stamp changes the fixture, not the fix (the fix no longer reads stamps).
- [The registry-backed specs skip silently when `CUE_REGISTRY` is unset or core `2.0.0-alpha.12` is not on GHCR] → run with `CUE_REGISTRY` exported and `OPM_TEST_REGISTRY_FORCE=1` (orchestration.md), so a skip is a failure.
- [A cluster whose Platform generated a Bug 1 or Bug 2 package keeps it after upgrade] → the process-local store is empty at start and `--platform-dir` is emptied, so the first reconcile rebuilds and refuses; the Platform shows `OverSubscribedContracts` and renders on a fresh store wait at `PlatformNotReady`. Every render on that package was already refused, so nothing that worked stops working.
- [Two majors of a DEFINING catalog enabled together make the whole platform value bottom] → pre-existing core issue (A's follow-up); it surfaces as `BuildFailed` or a `Contracts()` read error, before and after this change. Release notes do not claim every two-majors platform becomes `OverSubscribedContracts`.

## Migration Plan

Pre-GA, no migration. Merge after B is released and pinned (section 4). Rollback is reverting the PR, which restores the library pin and the operator's own count together.
