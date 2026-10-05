#!/usr/bin/env bash
# The release-cascade wiring check: compares a product repo's cascade caller
# workflows with the shapes the open-platform-model/.github README documents
# for the commit this file comes from. Canonical copy:
# open-platform-model/.github .github/scripts/cascade/wiring-check.sh. Each of
# core, catalog_opm, library, opm-operator and cli keeps a byte-identical copy
# at .tasks/cascade/wiring-check.sh, taken from the .github commit its cascade
# references pin, plus its own values in .tasks/cascade/wiring-check.yaml.
#
# It guards against mistakes. The copy and the config live in the repo's own
# tree, so a PR can change them along with the workflows; review and the main
# ruleset guard against a deliberate edit. Online it also compares itself with
# the canonical file at the pinned SHA: that catches a copy that drifted or was
# not replaced at a pin bump. It is no substitute for review, since the copy
# under test runs the comparison: an edited copy passes if the same PR also
# removes the comparison, or changes what runs around the CI step. The CI
# shape rule refuses the plain routes (env, container or services on the CI
# job or workflow, a run: step before the wiring step); an earlier SHA-pinned
# action can still change the step's environment (GITHUB_ENV, GITHUB_PATH),
# so a reviewer reads every change to the CI workflow as a change to this
# check.
#
# Usage, from the repo root:
#   bash .tasks/cascade/wiring-check.sh [--pin-on-main] [<config>]
# Offline by default (task cascade:wiring:check). --pin-on-main, which the
# required CI step passes, also asks the GitHub API (gh, GH_TOKEN) that the
# one .github SHA is on .github's main, so a commit that exists only in a
# fork of .github (an "imposter commit") is refused, and then fetches
# .github/scripts/cascade/wiring-check.sh at that SHA and compares it byte for
# byte with this running file. Offline, a second line after the ok line says
# the copy was not compared.
# <config> defaults to .tasks/cascade/wiring-check.yaml:
#   pin-comment: .github main      # the comment after every .github SHA
#   receiver: true                 # false: notify only (core)
#   env-allow: [CUE_REGISTRY]      # release.yml workflow env keys allowed
#   publish-workflows: [release.yml, docs.yml]  # restore no Actions cache
#   ci: {workflow: ci.yml, job: ci}  # the required job that runs this check
#   notify:                        # this repo's notify-downstream values
#     needs: [release-please, publish-cue]
#     if: <the job's if: text>
#     tag: <the step's tag input>
#   publish:                       # receivers only
#     labels-managed: false        # a YAML boolean
#   extra-references:              # optional: more pinned .github references
#     - {file: module-deps.yml, kind: resolver}  # one entry per reference
#
# Beyond the cascade key it also binds the release App key: every job that
# reads RELEASE_APP_PRIVATE_KEY declares `environment: release`, and only
# those jobs do (owner decision 29). The rule needs no config key.
#
# Exit status: 0 the shapes match (prints "cascade wiring: ok, .github <sha>
# (<pin comment>)"); 1 a mismatch, or with --pin-on-main a SHA not on
# .github main, a copy that differs from the file at the SHA, or an API call
# that failed (every problem is printed on stderr); 2 usage, a missing tool or
# a bad config.
#
# Tools: bash, coreutils, sed, grep, mikefarah yq v4; gh with --pin-on-main
# (through "${CASCADE_GH:-gh}").
# shellcheck disable=SC2016 # the single-quoted ${{ }} strings are GitHub expressions, compared literally
set -euo pipefail

usage_err() { echo "cascade wiring: $*" >&2; exit 2; }
# The file bash is running: the copy --pin-on-main compares with the pinned one.
SELF=${BASH_SOURCE[0]}
PIN_ON_MAIN=false
if [ "${1:-}" = --pin-on-main ]; then PIN_ON_MAIN=true; shift; fi
[ $# -le 1 ] && [[ ${1:-} != -* ]] || usage_err "usage: wiring-check.sh [--pin-on-main] [<config>]"
CONFIG=${1:-.tasks/cascade/wiring-check.yaml}
# A tripwire only: bash sources BASH_ENV before this line runs, and that file
# can unset it. The CI shape rule below is what keeps both out of the step.
if [ "$PIN_ON_MAIN" = true ] && { [ -n "${BASH_ENV+x}" ] || [ -n "${ENV+x}" ]; }; then
  echo "cascade wiring: BASH_ENV or ENV is set; --pin-on-main runs without them" >&2
  exit 1
fi
W=.github/workflows
yq --version 2>/dev/null | grep -q mikefarah || usage_err "mikefarah yq v4 is required"
[ -f "$CONFIG" ] || usage_err "no config at $CONFIG"
[ -d "$W" ] || usage_err "no $W here; run from the repo root"

fail=0
bad() { echo "cascade wiring: $*" >&2; fail=1; }
eq() { [ "$2" = "$3" ] || bad "$1: expected [$2], got [$3]"; }
re() { [[ $3 =~ $2 ]] || bad "$1: [$3] does not match $2"; }
# y <filter> <file>: one yq read, as raw text.
y() { yq -r "$1" "$2"; }
# yj <filter> <file>: one yq read, as one-line JSON.
yj() { yq -o=json -I=0 "$1" "$2"; }

# --- the config ---------------------------------------------------------------

# The only names env-allow may list. A workflow-level env reaches the notify
# action's steps, which hold the App token, and too many variables make bash,
# git, gh, node, curl or the loader run code, read other config or send
# traffic elsewhere (BASH_ENV, GIT_*, GH_*, NODE_*, LD_*, XDG_*, SSL_*,
# CURL_*, *_PROXY and more) for a deny-list to be safe. So the names are
# allowed, not denied: the registry settings the five release workflows use,
# which none of those tools reads.
ENV_ALLOW_RE='^(CUE|OPM)_[A-Z0-9_]+$|^REGISTRY$|^IMAGE_NAME$'

cfg_err() { usage_err "$CONFIG: $*"; }
want_cfg=$(printf '%s' '["ci","env-allow","extra-references","notify","pin-comment","publish","publish-workflows","receiver"]')
[ "$(y 'type' "$CONFIG")" = '!!map' ] || cfg_err "not a YAML map"
for k in $(y 'keys | .[]' "$CONFIG"); do
  [[ $want_cfg == *"\"$k\""* ]] || cfg_err "unknown key $k"
done
[ "$(y '.receiver | type' "$CONFIG")" = '!!bool' ] || cfg_err "receiver must be true or false"
RECEIVER=$(y '.receiver' "$CONFIG")
PIN_COMMENT=$(y '.["pin-comment"] // ""' "$CONFIG")
[[ $PIN_COMMENT =~ ^[A-Za-z0-9._/-]+([\ ][A-Za-z0-9._/-]+)*$ ]] || cfg_err "pin-comment must be words without quotes, # or line breaks"
[ "$(y '.["env-allow"] | type' "$CONFIG")" = '!!seq' ] || cfg_err "env-allow must be a list (use [] for none)"
ENV_ALLOW=" "
while IFS= read -r k; do
  [ -n "$k" ] || continue
  [[ $k =~ ^[A-Z][A-Z0-9_]*$ ]] || cfg_err "env-allow entry [$k] is not an upper-case variable name"
  [[ $k =~ $ENV_ALLOW_RE ]] || cfg_err "env-allow may not allow $k (only CUE_*, OPM_*, REGISTRY and IMAGE_NAME)"
  ENV_ALLOW+="$k "
done < <(y '.["env-allow"][] | tostring' "$CONFIG")
[ "$(y '.["publish-workflows"] | type' "$CONFIG")" = '!!seq' ] || cfg_err "publish-workflows must be a list"
PUBLISH_WFS=""
while IFS= read -r k; do
  [ -n "$k" ] || continue
  [[ $k =~ ^[A-Za-z0-9._-]+\.ya?ml$ ]] || cfg_err "publish-workflows entry [$k] is not a workflow file name"
  PUBLISH_WFS+="$k"$'\n'
done < <(y '.["publish-workflows"][] | tostring' "$CONFIG")
grep -qx release.yml <<<"$PUBLISH_WFS" || cfg_err "publish-workflows must list release.yml"
CI_WF=$(y '.ci.workflow // ""' "$CONFIG")
CI_JOB=$(y '.ci.job // ""' "$CONFIG")
[[ $CI_WF =~ ^[A-Za-z0-9._-]+\.ya?ml$ ]] || cfg_err "ci.workflow must be a workflow file name"
[[ $CI_JOB =~ ^[A-Za-z0-9_-]+$ ]] || cfg_err "ci.job must be a job id"
NOTIFY_NEEDS=$(yj '.notify.needs // [] | [.[] | tostring]' "$CONFIG")
NOTIFY_IF=$(y '.notify.if // ""' "$CONFIG")
NOTIFY_TAG=$(y '.notify.tag // ""' "$CONFIG")
[ "$NOTIFY_NEEDS" != '[]' ] && [ -n "$NOTIFY_IF" ] && [ -n "$NOTIFY_TAG" ] || cfg_err "notify needs needs, if and tag"
if [ "$RECEIVER" = true ]; then
  [ "$(y '.publish["labels-managed"] | type' "$CONFIG")" = '!!bool' ] || cfg_err "publish.labels-managed must be true or false"
  LABELS_MANAGED=$(y '.publish["labels-managed"]' "$CONFIG")
else
  [ "$(y '.publish // "none"' "$CONFIG")" = none ] || cfg_err "publish is only for a receiver"
fi

# More .github references the repo declares, one item each. The only kind is
# resolver, a checkout of .github (opm-operator's module-deps.yml); each
# declared one is held to the fixed references' rules below. Only a receiver
# declares one, at most one per file, and never in a file that already holds
# a fixed reference (a second resolver beside cascade-task.yml's, or a
# checkout inside a key-holding workflow).
EXTRA_REFS=""
if [ "$(y 'has("extra-references")' "$CONFIG")" = true ]; then
  [ "$(y '.["extra-references"] | type' "$CONFIG")" = '!!seq' ] || cfg_err "extra-references must be a list"
  n=$(y '.["extra-references"] | length' "$CONFIG")
  for ((i = 0; i < n; i++)); do
    it="extra-references item $((i + 1))"
    [ "$(I=$i y '.["extra-references"][env(I)] | type' "$CONFIG")" = '!!map' ] || cfg_err "$it is not a map"
    [ "$(I=$i yj '.["extra-references"][env(I)] | keys | sort' "$CONFIG")" = '["file","kind"]' ] || cfg_err "$it must have exactly the keys file and kind"
    [ "$(I=$i y '[.["extra-references"][env(I)][] | type] | unique | join(",")' "$CONFIG")" = '!!str' ] || cfg_err "$it: file and kind must be strings"
    ef=$(I=$i y '.["extra-references"][env(I)].file' "$CONFIG")
    ek=$(I=$i y '.["extra-references"][env(I)].kind' "$CONFIG")
    [[ $ef =~ ^[A-Za-z0-9._-]+\.ya?ml$ ]] || cfg_err "$it: file [$ef] is not a workflow file name"
    [ "$ek" = resolver ] || cfg_err "$it: kind [$ek] is not resolver, the only kind"
    [ "$RECEIVER" = true ] || cfg_err "$it: extra-references is only for a receiver"
    case "$ef" in
      release.yml | deps-cascade.yml | cascade-gates.yml | cascade-task.yml)
        cfg_err "$it: $ef already holds a fixed .github reference" ;;
    esac
    ! grep -qxF -- "$ef resolver" <<<"$EXTRA_REFS" || cfg_err "$it: $ef is declared twice"
    EXTRA_REFS+="$ef resolver"$'\n'
  done
fi

# --- the contract's fixed values ----------------------------------------------

# The stop switch. GitHub compares strings without regard to case, so the
# receiver is live when CASCADE_DRY_RUN is false in any letter case (False,
# FALSE); the expression itself is pinned in lower case.
DRY='${{ inputs.dry_run == true || vars.CASCADE_DRY_RUN != '\''false'\'' }}'
GATES_ONLY='${{ inputs.gates_only == true }}'
GROUP='${{ github.ref != '\''refs/heads/main'\'' && format('\''deps-cascade-{0}'\'', github.ref) || (inputs.gates_only && '\''deps-cascade-gates'\'' || '\''deps-cascade'\'') }}'
# A gates-only run never publishes: the caller reads its own input, never an
# output of the reusable job, which ran repo code.
PUBLISH_IF='!cancelled() && needs.cascade.outputs.compute-ok == '\''true'\'' && needs.cascade.outputs.dry-run == '\''false'\'' && inputs.dry_run != true && inputs.gates_only != true && vars.CASCADE_DRY_RUN == '\''false'\'' && github.ref == '\''refs/heads/main'\'' && contains(fromJSON('\''["push","recreate","close","conflict","too_long"]'\''), needs.cascade.outputs.action)'

# A key-holding job has exactly these keys: no env, container, services,
# defaults or strategy, so nothing from the repo can run while it holds the key.
JOB_KEYS='["environment","if","name","needs","permissions","runs-on","steps","timeout-minutes"]'

# needs_of <file> <job>: the job's needs as a one-line JSON list (a single
# string counts as a list of one).
needs_of() { J="$2" yq -o=json -I=0 '.jobs[strenv(J)].needs // [] | [] + . | [.[] | tostring]' "$1"; }

# key_job <file> <job> <action> <permissions JSON> <with keys JSON> <name> <timeout>
# A job that holds the App key: the cascade Environment, exactly one step (the
# SHA-pinned cascade action, with no env, if or shell of its own), the key and
# client id only as that step's inputs, and no input the contract does not pass.
key_job() {
  local f=$W/$1 j=$2 n=$1:$2
  if [ ! -f "$f" ]; then bad "$1 is missing"; return 0; fi
  eq "$n keys" "$JOB_KEYS" "$(J="$j" yj '.jobs[strenv(J)] // {} | keys | sort' "$f")"
  eq "$n name" "$6" "$(J="$j" y '.jobs[strenv(J)].name' "$f")"
  eq "$n timeout-minutes" "$7" "$(J="$j" y '.jobs[strenv(J)]["timeout-minutes"]' "$f")"
  eq "$n environment" cascade "$(J="$j" y '.jobs[strenv(J)].environment' "$f")"
  eq "$n runs-on" ubuntu-latest "$(J="$j" y '.jobs[strenv(J)]["runs-on"]' "$f")"
  eq "$n permissions" "$4" "$(J="$j" yj '.jobs[strenv(J)].permissions | sort_keys(.)' "$f")"
  eq "$n step count" 1 "$(J="$j" y '.jobs[strenv(J)].steps | length' "$f")"
  eq "$n step keys" '["name","uses","with"]' "$(J="$j" yj '.jobs[strenv(J)].steps[0] // {} | keys | sort' "$f")"
  eq "$n step name" "$6" "$(J="$j" y '.jobs[strenv(J)].steps[0].name' "$f")"
  eq "$n with keys" "$5" "$(J="$j" yj '.jobs[strenv(J)].steps[0].with // {} | keys | sort' "$f")"
  re "$n uses" "^open-platform-model/\.github/\.github/actions/$3@[0-9a-f]{40}\$" "$(J="$j" y '.jobs[strenv(J)].steps[0].uses' "$f")"
  eq "$n client-id" '${{ vars.CASCADE_APP_CLIENT_ID }}' "$(J="$j" y '.jobs[strenv(J)].steps[0].with["client-id"]' "$f")"
  eq "$n private-key" '${{ secrets.CASCADE_APP_PRIVATE_KEY }}' "$(J="$j" y '.jobs[strenv(J)].steps[0].with["private-key"]' "$f")"
}

# --- YAML anchors -------------------------------------------------------------

# GitHub resolves anchors and aliases in workflow files, and every check
# below reads the text a key holds: `environment: *e` would read as the
# string "*e". So no workflow file may use either (a merge key `<<: *m` is an
# alias too).
for f in "$W"/*.yml "$W"/*.yaml; do
  [ -e "$f" ] || continue
  eq "${f##*/} YAML anchors and aliases" 0 "$(yq '[.. | select(kind == "alias" or anchor != "")] | length' "$f")"
done

# --- notify -------------------------------------------------------------------

r=$W/release.yml
key_job release.yml notify-downstream cascade-notify '{"contents":"read"}' '["client-id","private-key","tag"]' 'Notify downstream' 20
if [ -f "$r" ]; then
  eq "release.yml:notify-downstream needs" "$NOTIFY_NEEDS" "$(needs_of "$r" notify-downstream)"
  eq "release.yml:notify-downstream if" "$NOTIFY_IF" "$(y '.jobs["notify-downstream"].if' "$r")"
  eq "release.yml:notify-downstream tag" "$NOTIFY_TAG" "$(y '.jobs["notify-downstream"].steps[0].with.tag' "$r")"
  # Workflow-level env reaches the notify action's steps, so its keys come
  # from env-allow: a deny-list alone would miss a variable that makes a bash
  # step holding the token run code. The env must be a plain map, so an
  # expression cannot hide its keys.
  re "release.yml env type" '^!!(null|map)$' "$(y '.env | tag' "$r")"
  extra=""
  while IFS= read -r k; do
    [ -n "$k" ] || continue
    [[ $ENV_ALLOW == *" $k "* ]] || extra+="$k "
  done < <(y '.env // {} | select(tag == "!!map") | keys | .[]' "$r")
  eq "release.yml env keys outside env-allow [${ENV_ALLOW# }]" "" "${extra% }"
fi

# --- the receiver -------------------------------------------------------------

if [ "$RECEIVER" = true ]; then
  d=$W/deps-cascade.yml
  key_job deps-cascade.yml publish cascade-publish '{"contents":"read","pull-requests":"read"}' \
    '["client-id","dry-run","gates-only","labels-managed","private-key"]' Publish 15
  if [ -f "$d" ]; then
    eq "deps-cascade.yml top-level keys" '["concurrency","jobs","name","on","permissions"]' "$(yj 'keys | sort' "$d")"
    eq "deps-cascade.yml permissions" '{}' "$(yj '.permissions' "$d")"
    eq "deps-cascade.yml triggers" '["repository_dispatch","schedule","workflow_dispatch"]' "$(yj '.on | keys | sort' "$d")"
    eq "deps-cascade.yml repository_dispatch types" '["upstream-released"]' "$(yj '.on.repository_dispatch.types' "$d")"
    eq "deps-cascade.yml dispatch inputs" '{"dry_run":"boolean","gates_only":"boolean"}' \
      "$(yj '.on.workflow_dispatch.inputs // {} | with_entries(.value = .value.type) | sort_keys(.)' "$d")"
    eq "deps-cascade.yml jobs" '["cascade","publish"]' "$(yj '.jobs | keys | sort' "$d")"
    eq "deps-cascade.yml concurrency.group" "$GROUP" "$(y '.concurrency.group' "$d")"
    eq "deps-cascade.yml concurrency.cancel-in-progress" false "$(y '.concurrency["cancel-in-progress"]' "$d")"
    eq "deps-cascade.yml cascade keys" '["name","permissions","uses","with"]' "$(yj '.jobs.cascade | keys | sort' "$d")"
    eq "deps-cascade.yml cascade permissions" '{"contents":"read","pull-requests":"read","statuses":"write"}' \
      "$(yj '.jobs.cascade.permissions | sort_keys(.)' "$d")"
    re "deps-cascade.yml cascade uses" '^open-platform-model/\.github/\.github/workflows/cascade-receive\.yml@[0-9a-f]{40}$' "$(y '.jobs.cascade.uses' "$d")"
    extra=$(y '.jobs.cascade.with // {} | keys | .[]' "$d" | grep -vxE 'dry-run|gates-only|g2-mode|g3-mode|setup-go|setup-cue|cue-version' || true)
    eq "deps-cascade.yml cascade inputs cascade-receive.yml does not take" "" "${extra//$'\n'/ }"
    eq "deps-cascade.yml cascade dry-run" "$DRY" "$(y '.jobs.cascade.with["dry-run"]' "$d")"
    eq "deps-cascade.yml cascade gates-only" "$GATES_ONLY" "$(y '.jobs.cascade.with["gates-only"]' "$d")"
    eq "deps-cascade.yml publish needs" '["cascade"]' "$(needs_of "$d" publish)"
    eq "deps-cascade.yml publish if" "$PUBLISH_IF" "$(y '.jobs.publish.if' "$d")"
    eq "deps-cascade.yml publish dry-run" "$DRY" "$(y '.jobs.publish.steps[0].with["dry-run"]' "$d")"
    eq "deps-cascade.yml publish gates-only" "$GATES_ONLY" "$(y '.jobs.publish.steps[0].with["gates-only"]' "$d")"
    eq "deps-cascade.yml publish labels-managed" "$LABELS_MANAGED" "$(yj '.jobs.publish.steps[0].with["labels-managed"]' "$d")"
  else
    bad "deps-cascade.yml is missing"
  fi
  c=$W/cascade-gates.yml
  if [ -f "$c" ]; then
    eq "cascade-gates.yml top-level keys" '["concurrency","jobs","name","on","permissions"]' "$(yj 'keys | sort' "$c")"
    eq "cascade-gates.yml permissions" '{}' "$(yj '.permissions' "$c")"
    eq "cascade-gates.yml triggers" '{"pull_request_target":{"types":["opened","reopened","synchronize"]}}' "$(yj '.on' "$c")"
    eq "cascade-gates.yml jobs" '["gates"]' "$(yj '.jobs | keys' "$c")"
    eq "cascade-gates.yml gates keys" '["name","permissions","uses","with"]' "$(yj '.jobs.gates | keys | sort' "$c")"
    eq "cascade-gates.yml gates permissions" '{"actions":"write","statuses":"write"}' "$(yj '.jobs.gates.permissions | sort_keys(.)' "$c")"
    re "cascade-gates.yml gates uses" '^open-platform-model/\.github/\.github/workflows/cascade-gates\.yml@[0-9a-f]{40}$' "$(y '.jobs.gates.uses' "$c")"
  else
    bad "cascade-gates.yml is missing"
  fi
fi

# --- who reads the key --------------------------------------------------------

# Only the caller-owned jobs above read the key or declare the cascade
# Environment, and no call into .github passes secrets (inherit included).
# GitHub matches secret, Environment, owner and repo names without regard to
# case, so every match here ignores case. A key reader is any expression (a
# ${{ }} in any string, or a whole if: value) that names
# secrets.CASCADE_APP_PRIVATE_KEY, or that uses the secrets context any other
# way than secrets.<name>: secrets[...] (whatever the index, format() too),
# secrets.* and secrets passed whole to a function such as toJSON(secrets).
# An environment (string or map) that mentions cascade in any case, or is an
# expression, counts as declaring the cascade Environment.
want_key="release.yml:jobs.notify-downstream.steps.0.with.private-key"
want_env="release.yml:notify-downstream"
if [ "$RECEIVER" = true ]; then
  want_key=$(printf '%s\n%s' "deps-cascade.yml:jobs.publish.steps.0.with.private-key" "$want_key")
  want_env=$(printf '%s\n%s' "deps-cascade.yml:publish" "$want_env")
fi
GH_REF_RE='(?i)^open-platform-model/\.github/'
got_key="" got_env="" got_sec=""
for f in "$W"/*.yml "$W"/*.yaml; do
  [ -e "$f" ] || continue
  b=${f##*/}
  got_key+=$(yq -r '.. | select(tag == "!!str") | select(
      ([match("(?s)\\$\\{\\{.*?\\}\\}"; "g") | .string] + [select((path | .[-1] | tostring) == "if")])
      | ((map(select(test("(?i)secrets\\s*\\.\\s*cascade_app_private_key"))) | length)
        + (map(sub("(?i)secrets\\s*\\.\\s*[A-Za-z_][A-Za-z0-9_-]*"; "")) | map(select(test("(?i)(^|[^A-Za-z0-9_.])secrets([^A-Za-z0-9_-]|$)"))) | length)) > 0
    ) | path | join(".")' "$f" | sed "s|^|$b:|")$'\n'
  got_env+=$(yq -r '.jobs // {} | to_entries[] | select(.value.environment // "" | tostring | test("(?i)cascade|\\$\\{\\{")) | .key' "$f" | sed "s|^|$b:|")$'\n'
  got_sec+=$(R="$GH_REF_RE" yq -r '.jobs // {} | to_entries[] | select((.value.uses // "") | test(strenv(R))) | select(.value | has("secrets")) | .key' "$f" | sed "s|^|$b:|")$'\n'
done
eq "secrets.CASCADE_APP_PRIVATE_KEY readers" "$want_key" "$(printf '%s' "$got_key" | sed '/^$/d' | sort)"
eq "cascade Environment jobs" "$want_env" "$(printf '%s' "$got_env" | sed '/^$/d' | sort)"
eq "calls into .github that pass secrets" "" "$(printf '%s' "$got_sec" | sed '/^$/d')"

# --- the release key ----------------------------------------------------------

# The release App key (RELEASE_APP_PRIVATE_KEY, owner decision 29) is an
# Environment secret of the main-only Environment `release`. A job reads it
# when any expression (a ${{ }} in a string, or a whole if:) names it in any
# case, or when it passes `secrets: inherit`; each such job declares exactly
# `environment: release`, and no other job declares `release` (any case, a
# string or a map's name). A reusable-workflow call cannot declare an
# Environment, so `secrets: inherit` always fails here. The other forms of
# the secrets context are already refused above. A key read outside a job
# (a workflow-level env) is refused too.
rel_read="" rel_env=""
for f in "$W"/*.yml "$W"/*.yaml; do
  [ -e "$f" ] || continue
  b=${f##*/}
  rel_read+=$(yq -r '.. | select(tag == "!!str") | select(
      ([match("(?s)\\$\\{\\{.*?\\}\\}"; "g") | .string] + [select((path | .[-1] | tostring) == "if")])
      | map(select(test("(?i)secrets\\s*\\.\\s*release_app_private_key"))) | length > 0
    ) | path | ((select(.[0] == "jobs") | .[1] | tostring) // ("<outside a job: " + join(".") + ">"))' "$f" | sed "s|^|$b:|")$'\n'
  rel_read+=$(yq -r '.jobs // {} | to_entries[] | select((.value.secrets // "") == "inherit") | .key' "$f" | sed "s|^|$b:|")$'\n'
  rel_env+=$(yq -r '.jobs // {} | to_entries[] | .key + "\t" + ((.value.environment | (select(tag == "!!map") | .name // "") // (select(tag == "!!str"))) // "") + "\t" + (.value.environment | tag)' "$f" | sed "s|^|$b:|")$'\n'
done
rel_read=$(printf '%s' "$rel_read" | sed '/^$/d' | sort -u)
no_env="" stray_env=""
while IFS= read -r j; do
  [ -n "$j" ] || continue
  e=$(printf '%s\n' "$rel_env" | awk -F'\t' -v j="$j" '$1 == j { print $2 "/" $3; exit }')
  [ "$e" = 'release/!!str' ] || no_env+="$j "
done <<<"$rel_read"
while IFS=$'\t' read -r j e _; do
  [ -n "$j" ] || continue
  shopt -s nocasematch
  if [[ $e == release ]] && ! grep -qxF -- "$j" <<<"$rel_read"; then stray_env+="$j "; fi
  shopt -u nocasematch
done <<<"$rel_env"
eq "release key readers without environment: release" "" "${no_env% }"
eq "environment: release on jobs that do not read the release key" "" "${stray_env% }"

# --- publish workflows restore no cache ---------------------------------------

# Repo code that runs on main (compute, any CI job) can write the Actions
# cache of main's scope: a setup-go or buildx cache step saves after it ran,
# and the artifact runtime token it can read also allows cache writes. A
# publish job that restores that cache builds from it. So in every workflow
# the config lists as publishing: no cache action (any action whose name
# says cache), setup-go with cache: false, setup-node with
# package-manager-cache: false and no cache, no other cache input but
# no-cache, and no type=gha anywhere (buildx cache-from/cache-to). A
# reusable workflow called from there is not checked; docs-kit's sets
# cache: false.
while IFS= read -r pw; do
  [ -n "$pw" ] || continue
  f=$W/$pw
  if [ ! -f "$f" ]; then bad "publish workflow $pw is missing"; continue; fi
  eq "$pw cache use" "" "$(yq -r '
    (.jobs[]?.steps[]? | select(.uses // "" | sub("@.*$"; "") | test("(?i)cache")) | (path | join(".")) + " uses " + .uses),
    (.jobs[]?.steps[]? | select(.uses // "" | sub("@.*$"; "") | test("(?i)^actions/setup-go$"))
      | select((.with.cache | tostring) != "false") | (path | join(".")) + " setup-go without cache: false"),
    (.jobs[]?.steps[]? | select(.uses // "" | sub("@.*$"; "") | test("(?i)^actions/setup-node$"))
      | select((.with["package-manager-cache"] | tostring) != "false" or (.with.cache // "") != "")
      | (path | join(".")) + " setup-node without package-manager-cache: false, or with cache"),
    (.jobs[]?.steps[]? | select(.uses // "" | sub("@.*$"; "") | test("(?i)^actions/setup-(go|node)$") | not)
      | .with[]? | select(path | .[-1] | tostring | test("(?i)cache")) | select(path | .[-1] | tostring | test("(?i)^no-cache$") | not)
      | path | join(".")),
    (.. | select(tag == "!!str") | select(test("(?i)type\\s*=\\s*gha")) | "type=gha at " + (path | join(".")))
  ' "$f" | sort -u | tr '\n' ';' | sed 's/;$//')"
done <<<"$PUBLISH_WFS"

# --- the pin ------------------------------------------------------------------

# Every .github reference (the uses: lines and the cascade-task.yml resolver
# ref), in any letter case, carries one full SHA and the pin comment; a
# reference spelled in another case shows up as an unexpected one. A checkout
# counts as one of .github when its repository is any owner's .github or an
# expression (which could name it, ${{ github.repository_owner }}/.github),
# so it is held to the pin and the checkout rule below.
GH_CO_RE='(?i)(^|/)\.github$|\$\{\{'
refs=""
for f in "$W"/*.yml "$W"/*.yaml; do
  [ -e "$f" ] || continue
  b=${f##*/}
  refs+=$(R="$GH_REF_RE" CO="$GH_CO_RE" yq -r '
    (.jobs // {} | to_entries[] | select((.value.uses // "") | test(strenv(R))) | .value.uses + " " + (.value.uses | line_comment)),
    (.jobs // {} | to_entries[] | (.value.steps // [])[] | select((.uses // "") | test(strenv(R))) | .uses + " " + (.uses | line_comment)),
    (.jobs // {} | to_entries[] | (.value.steps // [])[] | select((.with.repository // "") | tostring | test(strenv(CO))) | "resolver@" + (.with.ref // "") + " " + ((.with.ref // "") | line_comment))
  ' "$f" | sed "s|^|$b |")$'\n'
done
refs=$(printf '%s' "$refs" | sed '/^$/d' | sort)
# "<file> <target>" with the SHA and comment stripped.
want_refs="release.yml open-platform-model/.github/.github/actions/cascade-notify"
if [ "$RECEIVER" = true ]; then
  want_refs=$(printf '%s\n' \
    "cascade-gates.yml open-platform-model/.github/.github/workflows/cascade-gates.yml" \
    "cascade-task.yml resolver" \
    "deps-cascade.yml open-platform-model/.github/.github/actions/cascade-publish" \
    "deps-cascade.yml open-platform-model/.github/.github/workflows/cascade-receive.yml" \
    "release.yml open-platform-model/.github/.github/actions/cascade-notify" | sort)
fi
want_refs=$(printf '%s\n%s' "$want_refs" "$EXTRA_REFS" | sed '/^$/d' | sort)
eq ".github references" "$want_refs" "$(printf '%s\n' "$refs" | sed -E 's/@[^ ]* .*$//' | sort)"
shas=$(printf '%s\n' "$refs" | sed -E 's/^[^ ]+ [^@]*@([^ ]*) .*$/\1/' | sort -u)
re "one .github SHA" '^[0-9a-f]{40}$' "$shas"
while IFS= read -r line; do
  [ -n "$line" ] || continue
  eq "pin comment on [${line% *}]" "$PIN_COMMENT" "$(printf '%s' "$line" | sed -E 's/^[^ ]+ [^ ]+ //')"
done <<<"$refs"

# Every .github checkout (the resolver, fixed or declared) is actions/checkout
# at a full SHA with exactly repository, ref, path and persist-credentials:
# false, so no token or SSH key reaches it.
bad_co=""
for f in "$W"/*.yml "$W"/*.yaml; do
  [ -e "$f" ] || continue
  b=${f##*/}
  bad_co+=$(CO="$GH_CO_RE" yq -r '.jobs // {} | to_entries[] | .key as $j | (.value.steps // []) | to_entries[]
    | select((.value.with.repository // "") | tostring | test(strenv(CO)))
    | select(((.value.uses // "") | test("^actions/checkout@[0-9a-f]{40}$") | not)
      or ((.value.with | keys | sort | join(",")) != "path,persist-credentials,ref,repository")
      or ((.value.with["persist-credentials"] | tag) != "!!bool")
      or (.value.with["persist-credentials"] != false))
    | $j + ".steps." + (.key | tostring)' "$f" | sed "s|^|$b:|")$'\n'
done
eq ".github checkouts not actions/checkout@<sha> with only repository, ref, path and persist-credentials: false" \
  "" "$(printf '%s' "$bad_co" | sed '/^$/d' | tr '\n' ' ' | sed 's/ $//')"

# A repository input that is an expression could name .github while the
# pinned references above cannot tell; and a run: step that clones or
# downloads .github bypasses the checkout rule. No repo needs either.
expr_repo="" run_fetch=""
for f in "$W"/*.yml "$W"/*.yaml; do
  [ -e "$f" ] || continue
  b=${f##*/}
  expr_repo+=$(yq -r '.jobs // {} | to_entries[] | .key as $j | (.value.steps // []) | to_entries[]
    | select((.value.with.repository // "") | tostring | test("\\$\\{\\{")) | $j + ".steps." + (.key | tostring)' "$f" | sed "s|^|$b:|")$'\n'
  run_fetch+=$(yq -r '.jobs // {} | to_entries[] | .key as $j | (.value.steps // []) | to_entries[]
    | select((.value.run // "") | tostring
      | test("(?i)open-platform-model/\\.github|(\\}\\}|repository_owner\\}?)\\s*/\\.github(\\.git)?([^A-Za-z0-9_./-]|$)"))
    | $j + ".steps." + (.key | tostring)' "$f" | sed "s|^|$b:|")$'\n'
done
eq "steps whose repository input is an expression" "" "$(printf '%s' "$expr_repo" | sed '/^$/d' | tr '\n' ' ' | sed 's/ $//')"
eq "run: steps that fetch .github outside the pinned checkouts" "" "$(printf '%s' "$run_fetch" | sed '/^$/d' | tr '\n' ' ' | sed 's/ $//')"

# --- the check runs on every PR -----------------------------------------------

# The required CI job runs this check, online, as a plain step: on every pull
# request (no path filter, which would leave the required check unreported),
# with no if: or continue-on-error that would let a failure pass, and with no
# shell or working directory of its own or from defaults that would run
# something else.
CI_RUN='bash .tasks/cascade/wiring-check.sh --pin-on-main'
CI_ENV='{"GH_TOKEN":"${{ github.token }}"}'
ci=$W/$CI_WF
if [ -f "$ci" ]; then
  eq "$CI_WF runs on pull_request" true "$(y '.on | has("pull_request")' "$ci")"
  eq "$CI_WF pull_request path filters" '[]' "$(yj '[.on.pull_request // {} | keys | .[] | select(. == "paths" or . == "paths-ignore")]' "$ci")"
  eq "$CI_WF:$CI_JOB exists" true "$(J="$CI_JOB" y '.jobs | has(strenv(J))' "$ci")"
  eq "$CI_WF:$CI_JOB if and continue-on-error" '[]' "$(J="$CI_JOB" yj '[.jobs[strenv(J)] // {} | keys | .[] | select(. == "if" or . == "continue-on-error")]' "$ci")"
  eq "$CI_WF defaults.run" '[]' "$(J="$CI_JOB" yj '[(.defaults.run // {} | keys | .[]), (.jobs[strenv(J)].defaults.run // {} | keys | .[])]' "$ci")"
  eq "$CI_WF:$CI_JOB steps running [$CI_RUN]" 1 \
    "$(J="$CI_JOB" C="$CI_RUN" y '[.jobs[strenv(J)].steps // [] | .[] | select(.run == strenv(C))] | length' "$ci")"
  eq "$CI_WF:$CI_JOB wiring step keys" '["env","name","run"]' \
    "$(J="$CI_JOB" C="$CI_RUN" yj '[.jobs[strenv(J)].steps // [] | .[] | select(.run == strenv(C))][0] // {} | keys | sort' "$ci")"
  eq "$CI_WF:$CI_JOB wiring step env" "$CI_ENV" \
    "$(J="$CI_JOB" C="$CI_RUN" yj '[.jobs[strenv(J)].steps // [] | .[] | select(.run == strenv(C))][0].env' "$ci")"
  # Nothing reaches the step from around it. The workflow's and the job's env
  # (plain maps, names from the fixed env-allow names only) would hand it
  # BASH_ENV, CASCADE_GH, PATH and the rest; a container or a service (which
  # can mount the workspace) runs it elsewhere or beside it; and a run: step
  # before it could write GITHUB_ENV or GITHUB_PATH, or leave a process behind
  # that swaps the file. So the steps before it are only SHA-pinned actions
  # from another repo, with no env, if or shell of their own.
  for scope in "" "jobs[strenv(J)]."; do
    lbl="$CI_WF${scope:+:$CI_JOB} env"
    re "$lbl type" '^!!(null|map)$' "$(J="$CI_JOB" y ".${scope}env | tag" "$ci")"
    eq "$lbl keys outside CUE_*, OPM_*, REGISTRY and IMAGE_NAME" "" \
      "$(J="$CI_JOB" y ".${scope}env // {} | select(tag == \"!!map\") | keys | .[]" "$ci" | grep -vE "$ENV_ALLOW_RE" | tr '\n' ' ' | sed 's/ $//' || true)"
  done
  eq "$CI_WF:$CI_JOB container and services" '[]' "$(J="$CI_JOB" yj '[.jobs[strenv(J)] // {} | keys | .[] | select(. == "container" or . == "services")]' "$ci")"
  eq "$CI_WF:$CI_JOB steps before the wiring step that are not a pinned action with only id, name, uses and with" "" \
    "$(J="$CI_JOB" C="$CI_RUN" yq -r '.jobs[strenv(J)].steps // [] | to_entries
      | (map(select(.value.run == strenv(C))) | .[0].key // 0) as $w
      | .[] | select(.key < $w)
      | select(((.value.uses // "") | test("^[A-Za-z0-9][A-Za-z0-9_.-]*/[A-Za-z0-9_./-]+@[0-9a-f]{40}$") | not)
        or ([.value | keys | .[] | select(. != "id" and . != "name" and . != "uses" and . != "with")] | length > 0))
      | "steps." + (.key | tostring)' "$ci" | tr '\n' ' ' | sed 's/ $//')"
else
  bad "$CI_WF is missing"
fi

[ "$fail" = 0 ] || exit 1

# --- the pin is on .github main -------------------------------------------------

# A SHA GitHub resolves under open-platform-model/.github may exist only in a
# fork of it. Comparing it with main proves it is main or one of main's
# ancestors: identical or ahead (main is ahead of it); behind or diverged is
# a commit main never had.
if [ "$PIN_ON_MAIN" = true ]; then
  st=$("${CASCADE_GH:-gh}" api "repos/open-platform-model/.github/compare/$shas...main" --jq .status) || {
    echo "cascade wiring: cannot compare .github $shas with main" >&2
    exit 1
  }
  case "$st" in
    identical | ahead) ;;
    *)
      echo "cascade wiring: .github $shas is not on .github main (compare status [$st])" >&2
      exit 1
      ;;
  esac
fi

# --- the copy is the file at the pin --------------------------------------------

# The canonical file at the pinned SHA, as raw bytes, compared with the file
# bash is running. Only after the SHA was found on main, so a fork-only
# commit is never fetched.
CANON=.github/scripts/cascade/wiring-check.sh
if [ "$PIN_ON_MAIN" = true ]; then
  pinned=$(mktemp)
  trap 'rm -f "$pinned"' EXIT
  "${CASCADE_GH:-gh}" api -H 'Accept: application/vnd.github.raw' \
    "repos/open-platform-model/.github/contents/$CANON?ref=$shas" >"$pinned" || {
    echo "cascade wiring: cannot fetch $CANON at .github $shas" >&2
    exit 1
  }
  cmp -s "$pinned" "$SELF" || {
    echo "cascade wiring: $SELF differs from $CANON at .github $shas; replace it with that file (.github README, \"Keeping the copy in sync\")" >&2
    exit 1
  }
fi
echo "cascade wiring: ok, .github $shas ($PIN_COMMENT)"
[ "$PIN_ON_MAIN" = true ] || echo "cascade wiring: the copy was not compared with .github $shas (offline; --pin-on-main compares it)"
