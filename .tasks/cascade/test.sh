#!/usr/bin/env bash
# test.sh: task deps:cascade:test. Runs task deps:cascade in throwaway copies of this tree
# against the contract's stub resolver (Phase 2 cascade contract §7, §8). Prints
# PASS <scenario> or FAIL <scenario>: <reason>; exits 0 when every scenario passes, 1
# otherwise. Nothing touches the real checkout.
#
# CASCADE_TEST_SET=offline runs the pre-checks and S1, S3, S3b, S6 (no GHCR or proxy
# access beyond a warm Go module cache). CASCADE_TEST_SET=all (the default) adds S2 and S4,
# and S5 when CASCADE_RESOLVER_REAL names the real resolver.
set -euo pipefail
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
repo=$(git -C "$here" rev-parse --show-toplevel)
STUB="$here/testdata/stub-resolve.sh"
STUB_SHA256=970130f7d55c07f5b86d4f5b6f392330427ff923eb34f93553656bcd4b893d9c
OLDER="$here/testdata/older.tsv"
S1_CALLS="$here/testdata/s1-calls.txt"

LIBKEY=github.com/open-platform-model/library
CATKEY=opmodel.dev/catalogs/opm@v4
COREKEY=opmodel.dev/core@v2
CLIKEY=github.com/open-platform-model/cli
SAMPLE_PLATFORM=config/samples/opmodel.dev_v1alpha1_platform.yaml
CATALOG_GO=test/fixtures/catalog.go

# The version-advance paths S2's first run leaves changed against the original tree:
# every fixture advances once, and its consumers follow (design.md, "The S2 golden list").
GOLDEN="config/samples/opmodel.dev_v1alpha1_moduleinstance.yaml
test/fixtures/catalogs/provider/identity/identity.cue
test/fixtures/modulepackages/hello/cue.mod/module.cue
test/fixtures/modulepackages/hello_web/cue.mod/module.cue
test/fixtures/modulepackages/podinfo/cue.mod/module.cue
test/fixtures/modulepackages/redis/cue.mod/module.cue
test/fixtures/modules/hello/identity/identity.cue
test/fixtures/modules/hello/moduleinstance.yaml
test/fixtures/modules/hello_web/identity/identity.cue
test/fixtures/modules/hello_web/moduleinstance.yaml
test/fixtures/modules/podinfo/identity/identity.cue
test/fixtures/modules/podinfo/moduleinstance.yaml
test/fixtures/modules/redis/identity/identity.cue
test/fixtures/modules/redis/moduleinstance.yaml"

# S4 freezes one moved module file for every OPM key it pins (design.md, "The S4 frozen choice").
S4_FILE=test/fixtures/modules/hello/cue.mod/module.cue

SET=${CASCADE_TEST_SET:-all}
case $SET in offline|all) ;; *) echo "CASCADE_TEST_SET must be offline or all, not '$SET'" >&2; exit 1 ;; esac

# Nothing from the caller's environment steers the runs.
unset CASCADE_RESOLVER CASCADE_ALLOW_DIRTY CASCADE_EXPECT CASCADE_WARNINGS CASCADE_BASE \
  CASCADE_NOTES_FILE CASCADE_SOURCE CASCADE_TAGS CASCADE_STUB_TABLE CASCADE_STUB_LOG \
  GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE
# A CI runner has no git identity; a developer may sign commits.
export GIT_AUTHOR_NAME=cascade-test GIT_AUTHOR_EMAIL=cascade-test@invalid \
  GIT_COMMITTER_NAME=cascade-test GIT_COMMITTER_EMAIL=cascade-test@invalid
export CASCADE_TODAY=2026-10-03

TMP=$(mktemp -d)
# The test's verdict is the exit status; a cleanup hiccup never replaces it.
# shellcheck disable=SC2329 # invoked by the EXIT trap
cleanup() {
  local rc=$?
  chmod -R u+w "$TMP" 2>/dev/null || printf 'warning: chmod of %s failed\n' "$TMP" >&2
  rm -rf "$TMP" || printf 'warning: could not remove %s\n' "$TMP" >&2
  exit "$rc"
}
trap cleanup EXIT

FAILED=0
pass() { printf 'PASS %s\n' "$1"; }
fail() {
  printf 'FAIL %s: %s\n' "$1" "$2"
  FAILED=1
  if [ -n "${3:-}" ] && [ -f "$3" ]; then sed 's/^/    /' "$3" | tail -n 25; fi
}

gitc() { git -c commit.gpgsign=false "$@"; }

# sandbox NAME: copy the tree into $TMP/NAME/r, commit it as base, print the path.
sandbox() {
  local d="$TMP/$1/r"
  mkdir -p "$d"
  (cd "$repo" && git ls-files -z --cached --others --exclude-standard | tar --null -T - -cf -) |
    tar -xf - -C "$d"
  (cd "$d" && git init -q && git add -A && gitc commit -q -m base)
  printf '%s\n' "$d"
}

# commit_setup DIR: commit the scenario's setup edits; print the setup SHA.
commit_setup() {
  (cd "$1" && git add -A && gitc commit -q --allow-empty -m setup && git rev-parse HEAD)
}

# run_task DIR BASE TABLE LOG: task -x deps:cascade in DIR against the stub; prints the exit.
run_task() {
  local rc=0
  (cd "$1" && CASCADE_RESOLVER="$STUB" CASCADE_BASE="$2" CASCADE_STUB_TABLE="$3" \
    CASCADE_STUB_LOG="$4.calls" task -x deps:cascade) >"$4" 2>&1 || rc=$?
  printf '%s\n' "$rc"
}

stub_cmp() { CASCADE_STUB_TABLE=/dev/null "$STUB" semver-cmp "$1" "$2"; }

# set_dep_v FILE KEY V: set the v: of "KEY": { ... } in a cue.mod/module.cue, if present.
set_dep_v() {
  grep -qF "\"$2\": {" "$1" || return 0
  awk -v k="\"$2\": {" -v v="$3" '
    index($0, k) { f = 1 }
    f && /^[[:space:]]*v:/ { sub(/"[^"]+"/, "\"" v "\""); f = 0 }
    { print }' "$1" >"$1.tmp" && mv "$1.tmp" "$1"
}

# --- Pre-checks ------------------------------------------------------------------------------

if yq --version 2>/dev/null | grep -q mikefarah; then
  pass "pre: mikefarah yq"
else
  fail "pre: mikefarah yq" "the stub needs mikefarah yq v4 on PATH"
fi

sum=$(sha256sum "$STUB" | awk '{print $1}')
if [ "$sum" = "$STUB_SHA256" ]; then
  pass "pre: stub checksum"
else
  fail "pre: stub checksum" "$STUB has sha256 $sum, not the contract's $STUB_SHA256"
fi

PRE=$(sandbox pre)
(cd "$PRE" && .tasks/cascade/pins.sh WORKTREE) >"$TMP/pins.worktree"
(cd "$PRE" && .tasks/cascade/pins.sh HEAD) >"$TMP/pins.head"
if [ -s "$TMP/pins.worktree" ] && cmp -s "$TMP/pins.worktree" "$TMP/pins.head"; then
  pass "pre: pins.sh WORKTREE equals HEAD"
else
  fail "pre: pins.sh WORKTREE equals HEAD" "the two reports differ or are empty" "$TMP/pins.head"
fi

# --- Stub tables, built from the unmodified tree ------------------------------------------------

tree_v() { awk -F'\t' -v k="$1" '$1 == k { print $4 }' "$TMP/pins.worktree"; }
older_v() { awk -F'\t' -v k="$1" '$1 == k { print $2 }' "$OLDER"; }
LIB_T=$(tree_v "$LIBKEY")
CAT_T=$(tree_v "$CATKEY")
CORE_T=$(tree_v "$COREKEY")
CLI_T=$(tree_v "$CLIKEY")

CURRENT="$TMP/current.tsv"
{
  printf 'newest\tgo\t%s\t%s\n' "$LIBKEY" "$LIB_T"
  printf 'newest\tcue\t%s\t%s\n' "$CATKEY" "$CAT_T"
  printf 'newest\tcue\t%s\t%s\n' "$COREKEY" "$CORE_T"
  printf 'newest\topm-cli\t-\t%s\n' "$CLI_T"
  printf 'pin-of\t%s\t%s\t%s\t%s\n' "$CATKEY" "$CAT_T" "$COREKEY" "$CORE_T"
  for d in "$PRE"/test/fixtures/modules/*/ "$PRE/test/fixtures/catalogs/provider/"; do
    [ -f "$d/identity/identity.cue" ] || continue
    m=$(awk -F'"' '/^module:/ { print $2; exit }' "$d/cue.mod/module.cue")
    v=$(awk -F'"' '/^Version:/ { print $2; exit }' "$d/identity/identity.cue")
    printf 'published\tcue\t%s\tv%s\n' "$m" "$v"
  done
} >"$CURRENT"

older_ok=1
for key in "$LIBKEY" "$CATKEY" "$COREKEY" "$CLIKEY"; do
  o=$(older_v "$key")
  t=$(tree_v "$key")
  if [ -z "$o" ] || [ -z "$t" ] || [ "$(stub_cmp "$o" "$t")" != -1 ]; then
    fail "pre: older.tsv" "\`older.tsv\` \`$key\` \`$o\` is not older than the tree's \`$t\`; pick an older published version"
    older_ok=
  fi
done
[ -z "$older_ok" ] || pass "pre: older.tsv older than the tree"
OLDER_TABLE="$TMP/older-table.tsv"
{ cat "$CURRENT"; awk -F'\t' '$1 == "pin-of"' "$OLDER"; } >"$OLDER_TABLE"

# --- S1: nothing to do ------------------------------------------------------------------------

D=$(sandbox s1)
base=$(cd "$D" && git rev-parse HEAD)
rc=$(run_task "$D" "$base" "$CURRENT" "$TMP/s1.log")
calls=$(sed -E 's/v[0-9]+\.[0-9]+\.[0-9]+[^ ]*/V/g' "$TMP/s1.log.calls" 2>/dev/null | LC_ALL=C sort)
if [ "$rc" != 3 ]; then
  fail S1 "exit $rc, want 3" "$TMP/s1.log"
elif [ -n "$(cd "$D" && git status --porcelain)" ]; then
  fail S1 "the tree changed" "$TMP/s1.log"
elif [ "$calls" != "$(LC_ALL=C sort "$S1_CALLS")" ]; then
  printf '%s\n' "$calls" >"$TMP/s1.got"
  fail S1 "the normalized stub log differs from testdata/s1-calls.txt" "$TMP/s1.got"
elif [ -n "$(awk '/^newest / && (!/ --current / || !/ --repo-root /)' "$TMP/s1.log.calls")" ]; then
  fail S1 "a newest call lacks --current or --repo-root"
else
  pass S1
fi

# --- S3: a resolver error leaves the tree untouched ---------------------------------------------

D=$(sandbox s3)
base=$(cd "$D" && git rev-parse HEAD)
awk -F'\t' -v OFS='\t' -v k="$LIBKEY" '$1 == "newest" && $3 == k { $4 = "ERROR" } { print }' \
  "$CURRENT" >"$TMP/s3.tsv"
rc=$(run_task "$D" "$base" "$TMP/s3.tsv" "$TMP/s3.log")
if [ "$rc" = 0 ] || [ "$rc" = 3 ]; then
  fail S3 "exit $rc, want neither 0 nor 3" "$TMP/s3.log"
elif [ -n "$(cd "$D" && git status --porcelain)" ]; then
  fail S3 "the tree changed" "$TMP/s3.log"
else
  pass S3
fi

# --- S3b: a predicate's 3 (pin-of with no core) is an error, not "nothing to do" ----------------

D=$(sandbox s3b)
base=$(cd "$D" && git rev-parse HEAD)
awk -F'\t' '$1 != "pin-of"' "$CURRENT" >"$TMP/s3b.tsv"
rc=$(run_task "$D" "$base" "$TMP/s3b.tsv" "$TMP/s3b.log")
if [ "$rc" = 0 ] || [ "$rc" = 3 ]; then
  fail S3b "exit $rc, want neither 0 nor 3" "$TMP/s3b.log"
elif [ -n "$(cd "$D" && git status --porcelain)" ]; then
  fail S3b "the tree changed" "$TMP/s3b.log"
else
  pass S3b
fi

# --- S6: a dirty tree is refused ----------------------------------------------------------------

D=$(sandbox s6)
base=$(cd "$D" && git rev-parse HEAD)
printf 'untracked\n' >"$D/cascade-test-untracked"
rc=$(run_task "$D" "$base" "$CURRENT" "$TMP/s6.log")
if [ "$rc" != 1 ]; then
  fail S6 "exit $rc, want 1" "$TMP/s6.log"
elif [ "$(cd "$D" && git status --porcelain)" != "?? cascade-test-untracked" ]; then
  fail S6 "something besides the untracked file changed" "$TMP/s6.log"
else
  pass S6
fi

if [ "$SET" = offline ]; then
  exit "$FAILED"
fi

# --- S2 setup: every pin location the task moves, lowered to older.tsv --------------------------

# lower_pins DIR: the S2 setup edits.
lower_pins() {
  local d=$1 f o_cat o_core
  o_cat=$(older_v "$CATKEY")
  o_core=$(older_v "$COREKEY")
  (cd "$d" && go mod edit -require="$LIBKEY@$(older_v "$LIBKEY")")
  sed -i "s/version: \"${CAT_T#v}\"/version: \"${o_cat#v}\"/" "$d/$SAMPLE_PLATFORM"
  sed -i "s/return \"${CAT_T#v}\"/return \"${o_cat#v}\"/" "$d/$CATALOG_GO"
  for f in "$d"/test/fixtures/modules/*/cue.mod/module.cue \
           "$d"/test/fixtures/catalogs/provider/cue.mod/module.cue \
           "$d"/test/fixtures/modulepackages/*/cue.mod/module.cue; do
    set_dep_v "$f" "$CATKEY" "$o_cat"
    set_dep_v "$f" "$COREKEY" "$o_core"
  done
  printf '%s\n' "$(older_v "$CLIKEY")" >"$d/.opm-cli-version"
  # Every location must have moved, or S2 proves less than it claims.
  local p
  p=$(cd "$d" && .tasks/cascade/pins.sh WORKTREE)
  [ "$(printf '%s\n' "$p" | awk -F'\t' -v k="$LIBKEY" '$1 == k { print $4 }')" = "$(older_v "$LIBKEY")" ] &&
    [ "$(printf '%s\n' "$p" | awk -F'\t' -v k="$CATKEY" '$1 == k { print $4 }')" = "$o_cat" ] &&
    [ "$(printf '%s\n' "$p" | awk -F'\t' -v k="$COREKEY" '$1 == k { print $4 }')" = "$o_core" ] &&
    grep -q "return \"${o_cat#v}\"" "$d/$CATALOG_GO"
}

# --- S2: older pins give the expected diff, and a second run is a no-op ---------------------------

D=$(sandbox s2)
orig=$(cd "$D" && git rev-parse HEAD)
if ! lower_pins "$D"; then
  fail S2 "the setup did not lower every pin location"
else
  setup=$(commit_setup "$D")
  rc=$(run_task "$D" "$setup" "$OLDER_TABLE" "$TMP/s2.log")
  changed=$(cd "$D" && { git diff --name-only "$orig"; git ls-files --others --exclude-standard; } | LC_ALL=C sort -u)
  if [ "$rc" != 0 ]; then
    fail S2 "first run exit $rc, want 0" "$TMP/s2.log"
  elif [ "$changed" != "$(printf '%s\n' "$GOLDEN" | LC_ALL=C sort)" ]; then
    printf '%s\n' "$changed" >"$TMP/s2.changed"
    fail S2 "the diff against the original tree is not the golden list" "$TMP/s2.changed"
  else
    # S5 reads the first run's tree, before it is committed.
    if [ -n "${CASCADE_RESOLVER_REAL:-}" ]; then
      s5_title=$( (cd "$D" && CASCADE_RESOLVER="$CASCADE_RESOLVER_REAL" CASCADE_BASE="$setup" \
        task -x deps:cascade:title) 2>"$TMP/s5.title.err") || s5_title="(exit $?)"
      s5_rc=0
      (cd "$D" && CASCADE_RESOLVER="$CASCADE_RESOLVER_REAL" CASCADE_BASE="$setup" \
        task -x deps:cascade:body) >"$TMP/s5.body" 2>"$TMP/s5.body.err" || s5_rc=$?
    fi
    ids_before=$(cd "$D" && cat test/fixtures/modules/*/identity/identity.cue test/fixtures/catalogs/provider/identity/identity.cue | grep '^Version:')
    (cd "$D" && git add -A && gitc commit -q -m run1)
    rc=$(run_task "$D" "$setup" "$OLDER_TABLE" "$TMP/s2b.log")
    ids_after=$(cd "$D" && cat test/fixtures/modules/*/identity/identity.cue test/fixtures/catalogs/provider/identity/identity.cue | grep '^Version:')
    if [ "$rc" != 3 ]; then
      fail S2 "second run exit $rc, want 3" "$TMP/s2b.log"
    elif [ -n "$(cd "$D" && git status --porcelain)" ] || [ "$ids_before" != "$ids_after" ]; then
      fail S2 "the second run changed the tree or advanced a fixture again" "$TMP/s2b.log"
    else
      pass S2
    fi
  fi
fi

# --- S5: title and body against the real resolver -------------------------------------------------

if [ -z "${CASCADE_RESOLVER_REAL:-}" ]; then
  printf 'SKIP S5: CASCADE_RESOLVER_REAL is not set\n'
elif [ -z "${s5_title+x}" ]; then
  fail S5 "S2's first run did not succeed, so there is nothing to title"
else
  want="fix(deps): bump 4 upstream pins"
  rows=$(grep -c '^| .* (`' "$TMP/s5.body" || :)
  last=$(grep '^## ' "$TMP/s5.body" | tail -n 1)
  if [ "$s5_title" != "$want" ]; then
    fail S5 "title '$s5_title', want '$want'" "$TMP/s5.title.err"
  elif [ "$s5_rc" != 0 ]; then
    fail S5 "body exit $s5_rc" "$TMP/s5.body.err"
  elif ! grep -q '^<!-- cascade-title: ' "$TMP/s5.body" || ! grep -q '^<!-- cascade-labels: ' "$TMP/s5.body"; then
    fail S5 "the body lacks a cascade-title or cascade-labels marker" "$TMP/s5.body"
  elif [ "$rows" != 4 ]; then
    fail S5 "the body has $rows moved-pin rows, want 4" "$TMP/s5.body"
  elif [ "$last" != "## Notes" ]; then
    fail S5 "the last section is '$last', not '## Notes'" "$TMP/s5.body"
  else
    pass S5
  fi
fi

# --- S4: a frozen module file is left alone, the rest still moves ---------------------------------

D=$(sandbox s4)
if ! lower_pins "$D"; then
  fail S4 "the setup did not lower every pin location"
else
  {
    [ ! -f "$D/.cascade-frozen" ] || cat "$D/.cascade-frozen"
    [ -f "$D/.cascade-frozen" ] || printf 'frozen:\n'
    printf -- '- path: %s\n  pins: [%s, %s]\n  reason: S4\n' "$S4_FILE" "$CATKEY" "$COREKEY"
  } >"$TMP/s4.frozen"
  mv "$TMP/s4.frozen" "$D/.cascade-frozen"
  setup=$(commit_setup "$D")
  before=$(sha256sum "$D/$S4_FILE")
  rc=$(run_task "$D" "$setup" "$OLDER_TABLE" "$TMP/s4.log")
  if [ "$rc" != 0 ]; then
    fail S4 "exit $rc, want 0" "$TMP/s4.log"
  elif [ "$(sha256sum "$D/$S4_FILE")" != "$before" ]; then
    fail S4 "$S4_FILE changed although it is frozen" "$TMP/s4.log"
  elif [ -z "$(cd "$D" && git status --porcelain -- go.mod)" ]; then
    fail S4 "library did not move" "$TMP/s4.log"
  else
    pass S4
  fi
fi

exit "$FAILED"
