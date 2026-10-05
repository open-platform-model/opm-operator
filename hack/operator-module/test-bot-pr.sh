#!/usr/bin/env bash
# test-bot-pr.sh: offline test of hack/operator-module/bot-pr.sh against a
# scratch bare origin and a stub gh. Cases: create the branch and the PR; a
# second run with the same change pushes nothing; a bot-only branch is rebuilt
# from main with a lease; a branch with a human commit is extended, never
# rewritten; a change outside modules/opm_operator/ is refused; a symlink, a
# hard link and a mode change under the module are refused. Needs git; no
# network. Prints PASS/FAIL per case, exits 1 on any FAIL.
set -euo pipefail
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
BOTPR=$here/bot-pr.sh
F=modules/opm_operator/operator/operator.cue

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE
export GIT_AUTHOR_NAME=human GIT_AUTHOR_EMAIL=human@invalid GIT_COMMITTER_NAME=human GIT_COMMITTER_EMAIL=human@invalid
export GH_REPO=example/opm-operator BOT_NAME='release-app[bot]' BOT_EMAIL='1+release-app[bot]@users.noreply.github.com'
export GH=$TMP/gh GH_LOG=$TMP/gh.log GH_PR=$TMP/gh.pr
# Stub gh: "pr list" prints the number in $GH_PR (if any); "pr create" writes 7 there.
cat >"$GH" <<'EOF'
#!/bin/sh
echo "gh $*" >>"$GH_LOG"
case "$1 $2" in
"pr list") [ -f "$GH_PR" ] && cat "$GH_PR"; exit 0 ;;
"pr create") echo 7 >"$GH_PR" ;;
esac
EOF
chmod +x "$GH"

gitq() { git -c commit.gpgsign=false -c init.defaultBranch=main "$@"; }
printf 'body\n' >"$TMP/body.md"

gitq init -q --bare "$TMP/origin.git"
gitq clone -q "$TMP/origin.git" "$TMP/seed" 2>/dev/null
mkdir -p "$TMP/seed/modules/opm_operator/operator"
printf 'Version: "1.0.0-beta.1"\n' >"$TMP/seed/$F"
printf 'x\n' >"$TMP/seed/README.md"
(cd "$TMP/seed" && gitq add -A && gitq commit -q -m base && gitq push -q origin HEAD:main)

FAILED=0
pass() { printf 'PASS %s\n' "$1"; }
fail() { printf 'FAIL %s: %s\n' "$1" "$2"; [ -z "${3:-}" ] || sed 's/^/    /' "$TMP/$3.log"; FAILED=1; }
# work NAME VERSION: a fresh clone of main with operator.cue set to VERSION.
work() {
  rm -rf "${TMP:?}/$1"
  gitq clone -q "$TMP/origin.git" "$TMP/$1" 2>/dev/null
  printf 'Version: "%s"\n' "$2" >"$TMP/$1/$F"
}
botpr() { (cd "$TMP/$1" && "$BOTPR" module/operator-image "$2" "$TMP/body.md") >"$TMP/$1.log" 2>&1; }
tip() { git -C "$TMP/origin.git" rev-parse "refs/heads/module/operator-image" 2>/dev/null || true; }

# create
work c1 1.0.0-beta.2
if ! botpr c1 "fix(deps): deploy operator v1.0.0-beta.2 from the operator module"; then
  fail create "exit non-zero" c1
elif [ "$(git -C "$TMP/origin.git" show "module/operator-image:$F")" != 'Version: "1.0.0-beta.2"' ] ||
  ! grep -q "^gh pr create --repo example/opm-operator --base main --head module/operator-image" "$GH_LOG"; then
  fail create "branch content or pr create call wrong" c1
else
  pass create
fi

# same change again: no push, PR edited
before=$(tip)
work c2 1.0.0-beta.2
if ! botpr c2 "fix(deps): deploy operator v1.0.0-beta.2 from the operator module"; then
  fail no-op "exit non-zero" c2
elif [ "$(tip)" != "$before" ] || ! grep -q "^gh pr edit 7 --repo example/opm-operator" "$GH_LOG"; then
  fail no-op "the branch moved, or the open PR was not edited" c2
else
  pass no-op
fi

# main moves; a bot-only branch is rebuilt from the new main with a lease
(cd "$TMP/seed" && printf 'y\n' >README.md && gitq commit -q -am "main moves" && gitq push -q origin HEAD:main)
work c3 1.0.0-beta.3
if ! botpr c3 "fix(deps): deploy operator v1.0.0-beta.3 from the operator module"; then
  fail rebuild "exit non-zero" c3
elif [ "$(git -C "$TMP/origin.git" rev-list --count main..module/operator-image)" != 1 ] ||
  ! git -C "$TMP/origin.git" merge-base --is-ancestor main module/operator-image ||
  ! grep -q "(rebuild)" "$TMP/c3.log"; then
  fail rebuild "the branch is not one bot commit on the new main" c3
else
  pass rebuild
fi

# a human commits on the branch: the next run extends it and keeps that commit
rm -rf "$TMP/h" && gitq clone -q -b module/operator-image "$TMP/origin.git" "$TMP/h" 2>/dev/null
(cd "$TMP/h" && printf 'note\n' >modules/opm_operator/NOTE && gitq add -A && gitq commit -q -m "human fix" && gitq push -q origin HEAD:module/operator-image)
human=$(tip)
work c4 1.0.0-beta.4
if ! botpr c4 "fix(deps): deploy operator v1.0.0-beta.4 from the operator module"; then
  fail extend "exit non-zero" c4
elif ! git -C "$TMP/origin.git" merge-base --is-ancestor "$human" module/operator-image ||
  [ "$(git -C "$TMP/origin.git" show "module/operator-image:$F")" != 'Version: "1.0.0-beta.4"' ] ||
  ! grep -q "(extend)" "$TMP/c4.log"; then
  fail extend "the human commit was rewritten, or the change is missing" c4
else
  pass extend
fi

# a change outside the module is refused before anything is pushed
before=$(tip)
work c5 1.0.0-beta.5
printf 'z\n' >"$TMP/c5/README.md"
if botpr c5 "fix(deps): x"; then
  fail outside "exit 0" c5
elif ! grep -qF "refusing a change outside modules/opm_operator/: README.md" "$TMP/c5.log" || [ "$(tip)" != "$before" ]; then
  fail outside "not refused by name, or the branch moved" c5
else
  pass outside
fi

# refused NAME MESSAGE: bot-pr fails in clone NAME, names MESSAGE, and pushes nothing.
refused() {
  local before
  before=$(tip)
  if botpr "$1" "fix(deps): x"; then
    fail "$1" "exit 0" "$1"
  elif ! grep -qF "$2" "$TMP/$1.log" || [ "$(tip)" != "$before" ]; then
    fail "$1" "not refused with '$2', or the branch moved" "$1"
  else
    pass "$1"
  fi
}

# a symlink under the module would commit another file's content
work symlink 1.0.0-beta.5
ln -s ../../.git/config "$TMP/symlink/modules/opm_operator/leak"
refused symlink "refusing a symlink: modules/opm_operator/leak"

# so would a hard link
work hardlink 1.0.0-beta.5
ln "$TMP/hardlink/README.md" "$TMP/hardlink/modules/opm_operator/leak"
refused hardlink "refusing a hard link: modules/opm_operator/leak"

# a mode change is refused
work mode 1.0.0-beta.5
chmod +x "$TMP/mode/$F"
refused mode "refusing a mode change 100644 -> 100755"

exit "$FAILED"
