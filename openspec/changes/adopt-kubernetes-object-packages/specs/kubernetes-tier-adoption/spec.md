## ADDED Requirements

### Requirement: The operator uses the library's object and label packages and keeps no copy

The operator SHALL take the Kubernetes resource wrapper over the kernel's compiled output, its JSON and unstructured conversion, the duplicate-identity check, the kind-class weights and stages, the OPM label keys and values, and the recognition of an OPM manager label value from the library's Kubernetes tier (`opm/k8s/object` and `opm/k8s/labels`). It SHALL declare no type, constant or function of its own for any of them and no alias to them. Its lint SHALL refuse an import of the package path that held its earlier copy, `github.com/open-platform-model/opm-operator/pkg/core`. Source: 0012:D3:R6.

#### Scenario: No local copy

- **WHEN** the operator's Go source is searched for the package `pkg/core`, a `Resource` type wrapping a CUE value with instance, component and transformer provenance, or a declaration of the `app.kubernetes.io/managed-by` or `module-instance.opmodel.dev/uuid` label key
- **THEN** none is found; every reader imports `opm/k8s/object` or `opm/k8s/labels`

#### Scenario: Lint refuses the old path

- **WHEN** a Go file in the repository imports `github.com/open-platform-model/opm-operator/pkg/core`
- **THEN** `task dev:lint` fails with a message naming the library packages to use instead

### Requirement: A rendered resource is exported from CUE once

The reconciler SHALL convert a render's resources with one call to the library's `object.Export`, which exports each resource from CUE once, and SHALL take both the render digest and the unstructured objects it applies from that one result, inside the render slot. A failure of the export SHALL be reported with the reason and message the reconciler reported before: a value that will not export is a render failure named "computing render digest", and exported JSON that will not decode to an object is an apply failure named "converting resources" with the resource.

#### Scenario: Digest and apply objects from one export

- **WHEN** a render of N resources is converted for apply
- **THEN** the library's export runs once over the N resources, and the render digest and the N apply objects both come from its result

#### Scenario: Export failure keeps its reason

- **WHEN** one rendered value fails to export
- **THEN** the conversion fails with reason `RenderFailed` and a message that begins "computing render digest", and the rendered resources are dropped
