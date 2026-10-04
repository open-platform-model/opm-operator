#!/usr/bin/env bash
# bot-pr.sh <branch> <title> <body-file>: open or update the operator module's
# bot pull request (module/operator-image, module/deps) from the working tree.
#
# Run in a checkout of main whose working tree holds the bot's change, by the
# publish job of release.yml (module-image-pr-publish), module-image.yml and
# module-deps.yml, with the release App's token. The change must lie under
# modules/opm_operator/ only, or the squash commit would release the operator
# too; anything else is refused before git is touched. Only plain files are
# written: a symlink, a hard link or a mode change is refused.
#
#   - no remote branch: create it from main with one commit;
#   - the branch holds only commits by BOT_EMAIL: rebuild it from main with
#     one commit and push with --force-with-lease on the fetched tip;
#   - a commit by anyone else is on it: merge main into it, add a commit when
#     the tree differs, and push without force; a human's commits are never
#     rewritten.
# Then create the PR, or edit the open one's title and body. A change already
# on the branch pushes nothing. GH_REPO names the repository for every gh call.
#
# Environment: GH_REPO, BOT_NAME, BOT_EMAIL; BASE (default main); REMOTE
# (default origin); GH (default gh), replaced by the offline test.
set -euo pipefail

[ $# -eq 3 ] || { echo "usage: $0 <branch> <title> <body-file>" >&2; exit 2; }
branch=$1 title=$2 body=$3
: "${GH_REPO:?GH_REPO must name the repository}"
: "${BOT_NAME:?BOT_NAME must name the bot}"
: "${BOT_EMAIL:?BOT_EMAIL must be the commit email of the bot}"
BASE=${BASE:-main}
REMOTE=${REMOTE:-origin}
GH=${GH:-gh}
M=modules/opm_operator

die() { echo "bot-pr: $*" >&2; exit 1; }
[[ $branch =~ ^module/[a-z0-9-]+$ ]] || die "'$branch' is not a module/* bot branch"
[ -n "$title" ] || die "empty title"
[ -f "$body" ] || die "no body file $body"
case $title in *$'\n'*) die "the title has a newline" ;; esac

# The change: every path under the module, nothing else.
mapfile -t paths < <(git status --porcelain --untracked-files=all | cut -c4-)
[ "${#paths[@]}" -gt 0 ] || { echo "bot-pr: nothing changed; no pull request"; exit 0; }
for p in "${paths[@]}"; do
  [[ $p == "$M/"* ]] || die "refusing a change outside $M/: $p"
  # A symlink or a hard link would carry another file's content (the
  # checkout's .git/config holds the push token) into the commit.
  [ ! -L "$p" ] || die "refusing a symlink: $p"
  [ -f "$p" ] || die "refusing to delete $p; the bot only writes files"
  [ "$(stat -c %h -- "$p")" = 1 ] || die "refusing a hard link: $p"
done

stash=$(mktemp -d)
trap 'rm -rf "$stash"' EXIT
for p in "${paths[@]}"; do
  mkdir -p "$stash/$(dirname "$p")"
  cp -p "$p" "$stash/$p"
done
restore() { cp -rp "$stash/$M/." "$M/"; }

# The bot's commits carry its identity whatever the environment sets.
gitc() {
  GIT_AUTHOR_NAME=$BOT_NAME GIT_AUTHOR_EMAIL=$BOT_EMAIL GIT_COMMITTER_NAME=$BOT_NAME \
    GIT_COMMITTER_EMAIL=$BOT_EMAIL git -c commit.gpgsign=false "$@"
}

git fetch --no-tags "$REMOTE" "+refs/heads/$BASE:refs/remotes/$REMOTE/$BASE"
old=''
if git fetch --no-tags "$REMOTE" "+refs/heads/$branch:refs/remotes/$REMOTE/$branch" 2>/dev/null; then
  old=$(git rev-parse "refs/remotes/$REMOTE/$branch")
fi

git reset -q --hard
git clean -qfd -- "$M"
if [ -z "$old" ]; then
  mode=create
  git checkout -q -B "$branch" "$REMOTE/$BASE"
else
  others=$(git log --format='%ae' "$REMOTE/$BASE..$old" | grep -vxF "$BOT_EMAIL" | sort -u || true)
  if [ -z "$others" ]; then
    mode=rebuild
    git checkout -q -B "$branch" "$REMOTE/$BASE"
  else
    mode=extend
    echo "bot-pr: $branch carries commits by ${others//$'\n'/, }; adding a commit, never rewriting"
    git checkout -q -B "$branch" "$old"
    gitc merge -q --no-edit "$REMOTE/$BASE" || die "merging $BASE into $branch conflicts; resolve it by hand"
  fi
fi
restore
git add -A -- "$M"
# Plain files only: no mode change, no executable bit, no gitlink.
while read -r om nm _; do
  om=${om#:}
  [ "$nm" = 100644 ] && { [ "$om" = 000000 ] || [ "$om" = 100644 ]; } ||
    die "refusing a mode change $om -> $nm under $M/; the bot only writes plain files"
done < <(git diff --cached --raw --no-renames)
if git diff --cached --quiet; then
  echo "bot-pr: $branch already carries this change"
else
  gitc commit -q -m "$title"
fi

if [ "$mode" != create ] && [ "$(git rev-parse "HEAD^{tree}")" = "$(git rev-parse "$old^{tree}")" ]; then
  echo "bot-pr: $branch already holds this tree; nothing pushed"
else
  case $mode in
  create) git push "$REMOTE" "HEAD:refs/heads/$branch" ;;
  rebuild) git push --force-with-lease="refs/heads/$branch:$old" "$REMOTE" "HEAD:refs/heads/$branch" ;;
  extend) git push "$REMOTE" "HEAD:refs/heads/$branch" ;;
  esac
  echo "bot-pr: pushed $branch ($mode)"
fi

pr=$("$GH" pr list --repo "$GH_REPO" --head "$branch" --base "$BASE" --state open --json number --jq '.[0].number // empty')
if [ -n "$pr" ]; then
  "$GH" pr edit "$pr" --repo "$GH_REPO" --title "$title" --body-file "$body"
  echo "bot-pr: updated pull request #$pr"
else
  "$GH" pr create --repo "$GH_REPO" --base "$BASE" --head "$branch" --title "$title" --body-file "$body"
fi
