## ADDED Requirements

### Requirement: Success history entry construction
The `internal/status` package MUST provide a `NewSuccessEntry` function that creates a `HistoryEntry` with action, phase, digests, inventory count, and auto-populated timestamps.

#### Scenario: Success entry created
- **WHEN** `NewSuccessEntry` is called with valid digests and inventory count
- **THEN** the returned entry has `StartedAt` and `FinishedAt` populated and all digest fields set

### Requirement: Failure history entry construction
The `internal/status` package MUST provide a `NewFailureEntry` function that creates a `HistoryEntry` for failed reconcile attempts.

#### Scenario: Failure entry created
- **WHEN** `NewFailureEntry` is called with action, message, and partial digests
- **THEN** the returned entry has timestamps populated and the message field set

### Requirement: Bounded history append
The `internal/status` package MUST provide a `RecordHistory` function that prepends an entry and trims to 10 entries maximum.

#### Scenario: Append to empty history
- **WHEN** `RecordHistory` is called on a status with no history
- **THEN** the history contains exactly one entry

#### Scenario: Trim at boundary
- **WHEN** `RecordHistory` is called and history already has 10 entries
- **THEN** the new entry is prepended and the oldest entry is removed, keeping exactly 10

#### Scenario: Ordering
- **WHEN** multiple entries are recorded
- **THEN** the most recent entry is at index 0

### Requirement: Monotonic sequence numbers
Each history entry's `Sequence` field MUST be monotonically increasing.

#### Scenario: Auto-increment
- **WHEN** a new entry is recorded
- **THEN** its `Sequence` is one greater than the highest existing sequence in the history
