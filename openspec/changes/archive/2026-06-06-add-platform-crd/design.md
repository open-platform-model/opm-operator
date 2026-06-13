## Context

The operator now owns a long-lived library `*kernel.Kernel` (`wire-library-kernel`)
but has no `#Platform` to feed `Materialize`. Enhancement 0001 §8 locked the
source design: one global, cluster-scoped, admin-owned `Platform` CRD named
`cluster`, inert until applied. This slice lands the **types only** — the typed
contract the next slice's reconciler reads.

Projection targets, confirmed against source:

- core `core/src/platform.cue`: `#Platform` = `{ kind, metadata{name!, description?, labels?, annotations?}, type!: string, #registry: [Path=#ModulePathType]: #Subscription, #composedTransformers?, #matchers? }`. `#Subscription = { enable: bool | *true, filter?: #SubscriptionFilter }`. `#SubscriptionFilter = { range?: string, allow?: [...#VersionType], deny?: [...#VersionType] }`. The `#`-prefixed fields (`#registry`, `#composedTransformers`, `#matchers`) are CUE-hidden / kernel-filled and are NOT spec input the CRD projects beyond `#registry`'s author-facing shape.
- library `opm/helper/synth/platform.go`: `PlatformInput{ Name, Type, SchemaCache, Description, Labels, Annotations, Subscriptions map[string]SubscriptionSpec }`; `SubscriptionSpec{ Enable *bool, Filter *FilterSpec }`; `FilterSpec{ Range string, Allow []string, Deny []string }`.

The CRD spec mirrors the author-facing surface (`type` + `registry` subscriptions);
the reconciler slice maps it onto `PlatformInput`.

## Goals / Non-Goals

**Goals:**

- A cluster-scoped singleton `Platform` CRD whose spec is a faithful, webhook-free
  projection of `#Platform`'s author surface and maps 1:1 onto `synth.PlatformInput`.
- Status shape ready for a `Materialized` condition + `observedGeneration`.
- Generated CRD/RBAC/deepcopy + a `cluster` sample, with the singleton CEL rule
  exercised by a test.

**Non-Goals:**

- Any reconciler/controller for `Platform` (next slice).
- `SynthesizePlatform`/`Materialize` calls, materialize cache, release gating.
- Projecting kernel-filled fields (`#composedTransformers`, `#matchers`) — those
  exist on the materialized twin, not the authored CR.
- Touching the legacy `catalog-provider-loading` path.

## Decisions

### CEL root rule for the singleton, not a webhook

**Decision:** `+kubebuilder:validation:XValidation:rule="self.metadata.name == 'cluster'"`
on the `Platform` type, plus cluster scope.

**Rationale:** §8.1 wants "one global Platform per cluster" with no webhook
overhead. `metadata.name` is the one metadata field a CEL root rule may read;
cluster scope + a fixed name ⇒ at most one object, enforced by the API server.
The repo has no existing CEL pattern, so this also sets the convention.

**Alternatives considered:** admission webhook (more moving parts, cert management
— rejected per Principle VII); controller-side reconcile-only-`cluster` defense
(kept as defense-in-depth in the reconciler slice, but insufficient alone — it
wouldn't reject the object at apply time).

### Spec mirrors the schema; no operator-invented fields

**Decision:** `PlatformSpec` = `{ Type string, Registry map[string]Subscription }`;
`Subscription = { Enable *bool, Filter *Filter }`; `Filter = { Range string, Allow []string, Deny []string }`. `Enable` is a pointer so "omitted ⇒ schema default true" survives JSON round-trip.

**Rationale:** A 1:1 projection keeps the reconciler's spec→`PlatformInput`
mapping mechanical and keeps the CRD honest to `#Platform`. `*bool` is required to
distinguish "unset" (defer to schema) from explicit `false`, matching
`SubscriptionSpec.Enable *bool`.

**Alternatives considered:** a non-pointer `Enable bool` defaulting to false
(loses the schema-default semantic — rejected); embedding raw CUE/JSON for the
registry (opaque, unvalidated — rejected; concrete types per repo style).

### Status mirrors existing CRDs

**Decision:** `conditions []metav1.Condition` (list-map by `type`) +
`observedGeneration`, with `GetConditions`/`SetConditions` accessors, matching
`ModuleRelease`/`Release`.

**Rationale:** Consistency with repo conventions and the Flux conditions helpers
already used; the reconciler slice sets `Materialized` without new status
plumbing.

## Risks / Trade-offs

- **CEL rule not enforced on older clusters** (XValidation needs K8s ≥ 1.25/1.29
  for some features) → the rule used (`self.metadata.name == 'cluster'`) is basic
  CEL supported on all currently targeted versions; the reconciler also reconciles
  only `cluster` as defense-in-depth (next slice).
- **`*bool` ergonomics in YAML** (users may be surprised omitted ⇒ true) → matches
  the schema default exactly and is documented on the field; the sample shows the
  common case.
- **Adding a kind with no reconciler looks incomplete** → intentional and
  spec'd; the resource is inert by design until the reconciler slice, mirroring
  §8.1 "inert until the Platform CR exists."

## Migration Plan

1. Author `api/v1alpha1/platform_types.go` (types + markers + condition accessors).
2. Register in `groupversion_info.go` SchemeBuilder.
3. `task dev:manifests dev:generate` → CRD base, RBAC, deepcopy.
4. Add `config/samples` `Platform` named `cluster`.
5. Validation gates + a test asserting the CEL singleton (accept `cluster`, reject
   other names) via envtest.

**Rollback:** revert the commit; the kind is additive and unreferenced by any
controller, so removal is clean.

## Open Questions

- Should the CRD live in the existing `releases.opmodel.dev` group or a new
  `platform.opmodel.dev` group? Default to `releases.opmodel.dev/v1alpha1` (the
  existing group) for now to avoid a second group+scheme; revisit if a platform
  API group is later warranted. Confirm during implementation.
