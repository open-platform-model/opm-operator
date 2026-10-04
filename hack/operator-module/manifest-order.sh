#!/usr/bin/env bash
# manifest-order.sh <install.yaml>: fail unless the Namespace and every
# CustomResourceDefinition come before every namespaced object, so that
# `kubectl apply --server-side -f install.yaml` on an empty cluster never
# applies an object into a namespace or of a kind that does not exist yet.
# It checks the order; it never reorders the render. Needs mikefarah yq v4.
set -euo pipefail
[ $# -eq 1 ] && [ -f "$1" ] || { echo "usage: $0 <install.yaml>" >&2; exit 2; }
yq --version 2>/dev/null | grep -q mikefarah || { echo "$0: mikefarah yq v4 is required" >&2; exit 2; }

# One line per document: <kind> <namespace or -> <name>.
mapfile -t docs < <(yq -N '.kind + " " + (.metadata.namespace // "-") + " " + .metadata.name' "$1")
[ "${#docs[@]}" -gt 0 ] || { echo "$0: $1 holds no object" >&2; exit 1; }

first_namespaced=-1 last_setup=-1 namespaces=0 crds=0
for i in "${!docs[@]}"; do
  read -r kind ns _ <<<"${docs[$i]}"
  case $kind in
  Namespace) namespaces=$((namespaces + 1)); last_setup=$i ;;
  CustomResourceDefinition) crds=$((crds + 1)); last_setup=$i ;;
  esac
  if [ "$ns" != - ] && [ "$first_namespaced" -lt 0 ]; then first_namespaced=$i; fi
done

rc=0
[ "$namespaces" -ge 1 ] || { echo "$1: no Namespace object" >&2; rc=1; }
[ "$crds" -ge 1 ] || { echo "$1: no CustomResourceDefinition" >&2; rc=1; }
if [ "$first_namespaced" -ge 0 ] && [ "$last_setup" -gt "$first_namespaced" ]; then
  echo "$1: ${docs[$last_setup]} (object $((last_setup + 1))) comes after the namespaced ${docs[$first_namespaced]} (object $((first_namespaced + 1)))" >&2
  rc=1
fi
[ "$rc" -ne 0 ] || echo "$1: the Namespace and $crds CRDs come before every namespaced object (${#docs[@]} objects)"
exit "$rc"
