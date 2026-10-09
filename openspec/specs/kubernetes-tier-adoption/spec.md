# kubernetes-tier-adoption Specification

## Purpose
Defines how the operator adopts the library's Kubernetes tier: it takes resource conversion and the OPM label vocabulary from `opm/k8s/object` and `opm/k8s/labels`, keeps no copy of them, and its lint refuses one reintroduced at the old path.

## Requirements

### Requirement: The operator uses the library's object and label packages and keeps no copy

The operator SHALL take the Kubernetes resource wrapper over the kernel's compiled output, its JSON and unstructured conversion, the duplicate-identity check, the OPM label keys and values, and the recognition of an OPM manager label value from the library's Kubernetes tier (`opm/k8s/object` and `opm/k8s/labels`). It SHALL declare no type, constant or function of its own for any of them and no alias to them. Its lint SHALL refuse an import of the package path that held its earlier copy, `github.com/open-platform-model/opm-operator/pkg/core`. Source: 0012:D3:R6.

#### Scenario: No local copy

- **WHEN** the operator's Go source is searched for the package `pkg/core`, a `Resource` type wrapping a CUE value with instance, component and transformer provenance, or a declaration of the `app.kubernetes.io/managed-by` or `module-instance.opmodel.dev/uuid` label key
- **THEN** none is found; every reader imports `opm/k8s/object` or `opm/k8s/labels`

#### Scenario: Lint refuses the old path

- **WHEN** a package exists at `github.com/open-platform-model/opm-operator/pkg/core` and a Go file in the repository imports it
- **THEN** `task dev:lint` fails on the rule `no-local-kubernetes-tier-copy`, with a message naming the library packages to use instead

### Requirement: The reconciler converts a render with one export

The reconciler SHALL convert a render's resources with one call to the library's `object.Export`, which exports each resource from CUE once. It SHALL take the render digest, the unstructured objects it applies and the inventory entries from that one result, inside the render slot. It SHALL drop the rendered CUE values as soon as the export returns, so the digest and the entries read exported data only. No other code path SHALL export a rendered resource. A failure of the export SHALL be reported by the conversion: a value that will not export is a render failure named "computing render digest", and exported JSON that will not decode to an object is an apply failure named "converting resources" with the resource. With the renderer's export gone, the conversion is the first place such a failure is reported.

#### Scenario: Digest and apply objects from one export

- **WHEN** a render of N resources is converted for apply
- **THEN** the render digest equals the library's `inventory.RenderDigest` over `object.Export` of the same resources, and the N apply objects are that export's `Exported.Object` values in input order

#### Scenario: Export failure keeps its reason

- **WHEN** one rendered value fails to export
- **THEN** the conversion fails with reason `RenderFailed` and a message that begins "computing render digest", and the rendered resources are dropped

#### Scenario: Decode failure keeps its reason

- **WHEN** one rendered value exports to JSON that does not decode to an object
- **THEN** the conversion fails with reason `ApplyFailed` and a message that begins "converting resources", and the rendered resources are dropped

#### Scenario: Inventory entries from the same export

- **WHEN** a render of N resources is converted for apply
- **THEN** the conversion carries N inventory entries, one per `Exported.Object` in input order, and the render result carries none

### Requirement: The operator uses the library's inventory package and keeps no copy

The operator SHALL compute the inventory stale set, the inventory digest and the render digest only with the library's `opm/k8s/inventory` (`StaleSet`, `Digest`, `RenderDigest`). It SHALL build inventory entries with that package's `NewEntry`. It SHALL declare no entry constructor, identity relation, stale-set function, inventory digest or render digest of its own. Its only inventory code is the conversion between the API's `InventoryEntry` and `inventory.Entry`, and a test of `internal/inventory` SHALL refuse any further export from that package, so a copy cannot come back there unnoticed. Source: 0012:D3:R6.

#### Scenario: No local copy

- **WHEN** the operator's Go source is searched for `ComputeStaleSet`, `ComputeDigest`, `NewEntryFromResource`, `IdentityEqual`, `K8sIdentityEqual` or a render digest function in `internal/status`
- **THEN** none is found, and every reader calls `opm/k8s/inventory`

#### Scenario: A copy in the conversion package fails the tests

- **WHEN** a function is exported from `internal/inventory` beyond `Current`, `FromEntries`, `FromEntry`, `ToEntries` and `ToEntry`
- **THEN** the package's closed-surface test fails

### Requirement: The operator judges the health of inventory objects only through the library's health package

The operator SHALL judge the readiness of an object in `status.inventory` only with the library's `opm/k8s/health` (`Evaluate`, `IsHealthy`, `Aggregate`, `ProgressDeadlineExceeded`). The code that computes the `Healthy` condition SHALL read no status field of a fetched object and SHALL declare no readiness rule of its own for any Kubernetes kind: no rollout, replica-count, Job, PersistentVolumeClaim or `Ready`-condition check, and no alias to the library's. The operator fetches each object with its own client and passes it in, because the library reads no cluster. This requirement covers applied inventory objects only; the operator's own resources (a `dependsOn` ModulePackage, a TransformerRegistration's provider, the `cluster` Platform) keep their own condition checks. Source: 0012:D3:R6.

#### Scenario: No local evaluator

- **WHEN** the non-test files of `internal/reconcile` are parsed by the package's unit test
- **THEN** none calls an `unstructured.Nested*` accessor and `health.go` holds no `"status"` string literal, so every verdict comes from `health.Evaluate`, `health.IsHealthy`, `health.Aggregate` and `health.ProgressDeadlineExceeded`

### Requirement: The operator decides prune and cleanup deletes only through the library's ownership package
Every delete of a cluster object that the operator makes in a prune of stale resources or in a deletion cleanup, for a ModuleInstance or a ModulePackage, MUST follow a delete verdict of the library's `opm/k8s/ownership` package on a live read of that object. The operator MUST take the kinds that are never deleted from the same package. The operator MUST NOT set the verdict's install admission input. Source: 0012:D4:R1.

The one other delete the operator makes, the delete inside a forced recreate, asks no verdict and MUST carry the UID precondition (capability `ssa-apply`).

A test MUST keep the list of places that may delete a cluster object closed. It MUST find a delete by the type of the receiver, a Kubernetes client, and not by the method name alone, so that a method of the same name on another type is not counted, and a second test MUST show that the matcher finds a delete of each form the client offers.

#### Scenario: The prune's outcome is the verdict's
- **GIVEN** one stale object for each answer of the library's delete verdict
- **WHEN** the prune runs
- **THEN** it deletes exactly the objects for which the verdict says proceed, and names each other object with the verdict's reason

#### Scenario: A new delete outside the listed places fails the tests
- **GIVEN** a change that adds a call that deletes a cluster object in a file the test does not list
- **WHEN** the unit tests run
- **THEN** the call-site test fails and names the file

#### Scenario: A method of the same name on another type is not a delete
- **GIVEN** the operator's calls that remove a status condition
- **WHEN** the call-site test runs
- **THEN** it does not count them

### Requirement: The operator decides every apply only through the library's ownership package
Every write of a cluster object that the operator makes on behalf of a ModuleInstance or a ModulePackage MUST follow an apply verdict of the library's `opm/k8s/ownership` package on a live read of that object. The operator MUST declare no ownership rule of its own for an apply: it compares no managed-by label, no UUID label and no adopt annotation itself. The operator MUST NOT set, change or remove the `opmodel.dev/adopt` annotation on any object. Source: 0012:D4:R2, 0012:D8:R6.

A test MUST keep the list of places that may write a cluster object closed: the staged apply, the dry runs of drift detection and of the claim check, and the status and finalizer patches of the operator's own kinds. It MUST find a write by the type of the receiver, as the test of the delete call sites does, and a second test MUST show that the matcher finds a write of each form the client and the resource manager offer.

#### Scenario: The apply's outcome is the verdict's
- **GIVEN** one rendered object for each answer of the library's apply verdict
- **WHEN** the guard runs
- **THEN** it allows exactly the objects for which the verdict says apply, and names each other object with the verdict's reason and message

#### Scenario: A new write outside the listed places fails the tests
- **GIVEN** a change that adds a call that creates, updates, patches or applies a cluster object in a file the test does not list
- **WHEN** the unit tests run
- **THEN** the call-site test fails and names the file

#### Scenario: No write of the adopt annotation
- **WHEN** the non-test Go files of the operator are parsed by a unit test
- **THEN** none names the library's adopt annotation key, so the annotation is read only inside the library's verdicts and is never written
