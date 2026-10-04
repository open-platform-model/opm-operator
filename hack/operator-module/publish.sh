#!/usr/bin/env bash
# publish.sh <version>: publish the operator module's tagged tree to GHCR, once.
#
# Run only by release.yml's module-publish job, against the checked-out module
# release tag, after the release check: the tag's copy on a release, main's on
# the recovery dispatch (workflow_dispatch module_tag). The registry mapping
# comes from the job's OPM_REGISTRY (opmodel.dev -> ghcr.io/open-platform-model).
#
# Repositories are never spelled out here: `cue mod resolve` maps the module
# path (from cue.mod/module.cue) through the same registry configuration opm
# publishes with. CUE keeps the full module path under the mapped prefix, so
# opmodel.dev/modules/opm_operator lands at
# ghcr.io/open-platform-model/opmodel.dev/modules/opm_operator.
#
# First run: GHCR holds no v<version>, so `opm module publish --version`
# pushes the tree (the flag only asserts identity.Version).
# Re-run ("Re-run failed jobs" after a later step failed, or the recovery
# dispatch): GHCR already holds v<version>. The cli would refuse with
# "already holds", so instead the tree
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
LOCAL_REGISTRY=${LOCAL_REGISTRY:-localhost:5000}
OPM=${OPM:-opm}
CRANE=${CRANE:-crane}
CUE=${CUE:-cue}
: "${OPM_REGISTRY:?OPM_REGISTRY must map opmodel.dev to the release registry}"

# Registry Policy: only CI publishes.
[ "${GITHUB_ACTIONS:-}" = true ] || { echo "$0: publishes only in release.yml's module-publish job" >&2; exit 2; }
[ -d "$M" ] || { echo "$0: no $M here; run from the repository root" >&2; exit 2; }

MODULE_PATH=$(sed -n 's/^module: *"\([^"@]*\)@v[0-9][0-9]*"$/\1/p' "$M/cue.mod/module.cue")
[ -n "$MODULE_PATH" ] || { echo "$0: cannot read the module path from $M/cue.mod/module.cue" >&2; exit 2; }

# ref REGISTRY_CONFIG: the OCI reference REGISTRY_CONFIG gives MODULE_PATH@v<v>.
ref() { CUE_REGISTRY=$1 "$CUE" mod resolve "$MODULE_PATH@v$v"; }

# Longest prefix wins: only the module itself goes to the job-local registry;
# core and the catalogs still resolve from the release mapping.
LOCAL_MAPPING="$MODULE_PATH=$LOCAL_REGISTRY+insecure,$OPM_REGISTRY"
held_ref=$(ref "$OPM_REGISTRY") || { echo "$0: cannot resolve $MODULE_PATH@v$v through OPM_REGISTRY" >&2; exit 2; }
local_ref=$(ref "$LOCAL_MAPPING") || { echo "$0: cannot resolve $MODULE_PATH@v$v through the job-local mapping" >&2; exit 2; }

if held=$("$CRANE" digest "$held_ref" 2>/dev/null); then
  echo "$held_ref already exists at $held; comparing it with this tag's tree" >&2
  OPM_REGISTRY=$LOCAL_MAPPING "$OPM" module publish "$M" --version "$v" >&2
  local_digest=$("$CRANE" digest --insecure "$local_ref") || {
    echo "::error::published this tag's tree to the job-local registry, but cannot read $local_ref back" >&2
    exit 1
  }
  if [ "$local_digest" != "$held" ]; then
    echo "::error::$held_ref holds $held, but this tag's tree publishes $local_digest; release the next module version" >&2
    exit 1
  fi
  echo "$held_ref already holds this tag's tree; nothing pushed" >&2
  printf 'digest=%s\n' "$held"
  exit 0
fi

"$OPM" module publish "$M" --version "$v" >&2
pushed=$("$CRANE" digest "$held_ref") || {
  echo "::error::published, but cannot read $held_ref back" >&2
  exit 1
}
echo "published $held_ref at $pushed" >&2
printf 'digest=%s\n' "$pushed"
