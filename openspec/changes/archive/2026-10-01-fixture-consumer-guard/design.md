## Context

Fixture modules live in `test/fixtures/modules/<m>` (`testing.opmodel.dev/modules/operator/<m>`); each `test/fixtures/modulepackages/<m>` is a CUE module that pins its module, core and catalogs/opm. PR CI (`test.yml`) seeds a job-local registry from the tree and runs the registry-backed specs with the mixed mapping. Reconcile phase impact: none (Source, Render, Apply, Prune and Status are untouched); this is test tooling only.

## Goals / Non-Goals

**Goals:**

- `task examples:pin` leaves a modulepackage pinning what its module pins.
- A modulepackage whose pins differ from what CUE resolves fails `test.yml`, naming it.
- The check is the cli's code, unchanged.

**Non-Goals:**

- `moduleinstance.yaml` and `config/samples`: they name a module version, not CUE deps, and `examples:pin` already covers them.

## Decisions

### D1. `examples:pin` copies the module's shared pins (text)

After the existing `v:` rewrite of the module dep, for every dep key in the module's `cue.mod/module.cue` that the modulepackage also lists, the modulepackage's `v:` takes the module's value, printing `pinned <m> modulepackage <dep> <old> -> <new> (follows the module)` when it changes. Text only, like the rest of the task: at bump time the new module version is on no registry, so `cue mod get` cannot run here. This is the workspace script's "follows the module" rule, moved to where the manual path runs.

### D2. `hack/fixtures.sh consumers` as the check

Shared with the cli (its design.md D1 has the full output table and the reference diff). Per consumer, in a scratch copy: `cue mod get <dep>@<pinned>` for every `testing.opmodel.dev` dep (same version, which forces the requirement walk), `cue mod tidy`, `diff -u`. `FAIL <dir>: <reason>` on a difference or any `cue` error; every consumer is checked before the exit; an EXIT trap removes the scratch dir; tracked consumers not listed fail; `FIX=1` writes back.

```yaml
consumers:
  vars:
    CONSUMERS_CUE_REGISTRY: '{{.CONSUMERS_CUE_REGISTRY | default .GHCR_CUE_REGISTRY}}'
  cmds:
    - FIXTURES_DIR={{.MODULES_DIR}} CUE_REGISTRY='{{.CONSUMERS_CUE_REGISTRY}}' FIX='{{.FIX}}' hack/fixtures.sh consumers $(find {{.RELEASES_DIR}} -mindepth 1 -maxdepth 1 -type d | sort)
```

A dedicated `CONSUMERS_CUE_REGISTRY` var, because the root Taskfile's `CUE_REGISTRY` var defaults to an all-local mapping (`opmodel.dev=localhost:5000`), which would send core lookups to a local registry when the caller exports nothing.

### D3. Wiring

`test.yml`: step `Fixture consumers follow their fixtures`, after "Seed the job-local registry from the tree", `run: task examples:consumers CONSUMERS_CUE_REGISTRY="$MIXED_CUE_REGISTRY"`. `dev:test:seeded` calls `:examples:consumers` with the mixed mapping after its seed.

## Research & Decisions

Runs in scratch copies of `main` (`git archive c3e4232`), a throwaway `registry:2` on `127.0.0.1:5593` (removed), empty `HOME` and `DOCKER_CONFIG`, and an `opm` shim that refuses a publish whose `testing.opmodel.dev` mapping is not the throwaway registry.

### Manual bump path

**Context**: is `examples:pin` really stale, and does D1 fix it?
**Explored**: podinfo modulepackage set to core `v2.0.0-alpha.6` and catalogs/opm `v4.0.1` (the state before a module move), `opm module version set 0.1.900` on the module, then `task examples:pin`.
**Decision**: D1.
**Rationale**: with the current task the modulepackage ends on `v0.1.900` with alpha.6 and `v4.0.1`; with D1 it ends on `v0.1.900`, beta.1 and `v4.4.4`, printing both "follows the module" lines.

### Check results

| Run | Result |
| --- | --- |
| `main` modulepackages, GHCR | four `ok`, rc 0 |
| podinfo modulepackage on alpha.6 / `v4.0.1`, GHCR | diff and FAIL for podinfo, the other three `ok`, rc non-zero |
| same, `FIX=1` | podinfo fixed and byte-identical to `main`, rc 0 |
| module seeded at `0.1.900`, modulepackage pinned by the new `examples:pin` | four `ok` |
| module seeded at `0.1.900`, modulepackage pinned by the current `examples:pin` | FAIL with the alpha.6 diff |
| the `0.1.900` pin under GHCR only | `FAIL ... cue mod get ...@v0.1.900 failed`, the other three still checked |

### Alternatives

1. Make `examples:pin` call `cue mod get` + `cue mod tidy` per modulepackage: fails at bump time (the new module version is unpublished) unless a seeded local registry runs, which `AGENTS.md` keeps optional. Rejected for the task; it is what `FIX=1` does when a registry is seeded.
2. Leave the manual path to the check alone: CI would catch it, but every manual bump would go red once first. Rejected.

## Risks / Trade-offs

- [Copy drift] `hack/fixtures.sh` changes in both repos; `task fixtures:lint` and back-to-back merges.
- [GHCR outage] the step fails with the rest of the job.
- [Text layout] D1 assumes the `"<dep>": {` / `v: "..."` layout `cue mod tidy` writes, as the task's existing rewrite already does.

## Appendix A: reference diff of `hack/fixtures.sh`

Identical to the cli change's appendix (applies to `c3e4232`). Section 1 applies it as is.

```diff
--- a/hack/fixtures.sh
+++ b/hack/fixtures.sh
@@ -33,6 +33,18 @@
 #            maps testing.opmodel.dev to (refuses a ghcr.io mapping)
 #   publish  publish the tree's fixtures to CUE_REGISTRY (default: GHCR);
 #            honours SINCE=<git-ref> and PRERELEASE=<id>
+#   consumers <dir>...
+#            check that each consumer (a dir holding a cue.mod that pins a
+#            fixture) pins exactly what CUE resolves for the fixture versions
+#            it names: in a scratch copy, `cue mod get <fixture>@<pinned>` per
+#            testing.opmodel.dev pin, then `cue mod tidy`, diffed against the
+#            committed module.cue. Needed because CUE keeps a dep the consumer
+#            already lists at its listed version: a consumer on a new fixture
+#            but a stale core passes `cue mod tidy --check` and evaluates
+#            against the stale core. Also fails on a tracked cue.mod outside
+#            FIXTURES_DIR that pins a fixture but is not listed. Resolves
+#            through CUE_REGISTRY (default: GHCR; the seeded mapping in PR CI).
+#            FIX=1 writes the resolved module.cue back instead of failing.
 #
 # Environment
 #   FIXTURES_DIR       fixture root; auto-detected (tests/fixtures/modules or
@@ -49,6 +61,7 @@
 #   SINCE              publish: skip fixtures unchanged since this git ref
 #   PRERELEASE         publish: append a SemVer pre-release segment to the tag
 #                      (e.g. e2e.gabc1234) so it never claims the release version
+#   FIX                consumers: 1 rewrites a drifted module.cue in place
 set -euo pipefail
 
 GHCR_REGISTRY='testing.opmodel.dev=ghcr.io/open-platform-model,opmodel.dev=ghcr.io/open-platform-model,registry.cue.works'
@@ -83,6 +96,7 @@
 BASE_REF=${BASE_REF:-origin/main}
 SINCE=${SINCE:-}
 PRERELEASE=${PRERELEASE:-}
+FIX=${FIX:-}
 
 require_tools() {
   command -v cue >/dev/null || die "cue not on PATH"
@@ -257,11 +271,111 @@
   publish_all
 }
 
+# fixture_deps <module.cue>: the testing.opmodel.dev dep keys a cue.mod pins
+# (dep keys only, never the `module:` line).
+fixture_deps() {
+  sed -n 's/^[[:space:]]*"\(testing\.opmodel\.dev\/[^"]*\)":[[:space:]]*{.*$/\1/p' "$1"
+}
+
+# dep_version <module.cue> <dep>: the v: pinned under <dep> (empty when absent).
+dep_version() {
+  awk -v p="\"$2\"" 'index($0, p) {f = 1} f && /v: "/ {match($0, /"[^"]+"/); print substr($0, RSTART + 1, RLENGTH - 2); exit}' "$1"
+}
+
+consumer_fail() {
+  echo "FAIL $1: $2" >&2
+}
+
+cmd_consumers() {
+  command -v cue >/dev/null || die "cue not on PATH"
+  [ "$#" -gt 0 ] || die "consumers: name the consumer dirs (each holds a cue.mod)"
+  export CUE_REGISTRY=${CUE_REGISTRY:-$GHCR_REGISTRY}
+  local scratch dir mod work deps dep ver out rc=0 ok listed f n=0
+  scratch=$(mktemp -d)
+  # shellcheck disable=SC2064 # expand now: $scratch is local to this function
+  trap "rm -rf '$scratch'" EXIT
+  echo "fixture consumers against $(testing_host)"
+  listed=" "
+  for dir in "$@"; do
+    dir=${dir%/}
+    dir=${dir#./}
+    n=$((n + 1))
+    mod="$dir/cue.mod/module.cue"
+    listed="${listed}${mod} "
+    echo "==> ${dir}"
+    if [ ! -f "$mod" ]; then
+      consumer_fail "$dir" "no cue.mod/module.cue"
+      rc=1
+      continue
+    fi
+    work="$scratch/$n"
+    mkdir -p "$work"
+    cp -R "$dir/." "$work/"
+    ok=1
+    deps=$(fixture_deps "$mod")
+    if [ -z "$deps" ]; then
+      consumer_fail "$dir" "pins no testing.opmodel.dev fixture; not a consumer"
+      rc=1
+      continue
+    fi
+    for dep in $deps; do
+      ver=$(dep_version "$mod" "$dep")
+      if [ -z "$ver" ]; then
+        consumer_fail "$dir" "no v: under \"$dep\""
+        ok=0
+        break
+      fi
+      # Same version on purpose: it forces CUE to walk the fixture's own
+      # requirements and raise every shared dep to at least the fixture's pin.
+      if ! out=$(cd "$work" && cue mod get "${dep%@*}@${ver}" 2>&1); then
+        echo "$out" >&2
+        consumer_fail "$dir" "cue mod get ${dep%@*}@${ver} failed (is that version published, or seeded into $(testing_host)?)"
+        ok=0
+        break
+      fi
+    done
+    if [ "$ok" -eq 1 ] && ! out=$(cd "$work" && cue mod tidy 2>&1); then
+      echo "$out" >&2
+      consumer_fail "$dir" "cue mod tidy failed"
+      ok=0
+    fi
+    if [ "$ok" -eq 0 ]; then
+      rc=1
+      continue
+    fi
+    if diff -u --label "$mod (committed)" --label "$mod (resolved)" "$mod" "$work/cue.mod/module.cue"; then
+      echo "    ok"
+    elif [ "$FIX" = "1" ]; then
+      cp "$work/cue.mod/module.cue" "$mod"
+      echo "    fixed: wrote the resolved module.cue"
+    else
+      consumer_fail "$dir" "module.cue differs from what CUE resolves for its fixture pins"
+      echo "     Re-run the workspace root task deps:pins:fixtures, apply the diff above," >&2
+      echo "     or re-run this with FIX=1 against a registry that holds the pinned fixture versions." >&2
+      rc=1
+    fi
+  done
+  # A consumer nobody listed is a consumer nobody checks.
+  if git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
+    while IFS= read -r f; do
+      case "$f" in "$FIXTURES_DIR"/*) continue ;; esac
+      [ -n "$(fixture_deps "$f")" ] || continue
+      case "$listed" in *" $f "*) continue ;; esac
+      consumer_fail "$f" "pins a testing.opmodel.dev fixture but is not a listed consumer"
+      rc=1
+    done < <(git ls-files -- '*cue.mod/module.cue')
+  else
+    echo "    (not a git work tree: skipped the unlisted-consumer check)"
+  fi
+  return $rc
+}
+
 case "$cmd" in
   pins) cmd_pins ;;
   check) cmd_check ;;
   seed) cmd_seed ;;
   publish) cmd_publish ;;
+  consumers) cmd_consumers "$@" ;;
   -h|--help|help) usage 0 ;;
-  *) die "unknown subcommand '$cmd' (pins|check|seed|publish)" ;;
+  *) die "unknown subcommand '$cmd' (pins|check|seed|publish|consumers)" ;;
 esac
```

## Appendix B: reference diff of `.tasks/`

Proved in scratch (applies to `c3e4232`). Section 1 applies the `examples.yaml` part, section 2 the `dev.yaml` part.

```diff
diff --git a/.tasks/dev.yaml b/.tasks/dev.yaml
index efa71d2..ea42a2b 100644
--- a/.tasks/dev.yaml
+++ b/.tasks/dev.yaml
@@ -62,6 +62,8 @@ tasks:
     cmds:
       - task: :examples:seed
         vars: { CUE_REGISTRY: '{{.MIXED_CUE_REGISTRY}}' }
+      - task: :examples:consumers
+        vars: { CONSUMERS_CUE_REGISTRY: '{{.MIXED_CUE_REGISTRY}}' }
       - task: test
         vars:
           TEST_CUE_REGISTRY: '{{.MIXED_CUE_REGISTRY}}'
diff --git a/.tasks/examples.yaml b/.tasks/examples.yaml
index 4d30a1c..95af6b9 100644
--- a/.tasks/examples.yaml
+++ b/.tasks/examples.yaml
@@ -64,8 +64,15 @@ tasks:
     cmds:
       - FIXTURES_DIR={{.MODULES_DIR}} OPM_BIN={{.OPM}} CUE_REGISTRY='{{.PUBLISH_REGISTRY}}' SINCE='{{.SINCE}}' PRERELEASE='{{.PRERELEASE}}' hack/fixtures.sh publish
 
+  consumers:
+    desc: 'Check that every modulepackage pins exactly what CUE resolves for the module version it names (hack/fixtures.sh consumers). Resolves through CONSUMERS_CUE_REGISTRY: GHCR by default (the root CUE_REGISTRY var defaults to an all-local mapping, so it is not reused), the seeded MIXED mapping in PR CI. FIX=1 writes the resolved cue.mod back.'
+    vars:
+      CONSUMERS_CUE_REGISTRY: '{{.CONSUMERS_CUE_REGISTRY | default .GHCR_CUE_REGISTRY}}'
+    cmds:
+      - FIXTURES_DIR={{.MODULES_DIR}} CUE_REGISTRY='{{.CONSUMERS_CUE_REGISTRY}}' FIX='{{.FIX}}' hack/fixtures.sh consumers $(find {{.RELEASES_DIR}} -mindepth 1 -maxdepth 1 -type d | sort)
+
   pin:
-    desc: 'Re-pin every consumer of an example module''s version to its declared version, optionally with a PRERELEASE segment (matches examples:publish PRERELEASE=<id>): the module''s moduleinstance.yaml, its modulepackage fixture''s cue.mod dep, and config/samples. Used by PR e2e to point the fleet at the just-published pre-release tags.'
+    desc: 'Re-pin every consumer of an example module''s version to its declared version, optionally with a PRERELEASE segment (matches examples:publish PRERELEASE=<id>): the module''s moduleinstance.yaml, its modulepackage fixture''s cue.mod dep (its shared core and catalog pins follow the module), and config/samples. Used by PR e2e to point the fleet at the just-published pre-release tags.'
     vars:
       PRERELEASE: '{{.PRERELEASE | default ""}}'
       SAMPLE_MI: '{{.SAMPLE_MI | default "config/samples/opmodel.dev_v1alpha1_moduleinstance.yaml"}}'
@@ -102,6 +109,18 @@ tasks:
           if [ -f "$mp" ] && grep -q "\"${path}\"" "$mp"; then
             awk -v p="\"${path}\"" -v v="${tag}" 'index($0,p) {f=1} f && /v: "/ {sub(/"[^"]+"/, "\"" v "\""); f=0} {print}' "$mp" > "$mp.tmp" && mv "$mp.tmp" "$mp"
             echo "pinned ${name} modulepackage dep -> ${tag}"
+            # Every other dep the modulepackage shares with the module (core,
+            # the catalogs) follows the module's pin: CUE keeps a dep the
+            # modulepackage already lists at its listed version, so a version
+            # re-pin alone would render the new module against a stale core.
+            for dep in $(sed -n 's/^[[:space:]]*"\([^"]*\)":[[:space:]]*{.*$/\1/p' "$dir/cue.mod/module.cue"); do
+              grep -q "\"${dep}\"" "$mp" || continue
+              want=$(awk -v p="\"${dep}\"" 'index($0,p) {f=1} f && /v: "/ {match($0, /"[^"]+"/); print substr($0, RSTART+1, RLENGTH-2); exit}' "$dir/cue.mod/module.cue")
+              have=$(awk -v p="\"${dep}\"" 'index($0,p) {f=1} f && /v: "/ {match($0, /"[^"]+"/); print substr($0, RSTART+1, RLENGTH-2); exit}' "$mp")
+              if [ -z "$want" ] || [ "$want" = "$have" ]; then continue; fi
+              awk -v p="\"${dep}\"" -v v="${want}" 'index($0,p) {f=1} f && /v: "/ {sub(/"[^"]+"/, "\"" v "\""); f=0} {print}' "$mp" > "$mp.tmp" && mv "$mp.tmp" "$mp"
+              echo "pinned ${name} modulepackage ${dep} ${have} -> ${want} (follows the module)"
+            done
           fi
           # config/samples ModuleInstance, when it instantiates this module.
           if [ -f "{{.SAMPLE_MI}}" ] && grep -q "path: ${path}$" "{{.SAMPLE_MI}}"; then
```
