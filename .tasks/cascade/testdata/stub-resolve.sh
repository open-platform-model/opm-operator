#!/usr/bin/env bash
# Cascade resolver stub, contract version 1 (Phase 2 cascade contract §7).
# Byte-identical in open-platform-model/.github and every repo's
# .tasks/cascade/testdata/. Answers from the TSV file $CASCADE_STUB_TABLE.
set -euo pipefail
die() { printf 'stub-resolve: %s\n' "$1" >&2; exit "${2:-1}"; }
[ -n "${CASCADE_STUB_TABLE:-}" ] || die "CASCADE_STUB_TABLE is not set"
[ -z "${CASCADE_STUB_LOG:-}" ] || printf '%s\n' "$*" >>"$CASCADE_STUB_LOG"
cmp_v() { # semver_cmp a b -> -1|0|1
  local a="${1%%+*}" b="${2%%+*}" lo
  [ "$a" = "$b" ] && { echo 0; return; }
  lo=$(printf '%s\n%s\n' "$a" "$b" | sed 's/-/~/' | sort -V | head -n1 | sed 's/~/-/')
  if [ "$lo" = "$a" ]; then echo -1; else echo 1; fi
}
col() { awk -F'\t' -v a="$1" -v b="$2" -v c="$3" -v n="$4" \
  '$1==a && $2==b && $3==c {print $n; exit}' "$CASCADE_STUB_TABLE"; }
cmd="${1:-}"; [ -n "$cmd" ] || die "usage: stub-resolve.sh <subcommand> ..." 2; shift
pos=(); cur=""; root="."
while [ $# -gt 0 ]; do
  case "$1" in
    --current) cur="$2"; shift 2 ;;
    --repo-root) root="$2"; shift 2 ;;
    --asset|--expect|--max-wait) shift 2 ;;
    --pre) shift ;;
    --json) die "--json is not supported by the stub" 2 ;;
    --*) die "unknown flag $1" 2 ;;
    *) pos+=("$1"); shift ;;
  esac
done
coord() { if [ "${pos[0]}" = opm-cli ]; then echo -; else echo "${pos[1]}"; fi; }
pinkey() { # §2.4 pin key for kind $1, coordinate $2
  case "$1" in
    opm-cli) echo github.com/open-platform-model/cli ;;
    release) echo "github.com/open-platform-model/$2" ;;
    *) echo "$2" ;;
  esac
}
case "$cmd" in
  newest)
    [ -n "$cur" ] || die "--current is required" 2
    k="${pos[0]}"; c=$(coord)
    if [ -n "${CASCADE_WARNINGS:-}" ]; then
      awk -F'\t' -v b="$k" -v c="$c" -v w="$(pinkey "$k" "$c")" \
        '$1=="warn" && $2==b && $3==c {print w "\t" $4}' \
        "$CASCADE_STUB_TABLE" >>"$CASCADE_WARNINGS"
    fi
    v=$(col newest "$k" "$c" 4)
    [ -n "$v" ] || die "no newest row for $k $c"
    [ "$v" != ERROR ] || die "simulated resolver error for $k $c"
    if [ "$(cmp_v "$v" "$cur")" = 1 ]; then echo "$v"; exit 0; fi
    exit 3 ;;
  published)
    k="${pos[0]}"; c=$(coord)
    if [ "$k" = opm-cli ]; then v="${pos[1]}"; else v="${pos[2]}"; fi
    awk -F'\t' -v b="$k" -v c="$c" -v v="$v" \
      '$1=="published" && $2==b && $3==c && $4==v {f=1} END {exit !f}' \
      "$CASCADE_STUB_TABLE" && exit 0
    exit 3 ;;
  pin-of)
    v=$(awk -F'\t' -v m="${pos[0]}" -v v="${pos[1]}" -v d="${pos[2]}" \
      '$1=="pin-of" && $2==m && $3==v && $4==d {print $5; exit}' "$CASCADE_STUB_TABLE")
    [ -n "$v" ] || exit 3; echo "$v" ;;
  language-of)
    v=$(col language-of "${pos[0]}" "${pos[1]}" 4); [ -n "$v" ] || exit 3; echo "$v" ;;
  check-files) exit 0 ;;
  frozen)
    f="$root/.cascade-frozen"; [ -f "$f" ] || exit 0
    K="${pos[0]}" yq -r '.frozen[] | select(.pins[] == strenv(K)) | .path' "$f" ;;
  is-frozen)
    f="$root/.cascade-frozen"; [ -f "$f" ] || exit 3
    while IFS= read -r p; do
      [ -n "$p" ] || continue
      p="${p%/}"
      case "${pos[0]}" in "$p"|"$p"/*) exit 0 ;; esac
    done < <(K="${pos[1]}" yq -r '.frozen[] | select(.pins[] == strenv(K)) | .path' "$f")
    exit 3 ;;
  hold)
    f="$root/.cascade-hold"; [ -f "$f" ] || exit 3
    today="${CASCADE_TODAY:-$(date -u +%F)}"
    line=$(K="${pos[0]}" yq -r '.holds[] | select(.pin == strenv(K)) | .max + " " + .expires' "$f")
    [ -n "$line" ] || exit 3
    [[ ! "${line#* }" < "$today" ]] || exit 3
    echo "${line%% *}" ;;
  semver-cmp) cmp_v "${pos[0]}" "${pos[1]}" ;;
  next-patch) awk -F. -v OFS=. '{$NF=$NF+1; print}' <<<"${pos[0]}" ;;
  *) die "subcommand $cmd is not supported by the stub" 2 ;;
esac
