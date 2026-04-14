## Context

Kind clusters use a separate containerd runtime that can't see the host's Docker/Podman images. `make docker-build` produces a local image (`controller:latest`) but the Kind Deployment can't pull it — resulting in `ErrImagePull`. The image must be explicitly loaded into the Kind cluster's containerd store.

Additionally, the default `imagePullPolicy` for `:latest` tags is `Always`, which attempts to pull from a remote registry and fails for locally-loaded images.

## Goals / Non-Goals

**Goals:**
- Single Makefile target builds the controller image and loads it into the Kind cluster.
- Corresponding Taskfile alias.
- Image pull works in Kind without external registry.

**Non-Goals:**
- Multi-arch image builds for Kind (single host arch is sufficient).
- Pushing to a remote registry.
- Managing image garbage collection in Kind.

## Decisions

**Makefile target: `kind-load`**

Sequence:
1. `make docker-build IMG=<img>` — build the image.
2. `kind load docker-image <img> --name <cluster>` — load into Kind's containerd.

The `KIND_CLUSTER` variable is already defined in the Makefile (`poc-controller-test-e2e`). Reuse it.

The `IMG` variable defaults to `controller:latest`. For Kind, this is fine — the image is loaded directly, not pulled from a registry.

**imagePullPolicy patch**

The Deployment in `config/manager/manager.yaml` doesn't set `imagePullPolicy`. For `:latest` tags, Kubernetes defaults to `Always`. Two options:

1. Add `imagePullPolicy: IfNotPresent` directly to `manager.yaml`. Simple but affects all environments.
2. Create a kustomize patch for Kind-specific overrides. Cleaner separation but more files.

Decision: Set `imagePullPolicy: IfNotPresent` directly in `manager.yaml`. Rationale:
- The default image is `controller:latest` — it's never pulled from a remote registry even in non-Kind deployments (you always set `IMG` explicitly for real deployments).
- A kustomize overlay is overkill for this POC.
- This matches the Kubebuilder default for generated projects.

## Risks / Trade-offs

- [IfNotPresent globally] If someone deploys to a real cluster with `IMG=controller:latest`, the old cached image might be used instead of a fresh pull. → In practice, real deployments always use a specific tag/digest, not `:latest`.
- [Kind cluster name coupling] The target uses `KIND_CLUSTER` variable. If the user has a differently-named cluster, they must override it. → Same pattern already used by `make test-e2e`.
