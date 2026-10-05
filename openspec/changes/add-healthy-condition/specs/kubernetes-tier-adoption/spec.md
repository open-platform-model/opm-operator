## ADDED Requirements

### Requirement: The operator judges readiness only through the library's health package

The operator SHALL judge the readiness of an applied object only with the library's `opm/k8s/health` (`Evaluate`, `IsHealthy`, `Aggregate`, `ProgressDeadlineExceeded`). It SHALL declare no readiness rule of its own for any Kubernetes kind: no rollout, replica-count, Job, PersistentVolumeClaim or `Ready`-condition check, and no alias to the library's. The operator fetches each object with its own client and passes it in, because the library reads no cluster. Source: 0012:D3:R6.

#### Scenario: No local evaluator

- **WHEN** the operator's Go source outside tests is searched for a read of `status.observedGeneration`, `status.updatedReplicas`, `status.availableReplicas` or a `Progressing` condition reason
- **THEN** none is found, and the `Healthy` condition is computed from `opm/k8s/health` results
