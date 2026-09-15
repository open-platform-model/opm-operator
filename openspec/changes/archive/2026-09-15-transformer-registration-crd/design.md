# Design: transformer-registration-crd

## Context

See `proposal.md` § Why, and `open-platform-model/opm-operator` issue 132 for the catalog-side contract this must match. The decisions are `enhancements/0015` D3 (the CR and its RBAC gate), D12 (the instance-derived name) and D15 (one registration per module); the pre-drafted shapes are that entry's `contracts/contracts.cue`.

Current state, read 2026-09-15:

- `api/v1alpha1/` holds three types in group `opmodel.dev`, version `v1alpha1`. `groupversion_info.go` uses `runtime.NewSchemeBuilder`, and **each type file registers itself in its own `init()`** (`platform_types.go`, `modulepackage_types.go`, `moduleinstance_types.go` each end with a `SchemeBuilder.Register(func(scheme *runtime.Scheme) error { scheme.AddKnownTypes(GroupVersion, …) })` block). `kubebuilder create api` would scaffold the wrong shape.
- House conventions: `omitzero` (not `omitempty`) on `metadata`/`status`, `Spec` marked `+required`, conditions as `[]metav1.Condition` with `+listType=map` / `+listMapKey=type`, and hand-written `GetConditions`/`SetConditions` so the Flux `conditions.Setter` interface is satisfied.
- `config/crd/bases/` holds exactly three files and `config/crd/kustomization.yaml` lists them by hand above the scaffold marker.
- `config/rbac/` ships the manager role (generated from `+kubebuilder:rbac` markers), leader-election and metrics roles, and three unused kubebuilder-scaffolded `moduleinstance_*_role.yaml` helpers. **There is no tenant or platform-admin role.** `docs/TENANCY.md`, cited by `CLAUDE.md`, does not exist.
- Rendered objects reach the cluster through an impersonated client built in `internal/apply/impersonate.go` from `spec.serviceAccountName`, the `--default-service-account` flag, or the controller identity. That impersonation is the whole of D3's gate.
- `test/integration/crdvalidation/` already asserts CRD markers against a real API server (`skewpolicy_test.go` spins its own envtest over `config/crd/bases`), which is the precedent for admission tests.

## Goals / Non-Goals

**Goals**

- The kind exists, is cluster-scoped, and carries `apiVersion: opmodel.dev/v1alpha1`, the literals the catalog renderer is corrected to emit.
- The API server refuses a malformed claim on its own terms, without trusting the authoring catalog.
- D3's RBAC gate becomes a shipped artifact rather than an assumption about the cluster's own roles.

**Non-Goals**

- Any reconciler for the kind. Acceptance, activation, the finalizer and regeneration are the three follow-on changes named in the proposal.
- `Platform.status.registry`, which belongs with regeneration.
- The readiness exclusion (D14): there is nothing to exclude from yet (see Risks).
- Fixing `PROJECT`, already stale and marked do-not-edit.

## Decisions

### The type

`api/v1alpha1/transformerregistration_types.go`, following `platform_types.go` exactly:

```go
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=treg
// +kubebuilder:validation:XValidation:rule="self.metadata.name.contains('.')",message="name must be the dot-joined <namespace>.<name> of the claiming instance"
// +kubebuilder:printcolumn:name="Catalog",type=string,JSONPath=".spec.catalog"
// +kubebuilder:printcolumn:name="Version",type=string,JSONPath=".spec.version"
// +kubebuilder:printcolumn:name="Accepted",type=string,JSONPath=".status.accepted"
// +kubebuilder:printcolumn:name="Active",type=string,JSONPath=".status.active"
type TransformerRegistration struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitzero"`
	// +required
	Spec   TransformerRegistrationSpec   `json:"spec"`
	// +optional
	Status TransformerRegistrationStatus `json:"status,omitzero"`
}

type TransformerRegistrationSpec struct {
	// +required
	// +kubebuilder:validation:MinLength=1
	Catalog string `json:"catalog"`
	// +required
	// +kubebuilder:validation:MinLength=1
	Version string `json:"version"`
	// +required
	Provides []string `json:"provides"`
	// +required
	ProviderRef ProviderReference `json:"providerRef"`
}

type TransformerRegistrationStatus struct {
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// +optional
	Accepted bool `json:"accepted,omitzero"`
	// +optional
	Active bool `json:"active,omitzero"`
}
```

`Provides` is `+required` but MAY be empty: a provider catalog that implements no provider-fulfilled contract is a claim acceptance refuses on its merits, not a malformed object. The distinction matters because the CRD is the wrong place to encode a rule the reconciler states better.

`ProviderReference` is a two-field namespace/name struct. `common_types.go` already carries reference types; this change adds one there rather than inventing a parallel shape, unless an existing one fits verbatim.

### The group is the operator's flat `opmodel.dev`, and the catalog moves to it

The kind lands in `api/v1alpha1` alongside the other three types, so its group is
`opmodel.dev` and the generated manifest is `config/crd/bases/opmodel.dev_transformerregistrations.yaml`.

`catalog_opm` `opm` 4.3.0 renders `apiVersion: opm.opmodel.dev/v1alpha1`, a different
group, and issue 132 records that spelling as the literal to match. It is wrong on the
operator's side of the contract, not a constraint on it: enhancement 0002 D5 moved this
repo from `releases.opmodel.dev` to a flat, kind-agnostic `opmodel.dev` and explicitly
rejected kind-specific and prefixed groups, at the cost of a full cluster migration.
Honouring the catalog's literal would reopen that decision and add a second API group
package, a second scheme registration and an ADR, to avoid changing two lines.

No enhancement decision pins the group. D3, D9 and D12 name the kind, its scope, its
RBAC gate and its name shape, and none names a group; the prefixed spelling entered
through the pre-drafted shape at `0015/contracts/contracts.cue:145`. So the catalog's
renderer and its golden fixture move to `opmodel.dev/v1alpha1` in a change of their own,
and issue 132's recorded JSON is corrected with it. The two sides are independent and no
module renders a registration today, so neither repo waits on the other.

### The name rule is a CEL rule, not a regex

D12's name is `<namespace>.<name>`, both DNS labels. A regex spelling the full grammar would duplicate Kubernetes' own name validation and drift from it. The CEL rule asserts the load-bearing half, that the name contains a dot, and the API server's existing object-name validation covers the rest. The `Platform` singleton rule is the precedent: a CEL `XValidation` on the resource root, no webhook.

### RBAC ships the platform-admin half only

`config/rbac/transformerregistration_admin_role.yaml` (new) grants `create`, `update`, `patch`, `delete` on `transformerregistrations`, listed in `config/rbac/kustomization.yaml`. The operator's manager role gains `get;list;watch` and the status verbs through `+kubebuilder:rbac` markers on the Platform controller, regenerated by `task dev:manifests`.

Nothing is bound. The gate works by absence: a tenant ServiceAccount holds only what a cluster admin gave it, and this repo ships no role granting create to tenants. Shipping the admin role gives an administrator something to bind deliberately; shipping a binding would decide the cluster's tenancy model for it.

### The CRD lands without a reconciler

An applied claim is stored, its status stays empty, and nothing acts on it. This is a deliberate intermediate state: the alternative, holding the CRD back until acceptance is written, makes the first provider module's apply fail on an unknown kind and couples two changes that are otherwise independent. `Platform`'s own history is the precedent — its CRD shipped before its reconciler, and the spec still records that step.

## Research & Decisions

### The readiness exclusion D14 asks for has nothing to exclude from

**Context**: D14 says the operator's wait-set aggregation must skip registration CRs by kind, or the package deadlocks: kstatus treats a CR carrying conditions as InProgress, and the registration's activation waits on the package being Ready.
**Explored**: `internal/reconcile/modulepackage.go` and `internal/apply/`. `ModulePackage.Ready` is set from the reconcile outcome alone; there is no per-object readiness aggregation. The only waiting is inside Flux's `ApplyAllStaged`, which calls `WaitForSet` on the cluster-definition and class stages only, never on the general resource stage.
**Decision**: Out of scope here, and recorded rather than silently skipped.
**Rationale**: the deadlock D14 predicts is not reachable today because the wait it depends on does not exist. It becomes reachable the moment anyone adds a general-stage wait, so the exclusion belongs with the change that introduces one, or with `registration-driven-regeneration`, whichever comes first. Writing a filter now would be dead code guarding a hazard the codebase cannot produce.

### D3's RBAC gate is currently an assumption, not an artifact

**Context**: D3 rests on "a tenant role does not carry create on a cluster-scoped resource".
**Explored**: all twelve files in `config/rbac/`, plus the impersonation path in `internal/apply/impersonate.go`. There is no tenant role and no platform-admin role in this repo; `docs/TENANCY.md` is cited by `CLAUDE.md` but absent.
**Decision**: ship the platform-admin ClusterRole in this change and state the gate's real shape in the spec.
**Rationale**: the decision's security property is only as good as the roles a cluster actually has. Shipping the role is the smallest thing that turns the claim into something an administrator can point at, without this repo deciding a tenancy model it has not otherwise expressed.

## Risks / Trade-offs

- [An inert CRD invites a hand-applied claim that is silently ignored] → the status subresource exists from day one and stays empty, which is visible in `kubectl get`; the follow-on change fills it. No module renders a claim today, so the reachable case is a deliberate hand-apply.
- [The CRD's literals drift from the catalog renderer's] → they are asserted in the spec and both sides are `v1alpha1`. The integration test applies an object shaped exactly as the renderer emits, so drift fails a test rather than a cluster.
- [Shipping an unbound ClusterRole looks like a no-op] → it is deliberate: binding it is the administrator's decision. The spec says so, so a later reader does not "fix" it with a binding.
- [`spec.provides` accepts an empty list] → intentional; the refusal belongs to acceptance, where it can name the catalog it re-derived from.

## Migration Plan

Three sections in one PR, squash title `feat(api): add the cluster-scoped TransformerRegistration CRD`. release-please cuts a minor. Rollback before release is a revert; after release, an operator upgrade that removes the CRD would orphan stored claims, so a later change would delete the kind deliberately rather than by rollback. No existing object or behaviour changes.

## Open Questions

None.
