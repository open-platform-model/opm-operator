#!/usr/bin/env bash
# pins.sh <WORKTREE|git-ref>: opm-operator's upstream pins, one TSV row each:
# <pin-key> <display> <class> <v-version> <labels> (Phase 2 cascade contract §4.1).
# WORKTREE reads the files on disk; anything else is read with git show <ref>:<path>.
# A file missing at that ref omits its row; any other failure exits 1.
set -euo pipefail
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source-path=SCRIPTDIR source=lib.sh
. "$here/lib.sh"
die() { printf 'pins.sh: %s\n' "$1" >&2; exit 1; }

ref="${1:-}"
[ -n "$ref" ] || die "usage: pins.sh <WORKTREE|git-ref>"
top=$(git rev-parse --show-toplevel) || die "not inside a git checkout"
cd "$top"
if [ "$ref" != WORKTREE ]; then
  git rev-parse -q --verify "$ref^{commit}" >/dev/null || die "not a commit: $ref"
fi

has() {
  if [ "$ref" = WORKTREE ]; then [ -f "$1" ]; else git cat-file -e "$ref:$1" 2>/dev/null; fi
}
show() {
  if [ "$ref" = WORKTREE ]; then cat "$1"; else git show "$ref:$1"; fi
}
# row KEY DISPLAY CLASS FILE VERSION
row() {
  valid_v "$5" || die "$4: no valid version for $1 (read '$5')"
  printf '%s\t%s\t%s\t%s\t\n' "$1" "$2" "$3" "$5"
}

f=go.mod
if has "$f"; then
  v=$(show "$f" | go_require_v github.com/open-platform-model/library)
  row github.com/open-platform-model/library library shipped "$f" "$v"
fi

f=config/samples/opmodel.dev_v1alpha1_platform.yaml
if has "$f"; then
  v=$(show "$f" | yaml_version_after opmodel.dev/catalogs/opm@v4:)
  row opmodel.dev/catalogs/opm@v4 "opm catalog" test "$f" "v$v"
fi

f=test/fixtures/modules/hello/cue.mod/module.cue
if has "$f"; then
  v=$(show "$f" | cue_dep_v opmodel.dev/core@v2)
  row opmodel.dev/core@v2 core test "$f" "$v"
fi

f=.opm-cli-version
if has "$f"; then
  v=$(show "$f" | tr -d '[:space:]')
  row github.com/open-platform-model/cli "opm CLI" release-tool "$f" "$v"
fi
