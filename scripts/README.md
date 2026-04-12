# Scripts

## orchestrate-changes.sh — Parallel OpenSpec Change Implementation

Launches Claude Code agents in parallel to implement OpenSpec changes, respecting
the dependency graph between changes. Each agent runs in its own git worktree on
a dedicated branch, and creates a PR back to `implement-claude`.

### How it works

1. Reads a hardcoded dependency DAG (18 changes, their prerequisites).
2. Tracks state in `.claude-agents/state.json` (pending/running/completed/failed).
3. Finds changes whose dependencies are all completed → launches them in parallel.
4. Each agent gets its own git worktree + branch (`impl/<change-name>`).
5. Agent runs `/openspec-apply-change`, verifies with `make`, commits, pushes, creates PR.
6. Orchestrator waits for the batch, updates state, launches the next wave.
7. Repeats until all changes are done or blocked by failures.

Resumable — re-run and it skips completed changes.

### Quick start

```bash
# Launch the full orchestration
./scripts/orchestrate-changes.sh run

# Check progress
./scripts/orchestrate-changes.sh status

# View dependency graph
./scripts/orchestrate-changes.sh graph
```

### Commands

| Command | Description |
|---|---|
| `run` | Run the orchestrator (launch agents, respect dependencies) |
| `status` | Show current state of all changes |
| `graph` | Print the dependency graph |
| `reset <change>` | Reset a specific change to pending |
| `reset-failed` | Reset all failed changes to pending |
| `retry` | Reset failed + re-run |
| `logs <change>` | View the log for a change |
| `clean` | Remove all worktrees, branches, and state |

### Configuration

| Env var | Default | Description |
|---|---|---|
| `MAX_CONCURRENT` | 6 | Maximum parallel agents |

```bash
# Run with at most 4 agents at a time
MAX_CONCURRENT=4 ./scripts/orchestrate-changes.sh run
```

### Dependency graph

```
Batch 1 (foundation):
  01-cli-dependency-and-inventory-bridge

Batch 2 (parallel after 01):
  02, 04, 06, 07, 08, 09, 10, 13, 18  (9 independent changes)

Batch 3 (depend on batch 2):
  03←02  05←01,04  12←09  14←08  16←07

Batch 4:
  15←08,09  17←07,16

Batch 5 (finale):
  11←02,03,05,06,08,09,10,12,13
```

### State and logs

- State file: `.claude-agents/state.json`
- Agent logs: `.claude-agents/logs/<change-name>.log`
- Agent prompts: `.claude-agents/prompts/<change-name>.md`

All under `.claude-agents/` which is gitignored.

### Cleanup

```bash
# Remove all worktrees, branches, and state
./scripts/orchestrate-changes.sh clean
```
