---
name: commit
description: Commit staged or related changes using Conventional Commits
user-invocable: true
argument-hint: "[message hint or scope]"
---

Create a git commit following these rules strictly.

## Workflow

1. Run `git status` and `git diff --cached` to understand what is staged.
2. If nothing is staged, look at unstaged changes and stage files that form a coherent, minimal commit. Prefer `git add <file>...` over `git add -A`.
3. If changes span multiple unrelated concerns, commit them separately, one commit per logical change. If unclear, ask for clarification or make a best effort to group related changes together, utilise the AskUserQuestion tool.
4. Write the commit message and create the commit.

## Commit Message Format

Use **Conventional Commits**: `type(scope): description`

Common types: `feat`, `fix`, `refactor`, `docs`, `chore`, `test`, `style`, `ci`, `build`, `perf`.

- Scope is optional but encouraged when it clarifies the change.
- Description must be lowercase, imperative mood, no period at the end.
- Keep the first line under 72 characters.
- **Almost never add a body.** The subject line should be sufficient. A body is only warranted for genuinely unusual cases, e.g., a non-obvious breaking change, a subtle reason the diff doesn't speak for itself, or context that would otherwise be lost. Default: no body.

## Message Content

Focus on **what** is being changed. Be specific but concise.

Good: `feat(backup): add s3 retention policy to k8up schedule`
Bad: `update backup stuff`
Bad: a one-line subject followed by a paragraph restating the diff

## Strictly Forbidden

- **Never** include `Co-Authored-By` lines or any AI/Claude attribution.
- **Never** mention Claude, AI, or assistants in commit messages.
- **Never** add trailing signatures or metadata lines.

## Arguments

If `$ARGUMENTS` is provided, use it as a hint for the commit message or scope, but still follow all rules above.
