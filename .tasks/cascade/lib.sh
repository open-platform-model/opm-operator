# shellcheck shell=bash
# Readers shared by pins.sh and cascade.sh. Each reads one file's text on stdin and
# prints the version it finds, or nothing.

# A full v-prefixed SemVer (Phase 2 cascade contract §2.2).
valid_v() {
  [[ $1 =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$ ]]
}

# cue_dep_v DEP: the v: inside the "DEP": { ... } block of a cue.mod/module.cue.
cue_dep_v() {
  awk -v k="\"$1\": {" '
    index($0, k) { f = 1; next }
    f && /^[[:space:]]*v:/ { if (match($0, /"[^"]+"/)) print substr($0, RSTART + 1, RLENGTH - 2); exit }
    f && /}/ { exit }'
}

# yaml_version_after ANCHOR: the first version: line after the line holding ANCHOR,
# unquoted (the sample Platform stores the catalog bare: version: "4.4.4").
yaml_version_after() {
  awk -v k="$1" '
    index($0, k) { f = 1; next }
    f && /^[[:space:]]*version:/ { v = $0; sub(/^[[:space:]]*version:[[:space:]]*/, "", v); gsub(/"/, "", v); print v; exit }'
}

# go_require_v MODULE: the version go.mod requires for MODULE (never a replace line).
go_require_v() {
  awk -v m="$1" '
    $1 == "replace" || $2 == "=>" { next }
    $1 == "require" && $2 == m { print $3; exit }
    $1 == m { print $2; exit }'
}

# identity_version: the Version: of a fixture's identity/identity.cue.
identity_version() {
  awk '/^Version:/ { if (match($0, /"[^"]+"/)) print substr($0, RSTART + 1, RLENGTH - 2); exit }'
}
