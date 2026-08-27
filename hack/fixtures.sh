#!/usr/bin/env bash
# hack/fixtures.sh: one flow for the repo's published test-fixture modules.
#
# IDENTICAL COPY in cli/hack/fixtures.sh and opm-operator/hack/fixtures.sh. The
# workspace root `task fixtures:lint` fails when the two drift; edit both.
#
# A fixture is a CUE module under $FIXTURES_DIR/<name>/ whose identity package
# (identity/identity.cue) is the single source of its ModulePath and Version.
# Fixtures live on the testing domain (testing.opmodel.dev/modules/<repo>/*),
# never under opmodel.dev/*, and are published through `opm module publish`, so
# every publish gate runs over them.
#
# The same coordinate is served from two places, and only the registry mapping
# decides which one a consumer sees:
#
#   PR CI      `seed`    the working tree, published into a job-local registry
#                        (testing.opmodel.dev=localhost:5000+insecure; deps from GHCR)
#   merge      `publish` GHCR (publish-fixtures.yml), the same coordinate
#   all else            GHCR, via the canonical mapping
#
# `check` keeps the two equivalent: a fixture whose directory changed since
# BASE_REF must carry a version that GHCR does not hold yet, because published
# CUE module versions are immutable and a same-version edit would test one
# thing at PR time and ship another.
#
# Subcommands
#   pins     print "<ModulePath>=<Version>" per fixture (from identity.cue)
#   check    run every publish gate (dry run) against UPSTREAM_REGISTRY and
#            enforce changed-implies-bumped against BASE_REF
#   seed     publish the tree's fixtures into the LOCAL registry CUE_REGISTRY
#            maps testing.opmodel.dev to (refuses a ghcr.io mapping)
#   publish  publish the tree's fixtures to CUE_REGISTRY (default: GHCR);
#            honours SINCE=<git-ref> and PRERELEASE=<id>
#
# Environment
#   FIXTURES_DIR       fixture root; auto-detected (tests/fixtures/modules or
#                      test/fixtures/modules) when unset
#   OPM_BIN            opm binary (default: opm on PATH)
#   CUE_REGISTRY       target mapping for seed/publish. The script exports
#                      OPM_REGISTRY=CUE_REGISTRY as well (an inherited value is
#                      overridden): `cue eval` reads CUE_REGISTRY while opm reads
#                      only --registry > OPM_REGISTRY > ~/.opm/config.cue, and a
#                      stale OPM_REGISTRY would send opm to a different registry
#                      than the one `seed` just validated.
#   UPSTREAM_REGISTRY  the published truth `check` verifies against (default: GHCR)
#   BASE_REF           changed-since ref for `check` (default: origin/main)
#   SINCE              publish: skip fixtures unchanged since this git ref
#   PRERELEASE         publish: append a SemVer pre-release segment to the tag
#                      (e.g. e2e.gabc1234) so it never claims the release version
set -euo pipefail

GHCR_REGISTRY='testing.opmodel.dev=ghcr.io/open-platform-model,opmodel.dev=ghcr.io/open-platform-model,registry.cue.works'

usage() {
  sed -n '2,/^set -euo/p' "$0" | sed '$d' | sed 's/^# \{0,1\}//'
  exit "${1:-0}"
}

die() {
  echo "fixtures: $*" >&2
  exit 1
}

cmd=${1:-}
[ -n "$cmd" ] || usage 2
shift

FIXTURES_DIR=${FIXTURES_DIR:-}
if [ -z "$FIXTURES_DIR" ]; then
  for d in tests/fixtures/modules test/fixtures/modules; do
    if [ -d "$d" ]; then
      FIXTURES_DIR=$d
      break
    fi
  done
fi
[ -n "$FIXTURES_DIR" ] && [ -d "$FIXTURES_DIR" ] || die "no fixtures dir (set FIXTURES_DIR)"

OPM_BIN=${OPM_BIN:-opm}
UPSTREAM_REGISTRY=${UPSTREAM_REGISTRY:-$GHCR_REGISTRY}
BASE_REF=${BASE_REF:-origin/main}
SINCE=${SINCE:-}
PRERELEASE=${PRERELEASE:-}

require_tools() {
  command -v cue >/dev/null || die "cue not on PATH"
  command -v "$OPM_BIN" >/dev/null || die "$OPM_BIN not on PATH (install the pinned cli release, or set OPM_BIN)"
}

fixture_dirs() {
  find "$FIXTURES_DIR" -mindepth 1 -maxdepth 1 -type d | sort
}

# identity <dir> <field>: read ModulePath or Version from the identity package.
# `cue eval` on the import-free identity package needs no registry access.
identity() {
  local dir=$1 field=$2 out
  [ -d "$dir/identity" ] || die "$(basename "$dir"): no identity/ package; not a publishable fixture"
  out=$(cd "$dir" && cue eval ./identity --out text -e "$field") || die "$(basename "$dir"): cannot read $field from identity/"
  [ -n "$out" ] || die "$(basename "$dir"): identity package declares no concrete $field"
  printf '%s' "$out"
}

# testing_host: the host CUE_REGISTRY maps testing.opmodel.dev to (longest
# matching prefix wins in CUE, so an explicit testing.opmodel.dev entry beats a
# bare opmodel.dev one).
testing_host() {
  local entry
  entry=$(tr ',' '\n' <<<"${CUE_REGISTRY:-}" | grep -E '^testing\.opmodel\.dev=' | head -1 || true)
  [ -n "$entry" ] || entry=$(tr ',' '\n' <<<"${CUE_REGISTRY:-}" | grep -E '^opmodel\.dev=' | head -1 || true)
  printf '%s' "${entry#*=}"
}

# changed_since <ref> <dir>: 0 when the dir differs from <ref>.
changed_since() {
  local ref=$1 dir=$2
  git rev-parse -q --verify "$ref^{commit}" >/dev/null 2>&1 || die "ref '$ref' does not resolve (fetch it, or set BASE_REF/SINCE)"
  ! git diff --quiet "$ref" -- "$dir"
}

# only_already_published <output>: the publish refused for exactly one reason,
# the tag already exists. Publish itself never skips (enhancement 0011 D15);
# idempotency is decided here, by the caller.
only_already_published() {
  grep -q 'already holds' <<<"$1" && grep -q '1 refusal' <<<"$1"
}

# publish_one <dir> <mode>: publish a fixture at its declared version (plus
# PRERELEASE). Prints the outcome; returns non-zero on a real failure.
publish_one() {
  local dir=$1 name ver tag srcdir out ok attempt delay
  name=$(basename "$dir")
  ver=$(identity "$dir" Version)
  tag="v${ver}"
  srcdir=$dir
  # A pre-release tag (PR e2e) is v<ver>-<id>: valid SemVer that sorts below the
  # eventual release cut and never collides with it. The DECLARED version has to
  # move with the tag: acquire-time identity checks (enhancement 0010 D11) require
  # metadata.version to equal the fetched tag. Stage a copy and let `opm module
  # version set` write it; it is offline and preserves the defaulted-disjunction
  # shape byte-for-byte.
  if [ -n "$PRERELEASE" ]; then
    tag="${tag}-${PRERELEASE}"
    srcdir=$(mktemp -d)
    cp -R "$dir/." "$srcdir/"
    if ! "$OPM_BIN" module version set "${ver}-${PRERELEASE}" "$srcdir" >/dev/null; then
      rm -rf "$srcdir"
      echo "FAIL ${name}: prerelease version set failed" >&2
      return 1
    fi
  fi
  echo "==> ${name}: publishing ${tag}"
  # GHCR applies a secondary rate limit to rapid writes (403 "exceeded a
  # secondary rate limit"), reached in practice when the whole fleet is pushed in
  # seconds. Back off and retry rather than fail the run for a throttle.
  out=""
  ok=1
  for attempt in 1 2 3 4; do
    if out=$("$OPM_BIN" module publish "$srcdir" 2>&1); then
      ok=0
      break
    fi
    grep -qiE 'secondary rate limit|429|too many requests' <<<"$out" || break
    delay=$((attempt * 20))
    echo "    rate-limited by the registry; retrying in ${delay}s (attempt ${attempt}/4)" >&2
    sleep "$delay"
  done
  [ "$srcdir" = "$dir" ] || rm -rf "$srcdir"
  if [ "$ok" -eq 0 ]; then
    echo "$out"
    return 0
  fi
  if only_already_published "$out"; then
    echo "    ${tag} already present; nothing to do"
    return 0
  fi
  echo "$out" >&2
  echo "FAIL ${name}: publish refused" >&2
  return 1
}

publish_all() {
  local dir name rc=0
  export CUE_REGISTRY
  export OPM_REGISTRY=$CUE_REGISTRY
  for dir in $(fixture_dirs); do
    name=$(basename "$dir")
    if [ -n "$SINCE" ] && ! changed_since "$SINCE" "$dir"; then
      echo "skip ${name}: unchanged since ${SINCE}"
      continue
    fi
    publish_one "$dir" || rc=1
  done
  return $rc
}

cmd_pins() {
  local dir
  for dir in $(fixture_dirs); do
    printf '%s=%s\n' "$(identity "$dir" ModulePath)" "$(identity "$dir" Version)"
  done
}

cmd_check() {
  require_tools
  local dir name tag out rc=0 found=0
  export CUE_REGISTRY=$UPSTREAM_REGISTRY
  export OPM_REGISTRY=$UPSTREAM_REGISTRY
  for dir in $(fixture_dirs); do
    found=$((found + 1))
    name=$(basename "$dir")
    tag="v$(identity "$dir" Version)"
    echo "==> ${name}: gates at ${tag}"
    if out=$("$OPM_BIN" module publish --dry-run "$dir" 2>&1); then
      echo "$out"
      continue
    fi
    echo "$out"
    if ! only_already_published "$out"; then
      echo "FAIL ${name}: publish gates refused" >&2
      rc=1
      continue
    fi
    if changed_since "$BASE_REF" "$dir"; then
      echo "FAIL ${name}: changed since ${BASE_REF} but ${tag} is already published upstream." >&2
      echo "     Published versions are immutable: bump it (opm module version set <semver> $dir)" >&2
      echo "     so PR CI (tree) and post-merge (registry) test the same content." >&2
      rc=1
      continue
    fi
    echo "    ${tag} already published and the fixture is unchanged since ${BASE_REF}; ok"
  done
  [ "$found" -gt 0 ] || die "no fixtures under $FIXTURES_DIR"
  return $rc
}

cmd_seed() {
  require_tools
  [ -n "${CUE_REGISTRY:-}" ] || die "seed: CUE_REGISTRY must map testing.opmodel.dev to a local registry"
  local host
  host=$(testing_host)
  case "$host" in
    ''|*ghcr.io*) die "seed: CUE_REGISTRY maps testing.opmodel.dev to '${host:-nothing}'; seed only publishes to a local registry (e.g. testing.opmodel.dev=localhost:5000+insecure)" ;;
  esac
  echo "seeding ${FIXTURES_DIR} into ${host}"
  publish_all
}

cmd_publish() {
  require_tools
  CUE_REGISTRY=${CUE_REGISTRY:-$GHCR_REGISTRY}
  echo "publishing ${FIXTURES_DIR} to $(testing_host)"
  publish_all
}

case "$cmd" in
  pins) cmd_pins ;;
  check) cmd_check ;;
  seed) cmd_seed ;;
  publish) cmd_publish ;;
  -h|--help|help) usage 0 ;;
  *) die "unknown subcommand '$cmd' (pins|check|seed|publish)" ;;
esac
