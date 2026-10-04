# operator-module Specification

## Purpose
Define the operator's own OPM module, `opmodel.dev/modules/opm_operator`: what it renders, the names and selector every version keeps, how its CRDs and RBAC stay equal to what the controller declares, the Pod Security posture of its pods, and the typed values a platform team tunes the operator with.

## Requirements

### Requirement: The module names the one operator release it deploys

The repository SHALL carry the operator's OPM module at the path `opmodel.dev/modules/opm_operator` on the v0 major, with a version of its own that is independent of the operator binary's. The module's source SHALL name the operator image it deploys by a published operator release's version tag and content digest, and SHALL expose that operator version as a value readable from the module's source without rendering it and without a cluster. Every rendered controller container SHALL run exactly that image, so one module version deploys exactly one operator version.

#### Scenario: The operator version is read without a render

- **WHEN** a tool evaluates the module's operator-version value from the source tree, with no cluster and no instance
- **THEN** it reads one operator release version, such as `1.0.0-beta.5`, and the tag and digest the module renders the image with

#### Scenario: The rendered image matches the named release

- **WHEN** the module is rendered with default values
- **THEN** the controller container's image is `ghcr.io/open-platform-model/opm-operator:v<operator version>@sha256:<digest>` with the version and digest the module names

### Requirement: The module deploys only an operator that refuses its own instance

The operator release the module names SHALL be at or above the repository's recorded minimum operator version, the first operator release that refuses to reconcile the instance deploying the operator. The module's tests SHALL fail, naming both versions, when the module names an older operator release, so no module version can deploy an operator that would adopt, prune or block on its own CLI-owned instance.

#### Scenario: An older operator is refused

- **WHEN** the module's operator version is set below the recorded minimum operator version
- **THEN** the module's tests fail naming the module's operator version and the minimum

### Requirement: Every object renders through a catalog resource

The module SHALL render the operator's CRDs, its Namespace, the controller Deployment, its ServiceAccount, the metrics Service, and every Role, ClusterRole, RoleBinding and ClusterRoleBinding through the first-party catalog resource made for that kind, apart from the exception below. The administrator ClusterRoles the operator ships for users to bind SHALL render with no binding. Those administrator ClusterRoles SHALL render through the catalog's raw-objects resource, in one component that holds them and nothing else, with their rules taken from the generated RBAC data; no other object SHALL use the raw-objects resource. The module SHALL render the operator's Namespace itself, so an install records it as an object of the instance.

#### Scenario: The render contains the whole install shape

- **WHEN** the module is rendered with default values for the instance `opm-operator` in `opm-operator-system`
- **THEN** the result holds 19 objects: 4 CRDs, 1 Namespace, 1 ServiceAccount, 1 Role, 7 ClusterRoles, 1 RoleBinding, 2 ClusterRoleBindings, 1 Service and 1 Deployment

#### Scenario: Raw objects only for the administrator roles

- **WHEN** the module's components are listed
- **THEN** exactly one of them carries the catalog's raw-objects resource, and it renders exactly `opm-operator-metrics-reader`, the three `opm-operator-moduleinstance-*-role` ClusterRoles and `opm-operator-transformerregistration-admin-role`

#### Scenario: Administrator roles render unbound

- **WHEN** the module is rendered
- **THEN** `opm-operator-metrics-reader`, the three `opm-operator-moduleinstance-*-role` ClusterRoles and `opm-operator-transformerregistration-admin-role` render with no binding naming them

### Requirement: Names stay those of the earlier manifest

The module SHALL render the Namespace `opm-operator-system`, the Deployment `opm-operator-controller-manager`, the ServiceAccount `opm-operator-controller-manager`, the Service `opm-operator-controller-manager-metrics-service`, the Role `opm-operator-leader-election-role`, the ClusterRoles `opm-operator-manager-role`, `opm-operator-metrics-auth-role`, `opm-operator-metrics-reader`, `opm-operator-moduleinstance-admin-role`, `opm-operator-moduleinstance-editor-role`, `opm-operator-moduleinstance-viewer-role` and `opm-operator-transformerregistration-admin-role`, and the CRDs `moduleinstances.opmodel.dev`, `modulepackages.opmodel.dev`, `platforms.opmodel.dev` and `transformerregistrations.opmodel.dev`, the names an operator installed from an earlier release's manifest has. Only a role binding MAY take the name the catalog derives from its role.

#### Scenario: The fixed names render

- **WHEN** the module is rendered with default values
- **THEN** every name above appears with its kind, and every namespaced object is in `opm-operator-system`

#### Scenario: Bindings take catalog names

- **WHEN** the module is rendered
- **THEN** the three bindings are named by the catalog from their roles, and each binds its role to the ServiceAccount `opm-operator-controller-manager` in `opm-operator-system`

### Requirement: The module renders only for the operator's fixed instance coordinates

The module SHALL render only for an instance named `opm-operator` in the namespace `opm-operator-system`, and SHALL refuse any other name or namespace with an error naming the expected coordinates, so a cluster holds at most one operator and no instance of the module renders the operator's objects under other names or records them from another namespace.

#### Scenario: Another instance name is refused

- **WHEN** the module is rendered for an instance named `opm` in `opm-system`
- **THEN** the render fails, naming `opm-operator` and `opm-operator-system`, and produces no objects

### Requirement: One Deployment selector across every module version

The controller Deployment's `spec.selector` SHALL be the same in every version of the module: the selector the first module version renders is fixed, and a later version, a catalog or core pin change, or any `#config` value SHALL NOT change it, so moving between module versions never requires deleting the Deployment. The pod template's labels SHALL keep `control-plane: controller-manager`.

#### Scenario: A pin bump would change the selector

- **WHEN** a change to the module or to its catalog or core pins makes the rendered selector differ from the fixed one
- **THEN** the module's tests fail naming the selector difference, before the change can merge

#### Scenario: Values never touch the selector

- **WHEN** the module is rendered with every `#config` field set to a non-default value
- **THEN** the Deployment's selector equals the fixed one

### Requirement: The CRDs are the controller's generated CRDs

The CRDs the module renders SHALL be identical, in `spec` and in the controller-gen version annotation, to the CRD manifests `task dev:manifests` generates from the API types of the same tree. Every CRD field the controller emits SHALL either render or refuse the render; none SHALL be dropped.

#### Scenario: Rendered CRDs equal the generated YAML

- **WHEN** the module is rendered and its CRDs are compared with `config/crd/bases/*.yaml` of the same tree
- **THEN** each CRD's `spec` is equal

#### Scenario: A field the catalog cannot carry refuses

- **WHEN** a generated CRD carries a field the catalog's CRD resource does not accept, such as `spec.conversion`
- **THEN** the render fails naming that field, instead of rendering the CRD without it

### Requirement: The controller's RBAC is the RBAC it declares

The rules of every Role and ClusterRole the module renders SHALL equal the rules of the corresponding role in `config/rbac` of the same tree: `opm-operator-manager-role` the rules `task dev:manifests` generates from the controller's RBAC markers, and the other seven roles the rules of their `config/rbac` files. The module SHALL hold no hand-written rule, and SHALL grant the controller's ServiceAccount no rule beyond those.

#### Scenario: A marker change reaches the module

- **WHEN** a controller RBAC marker gains a verb, `task dev:manifests` regenerates `config/rbac/role.yaml`, and the module is regenerated
- **THEN** the rendered manager ClusterRole carries the new verb and nothing else changed

#### Scenario: A new role in the kustomization reaches the module

- **WHEN** a Role or ClusterRole file is added to the `resources` of `config/rbac/kustomization.yaml` and the module's data are not regenerated
- **THEN** the drift check fails naming the role, with no list of role files to update by hand

### Requirement: Drift between the module and the controller fails the check

A check SHALL regenerate the module's CRD and RBAC data from `config/` and fail with a readable difference when the committed module data differ, and SHALL first fail when `config/` itself differs from what `task dev:manifests` generates. The check SHALL run on every pull request, and SHALL be runnable against the `config/` tree of a given operator release tag, so a module release can refuse to publish a module whose CRDs or RBAC differ from the operator release it deploys.

#### Scenario: A pull request with stale module data fails

- **WHEN** a pull request changes an API type or RBAC marker and does not regenerate the module's data
- **THEN** the check fails, naming the stale file and showing the difference

#### Scenario: Stale generated config fails first

- **WHEN** a pull request changes an API type and does not run `task dev:manifests`
- **THEN** the check fails on `config/` before comparing the module's data

#### Scenario: Checked against an operator release

- **WHEN** the check runs against the operator release tag the module names, and the module's data equal that tag's `config/`
- **THEN** it passes; and when they differ, it fails

### Requirement: The operator's pods satisfy Pod Security restricted

The pod template the module renders SHALL satisfy the Kubernetes Pod Security `restricted` profile with default values and with any `#config` value: `runAsNonRoot: true` and `seccompProfile: RuntimeDefault` at pod level, and on the container `allowPrivilegeEscalation: false`, all capabilities dropped. The pod-level seccomp profile covers the container, as in the earlier manifest's pod template; the container does not repeat it.

#### Scenario: Admitted under restricted enforcement

- **WHEN** a Pod built from the rendered Deployment's pod template is created in a namespace labeled `pod-security.kubernetes.io/enforce: restricted`
- **THEN** the API server admits it

#### Scenario: Admitted with every value set

- **WHEN** the module is rendered with every `#config` field set to a non-default value and a Pod is built from its pod template
- **THEN** the Pod is admitted under `restricted` as well

### Requirement: The controller's pod and Service stay those of the kustomize tree

While `config/manager` and `config/default` still produce the operator's install manifest, the controller Deployment's pod spec and the metrics Service the module renders SHALL equal those of a kustomize build of `config/default`, apart from the image, labels, the selector, the fields the catalog sets to Kubernetes API defaults, and the bindings' names. A test SHALL compare them on every pull request, so the two sources of the install shape cannot drift apart before the module becomes the only one.

#### Scenario: A manifest-only edit fails the test

- **WHEN** a pull request changes the manager container's arguments, probes, environment, volumes or security context in `config/manager` and not in the module
- **THEN** the module's render test fails naming the differing field

### Requirement: The operator's tuning is typed instance values

The module's `#config` SHALL type these values and render each into the controller Deployment: the operator image's repository, the registry mapping the operator resolves modules through (rendered as `--registry`), the default service account the operator applies as (rendered as `--default-service-account`), the controller container's resources, the replica count, and additional controller arguments appended after every typed argument. Each unset optional value SHALL leave its argument out. Defaults SHALL reproduce the earlier manifest's Deployment: one replica, requests `100m` CPU and `256Mi` memory, limits `2` CPU and `4Gi` memory. Each of those four resource quantities SHALL default on its own, so a value that sets one of them keeps the defaults of the others. The memory limit SHALL always resolve to a value, default or given, so the container's `GOMEMLIMIT` is always defined; it SHALL be derived from that memory limit, not set as a value.

#### Scenario: Every value reaches the Deployment

- **WHEN** the module is rendered with a repository, a registry mapping, a default service account, resources, two replicas and two extra arguments
- **THEN** the Deployment runs two replicas of the image from that repository, the container carries `--registry=<mapping>`, `--default-service-account=<name>` and the two extra arguments after them, and the given resources

#### Scenario: The Go memory limit follows the memory limit

- **WHEN** the module is rendered with a memory limit
- **THEN** the container's `GOMEMLIMIT` is the floor of 80 percent of that limit in MiB (`3276MiB` for `4Gi`), so the soft limit never exceeds the hard one

#### Scenario: An unsupported memory unit is refused

- **WHEN** the module is rendered with a memory limit given as a plain number of bytes, such as `4294967296`
- **THEN** the render fails with a message naming the accepted units, `Mi` and `Gi`

#### Scenario: One resources field keeps the other defaults

- **WHEN** the module is rendered with only the memory limit set, to `8Gi`
- **THEN** the container keeps the default CPU limit `2` and the default requests `100m` CPU and `256Mi` memory, and its `GOMEMLIMIT` is `6553MiB`

### Requirement: The configured registry mapping has one place

The registry mapping and the default service account SHALL be settable only through their typed values. The module SHALL refuse extra arguments that set `--registry` or `--default-service-account`, or that override the arguments the module renders itself (`--metrics-bind-address`, `--leader-elect`, `--health-probe-bind-address`), in either flag spelling the operator's flag parser accepts (one dash or two), so the mapping recorded in the instance's values is the one the operator runs with.

#### Scenario: The mapping through extra arguments is refused

- **WHEN** the module is rendered with an extra argument `--registry=example.com`
- **THEN** the render fails, naming the argument and the typed value to use instead

#### Scenario: The single-dash spelling is refused too

- **WHEN** the module is rendered with an extra argument `-registry=example.com`
- **THEN** the render fails the same way

### Requirement: A mirror needs only the repository value

Setting the image repository value SHALL change only the repository of the controller image: the rendered image SHALL keep the tag and digest the module names.

#### Scenario: A mirrored repository

- **WHEN** the module is rendered with the repository `registry.internal/opm/opm-operator`
- **THEN** the image is `registry.internal/opm/opm-operator:v<operator version>@sha256:<digest>`

### Requirement: The image's tag and digest are not values

The module's `#config` SHALL NOT accept an image tag, digest or pull policy, so no recorded value can keep an earlier release's image when the instance moves to another module version.

#### Scenario: A tag value is refused

- **WHEN** the module is rendered with a value setting the image tag
- **THEN** the render fails, the field not being allowed

#### Scenario: A digest value is refused

- **WHEN** the module is rendered with a value setting the image digest
- **THEN** the render fails, the field not being allowed

### Requirement: The module renders against its own pins with no cluster

The module SHALL render to the same objects against a platform generated from its own dependency pins, with no cluster and no Platform, for the same values. A test SHALL render it that way on every pull request.

#### Scenario: Offline render

- **WHEN** the module is rendered against a platform generated from the catalog release its `cue.mod` pins, with no kubeconfig
- **THEN** the render succeeds and yields the 19 objects
