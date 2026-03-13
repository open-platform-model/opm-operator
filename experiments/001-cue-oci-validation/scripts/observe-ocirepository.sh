#!/usr/bin/env bash

set -euo pipefail

if [[ $# -lt 1 || $# -gt 2 ]]; then
	printf 'usage: %s <name> [namespace]\n' "$0" >&2
	exit 1
fi

name="$1"
namespace="${2:-default}"

kubectl get ocirepository "$name" -n "$namespace" -o yaml

printf '\n---\n'
printf 'ready: '
kubectl get ocirepository "$name" -n "$namespace" -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}'
printf '\nrevision: '
kubectl get ocirepository "$name" -n "$namespace" -o jsonpath='{.status.artifact.revision}'
printf '\ndigest: '
kubectl get ocirepository "$name" -n "$namespace" -o jsonpath='{.status.artifact.digest}'
printf '\nurl: '
kubectl get ocirepository "$name" -n "$namespace" -o jsonpath='{.status.artifact.url}'
printf '\n'
