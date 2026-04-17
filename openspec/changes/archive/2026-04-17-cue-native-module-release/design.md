## Context

The controller loads a raw `#Module` from OCI (via Flux source-controller) and passes it
to the render pipeline. The render pipeline expects a `#ModuleRelease` — specifically a
concrete `components` field. The `#ModuleRelease` schema in the catalog materializes
`#components` → `components` via a for-comprehension. The controller never builds a
`#ModuleRelease`, so every reconcile fails.

The CLI solves this by having users write a `release.cue` that imports `#ModuleRelease`
and the target module. CUE evaluation handles materialization. The controller needs to
replicate this — synthesize the equivalent of a `release.cue` at reconcile time.

CUE's module system resolves imports from OCI registries natively. The Flux
source-controller layer is redundant for CUE module delivery.

## Goals / Non-Goals

**Goals:**

- Controller synthesizes a `#ModuleRelease` CUE package from ModuleRelease CR fields.
- CUE's native module system resolves the target module from the OCI registry.
- The render pipeline receives a proper `#ModuleRelease` value with concrete `components`.
- Remove `sourceRef` from the ModuleRelease CR.
- Add registry configuration via `--registry` flag / `OPM_REGISTRY` env var.

**Non-Goals:**

- Private registry authentication (future).
- Semver range resolution or auto-upgrade (future).
- Removing `internal/source/` package (retained for future use).
- BundleRelease changes (separate change).
- Full `#ModuleRelease` features (auto-secrets, policies) in synthesized package (incremental).

## Research & Decisions

### Module loading approach

**Context**: The controller needs to evaluate a `#ModuleRelease` that references a
`#Module`. CUE import resolution requires either same-module references or external
dependencies from an OCI registry. Local path imports are not supported across module
boundaries.

**Explored**:

1. Same-package injection — add `_release.cue` to extracted module directory.
   Rejected: `m.#Module` at package root constrains all fields; `components` and
   `values` are not part of `#Module` and get rejected.

2. Subpackage restructuring — move module files into subdirectory, inject release at
   root. Under investigation in experiment 002. Viable if CUE allows importing a
   subpackage from a restructured layout. Still requires Flux for OCI download.

3. Standalone synthesized module — generate a separate CUE module that imports the
   target module as an external dependency via CUE's native OCI resolution.

**Decision**: Option 3 — standalone synthesized module.

**Rationale**: Cleanest separation. No filesystem manipulation of downloaded artifacts.
No Flux dependency. CUE handles OCI resolution natively. The synthesized package is
identical to what CLI users write — maximum fidelity with the CLI pipeline.

### Registry configuration

**Context**: CUE's `load.Instances` resolves external dependencies via OCI registries.
The controller needs to configure which registry to use.

**Decision**: Accept `--registry` flag and `OPM_REGISTRY` environment variable. Set
`CUE_REGISTRY` from these before CUE evaluation.

**Rationale**: Matches existing workspace convention (`OPM_REGISTRY` is already used).
`CUE_REGISTRY` is the standard env var CUE respects. Flag takes precedence over env var.

### Version pinning

**Context**: Need to decide how module versions are selected.

**Decision**: Explicit version pin in ModuleRelease CR `spec.module.version`. No
auto-upgrade. Reconciliation on CR change only.

**Rationale**: GitOps-compatible. Predictable. Avoids polling. Auto-upgrade can be
layered on later.

### Catalog version

**Context**: The synthesized package depends on the catalog
(`opmodel.dev/core/v1alpha1@v1`). Need to know which version to pin.

**Decision**: Hardcode the catalog version in the controller binary. Update it when
the controller is released.

**Rationale**: Simplest approach. The controller is tightly coupled to the catalog
schema version it was built against. Runtime catalog version selection adds
complexity with no current need.

## Design

### CR Schema

```go
type ModuleReleaseSpec struct {
    Suspend            bool             `json:"suspend,omitempty"`
    Module             ModuleReference  `json:"module"`
    Values             *RawValues       `json:"values,omitempty"`
    Prune              bool             `json:"prune,omitempty"`
    ServiceAccountName string           `json:"serviceAccountName,omitempty"`
    Rollout            *RolloutSpec     `json:"rollout,omitempty"`
}

type ModuleReference struct {
    // Path is the CUE module import path.
    // Example: "opmodel.dev/modules/cert_manager@v0"
    Path    string `json:"path"`

    // Version is the pinned module version.
    // Example: "v0.2.1"
    Version string `json:"version"`
}
```

`SourceReference` type and `sourceRef` field are removed.

Example CR:

```yaml
apiVersion: releases.opmodel.dev/v1alpha1
kind: ModuleRelease
metadata:
  name: cert-manager
  namespace: cert-manager
spec:
  module:
    path: opmodel.dev/modules/cert_manager@v0
    version: v0.2.1
  values:
    replicas: 2
  prune: true
```

### Synthesized CUE Package

The controller generates a temporary CUE module at reconcile time. Two files:

**`cue.mod/module.cue`**:

```cue
module: "opmodel.dev/controller/release@v0"
language: {
    version: "v0.16.1"
}
deps: {
    "opmodel.dev/core/v1alpha1@v1": {
        v: "<hardcoded catalog version>"
    }
    "<spec.module.path>": {
        v: "<spec.module.version>"
    }
}
```

**`release.cue`**:

```cue
package release

import (
    mr "opmodel.dev/core/v1alpha1/modulerelease@v1"
    mod "<spec.module.path>"
)

mr.#ModuleRelease

metadata: {
    name:      "<metadata.name>"
    namespace: "<metadata.namespace>"
}

#module: mod
```

Values are injected via `FillPath(cue.ParsePath("values"), compiledValues)` after
loading, not inlined in the CUE file. This matches the CLI's approach in
`ParseModuleRelease`.

### Package Lifecycle

1. Create temp directory: `os.MkdirTemp("", "opm-release-*")`
2. Write `cue.mod/module.cue` and `release.cue`
3. Load via `load.Instances([]string{"."}, &load.Config{Dir: tmpDir})`
4. CUE resolves dependencies from OCI registry (respects `CUE_REGISTRY`)
5. Build instance, fill values, validate concreteness
6. Pass to `ParseModuleRelease` → `ProcessModuleRelease` (existing pipeline)
7. Clean up temp directory (`defer os.RemoveAll`)

### Registry Configuration

```go
// In controller main or manager setup:
var registry string

// --registry flag takes precedence
if flagRegistry != "" {
    registry = flagRegistry
} else {
    registry = os.Getenv("OPM_REGISTRY")
}

// Set CUE_REGISTRY before any CUE evaluation
if registry != "" {
    os.Setenv("CUE_REGISTRY", registry)
}
```

### Reconcile Phase Impact

**Source phase**: No longer fetches from Flux. Instead, the synthesis + CUE load
replaces the source phase. Source status fields (`artifactRevision`, `artifactDigest`,
`artifactURL`) MAY be repurposed to track the module path and version, or removed.

**Render phase**: Unchanged. Receives a proper `#ModuleRelease` value. The existing
`ParseModuleRelease` → `ProcessModuleRelease` pipeline works as-is.

**Apply phase**: Unchanged.

**Prune phase**: Unchanged.

**Status phase**: `status.source` fields MAY be updated to reflect the CUE module
path and version instead of Flux artifact metadata.

### Internal Package Changes

**New**: `internal/synthesis/` (or extend `internal/render/`)

- `SynthesizeRelease(name, namespace, modulePath, moduleVersion, catalogVersion string) (string, error)`
  Returns path to temp directory containing the synthesized CUE module.

**Modified**: `internal/render/module.go`

- `RenderModule` signature changes: accepts module path + version instead of
  directory path. Internally calls synthesis, then loads and evaluates.
- Or: a new function `RenderModuleFromRegistry` wraps synthesis + existing
  `RenderModule`.

**Modified**: `internal/reconcile/modulerelease.go`

- Source-fetching logic replaced with synthesis call.
- OCIRepository watch removed.
- Registry config passed through reconciler params.

**Retained**: `internal/source/` — kept for potential future use, not deleted.

### Constitution Update

Principle I ("Flux for Transport, OPM for Semantics") is superseded for CUE module
delivery. The controller uses CUE's native module system for transport. A note SHOULD
be added to the constitution acknowledging this architectural shift.
