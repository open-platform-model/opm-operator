#!/usr/bin/env bash
# image-tag-guard.sh: never overwrite a version image tag (0021:D10:R3).
#
#   image-tag-guard.sh probe REF REV
#     Prints "state=absent" when REF does not exist. When REF exists and its
#     linux/amd64 org.opencontainers.image.revision label equals REV, prints
#     "state=reuse" and "digest=<manifest digest>". Fails on any other content
#     or on any probe error other than "not found".
#
#   image-tag-guard.sh verify REF DIGEST
#     Fails unless REF resolves to DIGEST.
#
# The output is GITHUB_OUTPUT-shaped (key=value lines on stdout).
set -euo pipefail

usage() { echo "usage: $0 probe REF REV | verify REF DIGEST" >&2; exit 2; }

[ $# -eq 3 ] || usage
mode=$1 ref=$2 want=$3

# manifest_digest REF: prints the manifest (list) digest of REF; on failure
# prints the tool's output and returns non-zero.
manifest_digest() {
  local out
  out=$(docker buildx imagetools inspect "$1" --format '{{json .Manifest.Digest}}' 2>&1) || {
    printf '%s\n' "$out"; return 1; }
  jq -er 'select(type == "string" and startswith("sha256:"))' <<<"$out" || {
    printf 'unreadable digest: %s\n' "$out"; return 1; }
}

case $mode in
probe)
  if ! out=$(manifest_digest "$ref"); then
    case $out in
    *": not found"*) echo state=absent; exit 0 ;;
    esac
    echo "::error::probe of ${ref} failed: ${out}"
    exit 1
  fi
  digest=$out
  rev=$(docker buildx imagetools inspect "${ref%:*}@${digest}" \
    --format '{{ index (index .Image "linux/amd64").Config.Labels "org.opencontainers.image.revision" }}' 2>&1) || {
    echo "::error::cannot read the revision label of ${ref}: ${rev}"; exit 1; }
  if [ "$rev" != "$want" ]; then
    echo "::error::${ref} holds ${rev:-<no revision label>}, release commit is ${want}; release the next version"
    exit 1
  fi
  printf 'state=reuse\ndigest=%s\n' "$digest"
  ;;
verify)
  if ! got=$(manifest_digest "$ref"); then
    echo "::error::cannot read ${ref} after push: ${got}"
    exit 1
  fi
  if [ "$got" != "$want" ]; then
    echo "::error::${ref} resolves to ${got}, not the pushed ${want}"
    exit 1
  fi
  echo "${ref} resolves to ${want}"
  ;;
*) usage ;;
esac
