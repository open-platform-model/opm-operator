## 1. Ship the three viewer roles

- [ ] 1.1 Add `config/rbac/platform_viewer_role.yaml`, `modulepackage_viewer_role.yaml` and `transformerregistration_viewer_role.yaml` in the shape of design.md (scaffold header comment saying the operator does not use the role and it ships unbound; `get`, `list`, `watch` on the kind and `get` on `<kind>/status`; the two `app.kubernetes.io` labels and no `aggregate-to-*` label), and list them in `config/rbac/kustomization.yaml` after the moduleinstance roles with a comment pointing at the unbound rule; verify with `bin/kustomize build config/default | grep -c "viewer-role$"` printing 4.
- [ ] 1.2 Add the three role keys to `adminRoles` in `modules/opm_operator/components.cue` (and its five-roles comment), run `task operator-module:generate`, and update `test/integration/operatormodule/module_test.go` (`adminRoleNames`, the expected keys, the 19-object count to 22) and the README role table; verify with `task operator-module:drift`.
- [ ] 1.3 Add `test/integration/crdvalidation/viewer_roles_rbac_test.go`, reading the shipped YAMLs from `config/rbac/`: no `aggregate-to-*` label and only the verbs of the "Viewer roles grant reads only" requirement; with envtest, an unbound user is refused get/list on the three kinds; after a ClusterRoleBinding to the Platform and TransformerRegistration roles and a RoleBinding in one namespace to the ModulePackage role, the user may get/list/watch each kind (in that namespace for ModulePackages) and get status, is refused ModulePackages in another namespace, and is refused create, update, patch, delete and status update; checked by SubjectAccessReview; verify with `go test ./test/integration/crdvalidation -run TestViewerRolesRBAC`.
- [ ] 1.4 Run `task operator:installer` and confirm the `dist/install.yaml` diff is only the three ClusterRoles.
- [ ] 1.5 `task dev:manifests dev:generate dev:fmt dev:vet dev:lint dev:test` green, then commit `feat(rbac): ship unbound viewer roles for platforms, packages and registrations`

## 2. Document the roles

- [ ] 2.1 In `docs/site/start/install-the-operator.md`, after the ModuleInstance roles, name the three viewer roles, say which binding each needs (ClusterRoleBinding for Platforms and TransformerRegistrations, RoleBinding or ClusterRoleBinding for ModulePackages), that none is bound or aggregated into `view`, with one `kubectl create clusterrolebinding` example; verify with `task docs:bundle:check`.
- [ ] 2.2 `task docs:bundle:check dev:fmt dev:vet dev:lint dev:test` green, then commit `docs(site): name the platform viewer roles on the install page`

## 3. Archive

- [ ] 3.1 Archive the change into the main specs (`viewer-roles` new, `operator-module` modified), verify with `openspec validate --specs --strict` for the two specs, log the delivery to 0030 without decision numbers, then commit `docs(openspec): archive add-platform-viewer-roles`
