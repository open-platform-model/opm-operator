## ADDED Requirements

### Requirement: SourceRefGrant CRD
The operator MUST define a namespaced `SourceRefGrant` custom resource in the `releases.opmodel.dev` API group. A `SourceRefGrant` lives in the namespace that owns the source being referenced and declares which referrers may reference which source objects within that namespace. Its spec MUST contain a `from` list of referrer descriptors (`group`, `kind`, `namespace`) and a `to` list of referent descriptors (`group`, `kind`, optional `name`). The CRD MUST be additive and permissive only — it grants access and never denies it; absence of a matching grant is a denial.

#### Scenario: Grant is namespaced and lives with the source
- **WHEN** an administrator authors a `SourceRefGrant`
- **THEN** it is created in the namespace of the source object being shared, and its `from` entries name the referrer namespaces permitted to reference into that namespace

#### Scenario: Optional referent name
- **WHEN** a `to` entry omits `name`
- **THEN** the grant matches every object of the given `group`/`kind` in the grant's namespace; **WHEN** `name` is set, only the object with that exact name matches

### Requirement: Admin master switch
The controller MUST expose a `--allow-cross-namespace-source-refs` flag defaulting to `false`. When the flag is `false`, the cross-namespace grant mechanism MUST be inert: every cross-namespace source reference is denied regardless of any `SourceRefGrant` present in the cluster.

#### Scenario: Mechanism disabled by default
- **WHEN** the controller runs without `--allow-cross-namespace-source-refs` (or with it set to `false`)
- **THEN** a cross-namespace source reference is denied even if a matching `SourceRefGrant` exists

#### Scenario: Mechanism enabled
- **WHEN** the controller runs with `--allow-cross-namespace-source-refs=true`
- **THEN** cross-namespace references are evaluated against `SourceRefGrant` objects in the target namespace

### Requirement: Grant-matching policy permits a reference
When the master switch is enabled, a cross-namespace reference from a release of kind `K_from` in namespace `N_from` to a source of kind `K_to` named `S` in namespace `N_to` MUST be permitted if and only if some `SourceRefGrant` in `N_to` has a `from` entry matching (`group=releases.opmodel.dev`, `kind=K_from`, `namespace=N_from`) AND a `to` entry matching (`group=source.toolkit.fluxcd.io`, `kind=K_to`, and either no `name` or `name=S`). Evaluation MUST be performed on every reconcile so that deleting a grant revokes access on the next pass.

#### Scenario: Matching grant permits the reference
- **WHEN** the flag is enabled and a `SourceRefGrant` in the target namespace matches both the referrer and the referent
- **THEN** the policy permits the reference and `Resolve` reads the foreign source

#### Scenario: No matching grant denies the reference
- **WHEN** the flag is enabled but no `SourceRefGrant` in the target namespace matches both the referrer and the referent
- **THEN** the policy denies the reference and `Resolve` returns `ErrCrossNamespaceForbidden`

#### Scenario: Revocation takes effect on next reconcile
- **WHEN** a previously matching `SourceRefGrant` is deleted
- **THEN** the next reconcile of an affected Release denies the cross-namespace reference and the release stalls

### Requirement: Controller reads grants via cached informer
The controller MUST have cluster-wide read-only RBAC (`get;list;watch`) on `sourcerefgrants` and MUST evaluate grants from a cached informer rather than issuing a live API read per resolution, to keep grant evaluation off the hot API path.

#### Scenario: Grant lookup is cache-backed
- **WHEN** the policy evaluates a cross-namespace reference
- **THEN** it reads `SourceRefGrant` objects from the controller's cache, and the controller's RBAC grants only `get;list;watch` on `sourcerefgrants`
