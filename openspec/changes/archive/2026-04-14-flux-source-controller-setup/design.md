## Context

The controller watches `OCIRepository` CRDs from Flux source-controller. Without source-controller installed, the CRD doesn't exist and the controller can't reconcile anything. This is a hard prerequisite for any local Kind testing.

Flux provides both a CLI (`flux install`) and raw manifests. The CLI approach is simpler but adds a binary dependency. Raw manifests are more portable but harder to version-pin.

## Goals / Non-Goals

**Goals:**
- Single Makefile target installs Flux source-controller into the current cluster.
- Corresponding Taskfile alias for convenience.
- Minimal install — only `source-controller`, not the full GitOps toolkit.

**Non-Goals:**
- Installing the full Flux suite (kustomize-controller, helm-controller, notification-controller).
- Managing Flux lifecycle (upgrades, multi-tenancy).
- Making Flux installation production-ready.

## Decisions

**Use `flux install --components=source-controller`**

The `flux` CLI is the simplest path. Rationale:
- One command, handles CRDs + Deployment + RBAC + namespace creation.
- Idempotent — safe to re-run.
- Version pinned via the `flux` binary the developer has installed.
- Alternative considered: `kubectl apply` with raw manifests from Flux GitHub releases. Rejected — requires downloading, version tracking, and doesn't handle CRD ordering as cleanly.

**Makefile target: `install-flux`**

- Runs `flux install --components=source-controller`.
- Depends on `flux` CLI being available (fail with clear error if missing).
- Separate from `deploy` — Flux is cluster infrastructure, not controller deployment.

**Taskfile alias: `install-flux`**

- Delegates to `make install-flux`, consistent with existing Taskfile pattern.

**Uninstall target: `uninstall-flux`**

- Runs `flux uninstall --silent` for clean teardown.
- Taskfile alias: `uninstall-flux`.

## Risks / Trade-offs

- [flux CLI dependency] Developer must have `flux` installed. → Clear error message if missing. Flux CLI is a standard tool in the GitOps ecosystem.
- [Version drift] Different developers may have different `flux` versions. → Acceptable for POC. Could pin via Makefile-managed binary later (like `kustomize`, `controller-gen`).
- [Namespace creation] `flux install` creates `flux-system` namespace. → Standard convention, no conflict with controller namespace (`poc-controller-system`).
