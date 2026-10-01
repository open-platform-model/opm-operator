#!/usr/bin/env bash
# release-guard.sh assert-draft|publish TAG
set -euo pipefail
mode=$1 tag=$2
: "${GH_REPO:?GH_REPO must name the repository}"
rels=$(gh api --paginate "repos/${GH_REPO}/releases?per_page=100" \
  --jq ".[] | select(.tag_name == \"${tag}\") | {id, draft, prerelease, assets: [.assets[].name]}" | jq -s .)
n=$(jq length <<<"$rels")
[ "$n" -eq 1 ] || { echo "::error::expected exactly one release for ${tag}, found ${n}"; exit 1; }
draft=$(jq -r '.[0].draft' <<<"$rels")
case $mode in
assert-draft)
  [ "$draft" = true ] || { echo "::error::release ${tag} is published; release the next version"; exit 1; } ;;
publish)
  [ "$draft" = true ] || { echo "release ${tag} already published, nothing to do"; exit 0; }
  for a in install.yaml opm-examples.tar.gz; do
    jq -e --arg a "$a" '.[0].assets | index($a) != null' <<<"$rels" >/dev/null \
      || { echo "::error::draft ${tag} lacks ${a}"; exit 1; }
  done
  id=$(jq -r '.[0].id' <<<"$rels")
  gh api -X PATCH "repos/${GH_REPO}/releases/${id}" -F draft=false >/dev/null ;;
*) echo "::error::unknown mode ${mode}"; exit 2 ;;
esac
