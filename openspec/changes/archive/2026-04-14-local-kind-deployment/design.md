## Context

The individual deployment pieces exist as separate Makefile targets (from the other changes in this series), but there's no single command to go from zero to a running controller in Kind. A developer must manually sequence: create cluster → start registry → connect registry → install Flux → build image → load image → deploy controller → publish module → apply samples. This is error-prone and undocumented.

## Goals / Non-Goals

**Goals:**
- Single `make local-run` target that orchestrates the full sequence.
- Single `make local-clean` target that tears everything down.
- Corresponding Taskfile aliases for both.
- Idempotent — safe to re-run (each sub-target handles already-done state).

**Non-Goals:**
- Hot-reload or watch mode (rebuild on file change).
- Log streaming or dashboard integration.
- CI/CD pipeline (this is purely local developer workflow).

## Decisions

**`make local-run` orchestration sequence**

```
local-run:
  1. setup-test-e2e          # Create Kind cluster (existing target, idempotent)
  2. start-registry           # Ensure opm-registry is running
  3. connect-registry         # Bridge opm-registry to Kind network
  4. install-flux             # Install Flux source-controller
  5. publish-test-module      # Publish hello module to local registry
  6. kind-load                # Build controller image + load into Kind
  7. deploy                   # Deploy CRDs, RBAC, controller Deployment
  8. apply-samples            # Apply OCIRepository + ModuleRelease sample CRs
```

Step 8 is new — a small target that does `kubectl apply -f config/samples/source_v1_ocirepository.yaml -f config/samples/releases_v1alpha1_modulerelease.yaml`.

Each step is a separate Makefile target (composable). `local-run` is the convenience aggregator.

**`make local-clean` teardown sequence**

```
local-clean:
  1. undeploy                 # Remove controller + CRDs (ignore-not-found=true)
  2. uninstall-flux           # Remove Flux source-controller
  3. cleanup-test-e2e         # Delete Kind cluster
```

Does not stop or remove the registry — it's shared across clusters and managed by the catalog workspace.

**Makefile owns orchestration, Taskfile wraps**

Consistent with existing pattern. `local-run` and `local-clean` are Makefile targets. Taskfile adds:
```yaml
local-run:
  desc: Deploy controller to local Kind cluster (full setup)
  cmds:
    - make local-run
local-clean:
  desc: Tear down local Kind cluster and deployment
  cmds:
    - make local-clean
```

**`apply-samples` as a separate target**

Allows re-applying samples independently (e.g., after editing the ModuleRelease to test different values) without re-running the full setup.

## Risks / Trade-offs

- [Ordering sensitivity] Steps must run in order (e.g., Flux before deploy, registry before publish). → Makefile target dependencies enforce this via prerequisite syntax.
- [Partial failure] If step 5 fails (publish), steps 6-8 still can't work. → Each sub-target fails loudly. The developer diagnoses and re-runs `make local-run` (idempotent sub-targets skip completed work).
- [First run is slow] Building the image + downloading CUE deps + installing Flux takes time on first run. → Subsequent runs are fast (cached image, cached deps, idempotent Flux install). No mitigation needed for POC.
