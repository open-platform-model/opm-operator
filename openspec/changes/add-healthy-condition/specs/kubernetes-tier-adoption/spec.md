## ADDED Requirements

### Requirement: The operator judges the health of inventory objects only through the library's health package

The operator SHALL judge the readiness of an object in `status.inventory` only with the library's `opm/k8s/health` (`Evaluate`, `IsHealthy`, `Aggregate`, `ProgressDeadlineExceeded`). The code that computes the `Healthy` condition SHALL read no status field of a fetched object and SHALL declare no readiness rule of its own for any Kubernetes kind: no rollout, replica-count, Job, PersistentVolumeClaim or `Ready`-condition check, and no alias to the library's. The operator fetches each object with its own client and passes it in, because the library reads no cluster. This requirement covers applied inventory objects only; the operator's own resources (a `dependsOn` ModulePackage, a TransformerRegistration's provider, the `cluster` Platform) keep their own condition checks. Source: 0012:D3:R6.

#### Scenario: No local evaluator

- **WHEN** `internal/reconcile/health.go` is parsed by its package's unit test
- **THEN** it calls no `unstructured.Nested*` accessor and holds no `"status"` string literal, so every verdict comes from `health.Evaluate`, `health.IsHealthy`, `health.Aggregate` and `health.ProgressDeadlineExceeded`
