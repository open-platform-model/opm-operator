# live-flux-e2e

## Purpose

The live verification tier for the operator's Flux-facing and deployed-controller surfaces: a real source-controller at the pinned distribution version, a real artifact round-trip, and lifecycle/prune/orphan assertions against the running controller. (Test-infrastructure capability; product behavior is specified elsewhere — precedent: `test-registry-lifecycle`, `example-test-modules`.)

## Requirements

### Requirement: e2e installs source-controller at the pinned distribution version

The e2e environment SHALL install Flux's source-controller (and only source-controller) at a version pinned in one place, documented as matching the flux2 distribution whose library versions this repo's `go.mod` pins. CI SHALL install the flux CLI at the same pinned version before the suite runs.

#### Scenario: Pinned install

- **WHEN** the e2e environment is prepared
- **THEN** source-controller SHALL be installed at the pinned version, not the flux CLI's default/latest

#### Scenario: Pin moves with the library line

- **WHEN** the repo's Flux library pins (A1/D4 distribution set) are bumped
- **THEN** the same change SHALL bump the e2e pin (single variable, co-located documentation)

### Requirement: Live artifact pipeline proof

The e2e suite SHALL push the podinfo modulepackage fixture as a real OCI artifact (`flux push artifact` to the local registry), apply the fixture's `OCIRepository` and `ModulePackage`, and assert: the real source-controller reports an `Artifact` with revision and digest; the operator fetches and extracts that artifact; the `ModulePackage` reaches `Ready: True`; the rendered Deployment becomes Ready; and the artifact revision propagates into the `ModulePackage` status.

#### Scenario: Real artifact renders to a Ready workload

- **WHEN** the fixture artifact is pushed and the OCIRepository + ModulePackage are applied
- **THEN** the ModulePackage SHALL reach Ready with the rendered Deployment Ready
- **AND** the ModulePackage status SHALL carry the source-controller-reported artifact revision

#### Scenario: Suite gating

- **WHEN** the suite runs in CI (flux env marker set) without a reachable source-controller
- **THEN** the specs SHALL fail (not skip); outside CI without source-controller they SHALL skip with notice

### Requirement: Deployed-controller lifecycle, prune, and orphan proof

Against the running (deployed) controller, the e2e suite SHALL assert: a `ModuleInstance` reaches Ready with the cleanup finalizer registered; an update whose render drops a resource results in the live stale resource being pruned by the controller; and deleting an instance with `prune=false` removes the CR (finalizer released) while its rendered workloads remain in the cluster.

#### Scenario: Live prune on update

- **WHEN** a Ready instance's values change such that the render no longer contains a previously-applied resource
- **THEN** the deployed controller SHALL delete that live resource and the inventory SHALL shrink accordingly

#### Scenario: prune=false delete orphans live workloads

- **WHEN** a Ready instance with `spec.prune: false` is deleted
- **THEN** the CR SHALL be removed (finalizer released) and the rendered workloads SHALL remain live

### Requirement: No permanent Skip stubs in the live tier

e2e spec files SHALL contain only executable specs or explicitly-recorded future items; stubs whose coverage exists at a lower tier and whose live-tier value is subsumed by this capability's specs SHALL be deleted, with the parallel-instances and controller-restart scenarios remaining as the sole recorded future items.

#### Scenario: Stub census after landing

- **WHEN** the e2e suite is inspected after this change lands
- **THEN** `lifecycle_test.go`'s stubs SHALL be replaced by live specs, `prune_test.go`/`finalizer_test.go` stub files SHALL be gone, and only the concurrent scenarios SHALL remain recorded

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
