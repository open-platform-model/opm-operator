## ADDED Requirements

### Requirement: The operator uses the library's object and label packages and keeps no copy

The operator SHALL take the Kubernetes resource wrapper over the kernel's compiled output, its JSON and unstructured conversion, the duplicate-identity check, the kind-class weights and stages, the OPM label keys and values, and the recognition of an OPM manager label value from the library's Kubernetes tier (`opm/k8s/object` and `opm/k8s/labels`). It SHALL declare no type, constant or function of its own for any of them and no alias to them. Its lint SHALL refuse an import of the package path that held its earlier copy, `github.com/open-platform-model/opm-operator/pkg/core`. Source: 0012:D3:R6.

#### Scenario: No local copy

- **WHEN** the operator's Go source is searched for the package `pkg/core`, a `Resource` type wrapping a CUE value with instance, component and transformer provenance, or a declaration of the `app.kubernetes.io/managed-by` or `module-instance.opmodel.dev/uuid` label key
- **THEN** none is found; every reader imports `opm/k8s/object` or `opm/k8s/labels`

#### Scenario: Lint refuses the old path

- **WHEN** a package exists at `github.com/open-platform-model/opm-operator/pkg/core` and a Go file in the repository imports it
- **THEN** `task dev:lint` fails on the rule `no-local-kubernetes-tier-copy`, with a message naming the library packages to use instead

### Requirement: The reconciler converts a render with one export

The reconciler SHALL convert a render's resources with one call to the library's `object.Export`, which exports each resource from CUE once, and SHALL take both the render digest and the unstructured objects it applies from that one result, inside the render slot. A failure of the export SHALL be reported with the reason and message the reconciler reported before: a value that will not export is a render failure named "computing render digest", and exported JSON that will not decode to an object is an apply failure named "converting resources" with the resource. The renderer's own export of each resource for its inventory entries remains until the inventory adoption change replaces it.

#### Scenario: Digest and apply objects from one export

- **WHEN** a render of N resources is converted for apply
- **THEN** the render digest equals `RenderDigest` over `object.Export` of the same resources, and the N apply objects are that export's `Exported.Object` values in input order

#### Scenario: Export failure keeps its reason

- **WHEN** one rendered value fails to export
- **THEN** the conversion fails with reason `RenderFailed` and a message that begins "computing render digest", and the rendered resources are dropped

#### Scenario: Decode failure keeps its reason

- **WHEN** one rendered value exports to JSON that does not decode to an object
- **THEN** the conversion fails with reason `ApplyFailed` and a message that begins "converting resources", and the rendered resources are dropped
