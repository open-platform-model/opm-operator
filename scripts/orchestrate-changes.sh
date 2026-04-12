#!/usr/bin/env bash
# orchestrate-changes.sh — Launch parallel Claude Code agents to implement OpenSpec changes.
#
# Manages a dependency DAG across 18 changes. Each change runs in its own git
# worktree on a dedicated branch. Agents create PRs back to `implement-claude`.
#
# State is tracked in .claude-agents/state.json so the script is resumable —
# re-run it and it picks up where it left off.
set -euo pipefail

# ---------------------------------------------------------------------------
# Config
# ---------------------------------------------------------------------------
REPO_ROOT="$(git -C "$(dirname "$0")" rev-parse --show-toplevel)"
REPO_NAME="$(basename "$REPO_ROOT")"
PARENT_DIR="$(dirname "$REPO_ROOT")"
STATE_DIR="${REPO_ROOT}/.claude-agents"
STATE_FILE="${STATE_DIR}/state.json"
LOG_DIR="${STATE_DIR}/logs"
PROMPT_DIR="${STATE_DIR}/prompts"
BASE_BRANCH="implement-claude"
BRANCH_PREFIX="impl"
REMOTE="origin"
OWNER_REPO="open-platform-model/poc-controller"
MAX_CONCURRENT="${MAX_CONCURRENT:-6}"

# ---------------------------------------------------------------------------
# Dependency graph
# ---------------------------------------------------------------------------
# Format: "change:dep1,dep2,..." — empty after colon means no dependencies.
# This encodes the DAG from the OpenSpec change analysis.
declare -A DEPS=(
  ["01-cli-dependency-and-inventory-bridge"]=""
  ["02-source-resolution"]=""
  ["03-artifact-fetch-and-cue-validation"]="02-source-resolution"
  ["04-catalog-provider-loading"]="01-cli-dependency-and-inventory-bridge"
  ["05-cue-rendering-bridge"]="01-cli-dependency-and-inventory-bridge,04-catalog-provider-loading"
  ["06-digest-computation"]="01-cli-dependency-and-inventory-bridge"
  ["07-status-conditions"]=""
  ["08-ssa-apply"]="01-cli-dependency-and-inventory-bridge"
  ["09-prune-stale-resources"]="01-cli-dependency-and-inventory-bridge"
  ["10-history-tracking"]=""
  ["11-reconcile-loop-assembly"]="02-source-resolution,03-artifact-fetch-and-cue-validation,05-cue-rendering-bridge,06-digest-computation,08-ssa-apply,09-prune-stale-resources,10-history-tracking,12-finalizer-and-deletion,13-suspend-resume"
  ["12-finalizer-and-deletion"]="09-prune-stale-resources"
  ["13-suspend-resume"]=""
  ["14-drift-detection"]="08-ssa-apply"
  ["15-serviceaccount-impersonation"]="08-ssa-apply,09-prune-stale-resources"
  ["16-failure-counters"]="07-status-conditions"
  ["17-events-emission"]="07-status-conditions,16-failure-counters"
  ["18-metrics"]=""
)

# All changes in topological order (for display purposes).
ALL_CHANGES=(
  "01-cli-dependency-and-inventory-bridge"
  "02-source-resolution"
  "03-artifact-fetch-and-cue-validation"
  "04-catalog-provider-loading"
  "05-cue-rendering-bridge"
  "06-digest-computation"
  "07-status-conditions"
  "08-ssa-apply"
  "09-prune-stale-resources"
  "10-history-tracking"
  "11-reconcile-loop-assembly"
  "12-finalizer-and-deletion"
  "13-suspend-resume"
  "14-drift-detection"
  "15-serviceaccount-impersonation"
  "16-failure-counters"
  "17-events-emission"
  "18-metrics"
)

# ---------------------------------------------------------------------------
# Colors
# ---------------------------------------------------------------------------
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
DIM='\033[2m'
BOLD='\033[1m'
NC='\033[0m'

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------
log()      { echo -e "${DIM}[$(date +%H:%M:%S)]${NC} $*"; }
log_info() { echo -e "${DIM}[$(date +%H:%M:%S)]${NC} ${BLUE}INFO${NC}  $*"; }
log_ok()   { echo -e "${DIM}[$(date +%H:%M:%S)]${NC} ${GREEN}OK${NC}    $*"; }
log_warn() { echo -e "${DIM}[$(date +%H:%M:%S)]${NC} ${YELLOW}WARN${NC}  $*"; }
log_err()  { echo -e "${DIM}[$(date +%H:%M:%S)]${NC} ${RED}FAIL${NC}  $*"; }
log_run()  { echo -e "${DIM}[$(date +%H:%M:%S)]${NC} ${CYAN}RUN${NC}   $*"; }

die() { log_err "$@"; exit 1; }

# ---------------------------------------------------------------------------
# State management
# ---------------------------------------------------------------------------
init_state() {
  mkdir -p "$STATE_DIR" "$LOG_DIR" "$PROMPT_DIR"

  if [[ ! -f "$STATE_FILE" ]]; then
    local state='{}'
    for change in "${ALL_CHANGES[@]}"; do
      state=$(echo "$state" | jq --arg c "$change" '. + {($c): "pending"}')
    done
    echo "$state" | jq '.' > "$STATE_FILE"
    log_info "Initialized state file: ${STATE_FILE}"
  fi
}

get_state() {
  local change="$1"
  jq -r --arg c "$change" '.[$c] // "pending"' "$STATE_FILE"
}

set_state() {
  local change="$1" status="$2"
  local tmp="${STATE_FILE}.tmp"
  jq --arg c "$change" --arg s "$status" '.[$c] = $s' "$STATE_FILE" > "$tmp"
  mv "$tmp" "$STATE_FILE"
}

# ---------------------------------------------------------------------------
# Dependency resolution
# ---------------------------------------------------------------------------
deps_satisfied() {
  local change="$1"
  local dep_str="${DEPS[$change]}"

  # No dependencies — always ready.
  [[ -z "$dep_str" ]] && return 0

  IFS=',' read -ra dep_list <<< "$dep_str"
  for dep in "${dep_list[@]}"; do
    local dep_state
    dep_state=$(get_state "$dep")
    if [[ "$dep_state" != "completed" ]]; then
      return 1
    fi
  done
  return 0
}

find_ready() {
  local ready=()
  for change in "${ALL_CHANGES[@]}"; do
    local state
    state=$(get_state "$change")
    if [[ "$state" == "pending" ]] && deps_satisfied "$change"; then
      ready+=("$change")
    fi
  done
  echo "${ready[@]}"
}

# ---------------------------------------------------------------------------
# Branch helpers
# ---------------------------------------------------------------------------
ensure_base_branch() {
  # Create implement-claude branch from main if it doesn't exist.
  if ! git -C "$REPO_ROOT" show-ref --verify --quiet "refs/heads/${BASE_BRANCH}" 2>/dev/null; then
    log_info "Creating base branch: ${BASE_BRANCH}"
    git -C "$REPO_ROOT" branch "$BASE_BRANCH" main
  fi

  # Push base branch if not on remote.
  if ! git -C "$REPO_ROOT" show-ref --verify --quiet "refs/remotes/${REMOTE}/${BASE_BRANCH}" 2>/dev/null; then
    log_info "Pushing ${BASE_BRANCH} to ${REMOTE}"
    git -C "$REPO_ROOT" push -u "$REMOTE" "$BASE_BRANCH"
  fi
}

branch_name() {
  local change="$1"
  echo "${BRANCH_PREFIX}/${change}"
}

worktree_dir() {
  local change="$1"
  echo "${PARENT_DIR}/${REPO_NAME}-${change}"
}

# ---------------------------------------------------------------------------
# Agent prompt
# ---------------------------------------------------------------------------
build_prompt() {
  local change="$1"
  local prompt_file="${PROMPT_DIR}/${change}.md"

  cat > "$prompt_file" << 'PROMPT_EOF'
You are implementing OpenSpec change: CHANGE_NAME

## Instructions

1. Run `/openspec-apply-change CHANGE_NAME` to implement the change.
2. Follow all instructions from the skill — implement every task.
3. After implementation, run verification:
   - `make fmt vet` (always)
   - `make test` (always)
   - `make lint-fix` (if non-trivial changes)
4. Fix any issues found by verification.
5. Commit all changes with a descriptive conventional commit message.
6. Push the branch to origin.
7. Create a pull request:
   - Base branch: `implement-claude`
   - Title: short, descriptive of the change
   - Body: summary of what was implemented, test plan
   - Use `gh pr create --base implement-claude`

Do NOT ask for user input. Work autonomously to completion.
If you encounter a blocking error you cannot resolve, commit what you have,
push, create a draft PR with the error details, and exit.
PROMPT_EOF

  # Substitute the change name.
  sed -i "s/CHANGE_NAME/${change}/g" "$prompt_file"
  echo "$prompt_file"
}

# ---------------------------------------------------------------------------
# Launch one agent
# ---------------------------------------------------------------------------
launch_agent() {
  local change="$1"
  local branch wt_dir prompt_file log_file

  branch=$(branch_name "$change")
  wt_dir=$(worktree_dir "$change")
  log_file="${LOG_DIR}/${change}.log"

  # Create worktree if it doesn't exist.
  if [[ ! -d "$wt_dir" ]]; then
    if git -C "$REPO_ROOT" show-ref --verify --quiet "refs/heads/${branch}" 2>/dev/null; then
      git -C "$REPO_ROOT" worktree add "$wt_dir" "$branch"
    else
      git -C "$REPO_ROOT" worktree add -b "$branch" "$wt_dir" "$BASE_BRANCH"
    fi
  fi

  # Build prompt.
  prompt_file=$(build_prompt "$change")

  set_state "$change" "running"
  log_run "${change} → ${wt_dir} (branch: ${branch})"

  # Launch claude in the worktree, log output.
  # Subshell propagates claude's exit code so `wait` captures it.
  (
    cd "$wt_dir"
    claude -p "$(cat "$prompt_file")" > "$log_file" 2>&1
    exit_code=$?
    if [[ "$exit_code" -eq 0 ]]; then
      echo "EXIT_SUCCESS" >> "$log_file"
    else
      echo "EXIT_FAILURE:${exit_code}" >> "$log_file"
    fi
    exit "$exit_code"
  ) &

  # Return the PID.
  echo $!
}

# ---------------------------------------------------------------------------
# Wait for a batch of agents
# ---------------------------------------------------------------------------
wait_for_batch() {
  local -n _changes=$1
  local -n _pids=$2

  local i=0
  local failed=0

  for pid in "${_pids[@]}"; do
    local change="${_changes[$i]}"
    local log_file="${LOG_DIR}/${change}.log"

    if wait "$pid" 2>/dev/null; then
      set_state "$change" "completed"
      log_ok "${change}"
    else
      set_state "$change" "failed"
      log_err "${change} — check ${log_file}"
      failed=$((failed + 1))
    fi

    i=$((i + 1))
  done

  return "$failed"
}

# ---------------------------------------------------------------------------
# Display status
# ---------------------------------------------------------------------------
print_status() {
  echo ""
  echo -e "${BOLD}Change Status${NC}"
  echo -e "${DIM}─────────────────────────────────────────────────────${NC}"

  for change in "${ALL_CHANGES[@]}"; do
    local state dep_str icon
    state=$(get_state "$change")
    dep_str="${DEPS[$change]}"

    case "$state" in
      completed) icon="${GREEN}✓${NC}" ;;
      running)   icon="${CYAN}●${NC}" ;;
      failed)    icon="${RED}✗${NC}" ;;
      pending)   icon="${DIM}○${NC}" ;;
      skipped)   icon="${YELLOW}–${NC}" ;;
      *)         icon="?" ;;
    esac

    local deps_display=""
    if [[ -n "$dep_str" ]]; then
      deps_display=" ${DIM}← ${dep_str}${NC}"
    fi

    printf "  %b  %-45s %b%b\n" "$icon" "$change" "${DIM}${state}${NC}" "$deps_display"
  done

  echo -e "${DIM}─────────────────────────────────────────────────────${NC}"
  echo ""
}

# ---------------------------------------------------------------------------
# Commands
# ---------------------------------------------------------------------------
usage() {
  cat <<EOF
Usage: $(basename "$0") <command> [options]

Commands:
  run               Run the orchestrator (launch agents, respect dependencies)
  status            Show current state of all changes
  reset <change>    Reset a change to pending (re-run it)
  reset-failed      Reset all failed changes to pending
  retry             Reset failed changes and run
  logs <change>     Tail the log for a change
  clean             Remove all worktrees and state
  graph             Print the dependency graph

Options:
  MAX_CONCURRENT=N  Max parallel agents (default: 6)

Examples:
  ./scripts/orchestrate-changes.sh run
  ./scripts/orchestrate-changes.sh status
  ./scripts/orchestrate-changes.sh reset 07-status-conditions
  ./scripts/orchestrate-changes.sh retry
  MAX_CONCURRENT=4 ./scripts/orchestrate-changes.sh run
EOF
}

cmd_status() {
  init_state
  print_status
}

cmd_reset() {
  local change="$1"
  if [[ -z "${DEPS[$change]+x}" ]]; then
    die "Unknown change: ${change}"
  fi
  set_state "$change" "pending"
  log_info "Reset ${change} → pending"
}

cmd_reset_failed() {
  for change in "${ALL_CHANGES[@]}"; do
    if [[ "$(get_state "$change")" == "failed" ]]; then
      set_state "$change" "pending"
      log_info "Reset ${change} → pending"
    fi
  done
}

cmd_logs() {
  local change="$1"
  local log_file="${LOG_DIR}/${change}.log"
  if [[ ! -f "$log_file" ]]; then
    die "No log file for ${change}"
  fi
  less +G "$log_file"
}

cmd_graph() {
  echo ""
  echo -e "${BOLD}Dependency Graph${NC}"
  echo ""
  for change in "${ALL_CHANGES[@]}"; do
    local dep_str="${DEPS[$change]}"
    if [[ -z "$dep_str" ]]; then
      echo -e "  ${GREEN}${change}${NC} ${DIM}(no deps)${NC}"
    else
      echo -e "  ${YELLOW}${change}${NC} ${DIM}← ${dep_str}${NC}"
    fi
  done
  echo ""
}

cmd_clean() {
  log_warn "This will remove all worktrees and state. Continue? [y/N]"
  read -r confirm
  [[ "$confirm" =~ ^[Yy]$ ]] || { log_info "Aborted."; exit 0; }

  for change in "${ALL_CHANGES[@]}"; do
    local wt_dir branch
    wt_dir=$(worktree_dir "$change")
    branch=$(branch_name "$change")

    if [[ -d "$wt_dir" ]]; then
      log_info "Removing worktree: ${wt_dir}"
      git -C "$REPO_ROOT" worktree remove --force "$wt_dir" 2>/dev/null || true
    fi

    if git -C "$REPO_ROOT" show-ref --verify --quiet "refs/heads/${branch}" 2>/dev/null; then
      log_info "Deleting branch: ${branch}"
      git -C "$REPO_ROOT" branch -D "$branch" 2>/dev/null || true
    fi
  done

  git -C "$REPO_ROOT" worktree prune

  if [[ -d "$STATE_DIR" ]]; then
    rm -rf "$STATE_DIR"
    log_info "Removed state directory: ${STATE_DIR}"
  fi

  log_ok "Clean complete."
}

cmd_run() {
  init_state
  ensure_base_branch

  log_info "Orchestrator started (max concurrent: ${MAX_CONCURRENT})"
  print_status

  local total_failed=0

  while true; do
    # Find changes ready to launch.
    local ready_str
    ready_str=$(find_ready)

    if [[ -z "$ready_str" ]]; then
      # Check if anything is still running.
      local running=0
      for change in "${ALL_CHANGES[@]}"; do
        [[ "$(get_state "$change")" == "running" ]] && running=$((running + 1))
      done

      if [[ "$running" -gt 0 ]]; then
        # Something is still running but nothing new to launch — wait.
        sleep 5
        continue
      fi

      # Nothing running, nothing ready — we're done or blocked.
      break
    fi

    # Convert to array and cap at MAX_CONCURRENT.
    read -ra ready <<< "$ready_str"
    local batch=()
    local pids=()
    local launched=0

    # Count currently running.
    local current_running=0
    for change in "${ALL_CHANGES[@]}"; do
      [[ "$(get_state "$change")" == "running" ]] && current_running=$((current_running + 1))
    done

    local slots=$((MAX_CONCURRENT - current_running))

    for change in "${ready[@]}"; do
      [[ "$slots" -le 0 ]] && break
      local pid
      pid=$(launch_agent "$change")
      batch+=("$change")
      pids+=("$pid")
      slots=$((slots - 1))
      launched=$((launched + 1))
    done

    if [[ "$launched" -eq 0 ]]; then
      sleep 5
      continue
    fi

    log_info "Launched batch of ${launched} agent(s). Waiting..."
    echo ""

    # Wait for this batch to complete.
    local batch_failed=0
    wait_for_batch batch pids || batch_failed=$?
    total_failed=$((total_failed + batch_failed))

    print_status
  done

  # Final summary.
  echo ""
  echo -e "${BOLD}Orchestration Complete${NC}"
  echo ""

  local completed=0 failed=0 pending=0 skipped=0
  for change in "${ALL_CHANGES[@]}"; do
    case "$(get_state "$change")" in
      completed) completed=$((completed + 1)) ;;
      failed)    failed=$((failed + 1)) ;;
      pending)   pending=$((pending + 1)) ;;
      *)         skipped=$((skipped + 1)) ;;
    esac
  done

  echo -e "  ${GREEN}Completed:${NC} ${completed}"
  echo -e "  ${RED}Failed:${NC}    ${failed}"
  echo -e "  ${DIM}Pending:${NC}   ${pending} ${DIM}(blocked by failed deps)${NC}"
  echo ""

  if [[ "$failed" -gt 0 ]]; then
    log_warn "Some changes failed. Fix issues and run: $(basename "$0") retry"
    exit 1
  fi

  if [[ "$pending" -gt 0 ]]; then
    log_warn "Some changes still pending (blocked). Check dependencies."
    exit 1
  fi

  log_ok "All changes implemented successfully."
}

# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------
if [[ $# -eq 0 ]]; then
  usage
  exit 1
fi

case "$1" in
  run)          cmd_run ;;
  status)       cmd_status ;;
  reset)
    [[ $# -lt 2 ]] && die "Usage: $(basename "$0") reset <change-name>"
    init_state
    cmd_reset "$2"
    ;;
  reset-failed) init_state; cmd_reset_failed ;;
  retry)        init_state; cmd_reset_failed; cmd_run ;;
  logs)
    [[ $# -lt 2 ]] && die "Usage: $(basename "$0") logs <change-name>"
    cmd_logs "$2"
    ;;
  graph)        cmd_graph ;;
  clean)        cmd_clean ;;
  --help|-h)    usage ;;
  *)
    die "Unknown command: $1 (try --help)"
    ;;
esac
