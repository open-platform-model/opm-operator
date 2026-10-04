#!/usr/bin/env bash
# manifest-image.sh <install.yaml> [operator.cue]: fail unless every container
# of every Deployment in the rendered install manifest runs the operator image
# by exactly the tag and digest the module's operator package names
# (modules/opm_operator/operator/operator.cue by default):
#   ghcr.io/open-platform-model/opm-operator:v<Version>@<digest>
# It checks the render; it never rewrites it. Needs mikefarah yq v4.
set -euo pipefail
[ $# -ge 1 ] && [ $# -le 2 ] && [ -f "$1" ] || { echo "usage: $0 <install.yaml> [operator.cue]" >&2; exit 2; }
F=${2:-modules/opm_operator/operator/operator.cue}
[ -f "$F" ] || { echo "$0: no $F" >&2; exit 2; }
yq --version 2>/dev/null | grep -q mikefarah || { echo "$0: mikefarah yq v4 is required" >&2; exit 2; }

field() { sed -nE "s/^[[:space:]]*$1:[[:space:]]*\"([^\"]+)\".*/\\1/p" "$F" | head -n1; }
version=$(field Version)
repository=$(field repository)
digest=$(field digest)
[ -n "$version" ] && [ -n "$repository" ] && [ -n "$digest" ] || { echo "$0: $F: cannot read Version, repository and digest" >&2; exit 1; }
want="$repository:v$version@$digest"

mapfile -t images < <(yq -N 'select(.kind == "Deployment") | .spec.template.spec.containers[].image' "$1")
[ "${#images[@]}" -gt 0 ] || { echo "$1: no Deployment container" >&2; exit 1; }
rc=0
for image in "${images[@]}"; do
  [ "$image" = "$want" ] || { echo "$1: a Deployment runs $image, not the module's $want" >&2; rc=1; }
done
[ "$rc" != 0 ] || echo "$1: the operator runs $want" >&2
exit "$rc"
