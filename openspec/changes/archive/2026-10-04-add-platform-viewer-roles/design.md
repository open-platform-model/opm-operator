## Context

`config/rbac/` holds the operator's own roles (manager, leader election, metrics) and four roles it ships for users: the scaffolded `moduleinstance-{admin,editor,viewer}-role` and the deliberately unbound `transformerregistration-admin-role` (0015:D3). `config/rbac/kustomization.yaml` lists them; `config/default` adds the `opm-operator-` name prefix; `task operator:installer` renders `dist/install.yaml`, and the release workflow re-renders it with the digest-pinned image. The operator module (`modules/opm_operator`) reads every role in that kustomization through `hack/operator-module/generate.sh` into `zz_generated_rbac.cue`, and renders the user-facing roles as raw objects from the `adminRoles` list in `components.cue`; its render test pins the exact role set and object count.

Platform and TransformerRegistration are cluster-scoped; ModulePackage and ModuleInstance are namespaced.

## Goals / Non-Goals

**Goals:**

- Three read-only roles in the scaffolded viewer shape, reachable from every install path the operator ships (kustomize, `dist/install.yaml`, the operator module).
- A test that drives the shipped YAML through a real API server's authorizer, so a widened or narrowed role moves the test.

**Non-Goals:**

- Editor or admin roles for Platform or ModulePackage. 0030 asks for reads only; writes arrive with the self-service kinds (0027) or the marketplace.
- Changing `transformerregistration-admin-role`, which grants create/update/patch/delete but no get/list/watch. A platform admin who binds it today also needs read; the new viewer role supplies it, and widening the admin role is a separate decision.

## Decisions

### One viewer role per kind, not one combined role

Scaffolded kubebuilder viewer roles are per kind, and `moduleinstance-viewer-role` already sets that pattern. Per kind also matches scope: a RoleBinding to a ClusterRole only grants the namespaced kinds it names, so an administrator binds `modulepackage-viewer-role` per namespace and the two cluster-scoped roles with a ClusterRoleBinding. A combined role would read the same under a ClusterRoleBinding but would mix a namespace-bindable grant with two that a RoleBinding silently cannot deliver.

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  labels:
    app.kubernetes.io/name: opm-operator
    app.kubernetes.io/managed-by: kustomize
  name: platform-viewer-role
rules:
- apiGroups: [opmodel.dev]
  resources: [platforms]
  verbs: [get, list, watch]
- apiGroups: [opmodel.dev]
  resources: [platforms/status]
  verbs: [get]
```

### `get` on status, not `get`, `list`, `watch`

The API server serves only `get`, `update` and `patch` on a `status` subresource; list and watch requests go to the main resource and return status with it. `list` or `watch` on `*/status` would be dead rules. The scaffolded viewer shape grants exactly `get` there, and so do these roles.

## Research & Decisions

### Aggregation into the built-in `view` role

**Context**: The task asked to decide, with evidence, whether the roles carry `rbac.authorization.k8s.io/aggregate-to-view`.
**Explored**: 0030's question register lists OQ6 ("do the operator's viewer roles aggregate into the built-in `view` role?") as `Status: open`, `Blocking: acceptance`; 0030:D11 itself defers it to OQ6. The mechanics: `view` is most often bound per namespace with a RoleBinding, which cannot grant cluster-scoped kinds, so aggregating Platform and TransformerRegistration reads reaches only holders of a cluster-wide `view` binding, while aggregating ModulePackage reads hands every namespace viewer that namespace's packages (source reference, path, ServiceAccount name, inventory, history). The operator's existing roles carry no aggregation label (read from `config/rbac/`).
**Decision**: Ship without any `aggregate-to-*` label.
**Rationale**: The question is open and owned by the enhancement, not by this change. The two choices are not symmetric: adding the label later is a pure grant and needs no migration, while removing it later silently withdraws access from users who came to rely on it. Shipping unaggregated is the side that can be revisited.

### Where the aggregation guarantee is tested

**Context**: envtest runs an API server without kube-controller-manager, and the clusterrole-aggregation controller lives there, so the built-in `view` role is never populated in envtest.
**Explored**: An authorizer test of "a `view`-bound user cannot list Platforms" would pass in envtest whether or not the label were present.
**Decision**: The test asserts the absence of `aggregate-to-*` labels on the shipped YAML directly; the authorizer tests cover what bound and unbound subjects may do.
**Rationale**: A test that cannot fail measures nothing; the label is the whole mechanism.

## Reconcile phase impact

None. Source, Render, Apply, Prune and Status are untouched: the controller neither uses nor reconciles these roles, and the manager role is unchanged.

## Risks / Trade-offs

- [The operator module's render test pins the role set and count] → updated in the same section as the kustomization, since `task operator-module:drift` and the render test fail on the gap otherwise.
- [An administrator binds `platform-viewer-role` with a RoleBinding and sees no effect] → the install page says which binding each role needs.
- [ModulePackage status carries inventory and history] → no Secret data or values live there; reading it is the purpose of the role, and a bound subject could read the inventoried objects' names through the same namespace anyway.

## Migration Plan

Additive. Upgrading adds three ClusterRoles; rollback deletes them and any binding an administrator made to them stops granting anything.
