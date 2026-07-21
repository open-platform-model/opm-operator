# live-flux-e2e

## Purpose

The live verification tier for the operator's Flux-facing and deployed-controller surfaces: a real source-controller at the pinned distribution version, a real artifact round-trip, and lifecycle/prune/orphan assertions against the running controller. (Test-infrastructure capability; product behavior is specified elsewhere — precedent: `test-registry-lifecycle`, `example-test-modules`.)

## ADDED Requirements

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
