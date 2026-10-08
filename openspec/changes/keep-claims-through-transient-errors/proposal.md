## Why

An accepted `TransformerRegistration` loses its acceptance when the registry cannot be reached during one of its reconciles. The claim reconciler sends every catalog acquisition error except a wrong-kind artifact to a refusal, which clears `status.accepted`. The Platform reconciler then regenerates the platform package without the provider's catalog, dependent instances stop resolving, and the claim can flip back on the next Platform event. The Platform reconciler already treats the same failure as transient; the claim reconciler does not.

The failure needs a cold CUE module cache: the library serves an already fetched catalog version from disk with no registry call. The operator's cache is an `emptyDir`, so every new pod, and every claim that names a new catalog version, starts cold.

## What Changes

- A claim that is accepted keeps `status.accepted`, `status.active`, its `Active` condition and a `Ready` condition that reports the acceptance when the catalog acquisition fails with the library's typed transient registry failure (`ErrTransient`: no HTTP response, or a 5xx answer).
- The failure is reported without touching the verdict: `Reconciling=True` with reason `CatalogUnresolved`, and one Warning event that carries the registry error.
- The acquisition is retried on a capped, jittered exponential backoff (5 seconds doubling to 5 minutes), not on the 30-minute stalled recheck.
- Every other acquisition failure refuses the claim as today: a catalog the registry does not hold, refused credentials, a wrong-kind artifact, an unclassified error.
- A claim that is not accepted is refused on a transient failure as today.

No API type changes. No change to the acceptance rules between competing claims.

SemVer class: PATCH (a bug fix with no API change). Until GA it ships as the next `-beta.N`.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `registration-acceptance`: the unresolved-catalog refusal gains one exception, and a new requirement states that an accepted claim keeps its verdict through a transient registry failure.

## Impact

- Controller: `TransformerRegistrationReconciler` (`internal/controller/transformerregistration_controller.go`).
- API types: none. `TransformerRegistration` status fields and the CRD are unchanged.
- Conditions: `Reconciling=True` with reason `CatalogUnresolved` can now stand beside `Ready=True` with reason `Accepted` on a claim.
- Docs: `docs/site/diagnostics/operator-conditions.md` gains the new condition row.
- Dependencies: none added. The classification is the library's `ErrTransient` (library v1.0.0-beta.6, already pinned).
