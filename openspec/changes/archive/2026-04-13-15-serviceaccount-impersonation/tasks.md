## 1. Impersonated client builder

- [x] 1.1 Implement `NewImpersonatedClient(cfg *rest.Config, namespace, saName string) (client.Client, error)` in `internal/apply/`
- [x] 1.2 Set `cfg.Impersonate = rest.ImpersonationConfig{UserName: "system:serviceaccount:<namespace>:<name>"}`
- [x] 1.3 Verify ServiceAccount exists before building client (GET SA, stall if not found)

## 2. Reconcile integration

- [x] 2.1 In reconcile orchestrator: if `spec.serviceAccountName` is set, build impersonated client before Phase 5
- [x] 2.2 Pass impersonated client to `Apply` and `Prune` functions (or to `ResourceManager` constructor)
- [x] 2.3 If `spec.serviceAccountName` is empty, use controller's default client
- [x] 2.4 Classify SA-not-found and impersonation-denied errors as `FailedStalled`

## 3. RBAC markers

- [x] 3.1 Add RBAC marker for `impersonate` verb on `serviceaccounts` resource to controller
- [x] 3.2 Run `make manifests generate` to update RBAC manifests

## 4. Tests

- [x] 4.1 Write unit test: `NewImpersonatedClient` builds client with correct impersonation config
- [x] 4.2 Write envtest test: reconcile with valid SA uses impersonated identity for apply
- [x] 4.3 Write envtest test: reconcile with missing SA stalls with clear error
- [x] 4.4 Write envtest test: reconcile without SA specified uses controller client

## 5. Validation

- [x] 5.1 Run `make manifests generate` after RBAC changes
- [x] 5.2 Run `make fmt vet lint test` and verify all checks pass
