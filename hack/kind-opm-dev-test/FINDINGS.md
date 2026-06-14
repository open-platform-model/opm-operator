# kind-opm-dev operator test — findings

Status: **operator verified end-to-end; container-render blocker ROOT-CAUSED + FIXED (catalog).**
Date: 2026-06-13. Cluster: `kind-opm-dev` (kind name `opm-dev`). Operator: `opm-operator` v0.7.0 on `library v0.5.0`.

> **RESOLUTION (see §3a).** The non-concrete-container blocker is fixed in `catalog_opm`
> (branch `fix/container-non-concrete-marshal`, published locally as `opmodel.dev/catalogs/opm@v0.5.7`).
> web-app (`v1.0.10`, pinning catalog `v0.5.7`) now renders Deployment(2× nginx, 2/2 ready) + Service +
> HTTPRoute + 2 ConfigMaps and reconciles `Ready=True`. The §2/§3/§4 analysis below is retained as the
> investigation record; §3a documents the actual root cause and fix.

This document records a manual end-to-end test of the post-enhancement-0001 operator on a local kind
cluster, the bug that blocks container workloads, and the evidence that narrows its root cause to the
**library kernel** (`opm/compile`), not the catalog. Use it to resume the fix.

---

## 1. What was verified (works)

The operator is **proven end-to-end** via the `hello` ModuleRelease (a ConfigMap-only module):

- `Platform/cluster` materializes the catalog → `Ready=True / Materialized`.
- ModuleRelease gates on `ErrPlatformNotReady` until the platform store is populated, then renders.
- Render → **per-release ServiceAccount impersonation** (`spec.serviceAccountName: hello-applier`) → SSA apply.
- Rendered `ConfigMap/hello-hello-hello` carries correct data and provenance labels
  (`app.kubernetes.io/managed-by=opm-controller`, non-empty `module-release.opmodel.dev/uuid`,
  `module.opmodel.dev/version=0.0.2`).
- `.status` records digest + inventory entries.
- **Prune/GC**: deleting the ModuleRelease removes its ConfigMap; re-applying re-creates it cleanly.

So: materialize → gate → render → impersonate → apply → status/inventory → prune is all confirmed good.

---

## 2. The blocker — non-concrete container at marshal (web-app / all container workloads)

`web-app` (`opmodel.dev/library/testdata/modules/web-app@v1`, an nginx Deployment+Service+ConfigMaps
module) fails at render:

```
ModuleRelease web-app → Ready=False, reason=RenderFailed (Stalled)
building inventory entries: converting resource Deployment/default/web-app-web to unstructured:
  marshal json: cue: marshal error:
  platform.#composedTransformers."opmodel.dev/catalogs/opm/transformers/deployment-transformer@0.5.2"
    .#transform.output.spec.template.spec.containers:
    error in call to list.Concat: non-concrete value _
```

- The operator is behaving correctly: it materialized, matched `deployment-transformer@0.5.x`, ran the
  transform, and surfaced a precise CUE error when the apply layer marshals the `*core.Compiled` value
  to unstructured JSON.
- Reproduces identically against catalog **v0.5.0 and v0.5.2**.
- **Impact is broad**: every workload transformer (Deployment / StatefulSet / DaemonSet / Job /
  CronJob) assembles containers through the same `#ToK8sContainer` helper and the same
  `list.Concat([[_mainContainer], _convertedSidecars])` pattern. ConfigMap-only modules (`hello`) are
  unaffected, which is why the smoke test is green.

The offending expression — `catalog_opm/src/transformers/deployment_transformer.cue:127`:
```cue
containers: list.Concat([[_mainContainer], _convertedSidecars])
```

---

## 3. Why this is a kernel bug, not a catalog bug

Every attempt to reproduce the non-concreteness in **standalone CUE** *succeeds* (produces a concrete
list). The `_` only appears inside the **kernel's** transform execution. Evidence gathered:

1. **Main container is concrete in isolation.** `(#ToK8sContainer & {in: <clean nginx container>}).out`
   evaluates with `cue eval -c` to a fully concrete container — `image: "nginx:1.27"`,
   `imagePullPolicy: "IfNotPresent"` (default), `ports[].protocol: "TCP"` (default). Ruled out the
   earlier guesses (pullPolicy / protocol / image.reference all have defaults or compute concretely).

2. **The real published module's container is concrete too.** Loaded the *actual* published
   `web-app@v1.0.9`, extracted `#components.web.spec.container` (defaults applied), and ran it through
   `#ToK8sContainer` — `cue eval -c` returns a fully concrete container, `image.reference: "nginx:1.27"`.

3. **Sidecars resolve to `[]`.** The `#SidecarContainers` / `#InitContainers` trait *mixins* only
   register `#traits` — they do **not** inject `spec.sidecarContainers` into the component. So the
   transformer's own default `_sidecarContainers: [...] | *[]` applies → `[]`. Confirmed: an isolated
   `list.Concat([[mainContainer], (#ToK8sContainers & {in: [...]|*[]}).out])` is concrete.
   (A speculative "add `| *[]` to the traits" edit was tried and reverted — it is **inert** for this
   flow because the trait spec is never mixed into the component.)

4. **Therefore both `list.Concat` arguments are concrete in standalone CUE**, yet the kernel render
   yields `_`. The divergence is introduced by the kernel's compile/finalize/fill path.

Conclusion: the misbehaviour originates in the kernel's compile/fill path, but it is **avoidable from
the catalog** — see §3a for the confirmed root cause and the fix that shipped.

---

## 3a. ROOT CAUSE + FIX (confirmed by in-kernel instrumentation)

Instrumented `opm/compile/executePair` (temporary `OPM_DEBUG_COMPILE` block, since reverted) to dump
the deployment transformer's hidden fields during the real kernel render of web-app. Result:

```
_mainContainer:      concrete=true   validate=<nil>     # defined at #transform scope
_sidecarContainers:  concrete=true   validate=<nil>
_convertedSidecars:  concrete=true   validate=<nil>     # defined LOCALLY inside output.spec.template.spec
containers:          concrete=FALSE  → list.Concat: non-concrete value _
```

Every input validates concrete via a direct Go `LookupPath`, yet the `containers` expression that
references them is non-concrete. The discriminator is **scope**:

- `_mainContainer` is declared at **`#transform` scope** → resolves correctly.
- `_convertedSidecars` was declared **locally inside `output.spec.template.spec`** → after the kernel
  materializes the platform and `FillPath`s `#component`, its cross-references (to the outer
  `_sidecarContainers` and to the catalog's `#ToK8sContainers`) do **not** resolve when the value is
  consumed *in-expression* (`list.Concat`, or a `for` comprehension → "cannot range over … incomplete
  type _"). A direct `LookupPath` of the same path *does* resolve it — so `Validate(Concrete)` reports
  "concrete" while the in-CUE builtin sees an incomplete value.

**Fix (catalog-side, minimal & idiomatic):** move `_convertedSidecars` out of the local `output`
block up to **`#transform` scope** (alongside `_mainContainer`); keep `containers:
list.Concat([[_mainContainer], _convertedSidecars])`. Verified: `containers: concrete=true`.

Applied to **all five workload transformers** (they shared the identical local-`_convertedSidecars`
pattern and would all fail for any container workload):
`deployment`, `statefulset`, `daemonset`, `job`, `cronjob` — in
`catalog_opm` branch `fix/container-non-concrete-marshal`. Published locally as `…/catalogs/opm@v0.5.7`.

Validated end-to-end:
- `library` flow test `TestFlow_WebApp_OnOpmPlatform` passes against `v0.5.7`.
- Operator on `kind-opm-dev`: web-app `v1.0.10` (pins catalog `v0.5.7`) → `Ready=True`,
  Deployment `web-app-web` **2/2 ready** (`nginx:1.27`, replicas 2), Service + HTTPRoute + 2 ConfigMaps,
  all `managed-by=opm-controller`.

**Deeper (library) follow-up, optional:** the underlying kernel behaviour — a hidden field local to a
transformer `output` not resolving its references after `Materialize` + `FillPath`, while the same
field at `#transform` scope and direct `LookupPath` both resolve — is a real `opm/compile` /
`materialize` quirk. The catalog fix sidesteps it; a kernel-level fix (or a documented authoring
constraint "compute transformer outputs at `#transform` scope, not inside `output`") would prevent
recurrence. To reproduce: re-add the `OPM_DEBUG_COMPILE` dump in `executePair` and run the flow test
against a transformer with a local-in-`output` hidden field.

---

## 4. Where to look in the library (`opm/compile`)

Relevant code (read during this investigation):

- `opm/compile/execute.go` → `executePair(...)`:
  - `transformVal = platform.#composedTransformers[fqn].#transform`
  - `dataComp = dataComponents[compName]` — the **finalized, constraint-free** component value.
  - `unified := transformVal.FillPath(schema.Component, dataComp)` then `FillPath(schema.Context, ctxVal)`.
  - `outputVal := unified.LookupPath(schema.Output)`; StructKind → `Compiled.Value = outputVal`.
- `opm/compile/finalize.go` → `FinalizeValue`: `v.Syntax(cue.Final())` → `cueCtx.BuildExpr(expr)`.
  Applied to components at load time (rebuilds a value in a fresh context, dropping definitions/constraints).

Hypotheses considered (none fully confirmed — needs in-process instrumentation):

- **(A) Hidden-field detachment.** `_mainContainer` is defined at `#transform` scope (line 93) but
  referenced from inside `output` (line 127). If the kernel ever extracts/rebuilds `output` detached
  from its `#transform` siblings, `_mainContainer` dangles → `_`. *Counter-evidence:* `output` also
  references `#context` (outer) for `metadata.name`, which DOES resolve — so plain outer refs survive.
  Worth re-checking whether hidden (`_`-prefixed) siblings are treated differently from `#context`.
- **(B) Finalize stripping a computed field.** `FinalizeValue` (`Syntax(Final)`+`BuildExpr`) on the
  component (or output) could drop a conditionally-computed field (e.g. `#Image.reference`), leaving a
  bare type. *Counter-evidence:* the finalized container shows `reference` concrete in standalone evals.
- **(C) Fill-time interaction.** The non-concreteness may only arise from how `FillPath(#component,
  dataComp)` composes the finalized data with the transformer's `#ToK8sContainers` comprehension over
  the defaulted sidecar list, specifically inside `list.Concat`.

### Suggested next step (instrumentation)

In `executePair`, right after `outputVal := unified.LookupPath(schema.Output)`, for the deployment
case dump:
- `outputVal.LookupPath(spec.template.spec.containers).Syntax(cue.Final())` — see the literal `_`.
- the filled `unified.LookupPath(#component).spec.container` and the transformer's `_mainContainer` /
  `_convertedSidecars` (via a temporary exported alias) — compare against the standalone-concrete values.

A minimal repro harness already exists: `TestFlow_WebApp_OnOpmPlatform` in
`library/opm/kernel/flow_integration_test.go` drives plan→match→compile on the on-disk `web_app` +
`opm_platform` fixtures. It currently asserts success and does **not** marshal to unstructured — add a
`json`/`cue.Concrete(true)` step on each `Compiled.Value` to make it fail the same way the operator does.

### Secondary (orthogonal) matching discrepancy

`TestFlow_WebApp_OnOpmPlatform` / `flow-inspect` **fail to match** the `web` component
("no matching transformer") when `web_app` pins catalog **v0.5.0**, but **match** at **v0.5.2** — while
the operator matches at both 0.5.0 and 0.5.2. Suggests a version-availability/filter difference between
the on-disk `opm_platform` fixture's subscription and the operator's `Platform/cluster` CR. Independent
of the container bug, but it complicates using the flow test as the repro at 0.5.0 — pin the fixture to
0.5.2 to reach compile.

---

## 5. Operator findings (real, none fatal) — also worth fixing

1. **Manager crashloops without Flux source CRDs.** The `Release` controller's `SetupWithManager`
   watches `Bucket`/`GitRepository`/`OCIRepository` unconditionally (`cmd/main.go:272`), and there is
   **no flag to disable it**. On a ModuleRelease-only cluster the manager fails cache-sync (~every few
   minutes) and restarts. *Mitigation used here:* apply **only the Flux source CRD definitions** (no
   controllers, no `flux-system`). *Possible fix:* gate the `Release` controller registration behind a
   flag or a CRD-presence check.

2. **Restart drops the in-memory platform store and does not self-heal.** The materialized platform
   lives in a process-local store (`internal/platform/store.go`); after a manager restart it is empty
   until the Platform reconciles again. But `Materialize` updates only Platform `status` — it does
   **not** bump `metadata.generation` — so ModuleReleases already stalled on `PlatformNotReady` are
   **not re-enqueued** until their own spec changes (or a full resync). A fresh apply on a stable
   manager is fine. *Possible fix:* on platform (re)materialize, enqueue all ModuleReleases/Releases,
   or watch the Platform with a non-generation predicate.

3. **Drift-detection dry-run runs as the controller SA, not the impersonated applier SA.** A dry-run
   diff (`internal/reconcile/modulerelease.go:471`) is performed with the manager's own
   ServiceAccount, which lacks permissions on the rendered kinds → logs a non-fatal
   `Forbidden`/"Drift detection failed, continuing reconcile" before the real apply (correctly
   impersonated) succeeds. Noisy but harmless. *Possible fix:* run drift detection through the same
   impersonated client used for apply.

---

## 6. Reproduction / environment

**Cluster & registry**
- kind cluster `opm-dev` (kubectl context `kind-opm-dev`), k8s v1.34.3.
- Local registry container `opm-registry` (`registry:2`) on `localhost:5000`, attached to the `kind`
  docker network (in-cluster address `opm-registry:5000`).

**Published artifacts in the local registry** (already present)
- `opmodel.dev/core@v0` → `v0.4.0`
- `opmodel.dev/catalogs/opm@v0` → `v0.5.0`, `v0.5.1`, `v0.5.2`
- `testing.opmodel.dev/modules/hello@v0` → `v0.0.2` (new-shape; `v0.0.1` is old-shape)
- `opmodel.dev/library/testdata/modules/web-app@v1` → `v1.0.0..v1.0.9` (`v1.0.9` deps catalog `v0.5.2`)

**Cluster prerequisites applied during the test**
- Gateway API standard CRDs (web-app renders an `HTTPRoute`):
  `kubectl apply -f https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.2.1/standard-install.yaml`
- Flux **source CRDs only** (no controllers) — to stop the crashloop in finding #1:
  `flux install --components=source-controller --export` → keep only `kind: CustomResourceDefinition`
  docs → `kubectl apply`.

**Deploy the operator** (from `opm-operator/`)
```bash
KIND_CLUSTER=opm-dev IMG=opm-operator:dev task operator:controller:install
# patches --registry=testing.opmodel.dev=opm-registry:5000+insecure,opmodel.dev=opm-registry:5000+insecure,registry.cue.works
```

**Apply the workloads**
```bash
kubectl apply -f config/samples/releases_v1alpha1_platform.yaml   # Platform/cluster
task module:apply                                                  # hello (green)
kubectl apply -f hack/kind-opm-dev-test/web-app.yaml               # web-app (RenderFailed → this bug)
```

`hack/kind-opm-dev-test/web-app.yaml` = a `web-app-applier` SA + Role (configmaps/services/deployments/
httproutes) + RoleBinding + a `ModuleRelease` pinning `web-app@v1 v1.0.9`.

**Library-side repro** (to debug the kernel): in `library/`, pin
`testdata/modules/web_app/cue.mod/module.cue` dep `opmodel.dev/catalogs/opm@v0` to `v0.5.2`, then
```bash
CUE_REGISTRY='opmodel.dev=localhost:5000+insecure,testing.opmodel.dev=localhost:5000+insecure,registry.cue.works' \
  OPM_FLOW_TEST_FORCE=1 go test ./opm/kernel/... -run TestFlow_WebApp_OnOpmPlatform
```
(Add a marshal/`cue.Concrete(true)` assertion on `Compiled.Value` to surface the same failure.)

---

## 7. Open items checklist

- [x] Instrument `opm/compile/executePair` to dump the literal non-concrete container field + filled `#component`. *(done; reverted)*
- [x] Root-cause confirmed: local-in-`output` hidden field doesn't resolve post-fill. Fix landed in `catalog_opm` (all 5 workload transformers). *(§3a)*
- [x] Re-run web-app on `kind-opm-dev` → Deployment(2 nginx, 2/2) + Service + 2 ConfigMaps + HTTPRoute, `Ready=True`. ✅
- [x] Kernel-level fix so local-in-`output` hidden fields can't recur — landed in `library` branch
  `fix/materialized-platform-composed` (commit `5490696` "fix(materialize): source transforms from open
  composed map"); `MaterializedPlatform` now exposes the open `Composed` map and the executor reads each
  `#transform` from it instead of out of the closed `Package`. **Validated end-to-end on `kind-opm-dev`
  2026-06-14** via [`TEST-PLAN-kernel-hidden-field-fix.md`](./TEST-PLAN-kernel-hidden-field-fix.md): a
  container module pinned to the *buggy* catalog `v0.5.2` renders `Deployment/hello-web-web` 1/1
  (`Ready=True`) on the fixed operator, while the identical module on the unfixed operator (`v0.5.0`)
  fails with the `list.Concat: non-concrete value _` signature. Library guards
  `TestComposed_RendersConcreteWherePackageDoesNot` (`opm/materialize`) and
  `TestExecute_ReadsTransformFromComposedNotPackage` (`opm/compile`) pass.
- [ ] Commit + release the catalog fix (`catalog_opm` branch `fix/container-non-concrete-marshal` → real `v0.5.x`); re-pin `library/testdata/modules/web_app` + downstream to it.
- [ ] (operator) gate/disable the `Release` controller when Flux CRDs are absent (finding #1).
- [ ] (operator) re-enqueue releases on platform re-materialize (finding #2).
- [ ] (operator) run drift-detection through the impersonated client (finding #3).
- [ ] Investigate the `opm_platform` fixture vs Platform-CR matching discrepancy at catalog 0.5.0.
