## Context

`NewImpersonatedClient` (`internal/apply/impersonate.go`) builds the client that apply, prune and the health read use for an instance with an effective ServiceAccount. It sets `Impersonate-User` to `system:serviceaccount:<ns>:<name>` and, since the change `fix-reconcile-correctness-trio`, three `Impersonate-Group` values. The apiserver authorises each impersonation header against the caller: the user header needs `impersonate` on `serviceaccounts` (for a ServiceAccount user name) or on `users`; each group header needs `impersonate` on `groups`. So the three groups are the only reason for the `groups` grant, and nothing needs the `users` grant.

RBAC can limit `impersonate` on `groups` with `resourceNames`, but a ClusterRole cannot name `system:serviceaccounts:<namespace>` for namespaces that do not exist yet, and a rule without `resourceNames` lets the holder claim `system:masters`.

## Goals / Non-Goals

**Goals:**

- The controller's role MUST hold `impersonate` on `serviceaccounts` only.
- An impersonated apply MUST see the same user and groups as before, so every existing RoleBinding keeps its effect.
- A test MUST fail when the shipped role gains an impersonation right or loses one the apply path needs.

**Non-Goals:**

- Limiting which ServiceAccounts the controller may impersonate (a cluster-wide `serviceaccounts` grant stays; tenancy checks are separate work).
- The identity used by drift detection.

## Decisions

### Send no groups; let the apiserver derive them

The controller MUST set only the user name on the impersonation config. The apiserver's impersonation filter treats a user name of the form `system:serviceaccount:<ns>:<name>` as a ServiceAccount, authorises it as `impersonate` on that `serviceaccounts` object, and, when the request names no group, sets the groups a token of that ServiceAccount carries.

```go
func buildImpersonationConfig(namespace, saName string) rest.ImpersonationConfig {
	return rest.ImpersonationConfig{
		UserName: fmt.Sprintf("system:serviceaccount:%s:%s", namespace, saName),
	}
}
```

Alternative: keep sending groups and narrow the grant with `resourceNames: [system:serviceaccounts, system:authenticated]`. Rejected: the per-namespace group cannot be listed, so bindings to `system:serviceaccounts:<namespace>` would break, and the role would still hold a group right for no gain.

Alternative: keep `groups` unrestricted and remove only `users`. Rejected: it leaves the path to `system:masters` open (a ServiceAccount user plus the group `system:masters`).

### Remove the marker, regenerate everything

The `users;groups` kubebuilder marker is deleted and `serviceaccounts` keeps `get;impersonate`. `task dev:manifests` writes `config/rbac/role.yaml` and the operator module's `zz_generated_rbac.cue`; `task operator:installer` writes `dist/install.yaml`. No generated file is edited by hand.

### Prove it under the shipped role

The existing impersonation specs run as the envtest admin, which may impersonate anything, so they cannot see a missing or an extra grant. The new suite reads `config/rbac/role.yaml`, binds its rules to an envtest user, and runs `NewImpersonatedClient` and `apply.Apply` with that user's credentials:

- apply as a ServiceAccount bound directly: succeeds;
- apply as a ServiceAccount authorised only by a RoleBinding to `system:serviceaccounts:<namespace>`: succeeds;
- apply as a ServiceAccount with no binding: forbidden;
- a `SelfSubjectReview` through the impersonated client returns exactly the three groups;
- impersonating a user, a user with a group, or a ServiceAccount with `system:masters`: forbidden.

### Reconcile phase impact

Source and Render: none. Apply and Prune: same identity and groups on the wire as the apiserver sees them; one header kind fewer per request. Status: unchanged; a refused impersonation still stalls with `ImpersonationFailed`.

## Research & Decisions

### Does an impersonated ServiceAccount get its groups when none are sent?

**Context**: The main spec and a code comment say the apiserver derives no groups from the impersonated user name, which would make the `groups` grant necessary.

**Explored**: Run on envtest with the unchanged role and with the narrowed role, on kube-apiserver 1.34.0, 1.35.0 and 1.36.0. With no group sent and `impersonate` on `serviceaccounts` only: a `SelfSubjectReview` returns `system:serviceaccounts`, `system:serviceaccounts:<ns>` and `system:authenticated`; an apply authorised only by a RoleBinding to `system:serviceaccounts:<ns>` succeeds. With the unchanged role the same user could impersonate `jane` in `system:masters`. The upstream filter (`k8s.io/apiserver` v0.36.4, `pkg/endpoints/filters/impersonation/impersonation.go`, lines 89-90 and 136-143) does this on purpose: for a ServiceAccount user with no group specified it sets `serviceaccount.MakeGroupNames(namespace)`, and it adds `system:authenticated` for every non-anonymous impersonated user.

**Decision**: Send no groups and drop the `groups` grant with the `users` grant.

**Rationale**: It is the least right Kubernetes allows for this path, and the effective identity is the one a ServiceAccount token carries, which is what the old requirement wanted.

## Risks / Trade-offs

- [A future Kubernetes changes the derived groups] → the `SelfSubjectReview` spec and the group-binding spec run on the envtest version the repo pins, so a bump shows it.
- [Version skew during an upgrade: an old image under the new role sends groups and is refused, and instances stall with `ImpersonationFailed`] → the install manifest and the operator module change role and image together; a stalled instance recovers on its next reconcile after the new image runs. Roll back image and role together.
- [The role still impersonates any ServiceAccount in any namespace, `kube-system` included] → out of scope here; stated as a non-goal.
