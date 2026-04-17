# Testing Guide — poc-controller

Three test tiers, each with a distinct purpose and infrastructure requirement. Choose the lightest tier that can validate the behavior.

## Test Tiers

### Unit Tests — `internal/*/`

Package-scoped tests co-located with production code. Controller unit tests (`internal/controller/`) use envtest to get a real API server, but test a single reconciler in isolation. Non-controller packages use plain Go tests with no cluster.

**Use when:** testing a single package's logic — reconciliation flows, helper functions, status updates, error paths.

**Run:**

```bash
make test                          # all unit tests
go test ./internal/apply           # single package
```

**Infrastructure:** envtest (in-process API server + etcd). No cluster required.

### Integration Tests — `test/integration/`

Cross-package tests that exercise how multiple internal packages work together against a real API server. These validate the contract between packages — e.g., that the apply package correctly creates and manages real K8s resources using the expected SSA semantics.

**Use when:**

- Behavior depends on interaction between multiple internal packages.
- The test needs real K8s API behavior (server-side apply, field ownership, conflict resolution) but not a running controller or cluster networking.
- Validating that a subsystem works end-to-end as a unit, without full deployment.

**Run:**

```bash
make test                                    # included in default test target
go test ./test/integration/...               # all integration tests
go test ./test/integration/apply             # single integration package
```

**Infrastructure:** envtest. Same as unit tests — no cluster required.

### E2E Tests — `test/e2e/`

Full-stack tests against a Kind cluster with the controller deployed as a real workload. Validates the entire delivery chain: image build, deployment, RBAC, CRD installation, CertManager integration, and real reconciliation with actual pods.

**Use when:**

- Testing deployment correctness (manifests, RBAC, leader election, health probes).
- Behavior requires real cluster networking or webhooks.
- Validating the install path (`dist/install.yaml`).
- Final confidence check before release.

**Run:**

```bash
make setup-test-e2e                # create Kind cluster if missing
make test-e2e                      # build, deploy, test, cleanup
```

**Infrastructure:** Kind cluster. Slow. May install CertManager (skip with `CERT_MANAGER_INSTALL_SKIP=true`).

## Decision Flowchart

```
Is the behavior scoped to a single package?
  YES → unit test in that package
  NO  ↓

Does it need real K8s API behavior (SSA, field ownership, CRD lifecycle)
but NOT a deployed controller?
  YES → integration test in test/integration/
  NO  ↓

Does it need a running controller, real networking, RBAC, or webhooks?
  YES → e2e test in test/e2e/
```

## Conventions

All tiers share these conventions:

- Ginkgo v2 + Gomega.
- Descriptive `Describe`/`Context`/`It` text, `-ginkgo.focus`-friendly.
- `Eventually` for async K8s behavior, no sleeps.
- `Expect(err).NotTo(HaveOccurred())` or `Expect(...).To(Succeed())`.
- Package-local helpers for repeated assertions; keep setup readable.

### Build tags

- Unit and integration tests: no build tag (included in `make test`).
- E2E tests: `//go:build e2e` (excluded from `make test`, run via `make test-e2e`).

### File layout

| Tier | Location | Suite file | Build tag |
|------|----------|------------|-----------|
| Unit | `internal/<pkg>/` | `suite_test.go` | none |
| Integration | `test/integration/<area>/` | `suite_test.go` | none |
| E2E | `test/e2e/` | `e2e_suite_test.go` | `e2e` |
