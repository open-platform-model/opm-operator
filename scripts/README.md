# Scripts

## worktree-init.sh — Parallel AI Model Benchmarking

Creates isolated Git worktrees so multiple AI coding agents can work on the same
codebase simultaneously, each on its own branch.

Each worktree is a sibling directory to the main repo with its own branch
(`bench/<model>`), sharing the same Git object store — no cloning needed.

### Quick start

```bash
# Create worktrees for the models you want to benchmark
./scripts/worktree-init.sh claude-opus-4-6 gemini-3-1 gpt-5-4

# This produces:
#   ../poc-controller-gpt4o/          branch: bench/gpt4o
#   ../poc-controller-gemini/         branch: bench/gemini
#   ../poc-controller-claude-sonnet/  branch: bench/claude-sonnet
#   ../poc-controller-o3/             branch: bench/o3
```

Point each AI agent at its own directory. All branches start from the current
HEAD of the main repo, so every model starts from the same baseline.

### Commands

| Command | Description |
|---|---|
| `./scripts/worktree-init.sh <model> [...]` | Create a worktree + branch per model |
| `./scripts/worktree-init.sh --list` | List active bench worktrees |
| `./scripts/worktree-init.sh --clean` | Remove all bench worktrees and branches |
| `./scripts/worktree-init.sh --help` | Show usage |

### Comparing results

After each model has finished its work, compare branches from the main repo:

```bash
# Diff two models against each other
git diff bench/gpt4o bench/gemini

# Diff a model against the starting point
git diff HEAD bench/claude-sonnet

# Log all changes a model made
git log HEAD..bench/o3 --oneline
```

### Cleanup

```bash
# Remove all bench worktrees and delete their branches
./scripts/worktree-init.sh --clean
```

This removes the sibling directories and deletes the local `bench/*` branches.
The main repo is not affected.
