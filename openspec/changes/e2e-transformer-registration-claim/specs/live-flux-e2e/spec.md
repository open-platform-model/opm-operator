## ADDED Requirements

### Requirement: Deployed-controller registration claim proof

Against the deployed controller, the e2e suite SHALL drive a claim rendered by a real provider module through acceptance and into a platform build. The suite SHALL apply the sample Platform and the `backup_provider` fixture's `moduleinstance.yaml`. Its `TransformerRegistration`, `default.backup-provider`, names the published `backup` catalog fixture at a bare SemVer `spec.version`. The suite SHALL assert three things: the claim reports `accepted: true` and `active: true`; the Platform's `status.registry` lists the backup catalog with `source: Registration` at the claim's version, under a `status.packageIdentity` different from the one held before the claim; and the Platform reports `Ready=True` with reason `Generated`.

The suite SHALL then cover the `v`-prefixed spelling. It suspends the provider instance, so the controller does not re-apply the rendered claim, and sets the live claim's `spec.version` to the same build with a `v` prefix. The suite SHALL assert that the claim is re-judged at its new generation and stays accepted and active. It SHALL also assert that the Platform rebuilds with the `v`-prefixed version in `status.registry`, under another new package identity, and reports `Ready=True` with reason `Generated`.

The spec SHALL deploy and tear down its own controller. It SHALL need no registry credentials when the fixtures are on the public default registry, and it SHALL use the `LOCAL_REGISTRY` and `OPERATOR_DOCKER_CONFIG` overrides as the other deployed-controller specs do.

#### Scenario: A bare-version claim is accepted, activated and built

- **WHEN** the sample Platform is Ready and the `backup_provider` ModuleInstance is applied
- **THEN** the claim `default.backup-provider` SHALL report `accepted: true` and, once the provider is Ready, `active: true`
- **AND** the Platform's `status.registry` SHALL list `testing.opmodel.dev/catalogs/operator/backup@v0` with `source: Registration` at the claim's bare version, under a new `status.packageIdentity`
- **AND** the Platform SHALL report `Ready=True` with reason `Generated`

#### Scenario: A v-prefixed claim is accepted and built

- **WHEN** the provider instance is suspended and the live claim's `spec.version` is set to the same build with a `v` prefix
- **THEN** the claim's `status.observedGeneration` SHALL reach its new `metadata.generation` with `accepted: true` and `active: true`
- **AND** the Platform's `status.registry` SHALL list the backup catalog at the `v`-prefixed version, under a package identity different from the bare-version one
- **AND** the Platform SHALL report `Ready=True` with reason `Generated`

#### Scenario: Teardown releases the claim

- **WHEN** the spec finishes and deletes the provider ModuleInstance
- **THEN** the claim SHALL be pruned with its removal guard released, and the spec SHALL leave no claim, Platform or applier RBAC behind
