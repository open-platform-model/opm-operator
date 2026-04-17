## Context

Flux controllers use a standard condition model with `Ready`, `Reconciling`, and `Stalled` as the three primary meta-conditions. OPM adds `SourceReady` for tracking source availability separately. The `fluxcd/pkg/runtime/conditions` package provides helpers for getting/setting conditions, and `fluxcd/pkg/runtime/patch.SerialPatcher` provides safe concurrent-safe status patching.

## Goals / Non-Goals

**Goals:**
- Define condition type and reason constants matching the design docs.
- Provide helper functions: `MarkReconciling`, `MarkStalled`, `MarkReady`, `MarkNotReady`, `MarkSourceReady`, `MarkSourceNotReady`.
- Integrate `SerialPatcher` for status updates.

**Non-Goals:**
- Implementing the full reconcile loop condition transitions (that's change 11).
- kstatus alignment via `ResultFinalizer` (deferred until change 11).

## Decisions

### 1. Thin helpers wrapping Flux condition functions

The helpers call `conditions.MarkTrue/MarkFalse/MarkReconciling/MarkStalled` from `fluxcd/pkg/runtime/conditions`. No custom condition logic.

### 2. ModuleRelease must implement Flux's condition interfaces

For Flux condition helpers to work, `ModuleRelease` must implement `conditions.Getter` and `conditions.Setter`. Add these methods to the API type if not already present.

### 3. Reason constants are strings, not typed enums

Follow the Kubernetes convention of string reason constants. Use descriptive PascalCase names matching the design doc.

## Risks / Trade-offs

- **[Risk] Flux runtime version compatibility** — Flux runtime helpers assume specific condition semantics. Mitigation: pin to the same Flux version already in go.mod.
