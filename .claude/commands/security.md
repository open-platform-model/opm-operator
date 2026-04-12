---
name: "Security Audit"
description: Analyze code and architecture for security vulnerabilities, ranked by severity
category: Security
tags: [security, audit, analysis]
---

Perform a security audit of the codebase. Produces a severity-ranked report (CRITICAL / WARNING / SUGGESTION) — never modifies code.

**Input**: Optionally specify a target after `/security`:

- A directory path: `/security internal/api/` — audit that subtree
- A feature keyword: `/security authentication` — audit code related to that feature
- Nothing: `/security` — audit the full project (code + architecture)

**Steps**

1. Load the security-audit skill and execute it with the provided arguments (if any).
