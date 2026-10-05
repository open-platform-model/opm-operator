## ADDED Requirements

### Requirement: Deployed-controller registration claim proof

Against the deployed controller, the e2e suite SHALL drive a claim rendered by a real provider module through acceptance and into a platform build. The suite SHALL apply the sample Platform and the `backup_provider` fixture's `moduleinstance.yaml`. Its `TransformerRegistration`, `default.backup-provider`, names the published `backup` catalog fixture at a bare SemVer `spec.version`. The suite SHALL read the expected catalog path and version from the catalog fixture's identity package (`test/fixtures/fixtures.go`), never from a literal, and SHALL assert that the live claim's `spec.version` equals that version and carries no `v` prefix. The suite SHALL assert three things: the claim reports `accepted: true`, `active: true` and `Ready=True` with reason `Accepted`; the Platform's `status.registry` lists the backup catalog with `source: Registration` at the claim's version, under a `status.packageIdentity` different from the one held before the claim; and the Platform reports `Ready=True` with reason `Generated`.

The suite SHALL then cover the `v`-prefixed spelling. It suspends the provider instance, so the controller does not re-apply the rendered claim, and sets the live claim's `spec.version` to the same build with a `v` prefix. The suite SHALL assert that the claim is re-judged at its new generation: `status.observedGeneration` equals `metadata.generation`, the claim stays accepted and active, and its Ready condition is `True` with reason `Accepted` and a message naming the `v`-prefixed version. A deferred reconcile also sets `observedGeneration` and leaves `accepted` and `active` from the earlier verdict, so the Ready reason and message are what prove a new verdict. Only after that verdict SHALL the suite assert that the Platform's `status.registry` entry for the backup catalog names the same build (in either spelling, since the entry today records the claim's own spelling), under another new package identity that stays stable on a second read, and that the Platform reports `Ready=True` with reason `Generated`. The package identity is a function of the claims' coordinates, so its move is what shows the platform was regenerated for the edited claim.

The spec SHALL deploy and tear down its own controller. It SHALL need no registry credentials when the fixtures are on the public default registry, and it SHALL use the `LOCAL_REGISTRY` and `OPERATOR_DOCKER_CONFIG` overrides as the other deployed-controller specs do. Its last ordered step SHALL delete the provider instance and assert that the claim is pruned with its guard released. Teardown SHALL then remove any claim still present, with a bounded wait and a finalizer strip, before it removes the applier RBAC and the Platform and before it undeploys the controller and uninstalls the CRDs.

#### Scenario: A bare-version claim is accepted, activated and built

- **WHEN** the sample Platform is Ready and the `backup_provider` ModuleInstance is applied
- **THEN** the claim `default.backup-provider` SHALL carry the backup catalog fixture's bare version in `spec.version`
- **AND** the claim SHALL report `accepted: true`, `Ready=True` with reason `Accepted` and, once the provider is Ready, `active: true`
- **AND** the Platform's `status.registry` SHALL list `testing.opmodel.dev/catalogs/operator/backup@v0` with `source: Registration` at the claim's bare version, under a new `status.packageIdentity`
- **AND** the Platform SHALL report `Ready=True` with reason `Generated`

#### Scenario: A v-prefixed claim is accepted and built

- **WHEN** the provider instance is suspended and the live claim's `spec.version` is set to the same build with a `v` prefix
- **THEN** the claim's `status.observedGeneration` SHALL reach its new `metadata.generation` with `accepted: true` and `active: true`
- **AND** the claim's Ready condition SHALL be `True` with reason `Accepted` and a message containing `at v` followed by the build
- **AND**, once that verdict is recorded, the Platform's `status.registry` SHALL list the backup catalog at the same build, with or without the `v`, under a package identity different from the bare-version one and stable on a second read; the identity moves only because the claim coordinates keep the claim's own spelling, so a change that normalises the version to bare SemVer SHALL turn this into the same identity (no regeneration)
- **AND** the Platform SHALL report `Ready=True` with reason `Generated`

#### Scenario: Teardown releases the claim

- **WHEN** the spec's last ordered step deletes the provider ModuleInstance
- **THEN** the claim SHALL be pruned with its removal guard released, as a reported spec
- **AND** the spec's teardown SHALL leave no claim, Platform or applier RBAC behind, removing a leftover claim before the CRDs are uninstalled
