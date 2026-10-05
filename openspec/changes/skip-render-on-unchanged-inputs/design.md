## Context

Both reconcile loops render unconditionally once they get past their gates (ownership, finalizer, deletion, suspend, and for a ModulePackage `dependsOn` and source resolution). No-op detection happens after the render: the loop compares four digests (source, config, render, inventory) with the `lastApplied*` ones, and the render digest and inventory digest only exist once the render has run.

```go
// internal/reconcile/moduleinstance.go (origin/main dd0d798, abridged)
digests.Source = status.ModuleSourceDigest(mi.Spec.Module.Path, mi.Spec.Module.Version)
digests.Config = status.ConfigDigest(mi.Spec.Values)
params.RenderSlots.Run(ctx, func() { renderResult, converted, err = renderAndConvertInstance(...) })
...
isNoOp := noOp(digests, lastApplied, refused)
phases.driftRan = true
phases.driftFailed = detectDrift(ctx, params.ResourceManager, &mi, applyList)
if isNoOp { outcome = NoOp; return ctrl.Result{}, nil }
```

What triggers a reconcile today:

- **ModuleInstance**: a spec edit (`GenerationChangedPredicate`), a Platform event that moves a consumed field (`platformConsumedFieldsChanged`, from j1), a failed attempt's requeue, the manager's initial list at start, and the informer resync (controller-runtime default about 10 hours). A successful reconcile requeues nothing.
- **ModulePackage**: the same, plus a source revision change and a requeue on `spec.interval` (default 5 minutes) after every outcome.

So the renders that find nothing to do are: every ModulePackage interval, every object at operator start, every object on a Platform event that does not change what it renders against, and every resync. At operator start the platform store is empty, so those renders fail `PlatformNotReady` first, flip `Ready` to False, and render again on the backoff or when the regenerated platform's status write arrives.

Drift detection (`detectDrift`, SSA dry-run of the apply list) runs only in the ModuleInstance loop, on every reconcile that rendered. It reports `Drifted` and never re-applies. The ModulePackage loop runs no drift detection.

Reconcile phase impact: a new pre-render check before the artifact fetch (ModulePackage, after source resolution) or Render (ModuleInstance). When it passes, Render, drift detection, Apply, Prune and the status commit do not run. When it does not, every phase runs as today, and the status commit additionally records `lastAppliedInputs` on a successful apply or a `NoOp`.

## Goals / Non-Goals

**Goals:**

- A reconcile whose render inputs are unchanged since the last render that left the cluster holding their output does not render, unless that render is older than the drift render interval.
- A skip never hides an input change, a failed attempt, or an unobserved spec edit.
- An operator or library upgrade always renders once, so a change to how the operator computes its stored digests reaches every object.
- `--drift-render-interval=0` renders on every reconcile and writes on a `NoOp` exactly what it wrote before this change; only a successful apply also records the key, in the status write it makes anyway.

**Non-Goals:**

- Caching render output (g1). Nothing rendered is stored; the key is a digest of inputs only.
- A periodic re-render for ModuleInstances. Today a successful ModuleInstance reconcile requeues nothing; the interval bounds how often a reconcile renders, it does not schedule one.
- Keying on inputs the owner's decision does not name: the operator's `--registry` mapping, a module version republished with different content, cluster state read by the shrink guard. They reach the cluster on the first render after the interval, or on any render an input change causes. The interval is the backstop for exactly these.
- Changing no-op detection. After a render, the four digests decide `NoOp` as before.
- The ModulePackage's missing drift detection. It stays absent.

## Research & Decisions

### 1. The key's parts and encoding

**Context**: The owner named the parts: source, values, platform identity, operator/library version, skew.

**Explored**: `internal/status/digests.go` already computes the source digest per kind (`ModuleSourceDigest` for a ModuleInstance; the ModulePackage uses the Flux artifact digest) and the config digest (`ConfigDigest(spec.values)`; a ModulePackage carries no values and hashes empty input). The platform's identity is `internal/platform.PackageIdentity`, whose string form the Platform reconciler writes to `Platform.status.packageIdentity` and both renderers report as `RenderResult.PlatformIdentity`. It is a function of the Platform generation and the active claims (0015:D13/D17). The skew policy is `kernel.SkewPolicy` (an int), resolved from `Platform.spec.skewPolicy` by the Platform controller and held on the store record. The operator version is `version.Full()`, the string the Platform reconciler publishes to `status.operatorVersion`.

**Decision**: A plain struct in `internal/status`, and one digest function. The skew policy is carried by its API spelling (`Warn`, `Refuse`) so the key does not depend on the library's enum values. The digest is a SHA-256 over a fixed, versioned, length-prefixed encoding of the parts in a fixed order, independent of Go field names and JSON tags, in the `sha256:<hex>` form of the other digests.

```go
// internal/status/digests.go (sketch)
type RenderInputKey struct {
	Source          string // ModuleSourceDigest, or the Flux artifact digest
	Config          string // ConfigDigest(spec.values); ConfigDigest(nil) for a ModulePackage
	PackageIdentity string // Platform.status.packageIdentity / RenderResult.PlatformIdentity
	SkewPolicy      string // "Warn" or "Refuse"
	OperatorVersion string // version.Full()
	LibraryVersion  string // version.Library()
}

// Complete reports whether every part is set. An incomplete key is never
// recorded and never matches, so a missing part always means "render".
func (k RenderInputKey) Complete() bool

// Digest is "sha256:<hex>" over "opm-render-inputs/v1" and each part as
// "<len>:<value>", in the order of the fields above.
func (k RenderInputKey) Digest() string
```

**Rationale**: Length prefixes make the encoding unambiguous without escaping. The version tag in the encoding lets a later change alter the parts, and a changed encoding only ever causes one extra render (the stored digest no longer matches), never a wrong skip.

### 2. The pre-render key reads the Platform CR; the recorded key uses what the render leased

**Context**: The render reads the platform from the in-memory store, under a lease, after it gets a render slot. The pre-render check must decide before any of that. Owner j1 named `status.packageIdentity` as the pin-set field, and the g3 decision ties the key to it.

**Explored**: Two sources for the identity before rendering. (a) The store (`Store.Generated()`), the exact record a render would lease now. It is empty after every operator start until the Platform reconciler has rebuilt the platform, so it would make every object render at start, which is the burst this change exists to remove. (b) The Platform CR from the cached client: `status.packageIdentity` survives a restart, and its skew policy is `spec.skewPolicy`, resolved the way the Platform controller resolves it.

The two can disagree for a short time. The Platform reconciler records a new package in the store, then writes `status.packageIdentity`; a spec edit bumps `metadata.generation` (and may change `spec.skewPolicy`) before the platform is regenerated. Whatever the window, the recorded key must never name inputs newer than the ones the render used, or a later reconcile would skip a render it needs.

**Decision**: The pre-render key reads (b): `Platform.status.packageIdentity` and the resolved `spec.skewPolicy` of the `cluster` Platform, through the reconciler's cached client. A missing Platform or an empty `packageIdentity` makes the key incomplete (render). The key recorded on success or `NoOp` uses the render's own report: `RenderResult.PlatformIdentity` and the new `RenderResult.SkewPolicy`, both from the store record the render leased.

**Rationale**: Every disagreement makes the keys differ, which renders; none makes them match wrongly. Case by case:

- Store ahead of the CR (new package stored, status write not yet made): the pre-render key still names the old identity and may match, so the reconcile skips. The status write that follows moves `packageIdentity`, which `platformConsumedFieldsChanged` passes, which re-enqueues the object, whose key now differs.
- CR spec ahead of the store (skew policy edited, platform not yet regenerated): the pre-render skew differs from the recorded one, so the reconcile renders under the old package and records the old pair; the regeneration's status write then renders again under the new package. At most one extra render per edit, the same extra render the j1 predicate already accepts.
- Operator start: the CR still carries the last package identity, so an unchanged object skips with the store still empty, and never reaches `PlatformNotReady`. If its inputs did change, it renders, fails `PlatformNotReady` as today, and recovers as today.

The skew policy is a function of the Platform generation, which is part of the identity, so it rarely adds information; it stays in the key because the owner named it and because reading it costs nothing.

The singleton's name moves to `internal/platform` (`SingletonName = "cluster"`) and the resolution moves from the Platform controller to `internal/platform.ResolveSkewPolicy`, with `SkewPolicyName` for the API spelling, so both the Platform controller and the reconcile package use one definition.

### 3. The versions come from the binary, and an upgrade always renders

**Context**: The owner put the operator and library versions in the key, so an upgrade re-renders. A later planned change (the operator adopting the library's `opm/k8s/inventory` digests) changes every stored render and inventory digest once and is meant to apply every object once on its first reconcile after the upgrade. The skip must not defer that apply.

**Explored**: `version.Full()` is `v` plus the release-please-managed constant, with `+g<rev>[.dirty]` when the binary carries VCS info. The library version is in the binary's build info: `go version -m` on a `go build ./cmd` binary shows `dep github.com/open-platform-model/library v1.0.0-beta.4` (checked 2026-10-05 on dd0d798). A `go test` binary carries no dependency list (`debug.ReadBuildInfo` finds no library dep, checked in `internal/render`), so a helper that reads build info returns empty under tests.

**Decision**: `version.Library()` returns the library's module version from build info (the replacement's version, or `(devel)` with the replacement path, when it is replaced), and `""` when absent. `cmd/main.go` reads both versions once and passes them to both reconcilers, which pass them in their params; tests set them explicitly. An empty library version makes every key incomplete, so a binary without build info renders every time, and the manager logs both versions at startup. The rule for later changes is written into `docs/RENDERING.md` and the `render-input-key` spec: a change to how the operator computes a stored digest ships in an operator release (a new `version.Version`) or with a library bump, and either one changes the key, so every object renders on its first reconcile after the upgrade and the digest mismatch makes it apply once. An envtest spec pins it: an object whose recorded key was computed with an older operator version renders and applies when its render digest no longer matches.

**Rationale**: Reading the versions once at start and injecting them keeps the reconcile package free of process globals and lets tests drive the upgrade case directly. Failing open on an unknown library version costs renders, never correctness.

### 4. When a reconcile may skip

**Context**: "Skip render when the key matches" alone would skip in cases where a render is needed for reasons outside the key.

**Decision**: A reconcile skips only when all of these hold, checked in this order (cheapest first):

1. `params.DriftRenderInterval > 0` (the flag; zero in params also disables it, so every existing test and caller keeps rendering).
2. `status.lastAppliedInputs` is set, and `now - renderedAt < DriftRenderInterval`.
3. `Ready` is `True` with reason `ReconciliationSucceeded`.
4. `status.observedGeneration == metadata.generation`.
5. The pre-render key is complete and its digest equals `status.lastAppliedInputs.digest`.
6. ModulePackage only: the source just resolved (ref, revision, digest, URL) equals `status.source`.

```go
// internal/reconcile/inputs.go (sketch)
type renderSkip struct {
	interval        time.Duration
	operatorVersion string
	libraryVersion  string
	now             func() time.Time
}

func (s renderSkip) maySkip(obj conditions.Getter, gen, observed int64,
	recorded *releasesv1alpha1.RenderInputs, key status.RenderInputKey) bool {
	if s.interval <= 0 || recorded == nil || s.now().Sub(recorded.RenderedAt.Time) >= s.interval {
		return false
	}
	ready := apimeta.FindStatusCondition(obj.GetConditions(), status.ReadyCondition)
	if ready == nil || ready.Status != metav1.ConditionTrue || ready.Reason != status.ReconciliationSucceededReason {
		return false
	}
	return gen == observed && key.Complete() && key.Digest() == recorded.Digest
}
```

**Rationale**:

- (3) A failed or refused attempt leaves `Ready=False`, and `lastAppliedInputs` still names the last success. Without (3) a revert to the last applied inputs after a failed apply would skip, and a partly applied failed attempt would stay in the cluster unreported. With it, the reconcile renders, finds `NoOp` against the last applied digests, and drift detection reports what the failed attempt left, as today. A refused shrink (0015:D16) is `Ready=False` too, so its refusal is re-decided on every reconcile, as today. A suspended object is `Ready=False` with reason `Suspended`, so a resume renders.
- (4) A spec edit that changes no key part (`serviceAccountName`, `prune`, `rollout`) renders, as today, so a field the key does not cover can never be skipped past, and the commit after it writes `observedGeneration`, which `kubectl wait` and the cli read. A CLI handback is a spec edit (`spec.owner`), so it always renders, whatever the cli wrote to the digests meanwhile.
- (2) bounds how long anything outside the key can go unseen, and keeps drift detection running at most once per interval per object.
- (6) The key carries the artifact digest, not the revision. A Flux revision that moves without moving the digest (a commit touching only ignored paths) would otherwise leave `status.source.artifactRevision` stale, against the requirement that `status.source` reflects the resolved artifact. With (6) that reconcile renders, finds `NoOp`, and the `NoOp` patch records the new source, as today. The comparison is made against the persisted `status.source` before Phase 1 overwrites it in memory.

### 5. What a skipped reconcile does and does not do

**Decision**: A skip happens before the deferred status commit is armed (ModuleInstance) or makes it return without a patch (ModulePackage, whose source resolution runs after the defer is set up). Nothing is patched: every condition the skip read is already in its final state, and a patch would only write the transient `Reconciling` the loop sets at the start. No event is emitted (a ModulePackage `NoOp` emits one per interval today; a skipped interval does not). One `Info` log line, `Render inputs unchanged, skipping render`, with the time the next reconcile may render. No metric is added: the log line shows that a skip happened, and a counter was not part of the decision. The ModuleInstance returns `ctrl.Result{}`; the ModulePackage returns `RequeueAfter: interval`. `lastAppliedVersion`, `requiredContracts`, `Drifted`, the inventory, history and failure counters are left as they are, because none of them can have moved without a key part moving.

The ModuleInstance check sits after the suspend and resume checks and before `MarkReconciling`. The ModulePackage check sits after source resolution (Phase 1, which yields the artifact digest the key needs) and before the artifact fetch, so a skip also saves the download and extraction.

**Rationale**: A skipped reconcile is not an attempt: it proves nothing new, so it must not move `lastAttempted*`, history or `renderedAt`. Moving `renderedAt` on a skip would make the interval never expire.

### 6. When the key is recorded

**Decision**: The deferred commit writes `status.lastAppliedInputs` in two places, from one helper:

- on a successful apply (`reconciled`), next to the other `lastApplied*` fields;
- on a `NoOp` that rendered, next to `lastAppliedVersion` (a `NoOp` means the cluster holds what these inputs produce, so the key is re-proved and `renderedAt` moves), but only while `DriftRenderInterval > 0`. With the skip disabled nothing reads the field, and moving `renderedAt` would turn every `NoOp`, which today patches nothing that changed, into a status write.

Both build the key from the render's report (decision 2) and the params' versions. When that key is incomplete (a stub render with no identity, a binary with no library version), the field is cleared, so a stale key never survives a render whose inputs could not be named. A failed attempt, a refusal, a panic and a skip leave it. The `NoOp` patch set in `reconcile-loop-assembly` gains the field; `lastAttempted*`, inventory and history stay out of it.

```go
// internal/reconcile/inputs.go (sketch)
// The NoOp commit calls it only when DriftRenderInterval > 0.
func recordInputs(field **releasesv1alpha1.RenderInputs, key *status.RenderInputKey, now metav1.Time) {
	if key == nil { // this attempt did not render
		return
	}
	if !key.Complete() {
		*field = nil
		return
	}
	*field = &releasesv1alpha1.RenderInputs{Digest: key.Digest(), RenderedAt: now}
}
```

### 7. The API field

**Decision**: One optional struct-valued field on both status types, after `lastAppliedVersion`:

```go
// RenderInputs records the inputs of the last render that left the cluster
// holding its output.
type RenderInputs struct {
	// Digest is a SHA-256 over the render's inputs: the module source, the
	// values, the platform package identity, the catalog skew policy, and the
	// operator and library versions.
	Digest string `json:"digest"`
	// RenderedAt is when that render ran.
	RenderedAt metav1.Time `json:"renderedAt"`
}

// +optional
LastAppliedInputs *RenderInputs `json:"lastAppliedInputs,omitempty"`
```

**Rationale**: The owner asked for one additive field holding the key. The interval needs the time of the confirming render, and no existing field carries it (`lastAppliedAt` does not move on a `NoOp`, `lastAttemptedAt` deliberately does not move on a `NoOp`). Keeping both in one struct makes them move together. The `lastApplied` prefix follows `lastAppliedVersion`: it describes the last apply and is re-proved by a `NoOp` that rendered. The parts are not stored in plain text: the source and config are already in the `lastApplied*` digests, the package identity and versions are on the Platform, and a readable copy is more API than the decision asked for.

### 8. The interval's default

**Context**: The owner set no number. The ModulePackage default interval is 5 minutes; drift detection is informational only; renders are the operator's memory peak.

**Decision**: `--drift-render-interval` defaults to `30m`. `0` disables the skip and the `NoOp` record. A negative value exits at startup with the same message shape as `--max-concurrent-renders`; the check is a small function in `cmd` with a table test. The flag is not added to `config/manager/manager.yaml`: the default applies.

**Rationale**: An idle ModulePackage renders every 30 minutes instead of every 5, and an operator restart renders only objects whose last confirming render is older than 30 minutes or whose inputs moved. A drift report or an out-of-key change (decision 4, (2)) waits at most that long after a reconcile triggers. Thirty minutes is the writer's choice; it is one flag default, set in `cmd/main.go`, and the change does not depend on its value.

### 9. Order against neighbouring changes

The defer blocks this edits are the ones #236 (recovered panic) and #242 (`lastAppliedVersion`) shaped; both have merged. The ModulePackage Platform watch predicate (#237) is what turns a `packageIdentity` move into a reconcile for packages, which decision 2's first case relies on. The later inventory-digest adoption depends on decision 3's rule and is sequenced after this change. The library i3 and g2 adoption rewrites `resultFromRender`; this change sets `SkewPolicy` after `resultFromRender` returns, as #242 did for `ModuleVersion`, so the two do not collide.

## Risks / Trade-offs

- [Drift on an unchanged object is re-evaluated at most every 30 minutes, and only when a reconcile is triggered] → Drift detection is informational; `0` restores per-reconcile rendering. ModuleInstances had no periodic re-render before this change either.
- [A key part is left out by mistake and a needed render is skipped] → Condition (4) renders every spec edit whatever the key says, condition (3) renders after every failure, and the interval bounds anything else; the key's version tag lets a later change add a part at the cost of one render per object.
- [Status write per confirming render] → With the skip on, a `NoOp` that rendered now always writes (`renderedAt` moves). At most one write per object per interval, since a skip writes nothing. With `0`, a `NoOp` does not record and writes nothing new.
- [Identity from `Platform.status`, skew from `Platform.spec`] → After a `spec.skewPolicy` edit whose regeneration fails, the Platform keeps its old package and identity while the pre-render skew already names the new policy, so no key matches and every reconcile renders until the platform is fixed. It fails open (renders, never skips wrongly) and is documented in `docs/RENDERING.md`.
- [Platform recreated at the same generation] → `packageIdentity` is `gen-<generation>[-claims]`, so a Platform deleted and recreated to the same generation and claim set before an instance reconciles names the same identity; that instance may skip the new platform's content for up to the interval. Documented in `docs/RENDERING.md` beside the other out-of-key cases the interval bounds.
- [A dev image keeps its version] → Release images build without `.git`, so a development image built from `main` reports the same `version.Version` until a release bumps it. The upgrade rule holds for releases; a later change that moves stored digests runs its e2e with `--drift-render-interval=0` or expects the delay.
- [A later per-reconcile check placed after the render] → A check that must run on every reconcile (a planned `Healthy` condition that requeues until a rollout converges) would never run within the interval if it sits after the skip. `docs/RENDERING.md` states the rule: such a check runs before the skip, or makes its own not-yet-converged state a no-skip condition.
- [A Platform status write that fails leaves `packageIdentity` behind the store] → The pre-render key then matches the older identity and skips; the next successful status write re-enqueues the object. Before this change the render would have used the newer store record immediately. The window is one Platform reconcile.
- [The cli's e2e drives the embedded operator] → The cli e2e flows are spec edits and handbacks, which always render (condition 4). Checked when the operator kind suite runs in the last section; a cli e2e run is left to the cli's own CI.
