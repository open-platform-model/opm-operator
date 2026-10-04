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

The spec reads the catalog path and version through `fixtures.MustCatalog(GinkgoT(), "backup")`, as `backup_fixture_test.go` does, and never as literals (`AGENTS.md`). It asserts that the live claim's `spec.version` equals that coordinate's `Version` and has no `v` prefix, so the bare-spelling step checks that the spelling really is bare. The `v`-prefixed edit is `"v"+Version`.

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
- The edit bumps the claim's generation. The claim's predicate passes generation changes, so the claim is re-judged.

The two reconcilers do not run in the order "judge, then build". `activeClaims` and `claimContributionPredicate` read `status.accepted` and `status.active` without checking `status.observedGeneration`. The edit moves the claim's coordinate from `@0.1.0` to `@v0.1.0` while it still carries the verdict for `0.1.0`, so the Platform regenerates with `v0.1.0` at once, carried by that stale verdict, possibly before the claim reconciler has judged `v0.1.0`. If the re-judgement then refused, the Platform would regenerate again without the catalog. The Platform half of the step therefore proves the build only once the claim's own verdict for the new generation is on record.

The claim's own fields need care too. `deferVerdict` sets `observedGeneration` to the new generation, writes Ready=Unknown and leaves `accepted` and `active` as the previous verdict left them. So `observedGeneration == generation && accepted && active` holds after any deferred reconcile (`PlatformNotReady` while the Platform regenerates, `ProviderInventoryPending`, a `platformRequirements` read error), even when the eventual verdict for `v0.1.0` is a refusal. Only `accept` writes Ready=True with reason `Accepted`, and its message is `Claim accepted for catalog <catalog> at <version>`.

Section 1 runs this flow by hand on a throwaway cluster before the spec encodes it.

**Decision**: After the bare-spelling assertions, the spec patches `backup-provider` with `spec.suspend: true` and waits for `Ready=False/Suspended`. It then merge-patches the claim's `spec.version` to `v0.1.0` and waits, in this order:

1. the claim's `status.observedGeneration` to equal its new `metadata.generation`, with `accepted: true`, `active: true` and Ready=True with reason `Accepted` and a message containing `at v0.1.0`;
2. then the Platform's `status.registry` entry for the backup catalog to name the same build (`0.1.0` once any leading `v` is trimmed; see the spike findings below), under a `status.packageIdentity` different from the bare-spelling one, with Ready=True/`Generated`; a second read of `packageIdentity` shows it is stable.

The stale-verdict contribution is an operator gap outside this test-only change: a hand-edited claim contributes its new coordinate to the platform before it is judged, which skips the 0015:D11 checks for a short window. It goes to the supervisor as a follow-up issue.

**Spike findings** (section 1, 2026-10-04, throwaway kind cluster, branch image on library beta.4):

- Bare spelling: the claim `default.backup-provider` was rendered with `spec.version: 0.1.0`, accepted and active within seconds (Ready=True/`Accepted`, "Claim accepted for catalog testing.opmodel.dev/catalogs/operator/backup@v0 at 0.1.0"; Active=True/`ProviderReady`). The Platform moved to a new identity (`gen-1-e1f6ae3f2c30aa1a`) with the backup entry `{source: Registration, version: 0.1.0, enabled: true}` and Ready=True/`Generated`.
- `v`-prefixed spelling: after `suspend: true` the provider read Ready=False/`Suspended`. The merge-patch to `v0.1.0` held for the whole 137-second watch (no re-apply). Within 2 seconds the claim read generation 2, observedGeneration 2, accepted and active, Ready=True/`Accepted` with "... at v0.1.0", and the Platform read a new identity (`gen-1-716335246d4da9d5`), Ready=True/`Generated`. The identity stayed put for the rest of the watch.
- The Platform's backup entry recorded `version: v0.1.0`, the claim's own spelling, although the `ResolvedRegistryEntry.version` doc comment says "the bare SemVer build". The spec therefore checks that the entry names the same build in either spelling (the `v` trimmed), and relies on the package identity, a function of the claims' coordinates, to show the platform was regenerated for the edit. It does not pin the spelling of a status field whose documented contract disagrees with its behaviour. The disagreement goes to the supervisor as a follow-up.
- Deleting the suspended provider removed the instance and pruned the claim, and the claim's removal guard released it (both `kubectl wait --for=delete` calls returned 0). Deletion runs before the suspend check in the instance reconcile.
- Unrelated to the claim path, the controller log carried `Drift detection failed`: the drift dry-run of the rendered claim is sent as the controller's own ServiceAccount, which may not create `transformerregistrations` at cluster scope. The reconcile continues, so it does not affect this spec. It goes to the supervisor as a follow-up.

**Rationale**: This is the narrowest path that pushes a `v`-prefixed coordinate through both reconcilers on a deployed operator, and it changes no fixture. The alternatives:

- A second provider fixture cannot render `v0.1.0` (`#VersionType`).
- A hand-applied claim is refused `ProviderMismatch` before it reaches the catalog.

If section 1 finds that the edit is reverted or not re-judged, the change stops and reports to the supervisor. It does not pick another mechanism on its own.

### D3. No consumer render in e2e

The owner decision names acceptance and platform build. `backup_consumer` rendering through the claimed catalog is already covered at the integration tier (`backup_fixture_test.go`, third spec) and was checked once on a cluster (archived D4). Adding it here would add a removal-guard ordering to the teardown: the consumer has to go before the claim can. It would also make the `v`-prefixed step depend on the instance re-rendering when the platform changes. Neither is needed for i1.

### D4. N4: consumers absorbed library f1d9908

**Context**: Library f1d9908 (`feat(kernel)!`, first in v1.0.0-beta.2) makes `AcquireInstanceFromDir` validate an instance package's own values against the module's `#config`. Research N4 asked for a sweep of every consumer's instance packages.

**Explored** (at each repo's `origin/main` on 2026-10-04, SHAs refreshed when section 1 ran):

| Where | Instance packages | Verdict |
| --- | --- | --- |
| opm-operator (`8dc24b3`, library beta.4) | `test/fixtures/modulepackages/{hello,hello_web,podinfo,redis}`, acquired by `KernelPackageRenderer` | Every values key is declared in the module's `#config` with a matching type (`message`; `replicas`; `replicas`; `persistence.size`). Conforms. |
| cli (`abc0093c`, library beta.4) | `examples/instances/podinfo` (`replicas`), `tests/e2e/testdata/operator-owned` (`image`, `replicas`), `internal/workflow/render/testdata/skip-unprovided/instance` (`values: {}`), `tests/integration/inst-tree/testdata` (no values) | Conforms. `tests/e2e/testdata/vet-errors/instance` is invalid on purpose: its test expects the refusal. |
| cli scaffold | `opm instance init` (`internal/cmd/instance/init.go`) writes `values.cue` from the module's `initValues`/`debugValues` | Derived from the module, so it conforms by construction. |
| catalog_opm (`344ad4f`) | none | Not affected. |
| modules (`e4d3b65`) | none (`istio_ambient/testdata/values-full.cue` is passed as a values source, which was already validated before f1d9908) | Not affected. |
| opm-modules (`f16a187`) | none | Not affected. |

Both frontends already pin library beta.4, and their CI went green on that bump.

**Decision**: Nothing in opm-operator needs a fix. Section 1 confirms the operator half by running the registry-backed ModulePackage specs (`KernelPackageRenderer Integration`) against GHCR on beta.4. If one fails on an undeclared or mistyped value, the fix is to the fixture's `values.cue` in this change.

**Confirmed** (section 1): with the GHCR mapping and `OPM_TEST_REGISTRY_FORCE=1`, the focused run reported `5 Passed | 0 Failed | 0 Pending`, and the four "acquires the authored <pkg> package fixture" specs (hello, hello_web, podinfo, redis) ran and passed; the 63 skipped specs are the ones outside the focus. No fixture changed.

### D5. Running the spec

The e2e suite runs `make install`, `make deploy`, `make undeploy` and `make uninstall` against the current kube context. Uninstalling the CRDs deletes every ModuleInstance on the cluster. The shared `kind-opm-dev` cluster holds the cli's operator-owned state, so the spec runs on a throwaway kind cluster, `KIND_CLUSTER=opm-operator-test-e2e`, created and deleted for the run on the same kind provider as `kind-opm-dev`. The run still goes through `flock` on the workspace cluster lock, because it competes for the same container host.

A kubectl `--kubeconfig` flag cannot be passed into the suite: every `kubectl` and `make` call in it uses the ambient context. So the throwaway cluster is created with its own kubeconfig file (`kind create cluster --kubeconfig <path>`), and the run exports `KUBECONFIG=<path>` and checks that `kubectl config current-context` reads `kind-opm-operator-test-e2e` first. Without that, `make uninstall` could run against `kind-opm-dev`. The run is focused on the new spec (`-ginkgo.focus`) and passes `-timeout 30m`, as `task dev:e2e` does: a cold run (image build, cert-manager install, deploy, two platform builds) can pass Go's 10-minute default. The full suite is CI's job.

The section 1 spike uses the same environment.

## Risks / Trade-offs

- **GHCR availability.** The spec pulls the opm catalog and the backup fixtures from GHCR, like the podinfo spec. A registry outage fails it, as it fails the rest of the suite.
- **The `v`-prefixed path relies on suspension.** If suspend semantics change so that a suspended instance re-applies its output, the edit is reverted and the spec times out. The failure names the claim's version, so it reads as that cause.
- **A `backup` catalog bump needs two pull requests.** The spec resolves the claim's literal catalog version live from GHCR. A pull request that bumps `test/fixtures/catalogs/backup` and re-pins the claim in the same change publishes only `-e2e.g<sha>` pre-releases in CI, so the new real version is not on GHCR yet; the claim is refused `CatalogUnresolved` and this spec fails on that pull request. Before this change no e2e spec depended on the literal. The bump therefore lands as two pull requests, one that publishes the catalog and one that re-pins the claim. `test/fixtures/modules/README.md` says so next to the bump instructions.
- **Run time.** The spec adds one controller deploy, about one to three minutes on a warm cluster. That fits inside the suite's `-timeout 30m`.
