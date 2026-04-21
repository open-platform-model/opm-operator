# CRD Lifecycle Research — Design Input for OPM

Status: Research / design input. Not an accepted ADR.
Date: 2026-04-21
Audience: OPM model (catalog), CLI, opm-operator.

## TL;DR — Recommended Path

Treat CRDs as **privileged, platform-managed infrastructure** — not as ordinary module resources. Bake a four-part model into OPM:

1. **Explicit CRD ownership.** Each CRD is declared by exactly one module as `ownership: owns`. Other modules may `require` or `observe` a CRD but may not upgrade or delete it. Conflicts are rejected at admission, not silently resolved by last-write-wins.
2. **Phase separation.** CRD installation is a distinct pre-install stage with a `CustomResourceDefinition.status.Established=True` readiness gate. CR application never races with CRD establishment. This exists partially today via Flux SSA staging — formalize it and add the Established check.
3. **Breaking-change gate.** Module upgrades that shrink a CRD schema (remove property, widen required, narrow enum, change list type) are rejected unless the ModuleRelease explicitly sets `breaking: true`, and unless a storage-version migration plan is declared. Use CRD schema diffing at reconcile time — do not rely on Kubernetes validation ratcheting as a substitute.
4. **Refuse auto-delete.** CRDs outlive their installing module. `Prune` never deletes a CRD — deletion is an explicit operator action (`opm crd remove …`) with an orphan-CR count shown and confirmation required.

Transport: **Server-Side Apply** (already in use — retain). Field manager: `opm-controller` (already in use).

Everything else in this document is the justification for these four choices and the concrete shape they take in the ModuleRelease API, the reconcile loop, and the CLI.

---

## 1. OPM Today — What Already Exists

From a survey of `catalog/`, `modules/`, and `opm-operator/`:

| Concern | Current state | File |
| --- | --- | --- |
| CRD shipping | Bundled inline per module via CUE (`crds_data.cue`) | `modules/mongodb_operator/crds_data.cue:1-132`, `modules/clickhouse_operator/crds_data.cue:1-127`, `modules/otel_collector/crds_data.cue`, `modules/ch_vmm/crds_data.cue` |
| Apply transport | Flux `ResourceManager.ApplyAllStaged`, Server-Side Apply, field manager `opm-controller` | `opm-operator/internal/apply/apply.go:23-60`, `opm-operator/internal/apply/manager.go:23-29` |
| Apply ordering | Weight table, `WeightCRD = -100` (first) | `opm-operator/pkg/resourceorder/weights.go:7-114` |
| Reconcile phases | 7 phases; Phase 5 = apply, Phase 6 = prune | `opm-operator/adr/009-reconcile-loop-phases.md` |
| Stage gating | Cluster-def stage (CRD+Namespace+ClusterRole) applied with readiness wait, then class-def stage, then workloads | `opm-operator/docs/design/flux-ssa-staging.md` |
| Release dependencies | `Release.spec.dependsOn[]` waits for another Release's `Ready=True` | `opm-operator/api/v1alpha1/release_types.go:44-47` |
| Pruning | `Release.spec.prune: bool`, all-or-nothing on Release deletion | `release_types.go:44-47` |
| CRD-specific lifecycle | Nothing. No ownership model, no conflict detection, no breaking-change gate, no established-condition check, no storage-version migration, no dedicated decommission path | — |

### Gaps to Close

1. No intra-module phase expression — a module cannot say "CRD here, then workload, then CR".
2. No explicit wait on `CRD.status.conditions[Established]=True` — relies on implicit timing.
3. `dependsOn` is Release→Release only, not Release→CRD or Release→CR-kind-available.
4. No CRD schema versioning or incompatibility detection at reconcile.
5. Prune is binary; no phase-aware or type-aware cleanup.
6. No concept of a CRD being owned by a different Release than the one applying CRs against it.

These gaps are the target surface for this work.

---

## 2. Problem Space

Six concerns drive CRD lifecycle:

- **Ordering.** CRD must be Established before any CR of that kind, and before the controller that watches it starts reconciling. Most bugs in the ecosystem come from racing this.
- **Storage version migration.** Storage version is the single on-disk encoding in etcd. Changing it without re-writing existing objects leaves mixed-version storage — debuggable only with difficulty.
- **Schema evolution.** Kubernetes cannot un-persist fields that existing CRs carry. Removing a required field or narrowing an enum breaks existing objects.
- **Finalizer deadlock.** Deleting a CRD while CRs have finalizers and the controller is gone — CRs can never be cleaned up; the CRD cannot finish deletion.
- **Cascade deletion.** `kubectl delete crd X` cascades to every CR of kind X. No grace period, no confirmation. This is the Helm-was-right-to-be-cautious scenario.
- **Ownership conflict.** Two modules installing the same CRD (different versions) — whichever applies last wins, silently. Kubernetes has no built-in ownership protocol.

---

## 3. Prior Art

Strict survey; details omitted where they don't inform OPM choices.

### Helm

Install-only semantics from the `crds/` directory. No upgrade, no delete. Safe by abdication. Hard upgrade path — users must manually reconcile. Annotation-size trap on large CRDs under client-side apply.

**Takeaway:** Helm's refusal to delete CRDs is correct. Its refusal to upgrade is a bug we should not inherit.

### OLM

Explicit `spec.customresourcedefinitions.owned` per ClusterServiceVersion. Rejects dual ownership. Blocks upgrades that would invalidate existing CRs.

**Takeaway:** This is the right model for OPM. We don't need InstallPlan approval gates, but we do want the ownership declaration and conflict rejection.

### Flux CD

Convention: separate Kustomization for CRDs with `prune: false` and `dependsOn` ordering from the app Kustomization.

**Takeaway:** Encode this as a schema rule, not a convention. OPM already uses Flux's SSA engine — we have the substrate, we need the semantics.

### Argo CD

Sync waves for ordering; `ServerSideApply=true` for large CRDs; `prune: false` via annotation for per-resource opt-out.

**Takeaway:** Per-resource prune opt-out is useful. CRDs should always get it implicitly.

### Kubebuilder / Operator SDK

Default: ship CRDs in the operator bundle. Works for single-operator, breaks in platform-scale multi-module scenarios.

**Takeaway:** We're not Kubebuilder's target. We must do better — we are the platform.

### Crossplane v2 ManagedResourceDefinitions

Selective activation of CRDs from a large catalog. Reduces etcd/discovery load when a catalog ships hundreds of CRDs you don't all use.

**Takeaway:** Relevant longer-term if OPM's catalog grows. Not required now.

### Server-Side Apply vs. Client-Side

`kubectl.kubernetes.io/last-applied-configuration` annotation has a 256 KB hard limit; Prometheus/cert-manager-style CRDs exceed it. SSA uses server-side `managedFields` with no such limit.

**Takeaway:** Non-negotiable — SSA only. Already in use.

---

## 4. Official Guidance

### SIG API Machinery position

- Storage version migration is the blessed path for schema changes that invalidate existing objects. `kube-storage-version-migrator` is beta in 1.30, targeting stable in 1.35.
- Conversion webhooks work but are expensive; every read of a non-storage version hits the webhook. Plan your storage version carefully and migrate rather than convert long-term.
- CRD API deprecation should follow the core-API convention: deprecated version remains served for at least three releases before removal.

### Validation Ratcheting (KEP-4008, stable 1.33)

Existing CRs with invalid fields can be updated if the invalid part is not changed. This is damage control for legacy data, **not** a license to ship breaking changes. Does not ratchet `x-kubernetes-validations` with `oldSelf`, required-list changes, list-type changes, or map-key changes.

**Takeaway for OPM:** Ratcheting means a breaking change isn't instantly catastrophic. It does not mean we should ship breaking changes carelessly. Keep the breaking-change gate.

---

## 5. The Hard Questions

### 5.1 Bundled vs. separated CRD shipping

**Decision:** Separated at the *runtime* level, bundled at the *authoring* level. A module author writes CRDs alongside workloads in the same CUE (convenient). The runtime applies CRDs in a distinct phase with separate readiness and prune semantics. The author sees bundled; the cluster sees separated.

### 5.2 Who owns the CRD?

**Decision:** Exactly one Release claims `ownership: owns` per CRD. Conflict detection is mandatory. Enforcement is in the operator at admission-like validation time (reconciler rejects + surfaces condition `CRDConflict`), not an admission webhook yet.

### 5.3 Can a module upgrade shrink a schema?

**Decision:** No, unless `breaking: true` is declared on the ModuleRelease for that CRD and a storage-version migration plan is either (a) declared and executed, or (b) explicitly waived by an annotation acknowledging risk. Default-deny; explicit-allow.

### 5.4 When can a CRD be deleted?

**Decision:** Never by `prune`. Only by an explicit operator-initiated `opm crd remove` CLI flow that:

1. Lists orphaned CRs by count and namespace.
2. Requires human confirmation.
3. Optionally deletes CRs first (`--cascade`) with a separate confirmation.
4. Verifies the controller is absent or stopped.
5. Deletes the CRD.

This mirrors Helm's safety on delete but without Helm's inability to upgrade.

### 5.5 Reconciling Git-declarative ("CRD gone") vs. runtime-safe ("CRs still exist")

**Decision:** Declarative removal (module no longer references CRD) does **not** trigger deletion. It transitions the CRD into an `orphaned` state surfaced in status and CLI. Cleanup is an explicit, separate action. The declarative model describes intent; runtime safety gates execution of destructive intents.

---

## 6. Recommended Design for OPM

### 6.1 ModuleRelease API additions

```yaml
apiVersion: opm.opmodel.dev/v1alpha1
kind: ModuleRelease
metadata:
  name: cert-manager
spec:
  crds:
    - name: certificates.cert-manager.io
      group: cert-manager.io
      servedVersions: [v1]
      storageVersion: v1
      ownership: owns          # owns | requires | observes
      breaking: false          # opt-in flag for shrinking changes
    - name: issuers.cert-manager.io
      group: cert-manager.io
      servedVersions: [v1]
      storageVersion: v1
      ownership: owns
      breaking: false

  crdPolicy:
    applyStrategy: ServerSideApply
    requireEstablished: true
    establishedTimeout: 60s
    storageVersionMigration:
      mode: AutoOnBreaking     # Off | Manual | AutoOnBreaking
      timeout: 30m
    onConflict: Reject         # Reject | TakeOver (with annotation)
    onShrink: Reject           # Reject | AllowWithBreaking

status:
  phase: Active                # Installing | Active | Decommissioning | Failed
  crds:
    certificates.cert-manager.io:
      installed: true
      established: true
      storageVersion: v1
      observedGeneration: 7
      crCount: 42
      lastAppliedHash: "sha256:…"
  conditions:
    - type: CRDsEstablished
    - type: CRDSchemaCompatible
    - type: CRDConflictFree
    - type: StorageVersionMigrationComplete
```

Key fields:

- `ownership`: enforces single-owner invariant across Releases.
- `breaking`: author acknowledges incompatibility; reconciler surfaces it in events and metrics.
- `onConflict: TakeOver`: escape hatch; requires explicit annotation on the Release to avoid silent capture.
- `crdPolicy.requireEstablished`: always-on gate. Off only for integration tests.

### 6.2 Reconcile loop changes (extending `opm-operator/adr/009`)

Insert a dedicated CRD sub-phase inside Phase 5 (apply):

```
5.a  CRD admission          — schema diff vs. observed CRD; detect shrink, conflict, ownership violation
5.b  CRD apply              — SSA apply CRDs with field manager opm-controller
5.c  CRD established wait   — poll .status.conditions[Established]=True, up to establishedTimeout
5.d  Storage migration      — if AutoOnBreaking and shrink detected, create StorageVersionMigration,
                              wait, proceed only on success
5.e  Non-CRD cluster defs   — Namespace, ClusterRole, etc. (existing)
5.f  Class defs             — (existing)
5.g  Workloads + CRs        — (existing)
```

Admission logic (`5.a`) uses a structural diff of the CRD OpenAPI schema against the live CRD. Deterministic, no webhook needed at this stage.

Prune (Phase 6) gets a hard rule: **never delete a resource with kind `CustomResourceDefinition`.** The `prune: true` flag on Release does not override this. CRDs leave via `opm crd remove` only.

### 6.3 Conflict detection

On `5.a`, for each CRD in the Release with `ownership: owns`:

1. Look up whether any other Release claims `owns` for the same CRD name.
2. If yes, reject with `CRDConflict` condition and an event naming the conflicting Release.
3. If not, annotate the CRD on apply with `opm.opmodel.dev/owner=<release-ns>/<release-name>`.

Observation-only claims (`ownership: requires` / `observes`) skip conflict checks and do not annotate.

### 6.4 Breaking-change detection

Structural comparison between the incoming CRD schema and the currently-Established CRD schema. A change is "breaking" if any of:

- property removed under `spec`, `status`, or any `items` subschema;
- `required` list added to or expanded;
- enum values removed;
- `type` changed;
- `x-kubernetes-list-type` changed;
- `x-kubernetes-map-keys` changed;
- pattern tightened (best-effort; if uncertain, mark as breaking).

If breaking and `breaking: false` → reject with `CRDSchemaCompatible=False`.
If breaking and `breaking: true` and `storageVersionMigration.mode=Off` → reject unless ack annotation present.
If breaking and `breaking: true` and migration configured → proceed to `5.d`.

### 6.5 Storage version migration

When `mode=AutoOnBreaking` and a breaking change is detected, the operator creates a `StorageVersionMigration` CR targeting the affected GVR and waits for `status.conditions[Succeeded]=True`. The CR spec for the migration must be generated *after* the new CRD is Established, so that re-writes hit the new storage version.

Prerequisite: `storagemigration.k8s.io/v1beta1` must be present in the cluster. OPM should check discovery at startup and surface a degraded condition if absent.

### 6.6 Decommission flow

`opm crd remove <crd-name>` CLI path:

1. Look up owning Release. Refuse if Release is still Active (must be deleted first or CRD re-assigned).
2. Count CRs across all namespaces. Show count, sample names, finalizer presence.
3. Refuse if finalizers present and no controller found, unless `--force` with a second confirmation.
4. If `--cascade`, delete all CRs; wait for finalizers to clear.
5. Delete the CRD.
6. Remove OPM ownership annotation and update Release status history.

The operator never initiates this flow. It is always human-driven via CLI or an explicit `CustomResourceDefinitionRemoval` CR (optional future extension).

### 6.7 CLI surface

```
opm crd list                        # all OPM-tracked CRDs with owning Release + CR count
opm crd describe <name>             # schema version, ownership, last apply, breaking-change flags
opm crd conflicts                   # list conflicts (should be empty; alarmable)
opm crd migrate <name>              # run StorageVersionMigration manually
opm crd remove <name> [--cascade]   # decommission path with confirmations
opm release diff <release>          # shows CRD schema diff vs. live, highlights breaking
```

### 6.8 Observability

Metrics:

- `opm_crd_owned_total{release}` — gauge.
- `opm_crd_cr_count{crd}` — gauge per CRD.
- `opm_crd_conflict_total` — counter; alert > 0.
- `opm_crd_breaking_change_blocked_total{release,crd}` — counter.
- `opm_crd_storage_migration_duration_seconds` — histogram.
- `opm_crd_established_wait_seconds{crd}` — histogram; tail indicates cluster health problems.

Events on the Release and on the CRD itself:

- `CRDApplied`, `CRDEstablished`, `CRDConflict`, `CRDShrinkRejected`, `StorageMigrationStarted/Completed/Failed`.

---

## 7. What This Means for the Catalog (CUE Model)

Add a primitive-level concept for declaring CRD ownership at the module authoring layer. A module in `catalog/` should express:

```cue
#Module: {
    ...
    crds?: [...#CRDDeclaration]
}

#CRDDeclaration: {
    name:           string                 // e.g. "certificates.cert-manager.io"
    group:          string
    servedVersions: [...string]
    storageVersion: string
    ownership:      "owns" | "requires" | "observes"
    breaking:       *false | bool
}
```

The catalog compiler emits these declarations into the ModuleRelease spec automatically from the module's `crds_data.cue`. Module authors continue to write CRDs bundled with the workload (unchanged developer ergonomics); the compiler extracts metadata for the runtime to enforce policies.

This fits alongside the existing `#Policy`, `#Rule`, `#Orchestration`, and `#Claim` primitives (see `catalog/enhancements/006-claim-primitive` and sibling enhancement ADRs). CRD declaration is a schema-level concern, not a behavioural primitive — it sits at the module metadata layer rather than the component graph.

---

## 8. Implementation Roadmap

Sequenced to land value early and defer the risky migration pieces.

**Phase 1 — Observability (low risk, high info)**

- Annotate applied CRDs with `opm.opmodel.dev/owner`.
- Count CRs per tracked CRD; publish metrics.
- `opm crd list` / `describe`.
- No behaviour change; prepares the ground.

**Phase 2 — Established gating**

- Add `requireEstablished` path inside Phase 5 (default on).
- Add timeout and condition `CRDsEstablished`.
- Removes latent races that work "most of the time".

**Phase 3 — Ownership & conflict detection**

- Extend ModuleRelease spec with `crds[].ownership`.
- Catalog compiler emits declarations.
- Reconciler rejects conflicts with `CRDConflict` condition.
- Prune hard-guards against CRD deletion.

**Phase 4 — Schema diff and breaking-change gate**

- Implement structural diff.
- Add `breaking` flag, `onShrink` policy, rejection events.
- `opm release diff` CLI.

**Phase 5 — Storage version migration**

- Wire `StorageVersionMigration` CR creation and polling.
- `opm crd migrate` CLI.
- Discovery check at operator startup.

**Phase 6 — Decommission CLI**

- `opm crd remove` with cascade + confirmations.
- Cleanup of ownership annotations.

Phases 1–3 are the core value. Phases 4–5 are the compliance layer. Phase 6 is the relief valve.

---

## 9. Explicitly Not Doing (Yet)

- **Multi-owner CRDs.** OLM allows this; it is rare and complex. OPM rejects shared ownership until a concrete use case forces reconsideration.
- **Conversion webhooks.** Out of scope. If an operator ships one, OPM installs and tracks it as an ordinary webhook resource but does not author or mutate it.
- **CRD dry-run apply.** Useful later; not in the critical path.
- **Per-namespace CRD visibility.** Crossplane v2-style MRD activation. Revisit if the catalog grows beyond ~50 CRDs per install.
- **Admission webhook for ownership.** Reconciler-side enforcement first. Webhook comes if drift between reconciles becomes a problem.

---

## 10. Open Questions for Discussion

1. **ModuleRelease authoring UX.** Should `crds[]` in ModuleRelease be authored by the user, or purely compiler-emitted from the module definition? Leaning: compiler-emitted, user-overridable for `breaking` only.
2. **Ownership takeover.** Is `onConflict: TakeOver` acceptable with an explicit annotation, or too dangerous even behind an opt-in? Leaning: allow, require both annotation and a second annotation with the previous owner's name as acknowledgment.
3. **Release vs. cluster scope.** Ownership annotation carries `<ns>/<release-name>`. Should a cluster-scoped "CRDRegistry" CR exist as a central record, or is scanning Releases + CRD annotations sufficient? Leaning: start with annotations; add registry if query performance demands it.
4. **Catalog primitive naming.** `#CRDDeclaration` vs. embedding into `#Claim` with `kind: crd`. Leaning: separate primitive — CRDs are structural, `#Claim` is behavioural.
5. **Breaking detection algorithm.** Hand-rolled structural diff vs. CUE schema-based diff (since we already have CUE types). Leaning: CUE-based, with a fallback OpenAPI diff for schemas we didn't author.

---

## 11. Sources

Kubernetes core:

- <https://kubernetes.io/docs/tasks/extend-kubernetes/custom-resources/custom-resource-definition-versioning/>
- <https://kubernetes.io/docs/tasks/manage-kubernetes-objects/storage-version-migration/>
- <https://kubernetes.io/docs/reference/using-api/server-side-apply/>
- KEP-4008 (Validation Ratcheting): <https://github.com/kubernetes/enhancements/issues/4008>
- KEP-4192 (in-tree Storage Version Migration): <https://github.com/kubernetes/enhancements/issues/4192>
- kube-storage-version-migrator: <https://github.com/kubernetes-sigs/kube-storage-version-migrator>

Tool-specific:

- Helm CRD best practices: <https://helm.sh/docs/chart_best_practices/custom_resource_definitions/>
- OLM dependency resolution: <https://github.com/operator-framework/operator-lifecycle-manager/blob/master/doc/design/dependency-resolution.md>
- Gateway API CRD management: <https://gateway-api.sigs.k8s.io/guides/crd-management/>
- Kubebuilder CRD generation: <https://book.kubebuilder.io/reference/generating-crd.html>
- Crossplane ManagedResourceDefinitions: <https://docs.crossplane.io/latest/managed-resources/managed-resource-definitions/>

Community / incidents:

- CRD deletion behaviour: <https://marcincuber.medium.com/what-happens-when-you-delete-a-kubernetes-customresourcedefinition-crd-d5e741fb6441>
- Multi-tenancy pain: <https://www.vcluster.com/blog/kubernetes-crds-huge-pain-in-multi-tenant-clusters>
- ArgoCD SSA for bulky CRDs: <https://medium.com/@paolocarta_it/argocd-server-side-apply-for-bulky-crds-373cd3c0ac2a>
- `262144 bytes` annotation error: <https://www.arthurkoziel.com/fixing-argocd-crd-too-long-error/>
- Celonis on breaking CRD changes: <https://careers.celonis.com/blog/updating-crds-through-breaking-changes>

OPM internal references:

- `adr/009-reconcile-loop-phases.md`
- `docs/design/flux-ssa-staging.md`
- `pkg/resourceorder/weights.go`
- `internal/apply/apply.go`
- `api/v1alpha1/release_types.go`
- `catalog/enhancements/006-claim-primitive`
