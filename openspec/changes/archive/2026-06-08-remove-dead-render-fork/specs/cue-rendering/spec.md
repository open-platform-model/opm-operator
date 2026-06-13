## REMOVED Requirements

### Requirement: Module rendering from directory and values

**Reason**: The directory-and-values fork render bridge (`internal/render.RenderModule`, `pkg/render`) is deleted. ModuleRelease rendering now runs entirely through the library kernel (`Kernel.Compile`) via `KernelModuleRenderer`.
**Migration**: See the `kernel-module-renderer` and `platform-gated-rendering` capabilities — they cover rendering output and inventory entries for `ModuleRelease`.

### Requirement: Runtime labels injection

**Reason**: Runtime identity is no longer injected by the deleted fork bridge; the kernel renderers pass it as `CompileInput.RuntimeName` (`core.LabelManagedByControllerValue`), and the `managed-by`/ownership labels flow from the catalog through the kernel compile.
**Migration**: See `kernel-module-renderer`/`release-kernel-rendering` (RuntimeName wiring); the catalog supplies the label merge.

### Requirement: RenderResult includes inventory entries

**Reason**: The fork's `RenderModule` no longer exists; the kernel renderers build `RenderResult.InventoryEntries` via the same `buildInventoryEntries` bridge.
**Migration**: See `kernel-module-renderer`/`release-kernel-rendering`, which require inventory entries on the result.

### Requirement: CRD values to CUE conversion

**Reason**: The fork render bridge that performed this conversion is deleted; `KernelModuleRenderer` converts `RawValues` (JSON) to a `cue.Value` for `SynthesizeRelease`.
**Migration**: See `kernel-module-renderer` (values conversion to the kernel `SynthesizeRelease` input).
