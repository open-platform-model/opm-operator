## Context

The sample CRs in `config/samples/` are Kubebuilder scaffold stubs with `# TODO(user): Add fields here`. They don't demonstrate actual usage. For local Kind testing, we need working examples that reference the test OCI artifact and exercise the full reconcile pipeline.

The controller expects:
1. An `OCIRepository` (Flux source) pointing to a published OCI artifact.
2. A `ModuleRelease` referencing that OCIRepository and specifying the module path + values.

## Goals / Non-Goals

**Goals:**
- Working OCIRepository sample pointing to the test module in the local registry.
- Working ModuleRelease sample wired to that OCIRepository with real field values.
- Updated BundleRelease sample with valid (but minimal) fields.
- All samples immediately usable with `kubectl apply -f config/samples/`.

**Non-Goals:**
- Samples for every possible field combination.
- Samples for advanced features (impersonation, force-conflicts, suspend).

## Decisions

**OCIRepository sample: `config/samples/source_v1_ocirepository.yaml`**

New file. Points to the test module published by `test-oci-artifact` change:
- `url: oci://opm-registry:5000/opmodel.dev/test/hello` (in-cluster address)
- `interval: 1m`
- `insecure: true` (local registry, no TLS)
- Namespace: `default` (simple for testing)

**ModuleRelease sample update**

Replace the stub with:
- `spec.sourceRef`: kind `OCIRepository`, name matching the OCIRepository sample.
- `spec.module.path`: `opmodel.dev/test/hello` (the test module path).
- `spec.values`: `{ message: "deployed by opm controller" }`.
- `spec.prune: true`.
- Namespace: `default`.

**BundleRelease sample update**

Fill in valid fields but add a comment noting the controller is not yet implemented:
- `spec.sourceRef`: same OCIRepository reference.
- `spec.prune: true`.

**Kustomize integration**

Add the OCIRepository sample to `config/samples/kustomization.yaml` if one exists. If not, samples are applied individually via `kubectl apply -f`.

## Risks / Trade-offs

- [Registry address] The OCIRepository uses `opm-registry:5000` (in-cluster DNS). This only works after `make connect-registry`. → Documented in the sample comments and the local-kind-deployment orchestrator.
- [Namespace] Samples use `default` namespace for simplicity. → Acceptable for POC testing. Production deployments would use dedicated namespaces.
