## Why

Since library v1.0.0-beta.7 the kernel refuses a required `#config` value that the values leave unset, in both instance verbs (library#217, ADR-013 row g2). The ModuleInstance renderer still runs its own check of `spec.values` (`Kernel.ValidateConfigDetailed`) before synthesis, so one rule has two checks, two wordings and a spec requirement that forbids the kernel's wording. The owner decided the order in the kernel walkthrough: the kernel refuses, then the operator deletes its pre-check (opm-operator issue 258).

## What Changes

- The ModuleInstance renderer stops calling `Kernel.ValidateConfigDetailed`. Instance synthesis is the only check of `spec.values` against the module's `#config`.
- A failed synthesis is worded as the kernel's error with every CUE finding and its positions written out, where today the wrap prints only the first finding and no position. This keeps what the pre-check gave a user: every defect at once, and `spec.values:<line>:<column>` on a conflict.
- The message of every values defect on a ModuleInstance changes (table in `design.md`). The prefix `validating values against the module's #config: ` goes away. An unset required value reads `not fully concrete: values.<field>: incomplete value <type>`, the same phrase a ModulePackage reports today.
- Since library v1.0.0-beta.8 (pinned by opm-operator#273) the kernel names every unset required value as `values.<field>`, a value a component reads included, but its own text names the first and counts the rest. A ModulePackage reports that text today. The ModulePackage renderer now words the kernel's findings one by one too, with positions in the package relative to its CUE module root, so the message of an unchanged package does not change with the extraction directory.
- The event of a stalled render failure is cut to the event note limit of events.k8s.io/v1 (1024 characters): whole findings from the first, then `; and <N> more findings`. The condition keeps the whole message, bounded the same way at its own limit (32768). Before, nothing bounded either, and the API server refuses a longer event.
- No condition, status or reason changes. A values defect on a ModuleInstance stays `Ready=False`, `Stalled=True`, reason `RenderFailed`. A registry fetch failure during synthesis stays a retried `ResolutionFailed`.
- The reason a ModulePackage reports for the same defect (`ResolutionFailed`) is not touched; its message is (table in `design.md`, "After library v1.0.0-beta.8"). The two kinds keep different reasons; aligning them needs a typed error the kernel does not return today.
- `docs/site/diagnostics/operator-conditions.md` names the new wording in the `RenderFailed` row.
- Not breaking: no reason, condition type or status value changes, only message text. After GA this is a PATCH; during beta it ships in the next `-beta.N`.
- The change needs library v1.0.0-beta.8, which `main` pins. On v1.0.0-beta.7 the kernel named a component path for an unset value a component reads, and the first build of this change was held for that. The typed `ConfigValidationError` of library#223 is returned only by `ValidateConfigDetailed`, the call this change removes.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `module-instance-synthesis`: the requirements "An unset required config value is refused in the spec.values wording" and "Every unset required config value is named" are removed. Two requirements replace them: the kernel's refusal is the one a user reads and names every unset value, and a values failure reports every finding with its positions.
- `modulepackage-kernel-rendering`: the requirement "An unset required value that a component reads is named as a value" (first value and a count) is removed; "Every unset required value of a package is named" and "A package values failure reports every finding with stable positions" are added; "A package that leaves a required config value unset is refused" no longer says the kernel's text is carried unchanged.
- `events-emission`: a new requirement bounds the event of a stalled render failure.

## Impact

- Code: `internal/render/kernel_module_renderer.go` (the pre-check, its comment, the wording of a synthesis failure), `internal/render/findings.go` (the wording and its bounds), `internal/render/kernel_package_renderer.go` (one call), the tests beside them, and in `internal/reconcile` the two lines that emit the event of a stalled render failure (`moduleinstance.go`, `modulepackage.go`) plus tests in `resolution_test.go`.
- Controllers: ModuleInstance and ModulePackage, render phase and the event of its failure. Platform and TransformerRegistration are not touched.
- API types: none. No CRD, RBAC or generated file changes.
- Docs: `docs/site/diagnostics/operator-conditions.md`.
- Users: a tool or an alert that matches the text `validating values against the module's #config` or `#config.<field>: incomplete value` in a ModuleInstance condition message or event, or `(and N more errors)` in a ModulePackage message, stops matching. A match on the reason (`RenderFailed`, `ResolutionFailed`) keeps working.
- Dependencies: none added, no pin moved.
