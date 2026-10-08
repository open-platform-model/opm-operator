## Why

Since library v1.0.0-beta.7 the kernel refuses a required `#config` value that the values leave unset, in both instance verbs (library#217, ADR-013 row g2). The ModuleInstance renderer still runs its own check of `spec.values` (`Kernel.ValidateConfigDetailed`) before synthesis, so one rule has two checks, two wordings and a spec requirement that forbids the kernel's wording. The owner decided the order in the kernel walkthrough: the kernel refuses, then the operator deletes its pre-check (opm-operator issue 258).

## What Changes

- The ModuleInstance renderer stops calling `Kernel.ValidateConfigDetailed`. Instance synthesis is the only check of `spec.values` against the module's `#config`.
- A failed synthesis is worded as the kernel's error with every CUE finding and its positions written out, where today the wrap prints only the first finding and no position. This keeps what the pre-check gave a user: every defect at once, and `spec.values:<line>:<column>` on a conflict.
- The message of every values defect on a ModuleInstance changes (table in `design.md`). The prefix `validating values against the module's #config: ` goes away. An unset required value reads `not fully concrete: values.<field>: incomplete value <type>`, the same phrase a ModulePackage reports today.
- No condition, status or reason changes. A values defect on a ModuleInstance stays `Ready=False`, `Stalled=True`, reason `RenderFailed`. A registry fetch failure during synthesis stays a retried `ResolutionFailed`.
- The reason a ModulePackage reports for the same defect (`ResolutionFailed`) is not touched. The two kinds keep different reasons; aligning them needs a typed error the kernel does not return today.
- `docs/site/diagnostics/operator-conditions.md` names the new wording in the `RenderFailed` row.
- Not breaking: no reason, condition type or status value changes, only message text. After GA this is a PATCH; during beta it ships in the next `-beta.N`.
- No library release is needed. The change builds on the pinned library v1.0.0-beta.7. The typed `ConfigValidationError` of library#223 is returned only by `ValidateConfigDetailed`, the call this change removes.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `module-instance-synthesis`: the requirement "An unset required config value is refused in the spec.values wording" is removed. Two requirements replace it: the kernel's refusal is the one a user reads, and a values failure reports every finding with its positions.

## Impact

- Code: `internal/render/kernel_module_renderer.go` (the pre-check, its comment, the wording of a synthesis failure), `internal/render/required_values_test.go`, one row in `internal/reconcile/resolution_test.go`.
- Controllers: ModuleInstance only, render phase. ModulePackage, Platform and TransformerRegistration are not touched.
- API types: none. No CRD, RBAC or generated file changes.
- Docs: `docs/site/diagnostics/operator-conditions.md`.
- Users: a tool or an alert that matches the text `validating values against the module's #config` in a condition message or an event stops matching. A match on the reason `RenderFailed` keeps working.
- Dependencies: none added, no pin moved.
