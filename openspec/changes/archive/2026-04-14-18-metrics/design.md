## Context

Controller-runtime uses the `prometheus/client_golang` library and exposes a metrics endpoint at `/metrics` by default. Custom metrics are registered via `metrics.Registry` from `sigs.k8s.io/controller-runtime/pkg/metrics`. The kubebuilder scaffold already configures the metrics server; this change adds domain-specific collectors.

## Goals / Non-Goals

**Goals:**
- Define and register custom Prometheus metrics.
- Record metrics during reconcile phases.
- Use consistent metric naming following Prometheus conventions (`opm_controller_*`).
- Label metrics with `name` and `namespace` for per-resource granularity.

**Non-Goals:**
- Grafana dashboard definitions (operator tooling concern).
- Alert rule definitions (operator tooling concern).
- Metrics for BundleRelease (deferred).
- Custom metrics endpoint or authentication (use controller-runtime defaults).

## Decisions

### 1. Metric definitions

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `opm_controller_reconcile_total` | Counter | `name`, `namespace`, `outcome` | Reconcile attempts by outcome |
| `opm_controller_reconcile_duration_seconds` | Histogram | `name`, `namespace` | Reconcile duration |
| `opm_controller_apply_resources_total` | Counter | `name`, `namespace`, `action` | Resources applied (created/updated/unchanged) |
| `opm_controller_prune_resources_total` | Counter | `name`, `namespace` | Resources pruned |
| `opm_controller_inventory_size` | Gauge | `name`, `namespace` | Current inventory entry count |

### 2. Metric prefix: opm_controller_

All custom metrics use the `opm_controller_` prefix to avoid collisions with controller-runtime's built-in metrics.

### 3. Register in init function

Metrics are registered in an `init()` function in the `internal/metrics/` package, which runs at import time. This is the standard pattern for Prometheus metrics in Go.

### 4. Reconcile duration measured end-to-end

The histogram measures wall-clock time from Phase 0 start to Phase 7 completion. Suspend short-circuits and deletion cleanup are also measured.

### 5. Outcome label values match outcome type names

The `outcome` label on `reconcile_total` uses: `soft_blocked`, `no_op`, `applied`, `applied_and_pruned`, `failed_transient`, `failed_stalled`.

## Risks / Trade-offs

- **[Risk] High cardinality** — Per-resource labels (`name`, `namespace`) can create high cardinality if there are many ModuleReleases. Mitigation: acceptable for v1alpha1; operators with many releases can use metric relabeling to aggregate.
- **[Trade-off] No per-resource apply metrics** — We count total resources applied, not per-target-resource. Keeps cardinality bounded.
