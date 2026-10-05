#!/usr/bin/env bash
# test-publish.sh: offline test of hack/operator-module/publish.sh.
#
# Each case runs the script in a scratch directory holding the operator
# module's real cue.mod/module.cue at modules/opm_operator, under release.yml's
# registry mapping, with stubs for opm and crane over a file-backed fake
# registry (ref=digest lines). The opm stub stores what it publishes under the
# reference CUE resolves for the OPM_REGISTRY it is given, which is where the
# real cli pushes. The cases assert the literal GHCR and job-local references
# the module lands at, so a hand-spelled repository that drops the module
# path's opmodel.dev prefix (the opm_operator-v0.1.0 read-back failure) fails
# here. The stub derives its push location with the same `cue mod resolve` that
# publish.sh uses, so this test cannot catch a cli whose registry mapping
# differs from CUE's; the evidence that the cli pushes where CUE resolves is
# GHCR's listing of v0.1.0 and a one-off publish to a local registry, not this
# test. Needs cue; no network. Prints PASS/FAIL per case, exits 1 on any FAIL.
set -euo pipefail
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
repo=$(cd "$here/../.." && pwd)
PUBLISH=$repo/hack/operator-module/publish.sh
MODULE_CUE=$repo/modules/opm_operator/cue.mod/module.cue
# release.yml's module-publish mapping.
REGISTRY='opmodel.dev=ghcr.io/open-platform-model,registry.cue.works'
GHCR_REF=ghcr.io/open-platform-model/opmodel.dev/modules/opm_operator:v0.1.0
LOCAL_REF=localhost:5000/opmodel.dev/modules/opm_operator:v0.1.0
D1=sha256:1111111111111111111111111111111111111111111111111111111111111111
D2=sha256:2222222222222222222222222222222222222222222222222222222222222222

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
export CUE_CACHE_DIR=$TMP/cue-cache
unset CUE_REGISTRY

mkdir -p "$TMP/stubs"
# crane digest [--insecure] REF: the digest the fake registry holds for REF.
cat >"$TMP/stubs/crane" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
[ "$1" = digest ] || { echo "crane stub: unexpected $*" >&2; exit 2; }
shift
[ "$1" = --insecure ] && shift
printf 'crane digest %s\n' "$1" >>"$STUB_LOG"
d=$(awk -F= -v r="$1" '$1 == r { print $2 }' "$STUB_REGISTRY")
[ -n "$d" ] || { echo "crane stub: MANIFEST_UNKNOWN $1" >&2; exit 1; }
echo "$d"
EOF
# opm module publish DIR --version V: stores STUB_PUBLISHES (a digest, or
# empty to store nothing) under the reference CUE resolves through
# OPM_REGISTRY. STUB_LOCAL_PUBLISHES overrides it for a job-local mapping.
cat >"$TMP/stubs/opm" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
[ "$1 $2" = "module publish" ] && [ "$4" = --version ] || { echo "opm stub: unexpected $*" >&2; exit 2; }
printf 'opm OPM_REGISTRY=%s\n' "$OPM_REGISTRY" >>"$STUB_LOG"
path=$(sed -n 's/^module: *"\([^"@]*\)@v[0-9]*"$/\1/p' "$3/cue.mod/module.cue")
ref=$(CUE_REGISTRY=$OPM_REGISTRY cue mod resolve "$path@v$5")
d=$STUB_PUBLISHES
case $ref in localhost:*) d=${STUB_LOCAL_PUBLISHES-$d} ;; esac
[ -z "$d" ] || printf '%s=%s\n' "$ref" "$d" >>"$STUB_REGISTRY"
EOF
chmod +x "$TMP/stubs/crane" "$TMP/stubs/opm"
export OPM=$TMP/stubs/opm CRANE=$TMP/stubs/crane

FAILED=0
# run NAME HELD PUBLISHES LOCAL_PUBLISHES WANT_RC [MESSAGE...]: run the
# script with GHCR holding HELD (empty: nothing) at GHCR_REF; assert its exit
# and that its output and the stub log hold each MESSAGE.
run() {
  local name=$1 held=$2 pub=$3 lpub=$4 want=$5 rc=0 m d=$TMP/$1
  shift 5
  mkdir -p "$d/modules/opm_operator/cue.mod"
  cp "$MODULE_CUE" "$d/modules/opm_operator/cue.mod/module.cue"
  : >"$d/registry"
  [ -z "$held" ] || printf '%s=%s\n' "$GHCR_REF" "$held" >"$d/registry"
  (cd "$d" && GITHUB_ACTIONS=${CASE_GITHUB_ACTIONS-true} OPM_REGISTRY=$REGISTRY \
    STUB_REGISTRY=$d/registry STUB_LOG=$d/stub.log STUB_PUBLISHES=$pub STUB_LOCAL_PUBLISHES=$lpub \
    "$PUBLISH" 0.1.0) >"$d/out.log" 2>&1 || rc=$?
  touch "$d/stub.log"
  cat "$d/stub.log" >>"$d/out.log"
  if [ "$rc" != "$want" ]; then
    printf 'FAIL %s: exit %s, want %s\n' "$name" "$rc" "$want"
    sed 's/^/    /' "$d/out.log"
    FAILED=1
    return 0
  fi
  for m in "$@"; do
    if ! grep -qF -- "$m" "$d/out.log"; then
      printf 'FAIL %s: output lacks "%s"\n' "$name" "$m"
      sed 's/^/    /' "$d/out.log"
      FAILED=1
      return 0
    fi
  done
  printf 'PASS %s\n' "$name"
}

# First publish: pushes and reads the version back where CUE put it.
run first-publish "" "$D1" "" 0 "digest=$D1" "published $GHCR_REF at $D1" \
  "crane digest $GHCR_REF" "opm OPM_REGISTRY=$REGISTRY"

# Re-run or recovery over a held version that equals this tree: nothing pushed.
run reuse "$D1" "$D2" "$D1" 0 "digest=$D1" "$GHCR_REF already holds this tag's tree; nothing pushed" \
  "opm OPM_REGISTRY=opmodel.dev/modules/opm_operator=localhost:5000+insecure,$REGISTRY" \
  "crane digest $LOCAL_REF"
if grep -qF "$GHCR_REF=$D2" "$TMP/reuse/registry"; then
  printf 'FAIL reuse: pushed to GHCR\n'
  FAILED=1
fi

# A held version that differs from this tree: refused, nothing pushed.
run reuse-differs "$D1" "$D2" "$D2" 1 "$GHCR_REF holds $D1, but this tag's tree publishes $D2; release the next module version"

# The cli succeeded but the version cannot be read back.
run no-readback "" "" "" 1 "published, but cannot read $GHCR_REF back"

# Registry Policy: only CI publishes.
CASE_GITHUB_ACTIONS='' run not-ci "" "$D1" "" 2 "publishes only in release.yml's module-publish job"

exit "$FAILED"
