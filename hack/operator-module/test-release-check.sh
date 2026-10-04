#!/usr/bin/env bash
# test-release-check.sh: offline test of hack/operator-module/release-check.sh.
#
# Each case builds a scratch git repository holding the fixture module
# (hack/testdata/operator-module-release-check/module) at modules/opm_operator,
# two operator tags made in order (v1.0.0-beta.1, then v1.0.0-beta.2) and a
# minimum operator version file, edits the copy for the case, and runs the
# check with stubs that pass for the release guard, the image tag guard and the
# drift check. Every failing case asserts the failure the check names. Needs
# git and cue; no network. Prints PASS/FAIL per case, exits 1 on any FAIL.
set -euo pipefail
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
repo=$(cd "$here/../.." && pwd)
CHECK=$repo/hack/operator-module/release-check.sh
FIXTURE=$repo/hack/testdata/operator-module-release-check/module
DIGEST=sha256:1111111111111111111111111111111111111111111111111111111111111111

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
# The fixture's deps are never imported, so cue resolves nothing; a dead
# registry proves it.
export CUE_REGISTRY=localhost:1 CUE_CACHE_DIR=$TMP/cue-cache
export GIT_AUTHOR_NAME=release-check-test GIT_AUTHOR_EMAIL=release-check-test@invalid \
  GIT_COMMITTER_NAME=release-check-test GIT_COMMITTER_EMAIL=release-check-test@invalid
unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE

mkdir -p "$TMP/stubs"
printf '#!/bin/sh\nexit 0\n' >"$TMP/stubs/pass"
printf '#!/bin/sh\necho %s\n' "$DIGEST" >"$TMP/stubs/digest"
chmod +x "$TMP/stubs/pass" "$TMP/stubs/digest"
export RELEASE_GUARD=$TMP/stubs/pass IMAGE_TAG_GUARD=$TMP/stubs/digest DRIFT_CHECK=$TMP/stubs/pass

gitq() { git -c commit.gpgsign=false -c tag.gpgsign=false -c init.defaultBranch=main "$@"; }

# scratch NAME: a repo with the fixture module, tags v1.0.0-beta.1 and
# v1.0.0-beta.2 on successive commits, and the min file naming beta.1.
scratch() {
  local d=$TMP/$1
  mkdir -p "$d/modules" "$d/hack/operator-module"
  cp -r "$FIXTURE" "$d/modules/opm_operator"
  printf 'v1.0.0-beta.1\n' >"$d/hack/operator-module/min-operator-version"
  (cd "$d" && gitq init -q && gitq add -A && gitq commit -q -m one && gitq tag v1.0.0-beta.1 &&
    gitq commit -q --allow-empty -m two && gitq tag v1.0.0-beta.2)
  printf '%s\n' "$d"
}

set_identity() { sed -i "s/^Version: .*/Version:    \"$2\"/" "$1/modules/opm_operator/identity/identity.cue"; }
set_operator() { sed -i "s/^Version: .*/Version: \"$2\"/" "$1/modules/opm_operator/operator/operator.cue"; }
set_path_major() {
  sed -i "s/opm_operator@v0/opm_operator@v$2/" "$1/modules/opm_operator/cue.mod/module.cue" \
    "$1/modules/opm_operator/identity/identity.cue"
}

FAILED=0
# run NAME DIR VERSION WANT_RC [MESSAGE...]: run the check; assert its exit
# and that its output holds each MESSAGE.
run() {
  local name=$1 d=$2 v=$3 want=$4 rc=0 m
  shift 4
  (cd "$d" && "$CHECK" "$v") >"$TMP/$name.log" 2>&1 || rc=$?
  if [ "$rc" != "$want" ]; then
    printf 'FAIL %s: exit %s, want %s\n' "$name" "$rc" "$want"
    sed 's/^/    /' "$TMP/$name.log"
    FAILED=1
    return 0
  fi
  for m in "$@"; do
    if ! grep -qF -- "$m" "$TMP/$name.log"; then
      printf 'FAIL %s: output lacks "%s"\n' "$name" "$m"
      sed 's/^/    /' "$TMP/$name.log"
      FAILED=1
      return 0
    fi
  done
  printf 'PASS %s\n' "$name"
}

d=$(scratch clean)
run clean "$d" 0.1.0 0 "is releasable"

d=$(scratch dev-pin)
sed -i 's/v: "v4.6.0"/v: "v4.7.0-0.dev.3"/' "$d/modules/opm_operator/cue.mod/module.cue"
run dev-pin "$d" 0.1.0 1 "pins a development version" 'v4.7.0-0.dev.3'

d=$(scratch local-module)
printf 'module: "opmodel.dev/modules/opm_operator@v0"\n' >"$d/modules/opm_operator/cue.mod/local-module.cue"
run local-module "$d" 0.1.0 1 "local-module.cue is present"

d=$(scratch version-mismatch)
run version-mismatch "$d" 0.2.0 1 "identity.Version is '0.1.0', the release PR proposes '0.2.0'"

d=$(scratch major-mismatch)
set_identity "$d" 1.2.3
set_operator "$d" 1.0.0
run major-mismatch "$d" 1.2.3 1 "version 1.2.3 has major 1, the module path opmodel.dev/modules/opm_operator@v0 has major 0"

d=$(scratch ga-before-operator-ga)
set_path_major "$d" 1
set_identity "$d" 1.0.0
run ga-before-operator-ga "$d" 1.0.0 1 "is 1.0.0 or higher while the deployed operator v1.0.0-beta.2 is a prerelease"

d=$(scratch below-minimum)
set_operator "$d" 1.0.0-beta.1
printf 'v1.0.0-beta.2\n' >"$d/hack/operator-module/min-operator-version"
run below-minimum "$d" 0.1.0 1 "the deployed operator v1.0.0-beta.1 is older than the minimum operator version v1.0.0-beta.2"

d=$(scratch unresolvable-minimum)
printf 'v1.0.0-beta.9\n' >"$d/hack/operator-module/min-operator-version"
run unresolvable-minimum "$d" 0.1.0 1 "cannot resolve the minimum operator version v1.0.0-beta.9"
if grep -qF "is older than" "$TMP/unresolvable-minimum.log"; then
  printf 'FAIL unresolvable-minimum: reported as older, not as unresolvable\n'
  FAILED=1
fi

d=$(scratch two-failures)
sed -i 's/v: "v4.6.0"/v: "v4.7.0-0.dev.3"/' "$d/modules/opm_operator/cue.mod/module.cue"
run two-failures "$d" 0.2.0 1 "pins a development version" "the release PR proposes '0.2.0'"

exit "$FAILED"
