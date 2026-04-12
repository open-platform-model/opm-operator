---
name: "OPSX: Cleanup"
description: Analyze codebase for dead or obsolete code
category: Workflow
tags: [workflow, cleanup, experimental]
---

Analyze the codebase for dead, obsolete, or orphaned code. Reports findings — never modifies code.

**Input**: Optionally specify a change name after `/opsx:cleanup` (e.g., `/opsx:cleanup add-streaming-tokens`). If omitted and no change is inferable from context, run in standalone mode.

**Steps**

1. Load the openspec-cleanup skill and execute it with the provided arguments (if any).
