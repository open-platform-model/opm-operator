# render-input-key Specification

## Purpose
Name every input a render consumes in one digest, the render input key, record it on ModuleInstance and ModulePackage status after a render that confirmed the cluster, and let a reconcile whose inputs have not changed skip its render, so an idle object costs no render, lease or fetch. The drift render interval bounds how long a skip can last, so drift detection runs at most once per interval, on the first reconcile after it.

## Requirements

### Requirement: The render input key names what a render is a function of

The operator SHALL compute a render input key for a ModuleInstance or a ModulePackage from six parts:

- the source digest: for a ModuleInstance the digest of `spec.module.path@spec.module.version`, for a ModulePackage the resolved Flux artifact digest;
- the config digest: the digest of `spec.values` for a ModuleInstance, the digest of empty input for a ModulePackage;
- the platform package identity, in the string form `Platform.status.packageIdentity` carries;
- the catalog skew policy resolved from `Platform.spec.skewPolicy`, spelled as the API spells it (`Warn` when unset);
- the operator version, as the operator publishes it to `Platform.status.operatorVersion`;
- the library version the operator binary was built with.

The key's digest SHALL be `sha256:<hex>` over a fixed, versioned encoding of the parts in a fixed order, so the same parts always give the same digest and any change to a part gives a different one. A key with an empty part is incomplete: it SHALL never be recorded and never match.

Source: owner decision g3 of the kernel-plan walkthrough (2026-10-02): "Skip render when a pre-render input key (source, values, platform identity, operator/library version, skew) matches."

#### Scenario: Any part changes the digest

- **WHEN** two keys differ in exactly one part
- **THEN** their digests differ

#### Scenario: The same parts give the same digest

- **WHEN** a key is computed twice from the same parts
- **THEN** both digests are equal

#### Scenario: A missing part makes the key incomplete

- **WHEN** the Platform has no `status.packageIdentity`, or no Platform named `cluster` exists, or the binary carries no library version
- **THEN** the key is incomplete and the reconcile renders

### Requirement: The key of the last confirming render is recorded on status

ModuleInstance and ModulePackage SHALL carry an optional `status.lastAppliedInputs` with the key's `digest` and the time `renderedAt` of the render that produced it. The reconcilers SHALL write it on an attempt whose apply (and prune, when enabled) succeeded, and, while the drift render interval is greater than zero, on a `NoOp` that rendered, together with the other `lastApplied*` fields. The recorded key SHALL be built from the platform package identity and skew policy the render itself used (the platform record it leased), never from values read before the render. When that key is incomplete, the field SHALL be cleared.

An attempt that fails, is refused, panics, or skips its render SHALL leave the field as it was.

#### Scenario: A successful apply records the key

- **WHEN** a ModuleInstance renders against platform package `gen-3` and its apply succeeds
- **THEN** `status.lastAppliedInputs.digest` is the digest of the key with package identity `gen-3`
- **AND** `status.lastAppliedInputs.renderedAt` is the time of that reconcile

#### Scenario: A NoOp re-proves the key

- **WHEN** a reconcile renders and the outcome is `NoOp`
- **THEN** `status.lastAppliedInputs` is rewritten with the key of that render and the current time
- **AND** `lastAttempted*`, `inventory` and history are not modified

#### Scenario: A failed apply keeps the previous key

- **WHEN** an instance that recorded a key changes `spec.values` and the apply fails
- **THEN** `status.lastAppliedInputs` is unchanged

#### Scenario: The recorded identity is the one the render leased

- **WHEN** `Platform.status.packageIdentity` reads `gen-3` before the render, but the render leased package `gen-4`, and the apply succeeds
- **THEN** the recorded key carries package identity `gen-4`

### Requirement: A reconcile skips its render when its inputs are unchanged

Before rendering, a ModuleInstance or ModulePackage reconcile SHALL compute the key from the object (for a ModulePackage, after resolving its source), `Platform.status.packageIdentity`, the resolved `Platform.spec.skewPolicy` of the `cluster` Platform, and the running operator and library versions. It SHALL skip the render when, and only when, all of these hold:

- the drift render interval is greater than zero;
- `status.lastAppliedInputs` is set and its `renderedAt` is less than the drift render interval ago and not in the future;
- `Ready` is `True` with reason `ReconciliationSucceeded`;
- `status.observedGeneration` equals `metadata.generation`;
- the key is complete and its digest equals `status.lastAppliedInputs.digest`;
- for a ModulePackage, the source just resolved (ref, artifact revision, digest and URL) equals `status.source`.

A skipped reconcile SHALL NOT take a render slot, lease the platform, fetch the source artifact, render, run drift detection, apply, prune, emit an event or patch status. It is not a reconcile attempt: it records no outcome, no history and no `lastAttempted*`, and it SHALL NOT move `renderedAt`. A skipped ModuleInstance reconcile SHALL return without a requeue; a skipped ModulePackage reconcile SHALL requeue after `spec.interval`, as a `NoOp` does.

Source: owner decision g3 of the kernel-plan walkthrough (2026-10-02).

#### Scenario: Unchanged inputs skip the render

- **WHEN** a Ready ModuleInstance whose recorded key matches its current inputs, rendered 5 minutes ago, is reconciled with a 30-minute interval
- **THEN** the renderer is not called
- **AND** the instance's status is not patched

#### Scenario: A changed input renders

- **WHEN** the Platform's `status.packageIdentity` moves from `gen-3` to `gen-4` and the instance is reconciled within the interval
- **THEN** the instance renders

#### Scenario: A failed attempt renders on the next reconcile

- **WHEN** an instance's last attempt failed (`Ready=False`) and its spec was reverted to the inputs of the recorded key
- **THEN** the next reconcile renders

#### Scenario: A spec edit outside the key renders

- **WHEN** only a field outside the key changes (`spec.prune`, `spec.serviceAccountName`, `spec.rollout`), so `metadata.generation` moves and the key does not
- **THEN** the reconcile renders and the commit writes `status.observedGeneration`

#### Scenario: A new revision with the same digest renders once

- **WHEN** a ModulePackage's Flux source moves to a new revision whose artifact digest is unchanged, within the drift render interval
- **THEN** the package renders, the outcome is `NoOp`, and `status.source.artifactRevision` names the new revision

#### Scenario: A skipped package keeps its interval

- **WHEN** a ModulePackage whose artifact digest and other inputs are unchanged is requeued by its `spec.interval` within the drift render interval
- **THEN** the artifact is not fetched and the package is not rendered
- **AND** the reconcile requeues after `spec.interval`

### Requirement: The drift render interval bounds how long a render is skipped

The manager SHALL take `--drift-render-interval` (a duration, default `30m`): the longest a reconcile with unchanged inputs skips its render after the last render that recorded the key. A reconcile triggered after that renders, so drift detection runs at most once per interval per object while the inputs do not change. The interval SHALL NOT schedule a reconcile of its own. `0` SHALL disable the skip, so every reconcile renders, and a `NoOp` SHALL then leave `status.lastAppliedInputs` as it was. A negative value SHALL make the manager exit at startup with an error naming the flag.

Source: owner decision g3 of the kernel-plan walkthrough (2026-10-02): "drift via throttled re-render (at most every N minutes)".

#### Scenario: An old render is repeated

- **WHEN** a Ready ModuleInstance with a matching key last rendered 31 minutes ago is reconciled with a 30-minute interval
- **THEN** the instance renders, drift detection runs, and `renderedAt` moves on the `NoOp`

#### Scenario: Zero disables the skip

- **WHEN** the manager runs with `--drift-render-interval=0`
- **THEN** every reconcile renders, whatever `status.lastAppliedInputs` holds
- **AND** a reconcile that ends `NoOp` does not move `status.lastAppliedInputs.renderedAt`

#### Scenario: A negative interval is refused

- **WHEN** the manager starts with `--drift-render-interval=-1m`
- **THEN** it exits with an error naming `--drift-render-interval`

### Requirement: An operator or library upgrade renders every object once

Because the operator and library versions are key parts, the first reconcile of each object after either version changes SHALL render. The rule rests on every operator release changing `version.Version`; a development image built without a version bump keeps its key. A change to how the operator computes a stored digest (`lastApplied*` digests, `status.inventory.digest`) SHALL ship with an operator version change or a library version change, so that render finds the stored digests out of date and applies once. Starting the same operator and library again (a restart) SHALL NOT by itself make an object render while its key matches and the interval has not passed.

#### Scenario: A newer operator renders and applies once

- **WHEN** an instance's key was recorded by operator `v1.0.0-beta.9`, the running operator is `v1.0.0-beta.10`, and the render digest the new operator computes differs from `lastAppliedRenderDigest`
- **THEN** the reconcile renders and applies
- **AND** `status.lastAppliedInputs` then carries the new operator's key

#### Scenario: A restart does not render unchanged objects

- **WHEN** the operator restarts, the platform store is still empty, and an instance's key matches and was recorded 10 minutes ago
- **THEN** the instance is not rendered and stays `Ready=True`, without passing through `PlatformNotReady`

#### Scenario: The library inventory digests apply once and then converge

- **WHEN** an instance or package whose `status.inventory.digest` and `lastAppliedRenderDigest` were written in the earlier operator's encoding, with a key recorded by operator `v1.0.0-test`, is reconciled by operator `v1.0.1-test` that computes them with `opm/k8s/inventory`
- **THEN** the reconcile renders once and applies once, and the stored digests become the library's digests of the same entries and render
- **AND** the next reconcile under `v1.0.1-test` skips the render, and a render after the interval ends `NoOp` without a second apply

#### Scenario: A key recorded by the same version keeps the old digests until the interval

- **WHEN** an instance holds digests in the earlier encoding and a key recorded by the running operator version
- **THEN** the reconcile skips its render, because the key holds no digest
- **AND** the first render after the interval applies once and records the new digests
