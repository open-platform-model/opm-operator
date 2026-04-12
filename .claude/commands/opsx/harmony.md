---
name: "OPSX: Harmony"
description: Analyze OpenSpec changes for cross-change conflicts, contradictions, and drift
category: Workflow
tags: [workflow, harmony, experimental]
---

Analyze OpenSpec changes for cross-change consistency. Reports conflicts — never modifies files.

**Input**: Optionally specify a change name after `/opsx:harmony` (e.g., `/opsx:harmony profile-pin-lock`). If omitted, analyze all active changes against each other and against archived changes.

**Steps**

1. Load the openspec-harmonize-changes skill and execute it with the provided arguments (if any).
