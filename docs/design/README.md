# Design

## Summary

Collection of all design documents

- `module-release-api.md` - proposed `ModuleRelease` API, status model, and ownership-only inventory
- `module-release-reconcile-loop.md` - detailed initial `ModuleRelease` reconcile phases, status behavior, and flow diagrams
- `bundle-release-api.md` - proposed `BundleRelease` API and relationship to child `ModuleRelease` objects
- `controller-architecture.md` - controller architecture decisions, reconcile model, and shared API strategy
- `controller-tooling.md` - framework and tooling decisions for the controller implementation
- `experimental-scope-and-non-goals.md` - intentionally minimal controller scope, explicit non-goals, and experiment exit criteria
- `flux-gitops-toolkit-research.md` - research notes on Flux, GitOps Toolkit, and the local `fluxcd/pkg` repository
- `cue-oci-artifacts-and-flux-source-controller.md` - native CUE OCI module compatibility analysis with Flux `OCIRepository`
- `runtime-owned-labels-and-ownership-metadata.md` - ownership of rendered labels/annotations and runtime-controlled metadata
- `naming-taxonomy.md` - naming taxonomy for CRDs, workload metadata, inventory/status, and CUE modules
- `ssa-ownership-and-drift-policy.md` - server-side apply, pruning rules, drift detection, and ownership conflict policy
- `impersonation-and-privilege-escalation.md` - analysis of the `spec.serviceAccountName` escalation surface and option space for constraining it
- `rbac-delegation-crossplane-flux.md` - comparative research on how Crossplane (`rbac-manager`) and FluxCD handle RBAC delegation, with a proposed per-tenant direction
