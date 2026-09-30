## Context

See proposal.md (Why) for the bug and why it is reachable today. This change is E of the five-change set in `orchestration.md`; it is independent of A to D.

State on `origin/main` (2e348a6, library `v1.0.0-alpha.35`), read 2026-09-30:

- **Caller.** `transformerregistration_controller.go:188-200` reads the generated platform's `cue.mod/module.cue` through `platformRequirements(generated.Dir)` (major-qualified path to version, `modfile.Parse`), then `buildIncompatibility(cat, platformReqs)`; a non-empty message refuses with `BuildIncompatible`. Neither changes.
- **The index.** `transformerregistration_buildcompat.go:97-107` folds `platformReqs` into `byBase map[base]resolved{qualifiedPath, version}`, last write wins. Lines 115-148 walk the provider's requirements in sorted order, skip non-OPM and unshared paths, refuse `catalogMajor != platformMajor` as "majors are not comparable", skip an empty version on either side, and refuse `semver.Compare(catalog, platform) > 0` as "a provider cannot require a newer build".
- **The closure.** The Platform reconciler writes the closure of `platformmodule.Roots(entries)` (`platform_controller.go:225`), and `Roots` (library `opm/helper/platformmodule/generate.go:65-80`) adds every registry entry, disabled ones included ("a disabled entry still imports its catalog"). So `opm@v4` enabled plus `opm@v5` disabled yields both paths in `platformReqs`.
- **Measured.** Lines 99-107 copied against cue v0.17.1 with deps `{opmodel.dev/core@v2, opmodel.dev/catalogs/opm@v4 v4.2.0, opmodel.dev/catalogs/opm@v5 v5.0.0}`: over 1000 calls `byBase["opmodel.dev/catalogs/opm"]` held `opm@v4` 126 times and `opm@v5` 874 times.
- **Test helpers.** `transformerregistration_buildcompat_test.go`: `requiringCatalog(deps, contracts...)`, `platformDirWith(deps)` and `buildCompatReconciler(cat, platformDeps)` already take multi-path maps. They use global Gomega (`GinkgoT().TempDir()`, `Expect`), so new specs are `It` specs inside the existing build-compatibility `Describe` (its text ends in `build compatibility`).

## Goals / Non-Goals

**Goals:**

- The verdict and its message are a function of the inputs, never of map order.
- A provider is compared against the platform entry of its own major.
- A major mismatch is refused only when the platform resolves no path of that major, and its message lists every major it does resolve.

**Non-Goals:**

- Render-time checks: a provider built against `opm@v4` supplied to a render holding `opm@v5` (0026:D9:R4) is 0026's, not this change's.
- The contract-conflict half of 0026:D9:R7 (refuse a second provider only within one major). `subscribedContract` already keys by registry key with major; making the count per major is 0026's.
- Comparing against platform floors (0026:D7:R2's "static floors"): the operator compares against the generated closure's resolved versions, as today. Only which entry is compared changes.
- The contract-key collision (changes A to D).
- Any API, CRD, condition reason, generated file, fixture or library change.

## Decisions

### 1. Index the resolution by major-qualified path

`platformReqs` is already keyed by major-qualified path, so the same-major lookup is `platformReqs[qualified]` directly. A second index, base path to the sorted major-qualified platform paths, answers "does the platform carry this catalog at all" for the mismatch case.

```go
// buildIncompatibility, after catalogReqs is read.
majorsByBase := make(map[string][]string, len(platformReqs))
for qualified := range platformReqs {
	base, _, ok := ast.SplitPackageVersion(qualified)
	if !ok {
		continue
	}
	majorsByBase[base] = append(majorsByBase[base], qualified)
}
for _, qs := range majorsByBase {
	sort.Strings(qs)
}

for _, qualified := range paths { // sorted, as today
	if !inOPMNamespace(qualified) {
		continue
	}
	base, _, ok := ast.SplitPackageVersion(qualified)
	if !ok {
		continue
	}
	catalogVersion := catalogReqs[qualified]

	if platformVersion, same := platformReqs[qualified]; same {
		if catalogVersion == "" || platformVersion == "" {
			continue // a local replacement carries no version
		}
		if semver.Compare(catalogVersion, platformVersion) > 0 {
			return conservativeRefusal(base, qualified, catalogVersion, platformVersion,
				"a provider cannot require a newer build than the platform it runs in"), nil
		}
		continue
	}

	resolved := majorsByBase[base]
	if len(resolved) == 0 {
		continue // unshared
	}
	return conservativeRefusal(base, qualified, catalogVersion, resolvedMajors(resolved, platformReqs),
		"majors are not comparable, so the requirement is refused without comparing versions"), nil
}
```

`resolvedMajors` (unexported, in the same file, or inlined) is `strings.Join` over the sorted paths of `"<qualified> at <version>"`, with `", "`. A path the platform carries with no version (a local replacement) is written as the path alone.

**Alternatives considered.** Keep `byBase` but make it keep the highest major: deterministic, but still compares an `opm@v4` provider against `opm@v5` and refuses it, which is the bug on the case-D platform. Keep `byBase` but key it by `[]resolved`: the same as the chosen index, with one more type.

### 2. "Admitted entry" means any entry in the generated closure, disabled entries included

0026:D9:R7 says "the admitted entry of the provider's own major". Build compatibility is an MVS question over the generated `cue.mod` deps, and a disabled entry's module is in that graph (Context, "The closure"), so it counts as present. Enable-flag routing is judged elsewhere (`subscribedContract`, keyed by registry key). Requiring enabled-only would mean passing enable flags into `buildIncompatibility`, and would refuse an `opm@v5` provider on the case-D platform although its build resolves. Flagged to the user as a decision in the set's `user_decisions_needed`.

### 3. Message shape

Unchanged for the same-major refusal. For a mismatch, `conservativeRefusal`'s `platformVersion` argument (formatted with `%q`) becomes the joined list, so the message reads:

```text
Claimed catalog requires opmodel.dev/catalogs/opm@v6 at "v6.0.0" but the platform resolved
"opmodel.dev/catalogs/opm@v4 at v4.2.0, opmodel.dev/catalogs/opm@v5 at v5.0.0": majors are not
comparable, so the requirement is refused without comparing versions. The comparison is conservative: ...
```

On a one-major platform the platform side changes from `"v2.0.0"` to `"opmodel.dev/core@v2 at v2.0.0"`. No existing spec asserts that text (the different-major spec asserts only the reason; the wording spec uses a same-major refusal). `conservativeRefusal`'s signature and the rest of its wording are unchanged.

### 4. Doc comments

The lines 97-98 comment is replaced by one describing the two lookups. The function doc's "Per shared base path: a requirement in a different major than the platform carries is refused ..." paragraph becomes: the comparison uses the platform entry of the provider's own major; a requirement is refused as a major mismatch only when no platform path shares that major; within one major a greater requirement is refused. The "sorted order" paragraph gains that the mismatch message lists the platform's majors sorted, so the message is stable too. Cite `0026:D7:R2` once at the symbol beside `0015:D8`.

### Reconcile phase impact

Acceptance only (the TransformerRegistration Ready verdict). Source, Render, Apply, Prune and the Platform reconciler are untouched. Status: on a platform with two majors of one catalog, a claim's `Ready` reason and message become stable; a previously flapping `BuildIncompatible` refusal of an own-major provider becomes `Accepted`.

## Research & Decisions

### Red-first specs

**Context**: the bug is probabilistic, so a single call proves nothing.
**Explored**: the old index's odds on the three-path platform (126/1000 for `opm@v4`).
**Decision**: specs 1 to 3 call `buildIncompatibility` 200 times each and count the calls that deviate, asserting the count is zero so the failure message carries it. Spec 4 runs `judge()` once through `buildCompatReconciler`.
**Rationale**: expected old-code deviations: spec 1 (provider `opm@v4` at `v4.1.0`) about 175 of 200 refused; spec 2 (provider `opm@v5` at `v5.1.0`) about 25 of 200 worded as a major mismatch, passing on the old code with probability about 0.874^200, near 2e-12; spec 3 (provider `opm@v6`) message varies between two texts; spec 4 refused about 87% of runs. A spec that passes on the old code means the loop is too short, not that the bug is absent.

| # | Spec (It text) | Platform deps | Provider requires | Expect |
| --- | --- | --- | --- | --- |
| 1 | compares a provider against the resolved entry of its own major | core@v2 v2.0.0, opm@v4 v4.2.0, opm@v5 v5.0.0 | opm@v4 v4.1.0 | `""` on all 200 calls |
| 2 | refuses a newer build against its own major, never as a major mismatch | same | opm@v5 v5.1.0 | newer-build message naming `v5.0.0` on all 200; never "majors are not comparable" |
| 3 | refuses a third major with one stable message naming every resolved major | same | opm@v6 v6.0.0 | one distinct message over 200 calls, containing both `opm@v4 at v4.2.0` and `opm@v5 at v5.0.0` |
| 4 | accepts an own-major provider on a platform carrying a second major in its closure | same (case D: v4 enabled, v5 disabled, both in the closure) | opm@v4 v4.1.0 | `Accepted`, reason `Accepted` |

(Table paths abbreviate `opmodel.dev/catalogs/opm@vN` and `opmodel.dev/core@v2`; the specs write them in full.)

## Risks / Trade-offs

- [The one-major mismatch message changes text] → No spec, doc or consumer parses it; the reason is unchanged. Stated in proposal.md.
- [A disabled major now makes an own-major comparison available] → Intended (Decision 2): it reflects what the build resolves. If the user decides enabled-only, the fix gains an enable-flag input and spec 4's premise changes; the index shape stands.
- [Specs 1 to 3 loop 200 times] → Pure function calls over an in-memory catalog; negligible cost.

## Migration Plan

None. Ships in the next operator release; no stored state changes. A claim re-judged after the upgrade reaches the stable verdict on its next reconcile.
