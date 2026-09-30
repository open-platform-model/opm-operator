## Why

`TransformerRegistration` acceptance judges a provider's build compatibility (0015:D8) against the platform's generated `cue.mod`, and indexes that resolution by catalog path WITHOUT its major (`internal/controller/transformerregistration_buildcompat.go:97-107`, `byBase`). It ranges a Go map and keeps whichever entry it visits last, so on a platform carrying two majors of one catalog the verdict depends on map iteration order. Reproduced against cue v0.17.1 with deps `{opmodel.dev/core@v2, opmodel.dev/catalogs/opm@v4 v4.2.0, opmodel.dev/catalogs/opm@v5 v5.0.0}`: 1000 calls kept `opm@v4` 126 times and `opm@v5` 874 times. A provider built against `opm@v4` is then refused as a major mismatch about 87% of the time, and a provider on `opm@v5` that requires a newer build than the platform is refused with the wrong reason about 13% of the time.

The shape is reachable today, with no side-by-side majors: an enabled `opm@v4` beside a disabled `opm@v5` puts both majors in the generated `cue.mod`, because the library's `platformmodule.Roots` makes every registry entry a root, disabled entries included (enhancement 0026 experiment 01, case D). That platform is routable, so it is stored and every claim is judged against it.

Enhancement 0026 (05-risks, OQ17) recommends fixing this independently of 0026. This change is E of the five-change set in `orchestration.md`; it depends on none of the others and nothing consumes it.

## What Changes

- The build-compatibility check indexes the platform's resolution by major-qualified path. For each OPM-namespace path the provider requires, it compares against the platform entry of the provider's OWN major when one exists (same-major rule unchanged: refuse a newer requirement, skip an unversioned one).
- A major mismatch is refused only when no resolved platform path shares the provider's major. Its message names every resolved major of that path, sorted, so it is byte-identical on every call (`the platform resolved "opmodel.dev/catalogs/opm@v4 at v4.2.0, opmodel.dev/catalogs/opm@v5 at v5.0.0"`). On a one-major platform the mismatch message changes from the bare version to `"<path with major> at <version>"`; the reason (`BuildIncompatible`) and every other wording are unchanged.
- Four red-first Ginkgo specs pin the determinism in both directions, a third major, and the case-D platform end to end.
- The `registration-acceptance` spec says which platform entry a provider is compared against.

## Classification

**PATCH**, released as `fix(controller)`. No API type, field, CRD, condition reason, fixture or library pin changes. Behaviour changes only on a platform carrying two majors of one catalog, where the old verdict was random; on a one-major platform every verdict is unchanged and only the mismatch message's platform side gains the path. Complexity is flat (Principle VII): one index is replaced by another of the same size.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `registration-acceptance`: "A build-incompatible provider is refused at acceptance" compares against the platform entry of the provider's own major and refuses a different major only when no resolved path shares it; two scenarios added (own-major comparison, order independence).

## Impact

- `internal/controller/transformerregistration_buildcompat.go` (`buildIncompatibility` and its doc comment only; `conservativeRefusal`, `inOPMNamespace` and `platformRequirements` unchanged).
- `internal/controller/transformerregistration_buildcompat_test.go` (four new `It` specs in the existing build-compatibility `Describe`).
- Delivers 0026:D7:R2 and the shared-path half of 0026:D9:R7. Claims are decision-granular and neither decision is fully delivered, so no `enhancement.yaml`.
- No overlap with change C (`name-contract-collisions`, same repo): C rebases over this if it merges first.
