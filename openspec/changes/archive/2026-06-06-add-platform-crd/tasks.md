## 1. API types

- [x] 1.1 Create `api/v1alpha1/platform_types.go`: `Platform`, `PlatformList`, `PlatformSpec`, `PlatformStatus`, `Subscription`, `SubscriptionFilter`
- [x] 1.2 Add markers: `+kubebuilder:object:root=true`, `+kubebuilder:subresource:status`, `+kubebuilder:resource:scope=Cluster,shortName=plat`, and the CEL `+kubebuilder:validation:XValidation:rule="self.metadata.name == 'cluster'",message="Platform is a cluster singleton; the only permitted name is 'cluster'"`
- [x] 1.3 `PlatformSpec`: `Type string` (required, `+kubebuilder:validation:MinLength=1`); `Registry map[string]Subscription`. `Subscription{ Enable *bool, Filter *SubscriptionFilter }`; `SubscriptionFilter{ Range string, Allow []string, Deny []string }` with `omitempty` tags
- [x] 1.4 `PlatformStatus`: `ObservedGeneration int64`, `Conditions []metav1.Condition` (`+listType=map`,`+listMapKey=type`); add `GetConditions`/`SetConditions` accessors matching `Release`
- [x] 1.5 Optional printcolumns: `Type`, and `Ready`/`Materialized` condition status via JSONPath
- [x] 1.6 Register `Platform`/`PlatformList` in `groupversion_info.go` `SchemeBuilder.Register(...)` (used `init()` in `platform_types.go`, matching the `Release` convention)

## 2. Generated artifacts

- [x] 2.1 `task dev:generate` — regenerate `zz_generated.deepcopy.go` (do not hand-edit)
- [x] 2.2 `task dev:manifests` — generate `config/crd/bases/...platforms.yaml` + RBAC for the new kind; confirm `scope: Cluster` and the CEL rule render into the CRD (no RBAC generated — no reconciler/`+kubebuilder:rbac` markers this slice, as designed)
- [x] 2.3 Add the new CRD to `config/crd/kustomization.yaml` if not auto-included

## 3. Sample

- [x] 3.1 Add `config/samples` `Platform` named `cluster` with a `type` and one `registry` subscription (e.g. `opmodel.dev/catalogs/opm` with a `filter.range`); add to samples kustomization

## 4. Tests + validation gates

- [x] 4.1 Envtest test: applying `Platform` named `cluster` succeeds; applying any other name is rejected by the CEL rule
- [x] 4.2 Envtest test: a minimal spec (`type` + one registry entry, no `enable`/`filter`) is accepted; missing `type` is rejected
- [x] 4.3 `task dev:fmt dev:vet`
- [x] 4.4 `task dev:lint` (added Platform code is clean; 2 pre-existing `staticcheck` SA1019 warnings remain in `pkg/render/`, unrelated to this change)
- [x] 4.5 `task dev:test`
