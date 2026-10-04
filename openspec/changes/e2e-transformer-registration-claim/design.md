## Context

Two reconcilers take a claim from stored to built:

- The `TransformerRegistrationReconciler` judges it. It acquires the claimed catalog with the claim's own `spec.catalog` and `spec.version` (`AcquireCatalogFromRegistry`), re-derives `provides`, checks that the provider's inventory owns the claim, checks build compatibility, duplicates and contract providers, and then accepts it. Activation latches once the provider ModuleInstance is Ready.
- The `PlatformReconciler` folds every accepted and active claim into the generated platform. It calls `platformEntries`, `platformmodule.Roots` and `Closure`, `platformmodule.Generate`, then `AcquirePlatformFromDir`. Only after a successful build does it record `status.packageIdentity` and `status.registry`, and mark Ready with reason `Generated`.

Until library#170, the first step refused the bare version opm renders. Library#170 makes both steps accept either spelling: `Roots` adds the `v` that `cue.mod` needs, and `Generate` stamps the entry's `version` without it. Every test of this path in the repo either stops at the library verb (the integration spec) or was run by hand (the archived change `2026-10-04-add-active-provider-fixture`, design D4). Owner decision i1 asks for an e2e spec that drives a real claim through acceptance and platform build.

## Goals / Non-Goals

**Goals:**

- A deployed operator, built from the branch, accepts and activates a claim rendered by a real provider module, and builds it into the platform. This covers the bare spelling.
- The same claim with a `v`-prefixed `spec.version` is accepted, and the platform builds with it.
- Report N4: did the consumers absorb library f1d9908?

**Non-Goals:**

- Changing any reconciler, CRD, fixture or workflow. No reconcile phase (Source, Render, Apply, Prune, Status) changes.
- Rendering `backup_consumer` in e2e (D3).

## Research & Decisions

### D1. Reuse the published `backup_provider` fixture and the sample Platform

The spec applies `test/fixtures/modules/backup_provider/moduleinstance.yaml` and `config/samples/opmodel.dev_v1alpha1_platform.yaml`. The Platform subscribes `opmodel.dev/catalogs/opm@v4` at `4.4.4`. The backup catalog pins that build or an older one, so the build-compatibility check passes. The claim names `testing.opmodel.dev/catalogs/operator/backup@v0` at `0.1.0`. Those coordinates are public on GHCR, so the spec resolves them through the controller's default registry with no credentials.

In PR CI, `task examples:pin` re-pins the provider instance to the per-commit pre-release tag that `task examples:publish` just pushed. The claim inside it still names the real `0.1.0` catalog. The spec mounts GHCR credentials when `OPERATOR_DOCKER_CONFIG` is set, and the registry override when `LOCAL_REGISTRY` is set, as the podinfo spec does. A `LOCAL_REGISTRY` run needs the backup fixtures seeded there (`task examples:seed`).

The spec owns its controller deploy and teardown, so it does not depend on the order of the other top-level specs. The deploy steps duplicate the podinfo spec's. Moving them into a shared helper would edit specs this change does not need to touch, so this change leaves them duplicated.

Assertions read the API with `kubectl ... -o jsonpath`, like the rest of `test/e2e`:

```go
// claim verdict
kubectl get transformerregistration default.backup-provider \
  -o jsonpath={.status.accepted},{.status.active},{.status.observedGeneration},{.metadata.generation}
// platform build that carries the claim
kubectl get platform cluster \
  -o jsonpath='{.status.registry[?(@.catalog=="testing.opmodel.dev/catalogs/operator/backup@v0")].version}'
kubectl get platform cluster -o jsonpath='{.status.conditions[?(@.type=="Ready")].reason}'  // Generated
kubectl get platform cluster -o jsonpath={.status.packageIdentity}
```

`status.registry` and `status.packageIdentity` are written only after `AcquirePlatformFromDir` succeeds; a failed build leaves both naming the last good package. So an entry listing the claim's version, `source: Registration` and a package identity different from the one recorded before the claim, together with Ready=True/`Generated`, prove that the build carrying the claim succeeded.

### D2. The `v`-prefixed spelling arrives by editing the live claim with the provider suspended

**Context**: The supervisor asked for both spellings. opm's `#VersionType` (core `src/types.cue`) is a bare SemVer pattern. No module, `backup_provider` included, can render `v0.1.0`. A claim applied by hand naming a provider whose inventory does not own it is refused `ProviderMismatch` (`checkProviderIdentity`). So a `v`-prefixed claim that reaches the verdict can only come from an edit to an owned claim. The CRD admits that edit (`spec.version` has only `MinLength=1`), and it is the one way the spelling reaches a cluster.

**Explored**: Three things could stop the edit from reaching a verdict:

- The provider's next reconcile re-applies the rendered claim and reverts the edit. Setting `spec.suspend: true` on `backup-provider` stops this: a suspended instance skips every phase and keeps its inventory (`suspend-resume` spec).
- Suspension marks the instance `Ready=False/Suspended`. Activation latches (`gateActivation` returns early when the claim is already active), so the claim stays active.
- The edit bumps the claim's generation. The claim's predicate passes generation changes, so the claim is re-judged. `claimContributionPredicate` sees the coordinate move from `@0.1.0` to `@v0.1.0`, so the Platform regenerates under a new identity.

Section 1 runs this flow by hand on a throwaway cluster before the spec encodes it.

**Decision**: After the bare-spelling assertions, the spec patches `backup-provider` with `spec.suspend: true` and waits for `Ready=False/Suspended`. It then merge-patches the claim's `spec.version` to `v0.1.0` and waits for three things:

- the claim's `status.observedGeneration` to equal its new `metadata.generation`, with `accepted: true`, `active: true` and a Ready message naming `v0.1.0`;
- the Platform's `status.registry` entry for the backup catalog to read `v0.1.0`, under a `status.packageIdentity` different from the bare-spelling one;
- the Platform to report Ready=True/`Generated`.

**Rationale**: This is the narrowest path that pushes a `v`-prefixed coordinate through both reconcilers on a deployed operator, and it changes no fixture. The alternatives:

- A second provider fixture cannot render `v0.1.0` (`#VersionType`).
- A hand-applied claim is refused `ProviderMismatch` before it reaches the catalog.

If section 1 finds that the edit is reverted or not re-judged, the change stops and reports to the supervisor. It does not pick another mechanism on its own.

### D3. No consumer render in e2e

The owner decision names acceptance and platform build. `backup_consumer` rendering through the claimed catalog is already covered at the integration tier (`backup_fixture_test.go`, third spec) and was checked once on a cluster (archived D4). Adding it here would add a removal-guard ordering to the teardown: the consumer has to go before the claim can. It would also make the `v`-prefixed step depend on the instance re-rendering when the platform changes. Neither is needed for i1.

### D4. N4: consumers absorbed library f1d9908

**Context**: Library f1d9908 (`feat(kernel)!`, first in v1.0.0-beta.2) makes `AcquireInstanceFromDir` validate an instance package's own values against the module's `#config`. Research N4 asked for a sweep of every consumer's instance packages.

**Explored** (at each repo's `origin/main` on 2026-10-04):

| Where | Instance packages | Verdict |
| --- | --- | --- |
| opm-operator (`8dc24b3`, library beta.4) | `test/fixtures/modulepackages/{hello,hello_web,podinfo,redis}`, acquired by `KernelPackageRenderer` | Every values key is declared in the module's `#config` with a matching type (`message`; `replicas`; `replicas`; `persistence.size`). Conforms. |
| cli (`5180cad1`, library beta.4) | `examples/instances/podinfo` (`replicas`), `tests/e2e/testdata/operator-owned` (`image`, `replicas`), `internal/workflow/render/testdata/skip-unprovided/instance` (`values: {}`), `tests/integration/inst-tree/testdata` (no values) | Conforms. `tests/e2e/testdata/vet-errors/instance` is invalid on purpose: its test expects the refusal. |
| catalog_opm (`daae275`) | none | Not affected. |
| modules (`e4d3b65`) | none (`istio_ambient/testdata/values-full.cue` is passed as a values source, which was already validated before f1d9908) | Not affected. |
| opm-modules (`f16a187`) | none | Not affected. |

Both frontends already pin library beta.4, and their CI went green on that bump.

**Decision**: Nothing in opm-operator needs a fix. Section 1 confirms the operator half by running the registry-backed ModulePackage specs (`KernelPackageRenderer Integration`) against GHCR on beta.4. If one fails on an undeclared or mistyped value, the fix is to the fixture's `values.cue` in this change.

### D5. Running the spec

The e2e suite runs `make install`, `make deploy`, `make undeploy` and `make uninstall` against the current kube context. Uninstalling the CRDs deletes every ModuleInstance on the cluster. The shared `kind-opm-dev` cluster holds the cli's operator-owned state, so the spec runs on a throwaway kind cluster, `KIND_CLUSTER=opm-operator-test-e2e`, on podman, created and deleted for the run. The run still goes through `flock` on the workspace cluster lock, because it competes for the same podman host. It is focused on the new spec (`-ginkgo.focus`) with an explicit `--kubeconfig`/context. The full suite is CI's job.

## Risks / Trade-offs

- **GHCR availability.** The spec pulls the opm catalog and the backup fixtures from GHCR, like the podinfo spec. A registry outage fails it, as it fails the rest of the suite.
- **The `v`-prefixed path relies on suspension.** If suspend semantics change so that a suspended instance re-applies its output, the edit is reverted and the spec times out. The failure names the claim's version, so it reads as that cause.
- **Run time.** The spec adds one controller deploy, about one to three minutes on a warm cluster. That fits inside the suite's `-timeout 30m`.
