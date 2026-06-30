## 1. API field

- [x] 1.1 In `api/v1alpha1/moduleinstance_types.go`, add `type OwnerType string` with `OwnerCLI OwnerType = "cli"` and `OwnerOperator OwnerType = "operator"` constants.
- [x] 1.2 Add the `Owner OwnerType` field to `ModuleInstanceSpec` after `Suspend`, with markers `// +kubebuilder:validation:Enum=cli;operator` and `// +optional`, json tag `owner,omitempty`; doc comment noting absent/empty/operator ⇒ operator-managed, only explicit `cli` skips (no CRD default).
- [x] 1.3 Run `task dev:generate` to regenerate `zz_generated.deepcopy.go` (no hand-edits).

## 2. Status helper and reason

- [x] 2.1 In `internal/status/conditions.go`, add `ManagedExternallyReason = "ManagedExternally"` to the reason constants block.
- [x] 2.2 Add `MarkManagedExternally(obj conditions.Setter)` mirroring `MarkSuspended` but using `conditions.MarkUnknown(obj, ReadyCondition, ManagedExternallyReason, "ModuleInstance is managed externally by the CLI")` and deleting `Reconciling`/`Stalled`.

## 3. Reconcile skip gate

- [x] 3.1 In `internal/reconcile/moduleinstance.go`, insert the owner-skip branch immediately after the initial `Get`, **before** finalizer registration: when `mi.Spec.Owner == releasesv1alpha1.OwnerCLI` and `mi.DeletionTimestamp.IsZero()`, call `status.MarkManagedExternally(&mi)`, emit a `ManagedExternally` event, create the serial patcher, and patch with `patch.WithOwnedConditions{...all condition types...}` only — NOT `WithStatusObservedGeneration` — then return.
- [x] 3.2 In the same branch, when `mi.Spec.Owner == OwnerCLI` and `DeletionTimestamp` is non-zero, return immediately (no finalizer to remove, no prune). Confirm the existing finalizer/deletion/suspend flow is untouched for non-`cli` owners.

## 4. Regenerate manifests

- [x] 4.1 Run `task dev:manifests` to regenerate `config/crd/bases/opmodel.dev_moduleinstances.yaml` with the new `spec.owner` enum field (no hand-edits).
- [x] 4.2 Run `task operator:installer` to refresh `dist/install.yaml` (the artifact the CLI later embeds, 0006 B2).

## 5. Tests

- [x] 5.1 Add a reconcile test context for CLI-owned instances (mirroring the `Suspend check` context): assert no resources applied, `opmodel.dev/cleanup` finalizer absent, `Ready: Unknown / ManagedExternally`, and `status.observedGeneration` not set.
- [x] 5.2 Add a test asserting CLI-written `status.inventory` + `lastApplied*` survive a reconcile of a CLI-owned instance untouched (D25 boundary), and that re-reconciling an already-acknowledged instance produces no condition-transition change (idempotency).
- [x] 5.3 Add a test asserting deletion of a CLI-owned instance prunes nothing and is not blocked (no finalizer), and a handoff-fall-through test: flipping `spec.owner` to `operator` adds the finalizer and reconciles normally, overwriting `ManagedExternally`.

## 6. Validation gates

- [x] 6.1 `task dev:fmt dev:vet` — formatted and vet-clean.
- [x] 6.2 `task dev:test` — unit + integration (envtest) pass, including the new contexts.
- [x] 6.3 `task dev:lint` — golangci-lint clean (incl. `logcheck`, `ginkgolinter`). Note if e2e is skipped (no Kind).
