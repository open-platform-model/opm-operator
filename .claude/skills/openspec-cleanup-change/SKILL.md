---
name: openspec-cleanup
description: Analyze codebase for dead or obsolete code. Use standalone for broad sweeps or with a change name for targeted analysis of code made obsolete by a specific change.
user-invocable: true
argument-hint: "[change-name]"
---

Analyze the codebase for dead, obsolete, or orphaned code. Reports findings — never modifies code.

**Input**: Optionally specify a change name (e.g., `/opsx:cleanup add-streaming-tokens`). If omitted and no change is inferable from context, run in standalone mode.

## Mode Detection

- **Change name provided or inferable** → Change-Aware mode
- **No change context** → Standalone mode

---

## Mode 1: Change-Aware

Targeted analysis of code made dead or redundant by a specific OpenSpec change.

**Steps**

1. **Load change artifacts**

   ```bash
   openspec instructions apply --change "<name>" --json
   ```

   Read all context files from `contextFiles` (proposal, design, specs, tasks).

2. **Identify what was replaced or superseded**

   From the artifacts and any implemented code, extract:
   - Functions/methods that were rewritten or replaced
   - Types that were renamed, restructured, or removed
   - Routes/endpoints that changed or were removed
   - Files that were moved or deleted
   - Constants, config keys, or variables that only the old code used

3. **Launch an Explore subagent**

   Provide the subagent with the list of replaced/superseded items and instruct it to search for:
   - Remaining references to old function/method names
   - Imports of removed or moved files/packages
   - Tests that exercise removed or replaced behavior
   - Constants, config keys, or variables only used by removed code
   - Stale comments or TODOs referencing removed code
   - Type definitions only used by removed code

   The subagent MUST return for each finding: **file path**, **line number**, **what is dead**, **why it is dead**, and **confidence** (high / medium / low).

4. **Format and return the report** (see Output Format below)

---

## Mode 2: Standalone Sweep

Broad scan for dead code patterns across the codebase — not tied to any specific change.

**Steps**

1. **Launch an Explore subagent** with a broad search prompt:

   - Unexported functions/methods with zero callers
   - Unused type definitions (defined but never referenced)
   - Imports that are imported but not used (beyond what linters catch — e.g., packages imported in one file but the consuming code was removed from another)
   - Test files/functions for code that no longer exists
   - TODO/FIXME comments referencing completed or removed work
   - Orphaned constants or variables with no remaining consumers
   - Stale interface implementations where the interface was removed

   The subagent MUST return same format as Mode 1.

2. **Format and return the report** (see Output Format below)

---

## Output Format

```markdown
## Cleanup Report

### Summary
- **Mode**: Change-aware (`<change-name>`) | Standalone sweep
- **Candidates found**: N

### Suggested Removal Tasks

1. **Remove `handleOldRoute`** — `internal/api/routes.go:45`
   Replaced by `handleNewRoute` in this change. Zero remaining callers.
   Confidence: high

2. **Remove `OldResponseDTO`** — `internal/domain/responses.go:12-28`
   Only used by `handleOldRoute` which is now dead.
   Confidence: high

3. **Remove test `TestHandleOldRoute`** — `internal/api/routes_test.go:89-120`
   Tests removed handler. Will fail or be meaningless.
   Confidence: high

### No Action Needed
- `helperFunc` in `internal/utils/helpers.go:30` — investigated, still called by `serviceX`
```

Each suggested task should have enough detail that someone could act on it directly: what to remove, where it is, and why it is dead.

---

## Guardrails

- **NEVER make code changes** — this skill is analysis and reporting only
- **Always delegate analysis to an Explore subagent** — protect the main context window from the volume of file reads and grep operations needed for thorough analysis
- When confidence is low, still report but mark clearly as `low` — let the human or orchestrator decide
- In change-aware mode, **focus on code made dead by this change**, not pre-existing dead code
- In standalone mode, accept lower precision — cast wide, report candidly
- Include a "No Action Needed" section for items investigated but found still alive — this builds trust in the analysis
- If the Explore subagent finds nothing, report that explicitly: "No dead code candidates identified."
