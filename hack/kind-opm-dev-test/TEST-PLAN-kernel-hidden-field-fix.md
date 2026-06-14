# Test plan — kernel output-local hidden field fix, validated on `kind-opm-dev`

**Status:** ready to execute (fix lands on `library` branch `fix/materialized-platform-composed`).
**Owner:** _(assign)_
**Date drafted:** 2026-06-14.
**Cluster:** `kind-opm-dev` (kind name `opm-dev`), the same harness as [`FINDINGS.md`](./FINDINGS.md).

---

## 1. What is under test

The **library kernel fix** for the output-local hidden field bug
(`library/docs/design/transformer-output-hidden-field-scope-bug.md`; first surfaced operator-side in
[`FINDINGS.md`](./FINDINGS.md) §2/§3a).

Root cause (confirmed): `materialize.Materialize` `FillPath`-ed the composed transformer map into the
**closed, independently-built `c.#Platform`** value; reading a transformer's `#transform` back out of
that closed value corrupted the in-expression resolution of output-local hidden fields
(`_convertedSidecars`), so `containers: list.Concat([[_mainContainer], _convertedSidecars])` marshalled
as `non-concrete value _`.

The fix: `materialize.MaterializedPlatform` now exposes the **open** `Composed` map, and the executor
reads each `#transform` from it instead of out of the closed `Package`.

**This plan proves the kernel fix end-to-end through the operator on a real cluster.**

---

## 2. Why this test must pin a PRE-FIX catalog (the critical design point)

The catalog already shipped a *workaround* at `opmodel.dev/catalogs/opm@v0.5.5+` (it moved
`_convertedSidecars` to `#transform` scope). If you test with a workaround-era catalog, a container
workload renders **regardless of the kernel fix** — so it proves nothing.

To validate the *kernel* fix, the test module MUST pin a **buggy** catalog version whose workload
transformers still declare `_convertedSidecars` **inside `output`**:

| catalog version | `_convertedSidecars` placement | use here |
| --- | --- | --- |
| `v0.5.0`–`v0.5.4` | inside `output` (**buggy**) | ✅ **use `v0.5.2`** (matches cleanly; `0.5.0/0.5.1` hit the separate matcher miss) |
| `v0.5.5`+ | `#transform` scope (workaround) | ❌ masks the kernel fix — do NOT use |

So: **module pins catalog `@v0.5.2`**, operator built against the **fixed** library. A container
workload that renders here can only have rendered because the kernel stopped corrupting the transform.

---

## 3. Test vehicle — the `hello-web` module

The existing `hello` module (`test/fixtures/modules/hello/`) is ConfigMap-only and never exercised the
bug (`FINDINGS.md` §1). This plan introduces a minimal **container** sibling — `hello-web` — that
renders exactly one Deployment via the (buggy-catalog) `deployment-transformer`. It is the smallest
artifact that triggers the bug.

Author it under `test/fixtures/modules/hello-web/` (mirrors the proven
`library/testdata/modules/web_app` stateless component, trimmed to drop Service/HTTPRoute so only the
Deployment renders and RBAC stays minimal).

`test/fixtures/modules/hello-web/cue.mod/module.cue`:

```cue
module: "testing.opmodel.dev/modules/hello-web@v0"
language: version: "v0.17.0"
source: kind: "self"
deps: {
	// PRE-FIX catalog on purpose — see §2.
	"opmodel.dev/catalogs/opm@v0": { v: "v0.5.2" }
	"opmodel.dev/core@v0": { v: "v0.4.0" }
}
```

`test/fixtures/modules/hello-web/module.cue`:

```cue
// hello-web — minimal container workload. Renders a single Deployment via the
// catalog's deployment-transformer. Pinned to a PRE-FIX catalog (v0.5.2) so it
// exercises the kernel output-local hidden field fix, not the catalog workaround.
package hello_web

import m "opmodel.dev/core@v0"

m.#Module

metadata: {
	modulePath:  "testing.opmodel.dev/modules"
	name:        "hello-web"
	version:     "0.1.0"
	description: "Minimal container workload — renders one Deployment (kernel hidden-field fix probe)"
}

#config: {
	image: {repository: string | *"nginx", tag: string | *"1.27", digest: string | *""}
	replicas: int | *1
}

debugValues: {
	image: {repository: "nginx", tag: "1.27", digest: ""}
	replicas: 1
}
```

`test/fixtures/modules/hello-web/components.cue`:

```cue
package hello_web

import bp "opmodel.dev/catalogs/opm/blueprints/workload"

#components: {
	web: {
		metadata: {
			name: "web"
			// Gates the deployment-transformer (requiredLabels: workload-type=stateless).
			labels: "core.opmodel.dev/workload-type": "stateless"
		}
		bp.#StatelessWorkload
		spec: statelessWorkload: {
			container: {
				name:  "web"
				image: #config.image
				ports: http: {name: "http", targetPort: 8080}
			}
			scaling: count: #config.replicas
		}
	}
}
```

> Sanity-check the artifact before publishing:
> `cd test/fixtures/modules/hello-web && CUE_REGISTRY='…localhost:5000…' cue vet -c ./...`
> (a clean `vet -c` here is expected — the bug only manifests through the Go kernel, not pure CUE).

`hello-web` ModuleRelease + applier RBAC (apply after publishing) —
`hack/kind-opm-dev-test/hello-web.yaml`:

```yaml
---
apiVersion: v1
kind: ServiceAccount
metadata: { name: hello-web-applier, namespace: default }
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata: { name: hello-web-applier, namespace: default }
rules:
  - apiGroups: ["apps"]
    resources: ["deployments"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata: { name: hello-web-applier, namespace: default }
subjects:
  - { kind: ServiceAccount, name: hello-web-applier, namespace: default }
roleRef: { apiGroup: rbac.authorization.k8s.io, kind: Role, name: hello-web-applier }
---
apiVersion: releases.opmodel.dev/v1alpha1
kind: ModuleRelease
metadata: { name: hello-web, namespace: default }
spec:
  module:
    path: testing.opmodel.dev/modules/hello-web@v0
    version: v0.1.0
  serviceAccountName: hello-web-applier
  values: {}            # module defaults: nginx:1.27, replicas 1
  prune: true
```

---

## 4. Environment & prerequisites

Reuse the `FINDINGS.md` §6 environment. Confirm before starting:

- [ ] kind cluster `opm-dev` up (`kubectl config use-context kind-opm-dev`), k8s ≥ v1.34.
- [ ] Local registry `opm-registry` (`registry:2`) on `localhost:5000`, attached to the `kind` docker
      network (in-cluster `opm-registry:5000`).
- [ ] Published deps present in the registry:
  - `opmodel.dev/core@v0` → `v0.4.0`
  - `opmodel.dev/catalogs/opm@v0` → **`v0.5.2`** (the buggy version — verify it is present:
    `curl -s localhost:5000/v2/opmodel.dev/catalogs/opm/tags/list`)
- [ ] No Gateway API / extra CRDs required (hello-web renders only a Deployment).
- [ ] Flux **source CRDs only** applied if the manager still watches them unconditionally
      (`FINDINGS.md` finding #1) — otherwise the manager crashloops.

---

## 5. Build & deploy the FIXED operator

The operator pins `github.com/open-platform-model/library v0.5.0`, which does **not** contain the fix.
Build the manager image against the fix branch via a local `replace`:

```bash
cd opm-operator

# Point the library dependency at the local checkout on the fix branch.
go mod edit -replace github.com/open-platform-model/library=../library
go mod tidy
git -C ../library rev-parse --abbrev-ref HEAD     # expect: fix/materialized-platform-composed

# Build + load the image into kind, then (re)install the controller.
KIND_CLUSTER=opm-dev IMG=opm-operator:hiddenfix task docker:build
kind load docker-image opm-operator:hiddenfix --name opm-dev
KIND_CLUSTER=opm-dev IMG=opm-operator:hiddenfix task operator:controller:install
# installer patches --registry=testing.opmodel.dev=opm-registry:5000+insecure,opmodel.dev=opm-registry:5000+insecure,registry.cue.works
```

- [ ] Manager pod `Running`, no crashloop, logs show the platform materialized.

> Revert the `replace` (`go mod edit -dropreplace github.com/open-platform-model/library`) once the
> fix is released and the operator bumps the dependency normally.

---

## 6. Execution

```bash
cd opm-operator

# 6.1 Publish the hello-web module to the local registry.
task module:publish MODULE_DIR=test/fixtures/modules/hello-web MODULE_VERSION=v0.1.0

# 6.2 Ensure the Platform is materialized (Ready=True / Materialized).
kubectl apply -f config/samples/releases_v1alpha1_platform.yaml
kubectl wait --for=condition=Ready platform/cluster --timeout=120s

# 6.3 Apply the container workload.
kubectl apply -f hack/kind-opm-dev-test/hello-web.yaml

# 6.4 Observe.
kubectl -n default get modulerelease hello-web -w
kubectl -n default get deploy,po -l module-release.opmodel.dev/name=hello-web
```

---

## 7. Pass / fail criteria

**PASS** — all of:

- [ ] `ModuleRelease/hello-web` reaches `Ready=True` (reason `Reconciled`/applied), **not**
      `RenderFailed`.
- [ ] The manager logs contain **no** `list.Concat: non-concrete value _` /
      `cannot convert incomplete value "_"` error for
      `deployment-transformer@0.5.2 … output.spec.template.spec.containers` (the §2 failure signature).
- [ ] `Deployment/hello-web-web` exists, becomes **`1/1` ready**, image `nginx:1.27`.
- [ ] Pod runs (`kubectl -n default get po -l app.kubernetes.io/name=web`).
- [ ] Provenance labels on the Deployment: `app.kubernetes.io/managed-by=opm-controller`, non-empty
      `module-release.opmodel.dev/uuid`, `module.opmodel.dev/version=0.1.0`.
- [ ] `ModuleRelease.status` records a digest + an inventory entry for the Deployment.
- [ ] **Prune:** `kubectl delete -f hack/kind-opm-dev-test/hello-web.yaml` removes the Deployment;
      re-applying recreates it cleanly.

**FAIL** — any of:

- `ModuleRelease/hello-web` stalls `Ready=False reason=RenderFailed` with the `list.Concat … _`
  signature → the kernel fix is not in the running image (re-check §5 `replace` + image load), or the
  module is not pinned to a buggy catalog (re-check §2 — it must be `@v0.5.2`).
- Deployment never becomes ready (investigate image pull / RBAC, not the kernel fix).

---

## 8. Negative control (proves it is the KERNEL fix, not the catalog or the operator)

Run the **identical** module against an **unfixed** operator to confirm the fix is the differentiator:

```bash
cd opm-operator
go mod edit -dropreplace github.com/open-platform-model/library   # back to library v0.5.0 (no fix)
go mod tidy
KIND_CLUSTER=opm-dev IMG=opm-operator:nofix task docker:build
kind load docker-image opm-operator:nofix --name opm-dev
KIND_CLUSTER=opm-dev IMG=opm-operator:nofix task operator:controller:install
kubectl delete -f hack/kind-opm-dev-test/hello-web.yaml --ignore-not-found
kubectl apply  -f hack/kind-opm-dev-test/hello-web.yaml
```

- [ ] **Expected: `ModuleRelease/hello-web` → `RenderFailed`** with the `list.Concat: non-concrete
      value _` signature on `deployment-transformer@0.5.2`.

Then re-install the **fixed** image (§5) and confirm `hello-web` flips to `Ready=True` with no other
change. The only variable is the library version ⇒ the kernel fix is what unblocks the render.

> Already covered by automated guards in the library (no cluster needed), referenced for traceability:
> `opm/materialize` `TestComposed_RendersConcreteWherePackageDoesNot` and
> `opm/compile` `TestExecute_ReadsTransformFromComposedNotPackage`.

---

## 9. Teardown

```bash
kubectl delete -f hack/kind-opm-dev-test/hello-web.yaml --ignore-not-found
KIND_CLUSTER=opm-dev IMG=opm-operator:hiddenfix task operator:controller:uninstall   # optional
go mod edit -dropreplace github.com/open-platform-model/library && go mod tidy        # if not already
# (optionally) docker image rm opm-operator:hiddenfix opm-operator:nofix
```

The kind cluster, registry, and published deps are left intact for reuse.

---

## 10. Sign-off

**Executed 2026-06-14** on `kind-opm-dev` against `library` branch `fix/materialized-platform-composed`
(commit `5490696` "fix(materialize): source transforms from open composed map").

| Item | Result | Notes |
| --- | --- | --- |
| Fixed-operator render (§7) | ☑ **PASS** | `hello-web` → `Ready=True ReconciliationSucceeded`; `Deployment/hello-web-web` **1/1**, `nginx:1.27`; provenance `managed-by=opm-controller`, `uuid=afb6caaf-…`, `version=0.1.0`; inventory digest `sha256:4229484d…` + 1 Deployment entry; **zero** `list.Concat … _` in logs. |
| Negative control (§8) | ☑ **PASS** | Identical module on `opm-operator:nofix` (library `v0.5.0`) → `Ready=False RenderFailed` with the exact §2 signature on `deployment-transformer@0.5.2 … output.spec.template.spec.containers: list.Concat: non-concrete value _`; no Deployment. Re-installing the fixed image flipped it back to `Ready=True` with no other change. |
| Prune/recreate (§7) | ☑ **PASS** (with caveat) | Finalizer pruned the Deployment (`Deletion cleanup pruned resources deleted=1`, `Finalizer removed`); re-apply recreated it `1/1`. **Caveat:** `kubectl delete -f hello-web.yaml` deletes the applier SA *and* the MR together, so the finalizer can't impersonate the (now-gone) SA → MR strands `Ready=False DeletionSAMissing`. Recover by restoring the SA/RBAC (re-apply) and re-reconciling. To exercise prune cleanly, delete only the `ModuleRelease` (keep the SA), or split the SA/RBAC into a separate manifest. Not a kernel-fix issue. |
| Operator regressions watched (`FINDINGS.md` §5) | ☑ none new | Re-confirmed: #1 (Release-controller crashloop, mitigated by Flux source CRDs only), #2 (manager restart drops in-memory platform store; re-materialize the Platform after every rollout — hit repeatedly here), #3 (drift-detection dry-run runs as controller SA → noisy `Forbidden`). All pre-existing. |

### Execution deltas (environment + plan drift since drafting)

The plan ran end-to-end but several drafted assumptions did not hold as written:

1. **Registry off the kind network.** `opm-registry` was on the default bridge (`172.17.0.2`), not the
   `kind` network — cluster pods could not resolve `opm-registry:5000` (manager crashlooped at
   `Failed to resolve OPM core schema`). Fix: `docker network connect kind opm-registry`. The old pod
   only survived because it had cached the core schema at an earlier boot.
2. **Catalog `v0.5.2` was absent** (registry held only `v0.5.1`). Re-published from `catalog_opm` `main`
   (still pre-fix: `_convertedSidecars` inside `output`) via `task publish VERSION=v0.5.2` — correctly
   buggy per §2.
3. **`task docker:build` cannot build a local `replace`.** The Dockerfile's build context is
   `opm-operator/` only, so `go mod download` can't reach `../library`. Worked around by compiling the
   manager on the host (`CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build`) and wrapping the binary in a
   minimal distroless image from a clean temp context (the repo `.dockerignore` excludes non-Go files).
4. **Component shape in §3 was under-specified for catalog `v0.5.2`.** `#config.image` must be typed
   `res.#Image & {…}` (a plain struct closes out the computed `pullPolicy`/`reference`), and the
   `#StatelessWorkload` mixes in RestartPolicy/UpdateStrategy traits whose values are non-defaulted —
   the component must set `restartPolicy: "Always"` and `updateStrategy: type: "RollingUpdate"`, else
   render fails `ResolutionFailed: not fully concrete: …spec.restartPolicy: incomplete value …`.
   (The committed fixtures already incorporate these.)
5. **`cue vet -c` is NOT clean** (contra the §3 note) — it reports `…statelessWorkload.scaling: field
   not allowed` closedness, *identically for the proven `web_app` fixture*. This is a pure-CUE
   strict-concrete artifact of the blueprint `if`-propagation; the Go kernel render is the
   authoritative gate and it passes. `cue mod publish` succeeds regardless.
6. **Module republish needs a manager restart to take effect** — the manager caches the resolved module
   in-process, so re-publishing the same version is not re-read until the pod restarts (which then also
   drops the platform store, see regression #2).

On PASS, the open item in [`FINDINGS.md`](./FINDINGS.md) §7
("Kernel-level fix … so local-in-`output` hidden fields can't recur") is marked **done**, citing this run.

---

## 11. Cross-references

- `library/docs/design/transformer-output-hidden-field-scope-bug.md` — root cause (§11–§12) + landed
  fix (§13).
- `library/docs/design/repro-hidden-field/` — pure-CUE control proving the defect was in Go, not CUE.
- [`FINDINGS.md`](./FINDINGS.md) — original operator-side discovery (§2/§3a) and the standing
  environment (§6).
- Library regression guards: `opm/materialize/composed_open_test.go`,
  `opm/compile` `TestExecute_ReadsTransformFromComposedNotPackage`.
- Library branch: `fix/materialized-platform-composed`.
