## MODIFIED Requirements

### Requirement: Rendered values are dropped after conversion

A rendered resource carries the CUE value it was read from, and a held value keeps the whole build reachable. The ModuleInstance and ModulePackage reconcilers SHALL therefore drop the render result's CUE-backed resources as soon as the one export of them returns, before the render digest, the inventory entries and apply are computed from that export, so a reconcile holds a build only from the render to that conversion, and does both while it holds its render slot. Every later phase (shrink judgment, apply, prune, inventory and status) SHALL read the exported data (the unstructured objects and the inventory entries built from them) or the render result's plain data (warnings, required contracts, platform identity), never the dropped resources.

#### Scenario: A ModuleInstance reconcile drops the rendered resources

- **WHEN** a ModuleInstance renders and its resources are converted to unstructured objects
- **THEN** the render result's resources are nil from that point, and the instance still applies, records its inventory and reaches Ready

#### Scenario: A ModulePackage reconcile drops the rendered resources

- **WHEN** a ModulePackage renders and is not a no-op
- **THEN** the render result's resources are nil once converted for apply, and the package still applies, prunes and records its inventory

