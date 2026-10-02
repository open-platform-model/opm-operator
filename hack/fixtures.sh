#!/usr/bin/env bash
# hack/fixtures.sh: one flow for the repo's published test fixtures, modules
# and catalogs.
#
# IDENTICAL COPY in cli/hack/fixtures.sh and opm-operator/hack/fixtures.sh. The
# workspace root `task fixtures:lint` fails when the two drift; edit both.
#
# A fixture is a CUE module under $FIXTURES_DIR/<name>/ (a module) or
# $CATALOGS_DIR/<name>/ (a catalog) whose identity package
# (identity/identity.cue) is the single source of its ModulePath and Version.
# Fixtures live on the testing domain (testing.opmodel.dev/modules/<repo>/*,
# testing.opmodel.dev/catalogs/<repo>/*), never under opmodel.dev/*, and are
# published through `opm module publish` or `opm catalog publish` by the root
# they sit in, so every publish gate of their kind runs over them. Catalogs go
# first, so a module fixture may depend on a catalog fixture.
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
# thing at PR time and ship another. "Changed since" is measured from the
# merge-base of BASE_REF and HEAD, so a branch that merely lags main (a
# release-please branch after a fixture bump landed) is not "changed".
#
# Subcommands
#   pins     print "<ModulePath>=<Version>" per fixture (from identity.cue)
#   check    run every publish gate (dry run) against UPSTREAM_REGISTRY and
#            enforce changed-implies-bumped against BASE_REF
#   seed     publish the tree's fixtures into the LOCAL registry CUE_REGISTRY
#            maps testing.opmodel.dev to (refuses a ghcr.io mapping)
#   publish  publish the tree's fixtures to CUE_REGISTRY (default: GHCR);
#            honours SINCE=<git-ref> and PRERELEASE=<id>
#   consumers <dir>...
#            check that each consumer (a dir holding a cue.mod that pins a
#            fixture) pins exactly what CUE resolves for the fixture versions
#            it names: in a scratch copy, `cue mod get <fixture>@<pinned>` per
#            testing.opmodel.dev pin, then `cue mod tidy`, diffed against the
#            committed module.cue. Needed because CUE keeps a dep the consumer
#            already lists at its listed version: a consumer on a new fixture
#            but a stale core passes `cue mod tidy --check` and evaluates
#            against the stale core. Also fails on a tracked cue.mod outside
#            FIXTURES_DIR and CATALOGS_DIR that pins a fixture but is not
#            listed. Resolves through CUE_REGISTRY (default: GHCR; the seeded
#            mapping in PR CI).
#            FIX=1 writes the resolved module.cue back instead of failing.
#
# Environment
#   FIXTURES_DIR       module fixture root; auto-detected (tests/fixtures/modules
#                      or test/fixtures/modules) when unset
#   CATALOGS_DIR       catalog fixture root; defaults to the catalogs/ sibling of
#                      FIXTURES_DIR when that directory exists, otherwise none
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
#   FIX                consumers: 1 rewrites a drifted module.cue in place
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

CATALOGS_DIR=${CATALOGS_DIR:-}
if [ -z "$CATALOGS_DIR" ] && [ -d "$(dirname "$FIXTURES_DIR")/catalogs" ]; then
  CATALOGS_DIR=$(dirname "$FIXTURES_DIR")/catalogs
fi
[ -z "$CATALOGS_DIR" ] || [ -d "$CATALOGS_DIR" ] || die "CATALOGS_DIR '$CATALOGS_DIR' is not a directory"

OPM_BIN=${OPM_BIN:-opm}
UPSTREAM_REGISTRY=${UPSTREAM_REGISTRY:-$GHCR_REGISTRY}
BASE_REF=${BASE_REF:-origin/main}
SINCE=${SINCE:-}
PRERELEASE=${PRERELEASE:-}
FIX=${FIX:-}

require_tools() {
  command -v cue >/dev/null || die "cue not on PATH"
  command -v "$OPM_BIN" >/dev/null || die "$OPM_BIN not on PATH (install the pinned cli release, or set OPM_BIN)"
}

# fixture_dirs: every fixture, catalogs first.
fixture_dirs() {
  if [ -n "$CATALOGS_DIR" ]; then
    find "$CATALOGS_DIR" -mindepth 1 -maxdepth 1 -type d | sort
  fi
  find "$FIXTURES_DIR" -mindepth 1 -maxdepth 1 -type d | sort
}

# kind_of <dir>: the opm artifact kind a fixture publishes as, decided by the
# root it sits in (`opm catalog ...` or `opm module ...`).
kind_of() {
  if [ -n "$CATALOGS_DIR" ] && [[ $1 == "$CATALOGS_DIR"/* ]]; then
    echo catalog
  else
    echo module
  fi
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

# changed_since <ref> <dir>: 0 when the dir differs from the merge-base of
# <ref> and HEAD (the tree as it forked from <ref>, not <ref>'s current tip).
changed_since() {
  local ref=$1 dir=$2 base
  git rev-parse -q --verify "$ref^{commit}" >/dev/null 2>&1 || die "ref '$ref' does not resolve (fetch it, or set BASE_REF/SINCE)"
  base=$(git merge-base "$ref" HEAD) || die "no merge-base between '$ref' and HEAD"
  ! git diff --quiet "$base" -- "$dir"
}

# only_already_published <output>: the publish refused for exactly one reason,
# the tag already exists. Publish itself never skips (0011:D15);
# idempotency is decided here, by the caller.
only_already_published() {
  grep -q 'already holds' <<<"$1" && grep -q '1 refusal' <<<"$1"
}

# publish_one <dir> <mode>: publish a fixture at its declared version (plus
# PRERELEASE). Prints the outcome; returns non-zero on a real failure.
publish_one() {
  local dir=$1 name kind ver tag srcdir out ok attempt delay
  name=$(basename "$dir")
  kind=$(kind_of "$dir")
  ver=$(identity "$dir" Version)
  tag="v${ver}"
  srcdir=$dir
  # A pre-release tag (PR e2e) is v<ver>-<id>: valid SemVer that sorts below the
  # eventual release cut and never collides with it. The DECLARED version has to
  # move with the tag: acquire-time identity checks (0010:D11) require
  # metadata.version to equal the fetched tag. Stage a copy and let `opm <kind>
  # version set` write it; it is offline and preserves the defaulted-disjunction
  # shape byte-for-byte.
  if [ -n "$PRERELEASE" ]; then
    tag="${tag}-${PRERELEASE}"
    srcdir=$(mktemp -d)
    cp -R "$dir/." "$srcdir/"
    if ! "$OPM_BIN" "$kind" version set "${ver}-${PRERELEASE}" "$srcdir" >/dev/null; then
      rm -rf "$srcdir"
      echo "FAIL ${name}: prerelease version set failed" >&2
      return 1
    fi
  fi
  echo "==> ${name} (${kind}): publishing ${tag}"
  # GHCR applies a secondary rate limit to rapid writes (403 "exceeded a
  # secondary rate limit"), reached in practice when the whole fleet is pushed in
  # seconds. Back off and retry rather than fail the run for a throttle.
  out=""
  ok=1
  for attempt in 1 2 3 4; do
    if out=$("$OPM_BIN" "$kind" publish "$srcdir" 2>&1); then
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
  local dir name kind tag out rc=0 found=0
  export CUE_REGISTRY=$UPSTREAM_REGISTRY
  export OPM_REGISTRY=$UPSTREAM_REGISTRY
  for dir in $(fixture_dirs); do
    found=$((found + 1))
    name=$(basename "$dir")
    kind=$(kind_of "$dir")
    tag="v$(identity "$dir" Version)"
    echo "==> ${name} (${kind}): gates at ${tag}"
    if out=$("$OPM_BIN" "$kind" publish --dry-run "$dir" 2>&1); then
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
      echo "     Published versions are immutable: bump it (opm ${kind} version set <semver> $dir)" >&2
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

# fixture_deps <module.cue>: the testing.opmodel.dev dep keys a cue.mod pins
# (dep keys only, never the `module:` line).
fixture_deps() {
  sed -n 's/^[[:space:]]*"\(testing\.opmodel\.dev\/[^"]*\)":[[:space:]]*{.*$/\1/p' "$1"
}

# dep_version <module.cue> <dep>: the v: pinned under <dep> (empty when absent).
dep_version() {
  awk -v p="\"$2\"" 'index($0, p) {f = 1} f && /v: "/ {match($0, /"[^"]+"/); print substr($0, RSTART + 1, RLENGTH - 2); exit}' "$1"
}

consumer_fail() {
  echo "FAIL $1: $2" >&2
}

cmd_consumers() {
  command -v cue >/dev/null || die "cue not on PATH"
  [ "$#" -gt 0 ] || die "consumers: name the consumer dirs (each holds a cue.mod)"
  export CUE_REGISTRY=${CUE_REGISTRY:-$GHCR_REGISTRY}
  local scratch dir mod work deps dep ver out rc=0 ok listed f n=0
  scratch=$(mktemp -d)
  # shellcheck disable=SC2064 # expand now: $scratch is local to this function
  trap "rm -rf '$scratch'" EXIT
  echo "fixture consumers against $(testing_host)"
  listed=" "
  for dir in "$@"; do
    dir=${dir%/}
    dir=${dir#./}
    n=$((n + 1))
    mod="$dir/cue.mod/module.cue"
    listed="${listed}${mod} "
    echo "==> ${dir}"
    if [ ! -f "$mod" ]; then
      consumer_fail "$dir" "no cue.mod/module.cue"
      rc=1
      continue
    fi
    work="$scratch/$n"
    mkdir -p "$work"
    cp -R "$dir/." "$work/"
    ok=1
    deps=$(fixture_deps "$mod")
    if [ -z "$deps" ]; then
      consumer_fail "$dir" "pins no testing.opmodel.dev fixture; not a consumer"
      rc=1
      continue
    fi
    for dep in $deps; do
      ver=$(dep_version "$mod" "$dep")
      if [ -z "$ver" ]; then
        consumer_fail "$dir" "no v: under \"$dep\""
        ok=0
        break
      fi
      # Same version on purpose: it forces CUE to walk the fixture's own
      # requirements and raise every shared dep to at least the fixture's pin.
      if ! out=$(cd "$work" && cue mod get "${dep%@*}@${ver}" 2>&1); then
        echo "$out" >&2
        consumer_fail "$dir" "cue mod get ${dep%@*}@${ver} failed (is that version published, or seeded into $(testing_host)?)"
        ok=0
        break
      fi
    done
    if [ "$ok" -eq 1 ] && ! out=$(cd "$work" && cue mod tidy 2>&1); then
      echo "$out" >&2
      consumer_fail "$dir" "cue mod tidy failed"
      ok=0
    fi
    if [ "$ok" -eq 0 ]; then
      rc=1
      continue
    fi
    if diff -u --label "$mod (committed)" --label "$mod (resolved)" "$mod" "$work/cue.mod/module.cue"; then
      echo "    ok"
    elif [ "$FIX" = "1" ]; then
      cp "$work/cue.mod/module.cue" "$mod"
      echo "    fixed: wrote the resolved module.cue"
    else
      consumer_fail "$dir" "module.cue differs from what CUE resolves for its fixture pins"
      echo "     Re-run the workspace root task deps:pins:fixtures, apply the diff above," >&2
      echo "     or re-run this with FIX=1 against a registry that holds the pinned fixture versions." >&2
      rc=1
    fi
  done
  # A consumer nobody listed is a consumer nobody checks.
  if git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    while IFS= read -r f; do
      case "$f" in "$FIXTURES_DIR"/*) continue ;; esac
      if [ -n "$CATALOGS_DIR" ]; then
        case "$f" in "$CATALOGS_DIR"/*) continue ;; esac
      fi
      [ -n "$(fixture_deps "$f")" ] || continue
      case "$listed" in *" $f "*) continue ;; esac
      consumer_fail "$f" "pins a testing.opmodel.dev fixture but is not a listed consumer"
      rc=1
    done < <(git ls-files -- '*cue.mod/module.cue')
  else
    echo "    (not a git work tree: skipped the unlisted-consumer check)"
  fi
  return $rc
}

case "$cmd" in
  pins) cmd_pins ;;
  check) cmd_check ;;
  seed) cmd_seed ;;
  publish) cmd_publish ;;
  consumers) cmd_consumers "$@" ;;
  -h|--help|help) usage 0 ;;
  *) die "unknown subcommand '$cmd' (pins|check|seed|publish|consumers)" ;;
esac
