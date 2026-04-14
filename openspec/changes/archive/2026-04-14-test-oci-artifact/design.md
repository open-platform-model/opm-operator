## Context

The controller reconciles OPM modules from OCI artifacts fetched via Flux OCIRepository. To test locally in Kind, we need:
1. A self-contained test CUE module that renders simple Kubernetes resources.
2. That module published to an OCI registry accessible from inside the Kind cluster.
3. The existing local Docker registry (`opm-registry` on `localhost:5000`) connected to Kind's Docker network.

The workspace already has a local OCI registry setup in `../catalog/.tasks/registry/docker.yml` using `registry:2` on port 5000, container name `opm-registry`. CUE modules are published via `cue mod publish`.

## Goals / Non-Goals

**Goals:**
- Self-contained test CUE module in the poc-controller repo (no external dependency).
- Makefile target to publish the test module to the local registry.
- Kind cluster can reach the local registry via Docker network bridging.
- The test module follows OPM module conventions (metadata, #config, #components).
- Other modules from `../modules/` can also be manually published to the same registry for ad-hoc testing.

**Non-Goals:**
- Publishing to GHCR or any remote registry.
- Testing multiple modules or complex module interactions.
- Bundle module fixtures (BundleRelease is not implemented).

## Decisions

**Test module location: `test/fixtures/modules/hello/`**

A minimal CUE module that renders a single ConfigMap. Structure:
```
test/fixtures/modules/hello/
├── cue.mod/
│   └── module.cue    # module: "opmodel.dev/test/hello@v0"
├── module.cue         # metadata, #config, debugValues
└── components.cue     # #components with one ConfigMap
```

The module follows OPM conventions (imports `opmodel.dev/core/v1alpha1/module@v1`, uses `m.#Module`, etc.) but renders only a ConfigMap with `data.message` from `#config.message`. This exercises the full pipeline — CUE evaluation, resource rendering, SSA apply — with minimal cluster side-effects.

**Alternative considered**: Copy `metric_server` from `../modules/`. Rejected — introduces dependencies on catalog schemas being published at specific versions, and produces heavier resources (Deployment, RBAC) that complicate assertion in tests.

**Registry networking: `docker network connect kind opm-registry`**

When a Kind cluster is created, Docker creates a network named `kind`. The existing `opm-registry` container runs on the default Docker bridge. Connecting it to the `kind` network makes it reachable from Kind nodes at `opm-registry:5000`.

This is the standard [Kind + local registry](https://kind.sigs.k8s.io/docs/user/local-registry/) pattern. No Kind config changes needed — just a `docker network connect` after cluster creation.

From inside the cluster, the registry address is `opm-registry:5000` (not `localhost:5000`).

**Makefile target: `publish-test-module`**

Sequence:
1. Ensure the local registry is running (check `docker ps` for `opm-registry`, start if needed).
2. Set `CUE_REGISTRY=opmodel.dev=localhost:5000+insecure,registry.cue.works`.
3. Run `cue mod tidy` + `cue mod publish v0.0.1` from the test module directory.

**Makefile target: `connect-registry`**

Connects `opm-registry` to the `kind` Docker network (idempotent — ignores already-connected error).

**CUE dependency resolution**

The test module imports `opmodel.dev/core/v1alpha1/module@v1` and `opmodel.dev/opm/v1alpha1/schemas@v1`. These must be available in the registry. Two paths:
- If running the full workspace (`cd ../catalog && task publish:local`), they're already in `localhost:5000`.
- If not, the CUE registry fallback resolves them from `registry.cue.works` (the public CUE registry mirror of GHCR). This works if the host has internet access during `cue mod publish`.

The module's `cue.mod/module.cue` uses the standard registry mapping so either path works transparently.

## Risks / Trade-offs

- [Registry not running] If `opm-registry` isn't started, publish fails. → The Makefile target checks and starts it (same pattern as catalog's `registry:start`).
- [CUE dep resolution] First publish may be slow if catalog modules aren't cached locally. → One-time cost; CUE caches modules after first fetch.
- [Docker network assumption] Assumes Kind uses Docker (not Podman). → Matches Makefile's `CONTAINER_TOOL ?= docker` default. Podman support is out of scope for POC.
- [Module version collision] Re-publishing `v0.0.1` to a registry that already has it may fail or silently succeed depending on registry config. → Use `--force` flag or delete the tag first. For test fixtures this is acceptable.
