## Why

The controller's ClusterRole grants `impersonate` on `users`, `groups` and `serviceaccounts`, cluster-wide and unrestricted. With that role the controller can act as any user in any group, `system:masters` included, so a compromised controller is a cluster admin whatever the tenants' ServiceAccounts are allowed to do. The code only ever acts as a ServiceAccount in the instance's own namespace: `users` is used nowhere, and `groups` is needed only because the controller sends three group names on every impersonated request.

Those group names do not need to be sent. For an impersonated ServiceAccount user with no group named, the Kubernetes apiserver adds the ServiceAccount's own groups itself. The main spec says the opposite; an envtest run on Kubernetes 1.34, 1.35 and 1.36 shows the spec is wrong (design.md, "Research & Decisions").

## What Changes

- The controller stops sending `Impersonate-Group` headers. It names the ServiceAccount user only; the apiserver derives `system:serviceaccounts`, `system:serviceaccounts:<namespace>` and `system:authenticated`.
- The ClusterRole loses `impersonate` on `users` and on `groups`. It keeps `get` and `impersonate` on `serviceaccounts`. This holds in every RBAC source: the kubebuilder marker, `config/rbac/role.yaml`, the operator module's `zz_generated_rbac.cue` and `dist/install.yaml`.
- An envtest suite runs the apply path as a user that holds exactly the rules of the shipped `config/rbac/role.yaml`. It proves that an apply as a ServiceAccount succeeds, that a binding to `system:serviceaccounts:<namespace>` still authorises it, and that the role cannot impersonate a user or claim a group.

What an instance's ServiceAccount may do through its own bindings does not change. No API type, flag or status condition changes.

Release class: PATCH after GA (a privilege the controller never needed is removed; no behaviour a user relies on changes). On the beta line it ships as the next `-beta.N`. The commit is `fix(apply)`. Because it regenerates `modules/opm_operator/zz_generated_rbac.cue`, the same commit also reaches the operator module's changelog, as the repo rule for RBAC changes says.

Not in this change: the identity used for drift detection, the `--default-service-account` behaviour, and who may name a ServiceAccount on an instance.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `serviceaccount-impersonation`: the requirement that the controller sends the standard ServiceAccount groups is replaced by one on the identity the apiserver sees (same groups, none sent), and a requirement is added that the controller's role can impersonate ServiceAccounts only.

## Impact

- `internal/apply/impersonate.go`: no groups on the impersonation config.
- `internal/controller/moduleinstance_controller.go`: the `users;groups` RBAC marker is removed.
- Generated, by `task dev:manifests` and `task operator:installer`: `config/rbac/role.yaml`, `modules/opm_operator/zz_generated_rbac.cue`, `dist/install.yaml`.
- Tests: `internal/apply/impersonate_test.go`, a new `test/integration/reconcile/impersonation_rbac_test.go`.
- Affected controllers: ModuleInstance and ModulePackage (both build the impersonated client through the same function). No API type changes.
- Upgrade: an operator upgraded in place gets the narrower ClusterRole with the new image in one apply. An old image under the new role would be refused when it sends groups; a new image under the old role works. Roll back both together.
