#!/usr/bin/env bash
# Fail when the operator module's generated CRD and RBAC data no longer match
# the config/ tree they are generated from: regenerate into a temporary
# directory and diff it against the committed files, naming each stale file
# and each role or CRD that appears in only one of them.
#
#   hack/operator-module/drift-check.sh                 # against this tree's config/
#   hack/operator-module/drift-check.sh --ref <tag>     # against config/ of a git ref
#   SRC=/path/to/config hack/operator-module/drift-check.sh  # against an extracted tree
#
# PR CI runs it through `task operator-module:drift`, which first checks that
# config/ itself is current against controller-gen (the config/ half of
# `task dev:manifests`, which leaves the module files untouched). The --ref mode is for
# the module's release gate: a module release must match the config/ of the
# operator release it deploys, not main's.
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/../.." && pwd)
MOD=$ROOT/modules/opm_operator

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

ref=""
while [ $# -gt 0 ]; do
	case "$1" in
	--ref)
		[ $# -ge 2 ] || { echo "$0: --ref needs a git ref" >&2; exit 2; }
		ref=$2
		shift 2
		;;
	--ref=*)
		ref=${1#--ref=}
		shift
		;;
	*)
		echo "usage: $0 [--ref <git ref>]" >&2
		exit 2
		;;
	esac
done

if [ -n "$ref" ]; then
	[ -z "${SRC:-}" ] || { echo "$0: give --ref or SRC=, not both" >&2; exit 2; }
	mkdir -p "$tmp/ref"
	git -C "$ROOT" archive "$ref" config | tar -x -C "$tmp/ref"
	SRC=$tmp/ref/config
fi
src=${SRC:-$ROOT/config}

mkdir -p "$tmp/out"
SRC=$src OUT=$tmp/out "$ROOT/hack/operator-module/generate.sh" >/dev/null

# Top-level keys of a generated definition: the CRD or role names, quoted or
# not (cue fmt leaves an identifier-shaped name unquoted).
keys() { sed -En 's/^\t"?([^":[:space:]]+)"?: \{$/\1/p' "$1" | LC_ALL=C sort; }

rc=0
for f in zz_generated_crds.cue zz_generated_rbac.cue; do
	committed=$MOD/$f
	fresh=$tmp/out/$f
	if [ ! -f "$committed" ]; then
		echo "DRIFT: modules/opm_operator/$f is missing; run 'task dev:manifests'"
		rc=1
		continue
	fi
	if diff -u --label "modules/opm_operator/$f (committed)" --label "modules/opm_operator/$f (from $src)" \
		"$committed" "$fresh" >"$tmp/$f.diff"; then
		continue
	fi
	rc=1
	echo "DRIFT: modules/opm_operator/$f is stale against $src"
	comm -13 <(keys "$committed") <(keys "$fresh") | sed 's/^/  missing from the module: /'
	comm -23 <(keys "$committed") <(keys "$fresh") | sed 's/^/  no longer in config: /'
	head -80 "$tmp/$f.diff"
done
if [ "$rc" -eq 0 ]; then
	echo "no drift: modules/opm_operator/zz_generated_*.cue match $src"
else
	echo "regenerate with 'task dev:manifests'" >&2
fi
exit "$rc"
