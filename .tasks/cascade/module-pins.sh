#!/usr/bin/env bash
# module-pins.sh <WORKTREE|git-ref>: the operator module's upstream pins, for the title and
# body of its own cascade PR (task deps:cascade:module; Phase 2 cascade contract §4.1 row
# shape: <pin-key> <display> <class> <v-version> <labels>). Both are shipped: every user
# who installs the module receives them. The repository's pins.sh lists neither, so each
# key appears once per report. WORKTREE reads the file on disk; anything else is read with
# git show <ref>:<path>. A missing file omits both rows; any other failure exits 1.
set -euo pipefail
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source-path=SCRIPTDIR source=lib.sh
. "$here/lib.sh"
die() { printf 'module-pins.sh: %s\n' "$1" >&2; exit 1; }

ref="${1:-}"
[ -n "$ref" ] || die "usage: module-pins.sh <WORKTREE|git-ref>"
top=$(git rev-parse --show-toplevel) || die "not inside a git checkout"
cd "$top"
f=modules/opm_operator/cue.mod/module.cue
if [ "$ref" = WORKTREE ]; then
  [ -f "$f" ] || exit 0
  text=$(cat "$f")
else
  git rev-parse -q --verify "$ref^{commit}" >/dev/null || die "not a commit: $ref"
  git cat-file -e "$ref:$f" 2>/dev/null || exit 0
  text=$(git show "$ref:$f")
fi

# row KEY DISPLAY VERSION
row() {
  valid_v "$3" || die "$f: no valid version for $1 (read '$3')"
  printf '%s\t%s\tshipped\t%s\t\n' "$1" "$2" "$3"
}
row opmodel.dev/catalogs/opm@v4 "the operator module's opm catalog" "$(cue_dep_v opmodel.dev/catalogs/opm@v4 <<<"$text")"
row opmodel.dev/core@v2 core "$(cue_dep_v opmodel.dev/core@v2 <<<"$text")"
