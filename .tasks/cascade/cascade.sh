#!/usr/bin/env bash
# cascade.sh: task deps:cascade. Moves opm-operator's upstream pins to the newest published
# versions in the working tree only (no commit, no push, no branch), following the Phase 2
# cascade contract §5.2 and §6.3 and workspace RELEASING.md, sections "The cascade" and
# "What each repo's task moves".
#
# Exit 0: the working tree changed. Exit 3: nothing to do. Anything else: an error, after
# which the caller discards the tree. Run it as `task -x deps:cascade`, or go-task turns 3
# into 201.
#
# Three phases: A resolves every target (resolver calls only, no edit), B installs the opm
# CLI from the unmodified tree when a fixture version may be set, C edits.
set -euo pipefail
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source-path=SCRIPTDIR source=lib.sh
. "$here/lib.sh"

die() { printf 'deps:cascade: %s\n' "$1" >&2; exit "${2:-1}"; }
note() { printf 'deps:cascade: %s\n' "$1" >&2; }

# Only result() may report "nothing to do": a stray exit 3 from any other command (a
# resolver predicate outside an if, say) becomes an error.
RESULT_SET=
# shellcheck disable=SC2329 # invoked by the EXIT trap
on_exit() {
  local rc=$?
  if [ "$rc" = 3 ] && [ -z "$RESULT_SET" ]; then
    note "a step exited 3 before the result was known; reporting an error"
    exit 1
  fi
}
trap on_exit EXIT
trap 'note "failed at line $LINENO"' ERR

R="${CASCADE_RESOLVER:-}"
[ -n "$R" ] || die "CASCADE_RESOLVER is not set; run through task deps:cascade"
cd "$(git rev-parse --show-toplevel)"

LIBKEY=github.com/open-platform-model/library
CATKEY=opmodel.dev/catalogs/opm@v4
COREKEY=opmodel.dev/core@v2
CLIKEY=github.com/open-platform-model/cli
SAMPLE_PLATFORM=config/samples/opmodel.dev_v1alpha1_platform.yaml
SAMPLE_MI=config/samples/opmodel.dev_v1alpha1_moduleinstance.yaml
CATALOG_GO=test/fixtures/catalog.go
PROVIDER=test/fixtures/catalogs/provider
CUE_VERSION_FILE=.github/workflows/test.yml

# --- Rule 1: clean start, or a snapshot under CASCADE_ALLOW_DIRTY=1 -----------------------

snapshot() {
  { git status --porcelain --untracked-files=all
    git diff HEAD --binary
    git ls-files -z --others --exclude-standard | xargs -0 -r sha256sum
  } | sha256sum
}

START=
if [ "${CASCADE_ALLOW_DIRTY:-}" = 1 ]; then
  START=$(snapshot)
elif [ -n "$(git status --porcelain --untracked-files=all)" ]; then
  die "the working tree is not clean; commit or stash first, or set CASCADE_ALLOW_DIRTY=1"
fi

# --- Rule 2: state directory and warnings -------------------------------------------------

STATE="$(git rev-parse --absolute-git-dir)/cascade"
mkdir -p "$STATE"
: >"$STATE/warnings"
export CASCADE_WARNINGS="$STATE/warnings"

# warn KEY MESSAGE: the <pin-key>\t<message> format the resolver also appends.
warn() {
  printf '%s\t%s\n' "$1" "$2" >>"$CASCADE_WARNINGS"
  printf 'deps:cascade: warning: %s\n' "$2" >&2
}

# --- Rule 4: registries; never the Taskfile's localhost default ---------------------------

export CUE_REGISTRY='testing.opmodel.dev=ghcr.io/open-platform-model,opmodel.dev=ghcr.io/open-platform-model,registry.cue.works'
export OPM_REGISTRY="$CUE_REGISTRY"
export GOWORK=off

# --- Resolver calls ------------------------------------------------------------------------

# rcall VAR ARGS...: run the resolver, store its stdout in VAR and return 0 or 3. Any other
# exit ends the task with that code. Callers always use it inside an if or a case.
rcall() {
  local __var=$1 __out __rc
  shift
  if __out=$("$R" "$@"); then __rc=0; else __rc=$?; fi
  case $__rc in
    0|3) printf -v "$__var" '%s' "$__out"; return "$__rc" ;;
    *) die "resolver failed (exit $__rc): $*" "$__rc" ;;
  esac
}

# vcmp A B: set CMP to -1, 0 or 1 (SemVer precedence, from the resolver). Never called in
# a command substitution, where a failure would be lost inside a test.
CMP=
vcmp() {
  if [ "$1" = "$2" ]; then CMP=0; return 0; fi
  if rcall CMP semver-cmp "$1" "$2"; then return 0; fi
  die "semver-cmp $1 $2 answered 3"
}

# is_frozen FILE KEY: true when .cascade-frozen freezes KEY at FILE.
is_frozen() {
  local o
  if rcall o is-frozen "$1" "$2" --repo-root .; then return 0; fi
  return 1
}

# expect_for KEY: the version CASCADE_EXPECT names for KEY, if any.
expect_for() {
  local kv items
  read -ra items <<<"${CASCADE_EXPECT:-}"
  for kv in "${items[@]}"; do
    if [ "${kv%%=*}" = "$1" ]; then printf '%s' "${kv#*=}"; return 0; fi
  done
  return 0
}

# resolve VAR KIND COORD CURRENT KEY: VAR gets the target when the pin should move, or "".
resolve() {
  local var=$1 kind=$2 coord=$3 cur=$4 key=$5 exp args=()
  [ -z "$coord" ] || args+=("$coord")
  args+=(--current "$cur" --repo-root .)
  exp=$(expect_for "$key")
  [ -z "$exp" ] || args+=(--expect "$exp")
  if rcall "$var" newest "$kind" "${args[@]}"; then
    valid_v "${!var}" || die "newest $kind $coord printed '${!var}', not a version"
    return 0
  fi
  printf -v "$var" '%s' ""
}

# core_for C: set CORE_OF to the core catalog C pins (pin-of, cached). A catalog with no
# core row is an error, never "nothing to do".
declare -A PIN_OF=()
CORE_OF=
core_for() {
  if [ -z "${PIN_OF[$1]:-}" ]; then
    local o
    if rcall o pin-of "$CATKEY" "$1" "$COREKEY"; then
      valid_v "$o" || die "pin-of $CATKEY $1 printed '$o', not a version"
      PIN_OF[$1]=$o
    else
      die "catalog $1 pins no $COREKEY"
    fi
  fi
  CORE_OF=${PIN_OF[$1]}
}

# --- Text edits ----------------------------------------------------------------------------

# replace_file FILE: move FILE.tmp over FILE, refusing when nothing changed.
replace_file() {
  if cmp -s "$1.tmp" "$1"; then rm -f "$1.tmp"; die "$1: edit changed nothing"; fi
  mv "$1.tmp" "$1"
}

# set_cue_dep_v FILE KEY V: rewrite the v: inside the "KEY": { ... } block.
set_cue_dep_v() {
  awk -v k="\"$2\": {" -v v="$3" '
    index($0, k) { f = 1 }
    f && /^[[:space:]]*v:/ { sub(/"[^"]+"/, "\"" v "\""); f = 0 }
    { print }' "$1" >"$1.tmp"
  replace_file "$1"
}

# set_yaml_version_after FILE ANCHOR V: rewrite the first version: after the ANCHOR line,
# keeping its quoting.
set_yaml_version_after() {
  awk -v a="$2" -v v="$3" '
    index($0, a) && !d { f = 1 }
    f && /^[[:space:]]*version:/ {
      if ($0 ~ /"/) sub(/version:.*/, "version: \"" v "\""); else sub(/version:.*/, "version: " v)
      f = 0; d = 1
    }
    { print }' "$1" >"$1.tmp"
  replace_file "$1"
}

# set_catalog_go OLD NEW: rewrite the return literal inside CatalogVersion().
set_catalog_go() {
  awk -v o="return \"$1\"" -v n="return \"$2\"" '
    /^func CatalogVersion\(\)/ { f = 1 }
    f && index($0, o) { sub(o, n); f = 0 }
    f && /^}/ { f = 0 }
    { print }' "$CATALOG_GO" >"$CATALOG_GO.tmp"
  replace_file "$CATALOG_GO"
  [ -z "$(gofmt -l "$CATALOG_GO")" ] || die "$CATALOG_GO is not gofmt-clean after the edit"
}

# cue_module_path DIR: the module: of DIR/cue.mod/module.cue.
cue_module_path() {
  awk '/^module:/ { if (match($0, /"[^"]+"/)) print substr($0, RSTART + 1, RLENGTH - 2); exit }' \
    "$1/cue.mod/module.cue"
}

# Non-OPM deps of a module.cue as "<key> <v>" lines, for the third-party check.
cue_third_party() {
  awk '
    match($0, /^[[:space:]]*"[^"]+": \{/) { k = $0; sub(/^[[:space:]]*"/, "", k); sub(/".*/, "", k) }
    k != "" && /^[[:space:]]*v:/ { if (match($0, /"[^"]+"/)) print k, substr($0, RSTART + 1, RLENGTH - 2); k = "" }' "$1" |
    awk '$1 !~ /^(opmodel\.dev|testing\.opmodel\.dev)\//'
}

# go.mod requirements as "<module> <version>" lines, for the third-party check.
go_requires() {
  awk '
    /^require \(/ { f = 1; next }
    f && /^\)/ { f = 0; next }
    f && NF >= 2 { print $1, $2 }
    /^require [^(]/ { print $2, $3 }' go.mod | sort
}

# f_changed M FDIR IFILE: exit 0 when FDIR differs from M in any path other than
# IFILE, or when IFILE differs from M outside its ^Version: line; exit 1 otherwise.
f_changed() {
  local m="$1" d="$2" i="$3" p
  while IFS= read -r p; do
    [ "$p" = "$i" ] || return 0
  done < <({ git diff --name-only "$m" -- "$d"
             git ls-files --others --exclude-standard -- "$d"; } | sort -u)
  git cat-file -e "$m:$i" 2>/dev/null || return 0
  if diff -q <(git show "$m:$i" | grep -v '^Version:') \
             <(grep -v '^Version:' "$i") >/dev/null; then
    return 1
  fi
  return 0
}

# ===========================================================================================
# Rule 3: the steering files first.
o=
if rcall o check-files --repo-root .; then :; else die "check-files answered 3"; fi

# ===========================================================================================
# Phase A: resolve. No file is edited until every target is known.

LIB_NOW=$(go_require_v "$LIBKEY" <go.mod)
valid_v "$LIB_NOW" || die "go.mod: no version for $LIBKEY"
cat_bare=$(yaml_version_after "$CATKEY:" <"$SAMPLE_PLATFORM")
CAT_NOW="v$cat_bare"
valid_v "$CAT_NOW" || die "$SAMPLE_PLATFORM: no catalog version after $CATKEY"
CLI_NOW=$(tr -d '[:space:]' <.opm-cli-version)
valid_v "$CLI_NOW" || die ".opm-cli-version: '$CLI_NOW' is not a version"

LIB='' CAT='' CLI=''
resolve LIB go "$LIBKEY" "$LIB_NOW" "$LIBKEY"
resolve CAT cue "$CATKEY" "$CAT_NOW" "$CATKEY"
K=${CAT:-$CAT_NOW}

HOLD=
if rcall HOLD hold "$COREKEY" --repo-root .; then
  valid_v "$HOLD" || die "hold $COREKEY printed '$HOLD', not a version"
else
  HOLD=
fi

# Rule 7, contract §9.11: a hold on core below the core K pins holds the catalog too.
core_for "$K"
CMP=0
if [ -n "$CAT" ] && [ -n "$HOLD" ]; then vcmp "$CORE_OF" "$HOLD"; fi
if [ "$CMP" = 1 ]; then
  warn "$CATKEY" "catalog \`$K\` needs core \`$CORE_OF\`, above the hold \`$HOLD\`; catalog held too"
  CAT=
  K=$CAT_NOW
  core_for "$K"
fi

resolve CLI opm-cli "" "$CLI_NOW" "$CLIKEY"

M=$(git merge-base "${CASCADE_BASE:-origin/main}" HEAD)

# The plan. Each array maps a file or directory to what phase C writes there.
declare -A GETS=()       # cue module dir -> "path@v path@v" for cue mod get
declare -A FROZEN=()     # cue module dir -> frozen OPM keys, byte-checked after tidy
declare -A FINAL_CAT=()  # cue module dir -> its catalog after the run ("" if none)
declare -A FINAL_CORE=() # cue module dir -> its core after the run
declare -A ADV=()        # advance dir -> target bare version
T=$'\t'
TEXT_EDITS=()            # "kind<TAB>file<TAB>anchor<TAB>value[<TAB>old]"
CORE_TARGETS=()          # core versions some file moves to (rule 10)
CAT_MOVES=              # set when any file moves its catalog (rule 10)

# Text pins of the catalog: the sample Platform and CatalogVersion(), both bare.
vcmp "$K" "$CAT_NOW"
if [ "$CMP" = 1 ] && ! is_frozen "$SAMPLE_PLATFORM" "$CATKEY"; then
  TEXT_EDITS+=("yamlver${T}$SAMPLE_PLATFORM${T}$CATKEY:${T}${K#v}")
  CAT_MOVES=1
fi
go_bare=$(awk '/^func CatalogVersion\(\)/ { f = 1 } f && /return "/ { if (match($0, /"[^"]+"/)) print substr($0, RSTART + 1, RLENGTH - 2); exit }' "$CATALOG_GO")
valid_v "v$go_bare" || die "$CATALOG_GO: no version literal in CatalogVersion()"
vcmp "$K" "v$go_bare"
if [ "$CMP" = 1 ] && ! is_frozen "$CATALOG_GO" "$CATKEY"; then
  TEXT_EDITS+=("catalogo${T}$CATALOG_GO${T}-${T}${K#v}${T}$go_bare")
  CAT_MOVES=1
fi

# plan_cue_module DIR: catalog to K where below it, core to what the file's catalog pins.
plan_cue_module() {
  local d=$1 mf=$1/cue.mod/module.cue cat core t ct C capped="" gets="" frz=""
  cat=$(cue_dep_v "$CATKEY" <"$mf")
  core=$(cue_dep_v "$COREKEY" <"$mf")
  [ -n "$core" ] || die "$mf: no $COREKEY dep"
  t=$cat
  if [ -n "$cat" ]; then
    vcmp "$K" "$cat"
    [ "$CMP" != 1 ] || t=$K
  fi
  C=${t:-$K}
  core_for "$C"
  CMP=0
  [ -z "$HOLD" ] || vcmp "$CORE_OF" "$HOLD"
  if [ "$CMP" = 1 ]; then
    if [ -n "$cat" ] && [ "$t" != "$cat" ]; then
      warn "$CATKEY" "catalog \`$t\` needs core \`$CORE_OF\`, above the hold \`$HOLD\`; catalog held too (\`$mf\`)"
      t=$cat
      C=$cat
      core_for "$C"
    fi
    vcmp "$CORE_OF" "$HOLD"
    if [ "$CMP" = 1 ]; then CORE_OF=$HOLD; capped=1; fi
  fi
  ct=$core
  vcmp "$CORE_OF" "$core"
  case $CMP in
    1) ct=$CORE_OF ;;
    -1) [ -n "$capped" ] || warn "$COREKEY" "core \`$core\` is ahead of the core \`$CORE_OF\` that catalog \`$C\` pins (\`$mf\`)" ;;
  esac
  if [ "$t" != "$cat" ] || [ "$ct" != "$core" ]; then
    # Rule 8: a frozen key is left out of the get and byte-checked after tidy.
    if [ -n "$cat" ]; then
      if is_frozen "$mf" "$CATKEY"; then frz+=" $CATKEY"; t=$cat; fi
    fi
    if is_frozen "$mf" "$COREKEY"; then frz+=" $COREKEY"; ct=$core; fi
    # cue mod get names the module path without its @vN, at the exact version.
    [ "$t" = "$cat" ] || { gets+=" ${CATKEY%@*}@$t"; CAT_MOVES=1; }
    [ "$ct" = "$core" ] || { gets+=" ${COREKEY%@*}@$ct"; CORE_TARGETS+=("$ct"); }
  fi
  [ -z "$gets" ] || GETS[$d]=${gets# }
  [ -z "$frz" ] || FROZEN[$d]=${frz# }
  FINAL_CAT[$d]=$t
  FINAL_CORE[$d]=$ct
}

MODULE_DIRS=()
for d in test/fixtures/modules/*/; do
  d=${d%/}
  [ -f "$d/identity/identity.cue" ] && [ -f "$d/cue.mod/module.cue" ] || continue
  MODULE_DIRS+=("$d")
done
[ "${#MODULE_DIRS[@]}" -gt 0 ] || die "no fixture modules under test/fixtures/modules"
for d in "${MODULE_DIRS[@]}" "$PROVIDER"; do
  plan_cue_module "$d"
done

# Rule 10: an upstream language.version newer than the local CUE_VERSION is a warning.
language_check() { # KEY MODULE@vN VERSION
  local lang
  if rcall lang language-of "$2" "$3"; then
    if [ -z "$CUE_LOCAL" ]; then
      warn - "cannot read \`CUE_VERSION\` from \`$CUE_VERSION_FILE\`; \`language.version\` of \`$2\` \`$3\` not checked"
    else
      vcmp "$lang" "$CUE_LOCAL"
      [ "$CMP" != 1 ] || warn "$1" "\`$2\` \`$3\` declares CUE language \`$lang\`, newer than the local \`CUE_VERSION\` \`$CUE_LOCAL\` (\`$CUE_VERSION_FILE\`)"
    fi
  fi
}
if [ -n "$CAT_MOVES" ] || [ "${#CORE_TARGETS[@]}" -gt 0 ]; then
  if CUE_LOCAL=$(grep -oP "CUE_VERSION: '\K[^']+" "$CUE_VERSION_FILE" | head -n1) && valid_v "$CUE_LOCAL"; then :; else CUE_LOCAL=; fi
  [ -z "$CAT_MOVES" ] || language_check "$CATKEY" "$CATKEY" "$K"
  if [ "${#CORE_TARGETS[@]}" -gt 0 ]; then
    while IFS= read -r v; do language_check "$COREKEY" "$COREKEY" "$v"; done \
      < <(printf '%s\n' "${CORE_TARGETS[@]}" | sort -u)
  fi
fi

# Rule 11: version advance once per PR, decided now against the merge-base.
SETTER_NEEDED=
for d in "${MODULE_DIRS[@]}" "$PROVIDER"; do
  i=$d/identity/identity.cue
  cur=$(identity_version <"$i")
  [ -n "$cur" ] || die "$i: no Version"
  if ! git cat-file -e "$M:$i" 2>/dev/null; then
    ADV[$d]=$cur # new since the merge-base: its own version is the pending one
    continue
  fi
  B=$(git show "$M:$i" | identity_version)
  [ -n "$B" ] || die "$i at $M: no Version"
  target=$B
  if [ -n "${GETS[$d]:-}" ] || f_changed "$M" "$d" "$i"; then
    modpath=$(cue_module_path "$d")
    if rcall o published cue "$modpath" "v$B"; then
      if rcall o next-patch "v$B"; then target=${o#v}; else die "next-patch v$B answered 3"; fi
    fi
  fi
  ADV[$d]=$target
  [ "$target" = "$cur" ] || SETTER_NEEDED=1
done

# Consumers follow each fixture module in the same PR (contract §6.3 step 4, §9.6).
# plan_text KIND FILE ANCHOR NEW CUR KEY: plan one edit when it changes something and the
# file is not frozen for KEY.
plan_text() {
  [ "$4" != "$5" ] || return 0
  if is_frozen "$2" "$6"; then return 0; fi
  TEXT_EDITS+=("$1${T}$2${T}$3${T}$4")
}
for d in "${MODULE_DIRS[@]}"; do
  name=$(basename "$d")
  modpath=$(cue_module_path "$d")
  ver="v${ADV[$d]}"
  mp=test/fixtures/modulepackages/$name/cue.mod/module.cue
  if [ -f "$mp" ] && grep -qF "\"$modpath\": {" "$mp"; then
    plan_text cuedep "$mp" "$modpath" "$ver" "$(cue_dep_v "$modpath" <"$mp")" "$modpath"
    for key in "$CATKEY" "$COREKEY"; do
      have=$(cue_dep_v "$key" <"$mp")
      [ -n "$have" ] || continue
      if [ "$key" = "$CATKEY" ]; then want=${FINAL_CAT[$d]}; else want=${FINAL_CORE[$d]}; fi
      [ -n "$want" ] || continue
      # Catalog and core in a consumer only move up: a higher pin there is what CUE
      # resolves anyway (MVS), and pins never move backwards.
      vcmp "$want" "$have"
      [ "$CMP" = 1 ] || continue
      plan_text cuedep "$mp" "$key" "$want" "$have" "$key"
    done
  fi
  for f in "$d/moduleinstance.yaml" "$SAMPLE_MI"; do
    if [ ! -f "$f" ] || ! grep -q "path: $modpath\$" "$f"; then continue; fi
    plan_text yamlver "$f" "path: $modpath" "$ver" \
      "$(yaml_version_after "path: $modpath" <"$f")" "$modpath"
  done
done

# ===========================================================================================
# Phase B: the opm CLI for the version setters, from the unmodified .opm-cli-version.

OPM=
if [ -n "$SETTER_NEEDED" ]; then
  OPM_DIR="$STATE/bin/opm-$CLI_NOW"
  OPM="$OPM_DIR/opm"
  if [ ! -x "$OPM" ]; then
    note "installing the opm CLI $CLI_NOW into $OPM_DIR"
    GOBIN="$OPM_DIR" go install "github.com/open-platform-model/cli/cmd/opm@$CLI_NOW"
  fi
fi

# ===========================================================================================
# Phase C: edit, in rule 12 order. A failure here may leave a partly edited tree.

# 1. library (shipped).
if [ -n "$LIB" ]; then
  before=$(go_requires)
  go get "$LIBKEY@$LIB"
  go mod tidy
  while read -r mod old new; do
    [ "$mod" = "$LIBKEY" ] || [ "$old" = "$new" ] ||
      warn "$LIBKEY" "\`go mod tidy\` moved \`$mod\` from \`$old\` to \`$new\`"
  done < <(join <(printf '%s\n' "$before") <(go_requires))
fi

# 2. The catalog in the sample Platform and CatalogVersion() (test).
SAMPLES_EDITED=
apply_text() { # KIND FILE ANCHOR VALUE [OLD]
  case $1 in
    yamlver) set_yaml_version_after "$2" "$3" "$4" ;;
    cuedep) set_cue_dep_v "$2" "$3" "$4" ;;
    catalogo) set_catalog_go "$5" "$4" ;;
    *) die "unknown edit kind $1" ;;
  esac
  case $2 in config/samples/*) SAMPLES_EDITED=1 ;; esac
}
consumer_edits=()
for e in "${TEXT_EDITS[@]}"; do
  IFS=$'\t' read -r kind file anchor value old <<<"$e"
  if [ "$file" = "$SAMPLE_PLATFORM" ] || [ "$file" = "$CATALOG_GO" ]; then
    apply_text "$kind" "$file" "$anchor" "$value" "$old"
  else
    consumer_edits+=("$e")
  fi
done

# 3. Fixture modules and the provider catalog: cue mod get with exact versions, then tidy.
for d in "${MODULE_DIRS[@]}" "$PROVIDER"; do
  [ -n "${GETS[$d]:-}" ] || continue
  mf=$d/cue.mod/module.cue
  declare -A pinned=()
  read -ra frozen_keys <<<"${FROZEN[$d]:-}"
  for key in "${frozen_keys[@]}"; do pinned[$key]=$(cue_dep_v "$key" <"$mf"); done
  third_before=$(cue_third_party "$mf")
  read -ra get_args <<<"${GETS[$d]}"
  (cd "$d" && cue mod get "${get_args[@]}" && cue mod tidy)
  for key in "${frozen_keys[@]}"; do
    [ "$(cue_dep_v "$key" <"$mf")" = "${pinned[$key]}" ] ||
      die "$mf: cue mod tidy moved the frozen $key; freeze the whole module, or hold the upstream"
  done
  unset pinned
  while read -r key old new; do
    [ "$old" = "$new" ] || warn - "\`cue mod tidy\` moved \`$key\` from \`$old\` to \`$new\` in \`$mf\`"
  done < <(join <(printf '%s\n' "$third_before" | sort) <(cue_third_party "$mf" | sort))
done

# 4. Version advances, with the opm CLI's own setters.
for d in "${MODULE_DIRS[@]}" "$PROVIDER"; do
  cur=$(identity_version <"$d/identity/identity.cue")
  [ "${ADV[$d]}" != "$cur" ] || continue
  [ -n "$OPM" ] || die "internal: $d needs a version set but the opm CLI was not installed"
  if [ "$d" = "$PROVIDER" ]; then
    "$OPM" catalog version set "${ADV[$d]}" "$d"
  else
    "$OPM" module version set "${ADV[$d]}" "$d"
  fi
done

# 5. Consumers: modulepackages, moduleinstance.yaml files and the sample ModuleInstance.
for e in "${consumer_edits[@]}"; do
  IFS=$'\t' read -r kind file anchor value old <<<"$e"
  apply_text "$kind" "$file" "$anchor" "$value" "$old"
done

# 6. Regenerators: the hack/crdref block of the resource reference follows config/samples.
if [ -n "$SAMPLES_EDITED" ]; then
  if go run ./hack/crdref; then :; else
    warn - "\`hack/crdref\` failed; regenerate \`docs/site/reference/operator-resources.md\` by hand (\`task dev:docs:reference\`)"
  fi
fi

# 7. The opm CLI pin, last.
if [ -n "$CLI" ]; then
  printf '%s\n' "$CLI" >.opm-cli-version
fi

# ===========================================================================================
# Rule 13: the result.
changed=
if [ "${CASCADE_ALLOW_DIRTY:-}" = 1 ]; then
  [ "$(snapshot)" = "$START" ] || changed=1
else
  [ -z "$(git status --porcelain --untracked-files=all)" ] || changed=1
fi
RESULT_SET=1
if [ -n "$changed" ]; then exit 0; fi
exit 3
