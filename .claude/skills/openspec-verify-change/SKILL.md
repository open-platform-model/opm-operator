---
name: openspec-verify-change
description: Verify implementation matches change artifacts. Use when the user wants to validate that implementation is complete, correct, and coherent before archiving.
license: MIT
compatibility: Requires openspec CLI.
metadata:
  author: openspec
  version: "1.1"
  generatedBy: "1.3.0"
---

Verify that an implementation matches the change artifacts (specs, tasks, design).

**Input**: Optionally specify a change name. If omitted, check if it can be inferred from conversation context. If vague or ambiguous you MUST prompt for available changes.

## Steps

1. **If no change name provided, prompt for selection**

   Run `openspec list --json` to get available changes. Use the **AskUserQuestion tool** to let the user select.

   Show changes that have implementation tasks (tasks artifact exists).
   Include the schema used for each change if available.
   Mark changes with incomplete tasks as "(In Progress)".

   **IMPORTANT**: Do NOT guess or auto-select a change. Always let the user choose.

2. **Check status to understand the schema**
   ```bash
   openspec status --change "<name>" --json
   ```
   Parse the JSON to understand:
   - `schemaName`: The workflow being used (e.g., "spec-driven")
   - Which artifacts exist for this change

3. **Get the change directory and load artifacts**

   ```bash
   openspec instructions apply --change "<name>" --json
   ```

   This returns the change directory and context files. Read all available artifacts from `contextFiles`.

4. **Initialize verification report structure**

   Dimensions — always evaluated in this fixed order:
   1. **Completeness** — tasks and spec coverage
   2. **Correctness** — requirement implementation and scenario coverage
   3. **Coherence** — design adherence and pattern consistency
   4. **Cleanliness** — dead code left behind by the change

   Each dimension may produce CRITICAL, WARNING, or SUGGESTION issues.

5. **Verify Completeness**

   **Task Completion**:
   - If tasks.md exists in contextFiles, read it
   - Parse checkboxes: `- [ ]` (incomplete) vs `- [x]` (complete)
   - Count complete vs total tasks
   - If incomplete tasks exist:
     - Add CRITICAL issue for each incomplete task
     - Fix: "Complete task: <description>" or "Mark as done if already implemented"

   **Spec Coverage**:
   - If delta specs exist in `openspec/changes/<name>/specs/`:
     - Extract all requirements (marked with "### Requirement:")
     - For each requirement:
       - Search codebase for keywords related to the requirement
       - Assess if implementation likely exists
     - If requirements appear unimplemented:
       - Add CRITICAL: "Requirement not found: <requirement name>"
       - Fix: "Implement requirement X: <description>"

6. **Verify Correctness**

   **Requirement Implementation Mapping**:
   - For each requirement from delta specs:
     - Search codebase for implementation evidence
     - If found, note file paths and line ranges
     - Assess if implementation matches requirement intent
     - If divergence detected:
       - Add WARNING: "Implementation may diverge from spec: <details>"
       - Fix: "Review <file>:<lines> against requirement X"

   **Scenario Coverage**:
   - For each scenario in delta specs (marked with "#### Scenario:"):
     - Check if conditions are handled in code
     - Check if tests exist covering the scenario
     - If scenario appears uncovered:
       - Add WARNING: "Scenario not covered: <scenario name>"
       - Fix: "Add test or implementation for scenario: <description>"

7. **Verify Coherence**

   **Design Adherence**:
   - If design.md exists in contextFiles:
     - Extract key decisions (look for sections like "Decision:", "Approach:", "Architecture:")
     - Verify implementation follows those decisions
     - If contradiction detected:
       - Add WARNING: "Design decision not followed: <decision>"
       - Fix: "Update implementation or revise design.md to match reality"
   - If no design.md: Skip, note "No design.md to verify against" in Summary.

   **Code Pattern Consistency**:
   - Review new code for consistency with project patterns
   - Check file naming, directory structure, coding style
   - If significant deviations found:
     - Add SUGGESTION: "Code pattern deviation: <details>"
     - Fix: "Consider following project pattern: <example>"

8. **Verify Cleanliness**

   Load the **openspec-cleanup** skill in change-aware mode (pass the change name) and incorporate its report as the Cleanliness dimension.

   Map cleanup findings to verification issues:
   - High-confidence candidates → WARNING: "Dead code candidate: `<symbol>` in `<file>:<line>` — <reason>"
   - Medium/low-confidence candidates → SUGGESTION with the same format

   If cleanup reports no candidates: note "No dead code detected" in Summary.

   **Skip condition**: If no code changes have been implemented yet (all tasks incomplete), skip this check and note in Summary: "Cleanliness check skipped — no implementation to analyze."

9. **Generate the report** — MUST match the template in **Output Format** below, verbatim structure.

## Severity Decision Rules

Pin severity to the cause, not to model confidence. Confidence only breaks ties at the WARNING/SUGGESTION boundary.

| Severity    | ID prefix | Blocks archive? | Use for                                                                  |
|-------------|-----------|-----------------|--------------------------------------------------------------------------|
| CRITICAL    | `C`       | Yes             | Incomplete task, missing requirement implementation, broken scenario     |
| WARNING     | `W`       | No (should fix) | Spec/design divergence, uncovered scenario, high-confidence dead code    |
| SUGGESTION  | `S`       | No (nice to fix)| Pattern/style inconsistency, medium/low-confidence dead code, minor nits |

**Tie-break rule**: When uncertain between WARNING and SUGGESTION, pick SUGGESTION. Never demote a genuine CRITICAL to WARNING because of uncertainty — re-check evidence instead.

## Issue Numbering

- Number per category, starting at 1: `C1`, `C2`, ..., `W1`, `W2`, ..., `S1`, `S2`, ...
- Order within each category follows the fixed dimension sequence: Completeness → Correctness → Coherence → Cleanliness.
- IDs are stable within a single report. Do not renumber to fill gaps; if a check produces no issues in a dimension, the next dimension's issues continue the numbering without reset.

## Output Format

Emit exactly this structure. Do not add, remove, or rename sections. Use literal `None` when a category has no issues.

```
# Verification Report: <change-name>

## Summary

| Dimension    | Checked | Pass | Issues        |
|--------------|---------|------|---------------|
| Completeness | yes/no  | yes/no | N (C:a W:b S:c) |
| Correctness  | yes/no  | yes/no | N (C:a W:b S:c) |
| Coherence    | yes/no  | yes/no | N (C:a W:b S:c) |
| Cleanliness  | yes/no  | yes/no | N (C:a W:b S:c) |

**Skipped checks**: <list or "None">
**Final Assessment**: <one-line verdict — see rules below>

---

## Issues

### CRITICAL

- **C1** [<Dimension>] <one-line title>
  - Location: `<file:line>` or `N/A`
  - Evidence: <what you found or didn't find>
  - Fix: <specific action>

### WARNING

- **W1** [<Dimension>] <one-line title>
  - Location: `<file:line>` or `N/A`
  - Evidence: <what you found or didn't find>
  - Fix: <specific action>

### SUGGESTION

- **S1** [<Dimension>] <one-line title>
  - Location: `<file:line>` or `N/A`
  - Evidence: <what you found or didn't find>
  - Fix: <specific action>
```

**Final Assessment rules** (pick the first that matches):
- Any CRITICAL → `X critical issue(s) found. Fix before archiving.`
- No CRITICAL, any WARNING → `No critical issues. Y warning(s) to consider. Ready for archive with noted improvements.`
- All clear → `All checks passed. Ready for archive.`

**Issue entry rules**:
- Every issue has exactly four fields: ID+title line, Location, Evidence, Fix. No extra prose.
- Location uses backticks: `internal/controller/foo.go:42`. Use `N/A` only when no file applies.
- One issue per bullet. Do not merge multiple findings into one entry.
- If a category has no issues, the section body is the single word `None` (no bullets).

## Verification Heuristics

- **Completeness**: Objective checklist items (checkboxes, requirements list) only.
- **Correctness**: Keyword search, file-path analysis, reasonable inference — not perfect certainty.
- **Coherence**: Flag glaring inconsistencies, not style nits (those go to Code Pattern Consistency as SUGGESTION).
- **Actionability**: Every issue must name a file/line where applicable and a concrete fix. Reject vague recommendations like "consider reviewing".

## Graceful Degradation

- Only tasks.md exists → verify task completion only. Mark Correctness/Coherence/Cleanliness as `Checked: no` in Summary.
- Tasks + specs exist → verify Completeness + Correctness. Skip Coherence/Cleanliness per conditions above.
- Full artifacts → verify all four dimensions.
- Cleanliness requires implementation — skip if no code changes detected.
- Always list skipped checks in the Summary's "Skipped checks" line.
