## Why

The controller has no custom Prometheus metrics. Controller-runtime provides basic workqueue and reconcile duration metrics, but these don't capture domain-specific signals: how many resources are managed, how often applies/prunes occur, reconcile outcome distribution, or inventory size. Custom metrics enable Grafana dashboards, SLO tracking, and alerting on controller health.

## What Changes

- Register custom Prometheus metrics via controller-runtime's metrics registry.
- Expose metrics for: reconcile outcome counts (by outcome type), reconcile duration histogram, apply/prune resource counts, inventory size gauge, failure counter gauges.
- Metrics are labeled by ModuleRelease name and namespace.
- Metrics endpoint is the default controller-runtime `/metrics` path (already wired by kubebuilder scaffold).

## Capabilities

### New Capabilities
- `metrics`: Custom Prometheus metrics for reconcile outcomes, durations, resource counts, and operational health.

### Modified Capabilities

## Impact

- New `internal/metrics/` package with metric definitions and registration.
- `internal/reconcile/modulerelease.go` — Record metrics at reconcile milestones.
- `cmd/main.go` — Register custom metrics collector on startup.
- No API changes. Metrics are exposed via HTTP, not CRD status.
- SemVer: MINOR — new capability.
