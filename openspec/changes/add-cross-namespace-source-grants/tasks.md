## 1. Default-deny security fix (closes the CRITICAL on its own)

- [ ] 1.1 Add `ErrCrossNamespaceForbidden` sentinel to `internal/source` (alongside `ErrSourceNotFound` et al.)
- [ ] 1.2 Define `CrossNamespacePolicy` interface + `DenyAllCrossNamespacePolicy` in a new `internal/source/policy` package (default-deny `Allows(...) bool`)
- [ ] 1.3 Add the cross-namespace branch to `source.Resolve`: when `sourceRef.Namespace` differs from the release namespace, consult the policy; return `ErrCrossNamespaceForbidden` (wrapping target ns/name) when denied, before any API read
- [ ] 1.4 Thread the policy parameter through `Resolve` and wire `DenyAllCrossNamespacePolicy` at the `resolveReleaseSource` call site (`internal/reconcile/release.go`)
- [ ] 1.5 Map `ErrCrossNamespaceForbidden` to `Stalled` (not transient) in `resolveReleaseSource`, with a Warning event and `StalledRecheckInterval`
- [ ] 1.6 Unit tests in `internal/source`: cross-namespace denied (no API read), same-namespace and empty-namespace permitted, `errors.Is(ErrCrossNamespaceForbidden)` classification
- [ ] 1.7 Run `task dev:fmt dev:vet dev:test`; verify the default-deny path stalls a cross-namespace Release

## 2. SourceRefGrant API type

- [ ] 2.1 Add `api/v1alpha1/sourcerefgrant_types.go`: namespaced `SourceRefGrant` with `spec.from[]{group,kind,namespace}` and `spec.to[]{group,kind,name?}`; kubebuilder markers (`+kubebuilder:object:root=true`, namespaced scope, printcolumns)
- [ ] 2.2 Register `SourceRefGrant`/`SourceRefGrantList` in the scheme (`groupversion_info.go` SchemeBuilder)
- [ ] 2.3 Run `task dev:manifests dev:generate`; confirm generated CRD + DeepCopy, no hand-edits to generated files
- [ ] 2.4 Add `+kubebuilder:rbac:groups=releases.opmodel.dev,resources=sourcerefgrants,verbs=get;list;watch` on the Release controller; regenerate RBAC

## 3. Grant-backed policy + master switch

- [ ] 3.1 Implement `GrantPolicy` in `internal/source/policy`: matches a cross-namespace reference against cached `SourceRefGrant` objects in the target namespace per the design's matching algorithm (D6)
- [ ] 3.2 Implement the two-gate composition: when the flag is off, deny unconditionally; when on, delegate to `GrantPolicy`
- [ ] 3.3 Add the `--allow-cross-namespace-source-refs` flag (default `false`) to `cmd/main.go`
- [ ] 3.4 Wire a cached client/informer for `SourceRefGrant` and inject the composed policy into the Release reconcile params (replacing `DenyAllCrossNamespacePolicy`)
- [ ] 3.5 Add a manager watch on `SourceRefGrant` that re-enqueues Releases in namespaces whose grants changed
- [ ] 3.6 Unit tests for `GrantPolicy`: matching grant permits; no/partial-match denies; `name`-scoped vs kind-wide `to`; flag-off denies despite a matching grant

## 4. Integration coverage

- [ ] 4.1 Envtest (`test/integration`): flag off → cross-namespace Release stalls `ErrCrossNamespaceForbidden`
- [ ] 4.2 Envtest: flag on + matching `SourceRefGrant` → cross-namespace Release resolves and renders
- [ ] 4.3 Envtest: flag on, grant deleted → next reconcile stalls (revocation on next pass)

## 5. Docs and validation gates

- [ ] 5.1 Add a cross-namespace-source section to `docs/TENANCY.md`: consent model, two-gate requirement, grant-creation RBAC note, sample `SourceRefGrant`
- [ ] 5.2 Add a `config/samples` `SourceRefGrant` example
- [ ] 5.3 Run `task dev:manifests dev:generate dev:fmt dev:vet dev:lint dev:test`; confirm all gates green
