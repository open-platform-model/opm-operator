#!/usr/bin/env bash
# image.sh set <operator-tag>: point the operator module at a published
# operator release, for the module's image PR (release.yml module-image-pr,
# module-image.yml).
#
# It writes only Version and Image.digest in modules/opm_operator/operator/
# operator.cue (the tag follows Version) and prints, in GITHUB_OUTPUT shape:
#   changed=true|false   false when the module already deploys <operator-tag>
#   breaking=0|1         1 when the operator CHANGELOG sections after the
#                        module's previous operator tag, up to <operator-tag>,
#                        hold a breaking change, or a CRD under
#                        config/crd/bases at <operator-tag> stops serving a
#                        version it served at the previous tag
#   title=<PR title>     fix(deps): deploy operator <tag> from the operator module
#                        (fix(deps)!: when breaking=1)
# It refuses an operator tag whose release is not published, and a tag whose
# config/ (CRDs, RBAC) differs from MAIN_REF's: the module's generated data
# follow main's config/ on every PR, so the module release gate would refuse
# such a module. Network calls go through RELEASE_GUARD, IMAGE_TAG_GUARD and
# DRIFT_CHECK, which the offline test (test-image.sh) replaces with stubs.
set -euo pipefail

[ $# -eq 2 ] && [ "$1" = set ] || { echo "usage: $0 set <operator-tag>" >&2; exit 2; }
tag=$2
[[ $tag =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]] || { echo "$0: '$tag' is not an operator release tag" >&2; exit 2; }

M=modules/opm_operator
F=$M/operator/operator.cue
IMAGE_REPO=ghcr.io/open-platform-model/opm-operator
MAIN_REF=${MAIN_REF:-origin/main}
RELEASE_GUARD=${RELEASE_GUARD:-.github/scripts/release-guard.sh}
IMAGE_TAG_GUARD=${IMAGE_TAG_GUARD:-.github/scripts/image-tag-guard.sh}
DRIFT_CHECK=${DRIFT_CHECK:-hack/operator-module/drift-check.sh}

die() { echo "image.sh: $*" >&2; exit 1; }
[ -f "$F" ] || die "no $F here; run from the repository root"
git rev-parse --verify --quiet "$tag^{commit}" >/dev/null || die "cannot resolve $tag (missing tag or shallow checkout?)"
git rev-parse --verify --quiet "$MAIN_REF^{commit}" >/dev/null || die "cannot resolve $MAIN_REF"

"$RELEASE_GUARD" assert-published "$tag" >&2 || die "$tag is not a published operator release"

# The module's CRD and RBAC data equal MAIN_REF's config/; they must equal the tag's too.
if ! "$DRIFT_CHECK" --ref "$tag" >&2; then
  echo "image.sh: config/ at $tag differs from $MAIN_REF; the module cannot deploy $tag:" >&2
  git diff --name-only "$tag" "$MAIN_REF" -- config/crd/bases config/rbac | sed 's/^/  /' >&2
  die "release an operator carrying $MAIN_REF's config/, then move the module to it"
fi

digest=$("$IMAGE_TAG_GUARD" digest "$IMAGE_REPO:$tag") || die "cannot read the digest of $IMAGE_REPO:$tag"
[[ $digest =~ ^sha256:[0-9a-f]{64}$ ]] || die "'$digest' is not a sha256 digest"

field() { sed -nE "s/^[[:space:]]*$1:[[:space:]]*\"([^\"]+)\".*/\\1/p" "$F" | head -n1; }
prev_version=$(field Version)
prev_digest=$(field digest)
[ -n "$prev_version" ] && [ -n "$prev_digest" ] || die "$F: cannot read Version and digest"
prev=v$prev_version

if [ "$prev" = "$tag" ] && [ "$prev_digest" = "$digest" ]; then
  echo "image.sh: the module already deploys $IMAGE_REPO:$tag@$digest" >&2
  printf 'changed=false\nbreaking=0\ntitle=\n'
  exit 0
fi

# A clean tree before, exactly one changed file after: nothing else rides the PR.
dirty=$(git status --porcelain --untracked-files=all)
[ -z "$dirty" ] || die "the tree is not clean before the write: ${dirty//$'\n'/; }"
sed -i -E \
  -e "s/^(Version:[[:space:]]*)\"[^\"]+\"/\\1\"${tag#v}\"/" \
  -e "s/^([[:space:]]*digest:[[:space:]]*)\"[^\"]+\"/\\1\"$digest\"/" "$F"
[ "$(field Version)" = "${tag#v}" ] && [ "$(field digest)" = "$digest" ] || die "$F: the write did not take"
changed=$(git status --porcelain --untracked-files=all)
[ "$changed" = " M $F" ] || die "the write changed more than $F: ${changed//$'\n'/; }"

breaking=0
# CHANGELOG sections newer than the previous operator tag, up to the new one.
if git cat-file -e "$tag:CHANGELOG.md" 2>/dev/null &&
  git show "$tag:CHANGELOG.md" | awk -v new="## [${tag#v}]" -v old="## [${prev#v}]" '
    index($0, new) == 1 { on = 1 }
    index($0, old) == 1 { exit }
    on' | grep -q 'BREAKING CHANGES'; then
  echo "image.sh: the operator CHANGELOG after $prev, up to $tag, declares a breaking change" >&2
  breaking=1
fi
# Served CRD versions: every <crd> <version> served at prev must still be served at tag.
served() { # REF: "<file> <version>" for each served version of each CRD at REF
  local f
  git ls-tree --name-only "$1" config/crd/bases/ 2>/dev/null | grep -E '\.ya?ml$' | while read -r f; do
    git show "$1:$f" | yq -N '.spec.versions[] | select(.served == true) | .name' | sed "s|^|${f##*/} |"
  done | LC_ALL=C sort
}
if git rev-parse --verify --quiet "$prev^{commit}" >/dev/null; then
  dropped=$(LC_ALL=C comm -23 <(served "$prev") <(served "$tag"))
  if [ -n "$dropped" ]; then
    echo "image.sh: $tag stops serving CRD versions served at $prev: ${dropped//$'\n'/; }" >&2
    breaking=1
  fi
else
  echo "image.sh: the previous operator tag $prev is not in this repository; served CRD versions not compared" >&2
fi

bang=''
[ "$breaking" = 0 ] || bang='!'
printf 'changed=true\nbreaking=%s\ntitle=fix(deps)%s: deploy operator %s from the operator module\n' "$breaking" "$bang" "$tag"
