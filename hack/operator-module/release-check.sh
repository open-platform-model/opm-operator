#!/usr/bin/env bash
# release-check.sh <proposed-version>: the operator module's release gate.
#
# Runs on the module's release PR (the Lint job) and in the module publish job
# before anything is pushed. It fails, naming every failure before it exits,
# when the module would ship:
#   - a -0.dev. pin in cue.mod/module.cue, or a cue.mod/local-module.cue;
#   - an image that is not ghcr.io/open-platform-model/opm-operator:<tag>@<digest>,
#     where <tag> is a published operator release and <digest> what GHCR serves;
#   - an identity.Version other than the proposed version, or a major other
#     than the module path's;
#   - 1.0.0 or higher while the deployed operator is a prerelease;
#   - an operator older than hack/operator-module/min-operator-version;
#   - CRDs or RBAC that differ from config/ at the deployed operator tag.
#
# The network checks go through RELEASE_GUARD, IMAGE_TAG_GUARD and DRIFT_CHECK,
# which the offline test (test-release-check.sh) replaces with stubs. Git calls
# use the working directory's repository, which needs the operator tags.
set -uo pipefail

[ $# -eq 1 ] && [ -n "$1" ] || { echo "usage: $0 <proposed-version>" >&2; exit 2; }
proposed=$1

M=modules/opm_operator
IMAGE_REPO=ghcr.io/open-platform-model/opm-operator
RELEASE_GUARD=${RELEASE_GUARD:-.github/scripts/release-guard.sh}
IMAGE_TAG_GUARD=${IMAGE_TAG_GUARD:-.github/scripts/image-tag-guard.sh}
DRIFT_CHECK=${DRIFT_CHECK:-hack/operator-module/drift-check.sh}
MIN_OPERATOR_VERSION_FILE=${MIN_OPERATOR_VERSION_FILE:-hack/operator-module/min-operator-version}

[ -d "$M" ] || { echo "release-check: no $M here; run from the repository root" >&2; exit 2; }

failed=0
bad() { echo "release-check: FAIL: $*" >&2; failed=1; }
ok() { echo "release-check: ok: $*"; }
ex() { (cd "$M" && cue export "$@" --out text); }

semver_re='^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'
image_re='^ghcr\.io/open-platform-model/opm-operator:(v[^@]+)@(sha256:[0-9a-f]{64})$'

# Pins: no development pin, no local override.
if dev=$(grep -nE 'v: *"[^"]*-0\.dev\.' "$M/cue.mod/module.cue"); then
  bad "$M/cue.mod/module.cue pins a development version: ${dev//$'\n'/; }"
else
  ok "no development pin"
fi
if [ -e "$M/cue.mod/local-module.cue" ]; then
  bad "$M/cue.mod/local-module.cue is present; a release never ships a local override"
else
  ok "no local-module.cue"
fi

# Version: the proposed one, on the path's major, 0.x rules.
path=$(ex ./identity -e ModulePath) || { bad "cannot read identity.ModulePath"; path=; }
version=$(ex ./identity -e Version) || { bad "cannot read identity.Version"; version=; }
if [ "$version" != "$proposed" ]; then
  bad "identity.Version is '$version', the release PR proposes '$proposed'"
fi
if [[ $proposed =~ $semver_re ]]; then
  major=${BASH_REMATCH[1]}
  path_major=${path##*@v}
  if [ "$major" != "$path_major" ]; then
    bad "version $proposed has major $major, the module path $path has major $path_major"
  fi
else
  bad "proposed version '$proposed' is not a release SemVer X.Y.Z"
  major=
fi

# Image: a published operator release at the digest GHCR serves.
tag='' want=''
if img=$(ex ./operator -e '"\(Image.repository):\(Image.tag)@\(Image.digest)"'); then
  if [[ $img =~ $image_re ]]; then
    tag=${BASH_REMATCH[1]} want=${BASH_REMATCH[2]}
    ok "module deploys $img"
  else
    bad "image reference '$img' is not $IMAGE_REPO:v<version>@sha256:<64 hex>"
  fi
else
  bad "cannot read the image from $M/operator"
fi
if [ -n "$major" ] && [ "$major" -ge 1 ] && [ -n "$tag" ] && [[ $tag == *-* ]]; then
  bad "version $proposed is 1.0.0 or higher while the deployed operator $tag is a prerelease"
fi

if [ -n "$tag" ]; then
  if "$RELEASE_GUARD" assert-published "$tag" >/dev/null; then
    ok "$tag is a published operator release"
  else
    bad "$tag is not a published operator release"
  fi
  if got=$("$IMAGE_TAG_GUARD" digest "$IMAGE_REPO:$tag"); then
    if [ "$got" = "$want" ]; then
      ok "GHCR serves $want under $tag"
    else
      bad "$tag: the module names $want, GHCR serves $got"
    fi
  else
    bad "cannot read the digest GHCR serves under $IMAGE_REPO:$tag"
  fi

  # Minimum operator version, by ancestry: operator tags are cut from main in order.
  min=$(tr -d '[:space:]' <"$MIN_OPERATOR_VERSION_FILE" 2>/dev/null) || min=
  if [ -z "$min" ]; then
    bad "$MIN_OPERATOR_VERSION_FILE is missing or empty"
  elif ! git rev-parse --verify --quiet "$min^{commit}" >/dev/null ||
    ! git rev-parse --verify --quiet "$tag^{commit}" >/dev/null; then
    bad "cannot resolve the minimum operator version $min or the deployed tag $tag (shallow checkout or missing tag?)"
  elif ! git merge-base --is-ancestor "$min" "$tag"; then
    bad "the deployed operator $tag is older than the minimum operator version $min (refuse-own-instance)"
  else
    ok "$tag is at or above the minimum operator version $min"
  fi

  if "$DRIFT_CHECK" --ref "$tag"; then
    ok "CRDs and RBAC match config/ at $tag"
  else
    bad "the module's CRDs or RBAC differ from config/ at $tag; module releases wait for the image PR of an operator release carrying main's config/"
  fi
fi

if [ "$failed" -ne 0 ]; then
  echo "release-check: the operator module $proposed is not releasable" >&2
  exit 1
fi
echo "release-check: the operator module $proposed is releasable"
