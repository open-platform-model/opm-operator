#!/usr/bin/env bash
# Render a kustomize overlay with the manager image set, WITHOUT writing to the
# working tree.
#
# `kustomize edit set image` rewrites config/manager/kustomization.yaml in place,
# so running `task operator:installer` or `task operator:controller:install`
# used to clobber the committed image pin (example.com/opm-operator:v0.0.1) with
# whatever IMG happened to default to, leaving a spurious diff that is easy to
# commit by accident. Everything config/default references lives under config/,
# so rendering from a throwaway copy gives the same output and touches nothing
# tracked.
#
# Usage: hack/render-config.sh <kustomize-bin> <image> <overlay-path>
#   e.g. hack/render-config.sh bin/kustomize controller:latest config/default
# Writes the rendered manifest to stdout.
set -euo pipefail

if [ "$#" -ne 3 ]; then
	echo "usage: $0 <kustomize-bin> <image> <overlay-path>" >&2
	exit 2
fi

kustomize_bin="$(cd "$(dirname "$1")" && pwd)/$(basename "$1")"
image="$2"
overlay="$3"

case "$overlay" in
config/*) ;;
*)
	echo "$0: overlay must live under config/ (got '$overlay')" >&2
	exit 2
	;;
esac

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

cp -R config "$tmp/config"
(cd "$tmp/config/manager" && "$kustomize_bin" edit set image "controller=$image")
"$kustomize_bin" build "$tmp/$overlay"
