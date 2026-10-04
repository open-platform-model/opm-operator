## Why

The operator ships one role a cluster administrator can bind for reading OPM state, `moduleinstance-viewer-role`, and it covers ModuleInstances only. A non-admin can read no Platform, ModulePackage or TransformerRegistration through any role the operator ships, so every installation that wants a read-only user (the OPM portal's milestone 2 first among them) has to hand-write the same three rules. Enhancement 0030 asks the operator to ship them (0030:D11:R4), and the portal's milestone 2 cannot show a non-admin the Platform, ModulePackages or registrations until an operator release carries them.

## What Changes

- Three new ClusterRoles in `config/rbac/`, following the scaffolded per-kind viewer pattern of `moduleinstance-viewer-role`: `platform-viewer-role`, `modulepackage-viewer-role` and `transformerregistration-viewer-role` (installed as `opm-operator-*`). Each grants `get`, `list` and `watch` on its kind and `get` on its `status` subresource, and nothing else.
- All three ship **unbound**, like `transformerregistration-admin-role`: binding them is the cluster administrator's decision. One role per kind lets an administrator bind ModulePackage reads per namespace with a RoleBinding while Platform and TransformerRegistration reads, which are cluster-scoped, need a ClusterRoleBinding.
- None of them carries an `rbac.authorization.k8s.io/aggregate-to-*` label. Whether OPM viewer roles aggregate into the built-in `view` role is 0030:OQ6, open and acceptance-blocking; adding the label later is additive, removing it later withdraws access, so the change ships the reversible side.
- `config/rbac/kustomization.yaml`, the committed `dist/install.yaml`, the operator module's generated RBAC data and its raw-objects component carry the three roles; the install page names them.

Not in this change: the in-cluster portal's own ClusterRole (opm-portal ships it), any change to `moduleinstance-*` roles, an aggregation decision, and the existing `transformerregistration-admin-role`.

SemVer class: MINOR (new shipped manifests, nothing existing changes). During beta it ships as the next `1.0.0-beta.N`.

## Capabilities

### New Capabilities

- `viewer-roles`: the read-only ClusterRoles the operator ships for users to bind, what they grant, that they ship unbound and that they do not aggregate into built-in roles.

### Modified Capabilities

- `operator-module`: the operator module renders the three new roles with the other unbound administrator roles, so the object count, the raw-objects role list and the fixed-name list change.

## Impact

- `config/rbac/` (three new files, kustomization), `dist/install.yaml`, `modules/opm_operator/` (`components.cue`, regenerated `zz_generated_rbac.cue`, README), `test/integration/` (role test, operator-module render counts), `docs/site/start/install-the-operator.md`.
- No API type, controller, reconcile phase or manager RBAC changes. The controller does not use these roles.
- Affected kinds: Platform, ModulePackage, TransformerRegistration (read access only).
