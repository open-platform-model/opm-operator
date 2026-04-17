## Context

The design doc (`module-release-reconcile-loop.md`) specifies:
- History records compact metadata for successful apply, prune, and failed attempts.
- No full manifests or raw values stored.
- Retention: most recent 10 entries.
- Newest entries prepended (index 0 = most recent).

## Goals / Non-Goals

**Goals:**
- Construct history entries from reconcile outcomes.
- Prepend to status.history and trim to max entries.
- Populate timestamps automatically.

**Non-Goals:**
- Recording every periodic no-op (explicitly excluded by design).
- Storing rendered objects or values in history.

## Decisions

### 1. MaxHistoryEntries = 10

Hard-coded default matching the design doc. Not configurable via spec for v1alpha1.

### 2. Entry construction via typed helpers

`NewSuccessEntry` and `NewFailureEntry` set timestamps automatically via `metav1.Now()`. Callers provide digests, counts, and messages.

### 3. Sequence number auto-incremented

Each new entry's `Sequence` is set to `max(existing sequences) + 1`. Monotonically increasing across the bounded window.

## Risks / Trade-offs

- **[Trade-off] Fixed retention** — 10 entries may not be enough for debugging rapid failure loops. Acceptable for v1alpha1; can be made configurable later.
