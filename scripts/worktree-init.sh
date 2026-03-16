#!/usr/bin/env bash
# worktree-init.sh — Create one Git worktree + branch per AI model for parallel benchmarking.
set -euo pipefail

REPO_ROOT="$(git -C "$(dirname "$0")" rev-parse --show-toplevel)"
REPO_NAME="$(basename "$REPO_ROOT")"
PARENT_DIR="$(dirname "$REPO_ROOT")"
BRANCH_PREFIX="bench"

usage() {
    cat <<EOF
Usage:
  $(basename "$0") <model> [model ...]   Create a worktree per model
  $(basename "$0") --list                List active bench worktrees
  $(basename "$0") --clean               Remove all bench worktrees and branches
  $(basename "$0") --help                Show this help

Examples:
  $(basename "$0") gpt4o gemini claude-sonnet o3
  $(basename "$0") --clean
EOF
}

# --- list -------------------------------------------------------------------
cmd_list() {
    echo "Active bench worktrees:"
    git -C "$REPO_ROOT" worktree list --porcelain \
        | awk '/^worktree /{wt=$2} /^branch refs\/heads\/bench\//{print wt, $2}' \
        | column -t
}

# --- clean -------------------------------------------------------------------
cmd_clean() {
    local removed=0
    while IFS= read -r branch; do
        [ -z "$branch" ] && continue
        local name="${branch#refs/heads/${BRANCH_PREFIX}/}"
        local wt_dir="${PARENT_DIR}/${REPO_NAME}-${name}"

        if [ -d "$wt_dir" ]; then
            echo "Removing worktree ${wt_dir}"
            git -C "$REPO_ROOT" worktree remove --force "$wt_dir"
        fi

        echo "Deleting branch ${BRANCH_PREFIX}/${name}"
        git -C "$REPO_ROOT" branch -D "${BRANCH_PREFIX}/${name}" 2>/dev/null || true
        removed=$((removed + 1))
    done < <(git -C "$REPO_ROOT" for-each-ref --format='%(refname)' "refs/heads/${BRANCH_PREFIX}/")

    git -C "$REPO_ROOT" worktree prune
    echo "Cleaned up ${removed} worktree(s)."
}

# --- create ------------------------------------------------------------------
cmd_create() {
    local base_ref
    base_ref="$(git -C "$REPO_ROOT" rev-parse HEAD)"
    local created=0

    for model in "$@"; do
        local branch="${BRANCH_PREFIX}/${model}"
        local wt_dir="${PARENT_DIR}/${REPO_NAME}-${model}"

        if [ -d "$wt_dir" ]; then
            echo "SKIP  ${wt_dir} already exists"
            continue
        fi

        if git -C "$REPO_ROOT" show-ref --verify --quiet "refs/heads/${branch}" 2>/dev/null; then
            echo "SKIP  branch ${branch} already exists (use --clean first)"
            continue
        fi

        git -C "$REPO_ROOT" worktree add -b "$branch" "$wt_dir" "$base_ref"
        created=$((created + 1))
        echo "  OK  ${wt_dir}  ->  branch ${branch}"
    done

    echo ""
    echo "Created ${created} worktree(s) from $(git -C "$REPO_ROOT" rev-parse --short HEAD)."
    echo ""
    cmd_list
}

# --- main --------------------------------------------------------------------
if [ $# -eq 0 ]; then
    usage
    exit 1
fi

case "$1" in
    --help|-h)  usage ;;
    --list|-l)  cmd_list ;;
    --clean|-c) cmd_clean ;;
    --*)
        echo "Unknown flag: $1" >&2
        usage
        exit 1
        ;;
    *)          cmd_create "$@" ;;
esac
