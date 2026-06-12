# module-release-synthesis — delta

## REMOVED Requirements

### Requirement: BundleRelease does not depend on Flux source types

**Reason**: The `bundlerelease_controller`, the `BundleRelease` CRD, and `BundleRelease.spec.sourceRef` are deleted entirely; a requirement constraining the imports and RBAC markers of a controller that no longer exists is vacuous. The deferred "remove `spec.sourceRef` later" note is resolved by the deletion itself.
**Migration**: None — the constraint had no runtime behavior. `internal/source/` is unaffected: it is owned and exercised by the `Release` pipeline (see `release-artifact-loading` / `source-resolution` specs), not retained "for BundleRelease".
