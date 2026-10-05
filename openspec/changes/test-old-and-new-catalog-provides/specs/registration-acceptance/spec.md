## ADDED Requirements

### Requirement: Acceptance holds on both paths of the provider-set derivation

Acceptance SHALL reach the same verdict for a claim whether the library derives the claimed catalog's provider set from the `provides` field core computes, which a catalog built against core `v2.0.0-beta.3` or later carries, or from the deprecated fold over `#transformers`, which answers a catalog built against an older core. A registry-backed integration spec SHALL run the `TransformerRegistrationReconciler` on the claim `backup_provider` renders, against a generated platform, once with each kind of catalog. Each time it SHALL assert that the claim is accepted and that the catalog's derived provider set is exactly opm's backup trait. The old catalog SHALL be the published `backup` catalog fixture, acquired from the registry. The new catalog SHALL be the same fixture tree with its core pin moved to the library's default core, which must not be older than the core that introduced `provides`. Each spec SHALL also assert the facts that select its path: the old catalog pins a core older than the library's `ProvidesSince` and carries no `provides` field, and the new catalog pins a core at or after it and carries a `provides` field that equals the derived set. A library change that answers both catalogs by one path then fails a spec instead of passing it twice.

#### Scenario: A claim on a catalog built against an older core is accepted through the fold

- **WHEN** the claim rendered by `backup_provider` names the published `backup` catalog fixture, whose committed core pin predates `ProvidesSince`, and its provider instance's inventory owns the claim
- **THEN** the catalog carries no `provides` field and its derived provider set is exactly `opmodel.dev/catalogs/opm/traits/backup@v1alpha1`
- **AND** the reconciler records `accepted: true` and `Ready=True` with reason `Accepted`, so the exact-equality check did not refuse it

#### Scenario: A claim on a catalog built against a newer core is accepted through the decoded field

- **WHEN** the same claim is judged against the `backup` fixture tree with its core pin moved to the library's default core, at or after `ProvidesSince`
- **THEN** the catalog carries a `provides` field equal to its derived provider set, which is exactly `opmodel.dev/catalogs/opm/traits/backup@v1alpha1`
- **AND** the reconciler records `accepted: true` and `Ready=True` with reason `Accepted`

#### Scenario: The old-catalog case is not lost silently

- **WHEN** the `backup` catalog fixture's core pin is moved to `ProvidesSince` or later, or the library's default core predates it
- **THEN** the spec whose path premise no longer holds fails, naming that premise, instead of passing on the other path
