## MODIFIED Requirements

### Requirement: Every object renders through a catalog resource

The module SHALL render the operator's CRDs, its Namespace, the controller Deployment, its ServiceAccount, the metrics Service, and every Role, ClusterRole, RoleBinding and ClusterRoleBinding through the first-party catalog resource made for that kind, apart from the exception below. The administrator ClusterRoles the operator ships for users to bind, its viewer roles among them, SHALL render with no binding. Those administrator ClusterRoles SHALL render through the catalog's raw-objects resource, in one component that holds them and nothing else, with their rules taken from the generated RBAC data; no other object SHALL use the raw-objects resource. The module SHALL render the operator's Namespace itself, so an install records it as an object of the instance.

#### Scenario: The render contains the whole install shape

- **WHEN** the module is rendered with default values for the instance `opm-operator` in `opm-operator-system`
- **THEN** the result holds 22 objects: 4 CRDs, 1 Namespace, 1 ServiceAccount, 1 Role, 10 ClusterRoles, 1 RoleBinding, 2 ClusterRoleBindings, 1 Service and 1 Deployment

#### Scenario: Raw objects only for the administrator roles

- **WHEN** the module's components are listed
- **THEN** exactly one of them carries the catalog's raw-objects resource, and it renders exactly `opm-operator-metrics-reader`, the three `opm-operator-moduleinstance-*-role` ClusterRoles, `opm-operator-platform-viewer-role`, `opm-operator-modulepackage-viewer-role`, `opm-operator-transformerregistration-viewer-role` and `opm-operator-transformerregistration-admin-role`

#### Scenario: Administrator roles render unbound

- **WHEN** the module is rendered
- **THEN** `opm-operator-metrics-reader`, the three `opm-operator-moduleinstance-*-role` ClusterRoles, `opm-operator-platform-viewer-role`, `opm-operator-modulepackage-viewer-role`, `opm-operator-transformerregistration-viewer-role` and `opm-operator-transformerregistration-admin-role` render with no binding naming them

### Requirement: Names stay those of the earlier manifest

The module SHALL render the Namespace `opm-operator-system`, the Deployment `opm-operator-controller-manager`, the ServiceAccount `opm-operator-controller-manager`, the Service `opm-operator-controller-manager-metrics-service`, the Role `opm-operator-leader-election-role`, the ClusterRoles `opm-operator-manager-role`, `opm-operator-metrics-auth-role`, `opm-operator-metrics-reader`, `opm-operator-moduleinstance-admin-role`, `opm-operator-moduleinstance-editor-role`, `opm-operator-moduleinstance-viewer-role`, `opm-operator-platform-viewer-role`, `opm-operator-modulepackage-viewer-role`, `opm-operator-transformerregistration-viewer-role` and `opm-operator-transformerregistration-admin-role`, and the CRDs `moduleinstances.opmodel.dev`, `modulepackages.opmodel.dev`, `platforms.opmodel.dev` and `transformerregistrations.opmodel.dev`, the names the operator's own install manifest gives them. Only a role binding MAY take the name the catalog derives from its role.

#### Scenario: The fixed names render

- **WHEN** the module is rendered with default values
- **THEN** every name above appears with its kind, and every namespaced object is in `opm-operator-system`

#### Scenario: Bindings take catalog names

- **WHEN** the module is rendered
- **THEN** the three bindings are named by the catalog from their roles, and each binds its role to the ServiceAccount `opm-operator-controller-manager` in `opm-operator-system`
