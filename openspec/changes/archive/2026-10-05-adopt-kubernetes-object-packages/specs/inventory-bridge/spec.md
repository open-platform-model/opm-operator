## REMOVED Requirements

### Requirement: Component label constant
**Reason**: The operator declares no OPM label key of its own; the component-name key is the library's `labels.ComponentName` in `opm/k8s/labels` (0012:D3:R6).
**Migration**: `NewEntryFromResource` reads the component name under `labels.ComponentName`. The requirement "The operator uses the library's object and label packages and keeps no copy" (`kubernetes-tier-adoption`) covers the label keys.

### Requirement: CLI packages copied to `pkg/`
**Reason**: The last copied package, `pkg/core`, is deleted; the library's `opm/k8s/object` and `opm/k8s/labels` replace it, and the operator keeps no copy (0012:D3:R6).
**Migration**: Import `github.com/open-platform-model/library/opm/k8s/object` and `github.com/open-platform-model/library/opm/k8s/labels`. The lint refuses an import of the old path (`kubernetes-tier-adoption`, "Lint refuses the old path").

### Requirement: Process file remains in pkg/render (revised)
**Reason**: The operator has no `pkg/` tree left: `pkg/render` and its `ProcessModuleRelease` went with the move to the library kernel, and this change deletes the last package under `pkg/`.
**Migration**: None. The renderer is `KernelModuleRenderer` (`kernel-module-renderer`).

## MODIFIED Requirements

### Requirement: Inventory type alias preserved
The existing `type Current = releasesv1alpha1.Inventory` alias in `internal/inventory` MUST be preserved as a semantic marker used by other internal packages.

#### Scenario: Alias names the API inventory type
- **WHEN** `internal/inventory` is inspected
- **THEN** it declares `Current` as an alias of the API's `Inventory` type, not a distinct type
