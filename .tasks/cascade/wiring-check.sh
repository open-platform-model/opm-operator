#!/usr/bin/env bash
# Checks the release-cascade caller shapes on every PR (Phase 3 wiring contract
# version 3.1, sections 5.2 and 10.1, with the supervisor's addendum: an
# allow-list for release.yml's workflow env and runs-on for the key-holding
# jobs; and this repo's needs, if, tag and timeout-minutes of those jobs). Run
# from the repo root by `task cascade:wiring:check`, a step of the
# required Lint job; needs mikefarah yq v4. Prints every mismatch, then exits 1
# if there was one. It guards against mistakes; review and the main ruleset
# guard against a deliberate edit, which could change this file too.
# shellcheck disable=SC2016 # the single-quoted ${{ }} strings are GitHub expressions, compared literally
set -euo pipefail

RECEIVER=true              # core: false (notify only)
PIN_COMMENT='.github main' # before A merges: 'feat/add-release-cascade-workflows'
# The only keys release.yml's workflow-level env may have (this repo's
# registry, image name and CUE version); anything else, BASH_ENV, ENV and
# NODE_OPTIONS included, fails.
ENV_ALLOW='REGISTRY IMAGE_NAME CUE_VERSION'

W=.github/workflows
yq --version | grep -q mikefarah || { echo "mikefarah yq v4 is required" >&2; exit 1; }

fail=0
bad() { echo "cascade wiring: $*" >&2; fail=1; }
eq() { [ "$2" = "$3" ] || bad "$1: expected [$2], got [$3]"; }
re() { [[ $3 =~ $2 ]] || bad "$1: [$3] does not match $2"; }

DRY='${{ inputs.dry_run == true || vars.CASCADE_DRY_RUN != '\''false'\'' }}'
GROUP='${{ github.ref != '\''refs/heads/main'\'' && format('\''deps-cascade-{0}'\'', github.ref) || (inputs.gates_only && '\''deps-cascade-gates'\'' || '\''deps-cascade'\'') }}'
PUBLISH_IF='!cancelled() && needs.cascade.outputs.compute-ok == '\''true'\'' && needs.cascade.outputs.dry-run == '\''false'\'' && inputs.dry_run != true && vars.CASCADE_DRY_RUN == '\''false'\'' && github.ref == '\''refs/heads/main'\'' && contains(fromJSON('\''["push","recreate","close","conflict","too_long"]'\''), needs.cascade.outputs.action)'

# A key-holding job has exactly these keys: no env, container, services,
# defaults or strategy, so nothing from the repo can run while it holds the key.
JOB_KEYS='["environment","if","name","needs","permissions","runs-on","steps","timeout-minutes"]'

# key_job <file> <job> <action> <permissions as sorted one-line JSON> <with keys as sorted one-line JSON>
# A job that holds the App key: the cascade Environment, exactly one step (the
# SHA-pinned cascade action, with no env, if or shell of its own), the key and
# client id only as that step's inputs, and no input the contract does not pass.
key_job() {
  local f=$W/$1 j=$2 n=$1:$2
  eq "$n keys" "$JOB_KEYS" "$(yq -o=json -I=0 ".jobs[\"$j\"] // {} | keys | sort" "$f")"
  eq "$n step keys" '["name","uses","with"]' "$(yq -o=json -I=0 ".jobs[\"$j\"].steps[0] // {} | keys | sort" "$f")"
  eq "$n with keys" "$5" "$(yq -o=json -I=0 ".jobs[\"$j\"].steps[0].with // {} | keys | sort" "$f")"
  eq "$n environment" cascade "$(yq -r ".jobs[\"$j\"].environment" "$f")"
  eq "$n runs-on" ubuntu-latest "$(yq -r ".jobs[\"$j\"][\"runs-on\"]" "$f")"
  eq "$n permissions" "$4" "$(yq -o=json -I=0 ".jobs[\"$j\"].permissions | sort_keys(.)" "$f")"
  eq "$n step count" 1 "$(yq -r ".jobs[\"$j\"].steps | length" "$f")"
  re "$n uses" "^open-platform-model/\.github/\.github/actions/$3@[0-9a-f]{40}\$" "$(yq -r ".jobs[\"$j\"].steps[0].uses" "$f")"
  eq "$n client-id" '${{ vars.CASCADE_APP_CLIENT_ID }}' "$(yq -r ".jobs[\"$j\"].steps[0].with[\"client-id\"]" "$f")"
  eq "$n private-key" '${{ secrets.CASCADE_APP_PRIVATE_KEY }}' "$(yq -r ".jobs[\"$j\"].steps[0].with[\"private-key\"]" "$f")"
}

key_job release.yml notify-downstream cascade-notify '{"contents":"read"}' '["client-id","private-key","tag"]'
# The notify job's values (contract 4.6, opm-operator): it waits for
# publish-release, because the cli's release resolver needs the published
# release with install.yaml. Its if reads the operator package's own
# release_created, not the contract's releases_created: with the operator
# module as a second release-please package, releases_created is also true
# for a module-only release, whose root tag_name is empty
# (release-operator-module).
r=$W/release.yml
eq "release.yml:notify-downstream needs" '["release-please","publish-release"]' "$(yq -o=json -I=0 '.jobs.notify-downstream.needs' "$r")"
eq "release.yml:notify-downstream if" "needs.release-please.outputs.release_created == 'true' && vars.CASCADE_NOTIFY != 'off'" "$(yq -r '.jobs.notify-downstream.if' "$r")"
eq "release.yml:notify-downstream tag" '${{ needs.release-please.outputs.tag_name }}' "$(yq -r '.jobs.notify-downstream.steps[0].with.tag' "$r")"
eq "release.yml:notify-downstream timeout-minutes" 20 "$(yq -r '.jobs.notify-downstream["timeout-minutes"]' "$r")"
# Workflow-level env reaches the notify action's steps, so it is a map whose
# keys are all in ENV_ALLOW: nothing there can make a shell or node run code
# at startup.
eq "release.yml env type" '!!map' "$(yq -r '.env // {} | tag' "$W/release.yml")"
extra=""
while IFS= read -r k; do
  [ -n "$k" ] || continue
  [[ " $ENV_ALLOW " == *" $k "* ]] || extra+="$k "
done < <(yq -r '.env // {} | select(tag == "!!map") | keys | .[]' "$W/release.yml")
eq "release.yml env keys outside [$ENV_ALLOW]" "" "${extra% }"
if [ "$RECEIVER" = true ]; then
  key_job deps-cascade.yml publish cascade-publish '{"contents":"read","pull-requests":"read"}' '["client-id","dry-run","labels-managed","private-key"]'
  d=$W/deps-cascade.yml
  eq "deps-cascade.yml top-level keys" '["concurrency","jobs","name","on","permissions"]' "$(yq -o=json -I=0 'keys | sort' "$d")"
  eq "deps-cascade.yml concurrency.group" "$GROUP" "$(yq -r '.concurrency.group' "$d")"
  eq "deps-cascade.yml publish if" "$PUBLISH_IF" "$(yq -r '.jobs.publish.if' "$d")"
  eq "deps-cascade.yml publish needs" cascade "$(yq -r '.jobs.publish.needs' "$d")"
  eq "deps-cascade.yml publish timeout-minutes" 15 "$(yq -r '.jobs.publish["timeout-minutes"]' "$d")"
  eq "deps-cascade.yml cascade dry-run" "$DRY" "$(yq -r '.jobs.cascade.with["dry-run"]' "$d")"
  eq "deps-cascade.yml publish dry-run" "$DRY" "$(yq -r '.jobs.publish.steps[0].with["dry-run"]' "$d")"
  re "deps-cascade.yml cascade uses" '^open-platform-model/\.github/\.github/workflows/cascade-receive\.yml@[0-9a-f]{40}$' "$(yq -r '.jobs.cascade.uses' "$d")"
  re "cascade-gates.yml gates uses" '^open-platform-model/\.github/\.github/workflows/cascade-gates\.yml@[0-9a-f]{40}$' "$(yq -r '.jobs.gates.uses' "$W/cascade-gates.yml")"
fi

# Only the caller-owned jobs above read the key or declare the cascade
# Environment, and no call into .github passes secrets (inherit included).
want_key="release.yml:jobs.notify-downstream.steps.0.with.private-key"
want_env="release.yml:notify-downstream"
if [ "$RECEIVER" = true ]; then
  want_key=$(printf '%s\n%s' "deps-cascade.yml:jobs.publish.steps.0.with.private-key" "$want_key")
  want_env=$(printf '%s\n%s' "deps-cascade.yml:publish" "$want_env")
fi
got_key="" got_env="" got_sec=""
for f in "$W"/*.yml "$W"/*.yaml; do
  [ -e "$f" ] || continue
  b=${f##*/}
  got_key+=$(yq -r '.. | select(tag == "!!str" and test("secrets\.CASCADE_APP_PRIVATE_KEY")) | path | join(".")' "$f" | sed "s|^|$b:|")$'\n'
  got_env+=$(yq -r '.jobs // {} | to_entries[] | select(.value.environment == "cascade" or .value.environment.name == "cascade") | .key' "$f" | sed "s|^|$b:|")$'\n'
  got_sec+=$(yq -r '.jobs // {} | to_entries[] | select((.value.uses // "") | test("^open-platform-model/\\.github/")) | select(.value | has("secrets")) | .key' "$f" | sed "s|^|$b:|")$'\n'
done
eq "secrets.CASCADE_APP_PRIVATE_KEY readers" "$want_key" "$(printf '%s' "$got_key" | sed '/^$/d' | sort)"
eq "cascade Environment jobs" "$want_env" "$(printf '%s' "$got_env" | sed '/^$/d' | sort)"
eq "calls into .github that pass secrets" "" "$(printf '%s' "$got_sec" | sed '/^$/d')"

# Every .github reference (the uses: lines and the resolver checkouts' refs in
# cascade-task.yml and module-deps.yml) carries one full SHA and the pin comment.
refs=""
for f in "$W"/*.yml "$W"/*.yaml; do
  [ -e "$f" ] || continue
  b=${f##*/}
  refs+=$(yq -r '
    (.jobs // {} | to_entries[] | select((.value.uses // "") | test("^open-platform-model/\\.github/")) | .value.uses + " " + (.value.uses | line_comment)),
    (.jobs // {} | to_entries[] | (.value.steps // [])[] | select((.uses // "") | test("^open-platform-model/\\.github/")) | .uses + " " + (.uses | line_comment)),
    (.jobs // {} | to_entries[] | (.value.steps // [])[] | select(.with.repository == "open-platform-model/.github") | "resolver@" + (.with.ref // "") + " " + ((.with.ref // "") | line_comment))
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
    "module-deps.yml resolver" \
    "release.yml open-platform-model/.github/.github/actions/cascade-notify" | sort)
fi
eq ".github references" "$want_refs" "$(printf '%s\n' "$refs" | sed -E 's/@[^ ]* .*$//' | sort)"
shas=$(printf '%s\n' "$refs" | sed -E 's/^[^ ]+ [^@]*@([^ ]*) .*$/\1/' | sort -u)
re "one .github SHA" '^[0-9a-f]{40}$' "$shas"
while IFS= read -r line; do
  eq "pin comment on [${line% *}]" "$PIN_COMMENT" "$(printf '%s' "$line" | sed -E 's/^[^ ]+ [^ ]+ //')"
done <<< "$refs"

[ "$fail" = 0 ] || exit 1
echo "cascade wiring: ok, .github $shas ($PIN_COMMENT)"
