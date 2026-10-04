#!/usr/bin/env bash
# publish.sh <version>: publish the operator module's tagged tree to GHCR, once.
#
# Run only by release.yml's module-publish job, from the checked-out module
# release tag, after the release check. The registry mapping comes from the
# job's OPM_REGISTRY (opmodel.dev -> ghcr.io/open-platform-model).
#
# First run: GHCR holds no v<version>, so `opm module publish --version`
# pushes the tree (the flag only asserts identity.Version).
# Re-run ("Re-run failed jobs" after a later step failed): GHCR already holds
# v<version>. The cli would refuse with "already holds", so instead the tree
# is published to the job-local registry (LOCAL_REGISTRY, a services:
# container) and the two manifest digests are compared: equal is a reuse and
# succeeds without pushing; anything else fails, and the fix is the next
# module version. A probe that fails for another reason falls through to the
# cli, whose own check refuses a held version, so nothing is ever overwritten.
#
# Prints "digest=<sha256:...>" (GITHUB_OUTPUT shape) on success.
set -euo pipefail

[ $# -eq 1 ] && [[ $1 =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo "usage: $0 <X.Y.Z>" >&2; exit 2; }
v=$1
M=modules/opm_operator
MODULE_PATH=opmodel.dev/modules/opm_operator
GHCR_REPO=${GHCR_REPO:-ghcr.io/open-platform-model/modules/opm_operator}
LOCAL_REGISTRY=${LOCAL_REGISTRY:-localhost:5000}
OPM=${OPM:-opm}
CRANE=${CRANE:-crane}
: "${OPM_REGISTRY:?OPM_REGISTRY must map opmodel.dev to the release registry}"

[ -d "$M" ] || { echo "$0: no $M here; run from the repository root" >&2; exit 2; }

if held=$("$CRANE" digest "$GHCR_REPO:v$v" 2>/dev/null); then
  echo "$GHCR_REPO:v$v already exists at $held; comparing it with this tag's tree" >&2
  # Longest prefix wins: only the module itself goes to the job-local registry;
  # core and the catalogs still resolve from the release mapping.
  OPM_REGISTRY="$MODULE_PATH=$LOCAL_REGISTRY/modules/opm_operator+insecure,$OPM_REGISTRY" \
    "$OPM" module publish "$M" --version "$v" >&2
  local_digest=$("$CRANE" digest --insecure "$LOCAL_REGISTRY/modules/opm_operator:v$v")
  if [ "$local_digest" != "$held" ]; then
    echo "::error::$GHCR_REPO:v$v holds $held, but this tag's tree publishes $local_digest; release the next module version" >&2
    exit 1
  fi
  echo "$GHCR_REPO:v$v already holds this tag's tree; nothing pushed" >&2
  printf 'digest=%s\n' "$held"
  exit 0
fi

"$OPM" module publish "$M" --version "$v" >&2
pushed=$("$CRANE" digest "$GHCR_REPO:v$v") || {
  echo "::error::published, but cannot read $GHCR_REPO:v$v back" >&2
  exit 1
}
echo "published $GHCR_REPO:v$v at $pushed" >&2
printf 'digest=%s\n' "$pushed"
