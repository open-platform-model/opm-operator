#!/usr/bin/env bash
# test-image.sh: offline test of hack/operator-module/image.sh, the operator
# module's image PR script: its title and breaking rules and its refusals.
#
# Each case builds a scratch git repository: the fixture module at
# modules/opm_operator (deploying v1.0.0-beta.1), a CRD under config/crd/bases
# and a CHANGELOG.md, tagged v1.0.0-beta.1, then the case's v1.0.0-beta.2 on
# the next commit, which is also main. The release guard and the image tag
# guard are stubs that pass; the drift check stub compares config/ at the tag
# with main, which is what the real check's verdict follows. Needs git and
# mikefarah yq; no network. Prints PASS/FAIL per case, exits 1 on any FAIL.
set -euo pipefail
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
repo=$(cd "$here/../.." && pwd)
IMAGE=$repo/hack/operator-module/image.sh
T=$repo/hack/testdata/operator-module-image
MODULE=$repo/hack/testdata/operator-module-release-check/module
DIGEST=sha256:2222222222222222222222222222222222222222222222222222222222222222

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
export GIT_AUTHOR_NAME=image-test GIT_AUTHOR_EMAIL=image-test@invalid \
  GIT_COMMITTER_NAME=image-test GIT_COMMITTER_EMAIL=image-test@invalid
unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE

mkdir -p "$TMP/stubs"
printf '#!/bin/sh\nexit 0\n' >"$TMP/stubs/pass"
printf '#!/bin/sh\necho %s\n' "$DIGEST" >"$TMP/stubs/digest"
# drift --ref TAG: the module's data follow main's config/, so they match the
# tag exactly when config/ is equal at the tag and on main.
# shellcheck disable=SC2016 # $2 belongs to the stub, not to this script
printf '#!/bin/sh\nexec git diff --quiet "$2" main -- config\n' >"$TMP/stubs/drift"
chmod +x "$TMP/stubs/"*
export RELEASE_GUARD=$TMP/stubs/pass IMAGE_TAG_GUARD=$TMP/stubs/digest DRIFT_CHECK=$TMP/stubs/drift MAIN_REF=main

gitq() { git -c commit.gpgsign=false -c tag.gpgsign=false -c init.defaultBranch=main "$@"; }

# scratch NAME CRD CHANGELOG: v1.0.0-beta.1 with crd.yaml, then v1.0.0-beta.2
# (main) with the given CRD and CHANGELOG.
scratch() {
  local d=$TMP/$1
  mkdir -p "$d/modules" "$d/config/crd/bases"
  cp -r "$MODULE" "$d/modules/opm_operator"
  sed -i 's/^Version: .*/Version: "1.0.0-beta.1"/' "$d/modules/opm_operator/operator/operator.cue"
  cp "$T/crd.yaml" "$d/config/crd/bases/widgets.yaml"
  printf '# Changelog\n' >"$d/CHANGELOG.md"
  (cd "$d" && gitq init -q && gitq add -A && gitq commit -q -m one && gitq tag v1.0.0-beta.1)
  cp "$T/$2" "$d/config/crd/bases/widgets.yaml"
  cp "$T/$3" "$d/CHANGELOG.md"
  (cd "$d" && gitq add -A && gitq commit -q -m two && gitq tag v1.0.0-beta.2)
  printf '%s\n' "$d"
}

FAILED=0
fail() { printf 'FAIL %s: %s\n' "$1" "$2"; sed 's/^/    /' "$TMP/$1.log"; FAILED=1; }
# run NAME DIR WANT_RC: run image.sh set v1.0.0-beta.2; stdout to NAME.out.
run() {
  local rc=0
  (cd "$2" && "$IMAGE" set v1.0.0-beta.2) >"$TMP/$1.out" 2>"$TMP/$1.log" || rc=$?
  [ "$rc" = "$3" ] || { fail "$1" "exit $rc, want $3"; return 1; }
}
out() { sed -n "s/^$2=//p" "$TMP/$1.out"; }

# check_written NAME DIR: only operator.cue changed, to beta.2 at DIGEST.
check_written() {
  local st
  st=$(cd "$2" && git status --porcelain --untracked-files=all)
  if [ "$st" != " M modules/opm_operator/operator/operator.cue" ]; then
    fail "$1" "changed files: $st"; return 1
  fi
  if ! grep -q '^Version: "1.0.0-beta.2"' "$2/modules/opm_operator/operator/operator.cue" ||
    ! grep -q "digest: *\"$DIGEST\"" "$2/modules/opm_operator/operator/operator.cue"; then
    fail "$1" "operator.cue does not name v1.0.0-beta.2 at $DIGEST"
    return 1
  fi
}

d=$(scratch plain crd.yaml changelog-plain.md)
if run plain "$d" 0 && check_written plain "$d"; then
  if [ "$(out plain title)" != "fix(deps): deploy operator v1.0.0-beta.2 from the operator module" ] ||
    [ "$(out plain breaking)" != 0 ] || [ "$(out plain changed)" != true ]; then
    fail plain "outputs: $(tr '\n' ' ' <"$TMP/plain.out")"
  else
    printf 'PASS plain\n'
  fi
fi

d=$(scratch breaking-changelog crd.yaml changelog-breaking.md)
if run breaking-changelog "$d" 0 && check_written breaking-changelog "$d"; then
  if [ "$(out breaking-changelog title)" != "fix(deps)!: deploy operator v1.0.0-beta.2 from the operator module" ] ||
    [ "$(out breaking-changelog breaking)" != 1 ]; then
    fail breaking-changelog "outputs: $(tr '\n' ' ' <"$TMP/breaking-changelog.out")"
  else
    printf 'PASS breaking-changelog\n'
  fi
fi

d=$(scratch dropped-served-version crd-v1alpha1-dropped.yaml changelog-plain.md)
if run dropped-served-version "$d" 0 && check_written dropped-served-version "$d"; then
  if [ "$(out dropped-served-version breaking)" != 1 ] ||
    ! grep -qF "widgets.yaml v1alpha1" "$TMP/dropped-served-version.log"; then
    fail dropped-served-version "not breaking, or the dropped version is not named"
  else
    printf 'PASS dropped-served-version\n'
  fi
fi

d=$(scratch tag-behind-main crd.yaml changelog-plain.md)
cp "$T/crd-v1alpha1-dropped.yaml" "$d/config/crd/bases/widgets.yaml"
(cd "$d" && gitq commit -q -am three)
if run tag-behind-main "$d" 1; then
  if ! grep -qF "config/crd/bases/widgets.yaml" "$TMP/tag-behind-main.log"; then
    fail tag-behind-main "the differing CRD file is not named"
  elif [ -n "$(cd "$d" && git status --porcelain)" ]; then
    fail tag-behind-main "the tree changed"
  else
    printf 'PASS tag-behind-main\n'
  fi
fi

d=$(scratch write-outside crd.yaml changelog-plain.md)
printf 'stray\n' >>"$d/modules/opm_operator/identity/identity.cue"
if run write-outside "$d" 1; then
  if ! grep -qF "identity/identity.cue" "$TMP/write-outside.log"; then
    fail write-outside "the stray file is not named"
  else
    printf 'PASS write-outside\n'
  fi
fi

d=$(scratch already-deployed crd.yaml changelog-plain.md)
sed -i -E -e 's/^Version: .*/Version: "1.0.0-beta.2"/' \
  -e "s/(digest: *)\"[^\"]+\"/\\1\"$DIGEST\"/" "$d/modules/opm_operator/operator/operator.cue"
(cd "$d" && gitq commit -q -am deployed)
if run already-deployed "$d" 0; then
  if [ "$(out already-deployed changed)" != false ] || [ -n "$(cd "$d" && git status --porcelain)" ]; then
    fail already-deployed "changed is not false, or the tree changed"
  else
    printf 'PASS already-deployed\n'
  fi
fi

# The module already deploys beta.2; beta.1 is older and must be refused.
d=$(scratch older-tag crd.yaml changelog-plain.md)
sed -i -E -e 's/^Version: .*/Version: "1.0.0-beta.2"/' \
  -e "s/(digest: *)\"[^\"]+\"/\\1\"$DIGEST\"/" "$d/modules/opm_operator/operator/operator.cue"
(cd "$d" && gitq commit -q -am deployed)
rc=0
(cd "$d" && "$IMAGE" set v1.0.0-beta.1) >"$TMP/older-tag.out" 2>"$TMP/older-tag.log" || rc=$?
if [ "$rc" != 1 ]; then
  fail older-tag "exit $rc, want 1"
elif ! grep -qF "not newer than the deployed v1.0.0-beta.2" "$TMP/older-tag.log" ||
  [ -n "$(cd "$d" && git status --porcelain)" ] || [ -s "$TMP/older-tag.out" ]; then
  fail older-tag "not refused for the older tag, or the tree or outputs changed"
else
  printf 'PASS older-tag\n'
fi

exit "$FAILED"
