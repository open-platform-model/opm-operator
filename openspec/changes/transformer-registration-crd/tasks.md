# Tasks: transformer-registration-crd

Three sections. design.md carries no unverified assumption about this repo; its two research entries record what was read rather than what must still be proven, so section 1 is not a spike.

## 1. API type and generated manifests

- [x] 1.1 Add `api/v1alpha1/transformerregistration_types.go` per design.md § The type: cluster scope, status subresource, the CEL name rule, print columns, `Spec` with `Catalog`, `Version`, `Provides` and `ProviderRef` all required, `Status` with `Conditions`, `Accepted` and `Active`. Follow the house conventions exactly — `omitzero` on `metadata`/`status`, `+listType=map` conditions, hand-written `GetConditions`/`SetConditions`, and the per-type `init()` scheme registration copied from `platform_types.go`, not from `kubebuilder create api`. Verify: `go build ./...` passes and the file ends with a `SchemeBuilder.Register` block.
- [x] 1.2 Add `ProviderReference` to `api/v1alpha1/common_types.go` unless an existing reference type in that file fits verbatim; if one does, use it and say so in the type's doc comment. Verify: no second namespace/name reference struct is introduced.
- [x] 1.3 `task dev:manifests dev:generate`, then add `- bases/opmodel.dev_transformerregistrations.yaml` to `config/crd/kustomization.yaml` above the scaffold marker. Verify: the generated CRD carries `scope: Cluster`, the four required spec fields, the status subresource and the CEL rule; `zz_generated.deepcopy.go` gains the three new deepcopy funcs.
- [x] 1.4 `task dev:manifests dev:generate`, then `task dev:fmt dev:vet dev:lint dev:test` green, then commit `feat(api): add the cluster-scoped TransformerRegistration CRD`.

## 2. RBAC

- [ ] 2.1 Add `+kubebuilder:rbac` markers for `transformerregistrations` and `transformerregistrations/status` (get;list;watch and the status verbs) beside the existing Platform markers in `internal/controller/platform_controller.go`, then regenerate. Verify: `config/rbac/role.yaml` gains the rules and is not hand-edited.
- [ ] 2.2 Add `config/rbac/transformerregistration_admin_role.yaml` granting create, update, patch and delete on the kind, and list it in `config/rbac/kustomization.yaml`. Do NOT add a binding. Verify: `task operator:installer` renders `dist/install.yaml` containing the ClusterRole and no ClusterRoleBinding for it.
- [ ] 2.3 Doc-comment the role file with why it ships unbound (design.md § RBAC ships the platform-admin half only), so a later reader does not add a binding as a fix. Verify: the file states that binding it is the cluster administrator's decision.
- [ ] 2.4 `task dev:manifests dev:generate`, then `task dev:fmt dev:vet dev:lint dev:test` green, then commit `feat(rbac): ship the platform-admin role for transformer registrations`.

## 3. Admission tests

- [ ] 3.1 Add `test/integration/crdvalidation/transformerregistration_test.go` following `skewpolicy_test.go`: apply an object shaped exactly as the `catalog_opm` renderer emits and assert acceptance. Take the spec fields, labels and the dot-joined name from issue 132's recorded JSON, but `apiVersion: opmodel.dev/v1alpha1` from design.md's group decision — the issue's JSON records the prefixed group the catalog is being corrected away from. Verify: the test passes against envtest over `config/crd/bases`.
- [ ] 3.2 Assert the refusals: a claim with no `spec.catalog` is rejected naming the field; a claim named without a dot is rejected with the CEL message; a claim carrying `metadata.namespace` is rejected as cluster-scoped. Verify: each failing case asserts the API server's message, not just that an error occurred.
- [ ] 3.3 Assert the accepted edge: `spec.provides` as an empty list is accepted. Verify: the test names why in a comment, so it is not "fixed" later into a refusal.
- [ ] 3.4 `task dev:fmt dev:vet dev:lint dev:test` green, then commit `test(crdvalidation): cover the TransformerRegistration admission rules`.
