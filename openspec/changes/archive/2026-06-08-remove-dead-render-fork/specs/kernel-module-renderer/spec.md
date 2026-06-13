## REMOVED Requirements

### Requirement: Renderer is built but not wired

**Reason**: This transitional requirement froze the pre-cut-over state — `cmd/main.go` wiring `RegistryRenderer` with `KernelModuleRenderer` reachable only from tests. The cut-over (`platform-gated-rendering`) already flipped production to `KernelModuleRenderer`, and this slice deletes `RegistryRenderer` entirely, so there is no longer an "unwired" state to describe; the requirement is now false on both clauses.
**Migration**: See `platform-gated-rendering` (the manager wires `KernelModuleRenderer` against the materialized platform). `KernelModuleRenderer`'s rendering behavior is specified by the remaining requirements of this capability (compiled-item adaptation, inventory bridge, platform-readiness gating).
