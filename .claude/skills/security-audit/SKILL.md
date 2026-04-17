---
name: security-audit
description: Security audit skill — analyzes Kubernetes controller/operator code and manifests for vulnerabilities, RBAC over-privilege, CRD injection risks, and cloud-native security anti-patterns. Targets a specific path, feature, or the full project. Produces a severity-ranked report (CRITICAL / WARNING / SUGGESTION).
user-invocable: true
argument-hint: "[path-or-feature]"
---

Perform a security audit of the codebase. Reports findings ranked by severity — never modifies code.

**Input**: Optionally specify a target after the command:

- A directory path (e.g., `internal/controller/`) — scope to that subtree
- A feature name (e.g., `reconciliation`, `rbac`, `crd-validation`) — scope to code related to that feature
- Omit entirely — audit the full project (architecture + all code layers + manifests)

## Scope Detection

- **Path provided** → Targeted audit of that directory / file
- **Feature keyword provided** → Discover relevant code via Explore subagent, then audit
- **Nothing provided** → Full-project audit (architecture + all code layers + manifests)

---

## Audit Dimensions

The audit is organized into seven dimensions. Each dimension is checked against the in-scope code. Skip dimensions that are structurally irrelevant to the target (e.g., skip Webhook Security when no webhooks exist).

### Dimension 1: RBAC & Least Privilege

- Every `+kubebuilder:rbac` marker reviewed — no wildcard verbs (`*`), resources (`*`), or API groups (`*`)
- Verbs are minimal: no `escalate`, `bind`, or `impersonate` unless explicitly justified
- No `list`/`watch` on Secrets unless required — prefer `get` on specific names
- Status subresource RBAC separated from main resource RBAC
- Leader election Role scoped to operator namespace (not ClusterRole)
- ServiceAccount permissions match actual reconciler needs — no unused grants
- Generated `config/rbac/role.yaml` matches marker intent (run `task dev:manifests` and diff)
- No RBAC grants that enable privilege escalation (create on workloads + arbitrary serviceAccountName)
- `resourceNames` used where controller only accesses specific named resources

### Dimension 2: CRD Security & Validation

- No CRD fields allow user-controlled cross-namespace resource references (confused deputy)
  - Namespace-scoped CRDs must not accept a `namespace` field in resource references
  - Controller must use the CRD object's own namespace for all same-namespace lookups
- Kubebuilder validation markers present on all user-facing fields: `MinLength`, `MaxLength`, `Pattern`, `Enum`, `Minimum`, `Maximum`
- CEL validation rules (`x-kubernetes-validations`) enforce semantic constraints the schema cannot
- Immutable fields enforced via CEL `oldSelf == self` rules on Update
- No fields that accept arbitrary container images without digest validation
- No fields that accept arbitrary ServiceAccount references without restriction
- Status subresource enabled (`+kubebuilder:subresource:status`) — prevents spec tampering via status endpoint
- Validating/mutating webhooks configured with `failurePolicy: Fail` (not `Ignore`)
- Webhook `sideEffects: None` set correctly
- Webhook timeout reasonable (5-10s, not default 30s)

### Dimension 3: Reconciliation & Controller Security

- Reconciliation logic is idempotent — safe to run multiple times without side effects
- No TOCTOU (time-of-check/time-of-use) race conditions: prefer try-then-handle-error over check-then-act
- Security-sensitive decisions use live API server lookups, not stale cache reads
- Objects from cache are deep-copied before mutation (especially if `UnsafeDisableDeepCopy` is enabled anywhere)
- Conflict errors from optimistic locking handled correctly (re-fetch + retry, not ignore)
- `ObservedGeneration` tracked in status to detect spec/status desynchronization
- Finalizers only clean up resources the controller owns — no cascading deletion of unrelated resources
- No shared mutable state between reconciliations (global maps, package-level vars without synchronization)
- `MaxConcurrentReconciles` set appropriately; shared state protected if >1
- Error returns include context (`fmt.Errorf("...: %w", err)`) but never expose full object specs or secrets

### Dimension 4: Secret & Sensitive Data Handling

- No secrets, tokens, passwords, or private keys in:
  - CRD Status fields (status is readable by anyone with `get` on the resource)
  - Event messages (`r.Recorder.Event(...)` — events are namespace-readable)
  - Log messages (structured logging key-value pairs or `fmt.Sprintf` in messages)
  - Error messages returned to the API server
  - Condition messages on the resource
- Secret data extracted into local variables, used, then discarded — never stored in reconciler struct fields
- No `%v` or `%+v` formatting of K8s objects that may contain secret data
- Controller-runtime log objects (`log.Info("...", "object", obj)`) — verify production encoder doesn't dump full spec
- Secrets accessed only in the CRD's own namespace (no cross-namespace secret reads without explicit policy)
- No hardcoded credentials, tokens, or keys in source code
- Environment variables used for configuration — but not for secrets that should come from K8s Secrets or external secret managers

### Dimension 5: Container & Pod Security

- Controller pod runs as non-root (`runAsNonRoot: true`, explicit `runAsUser`/`runAsGroup`)
- Read-only root filesystem (`readOnlyRootFilesystem: true`)
- Privilege escalation blocked (`allowPrivilegeEscalation: false`)
- All Linux capabilities dropped (`capabilities.drop: ["ALL"]`)
- Seccomp profile set (`seccompProfile.type: RuntimeDefault`)
- Resource limits set (CPU + memory) to prevent DoS / noisy-neighbor
- Liveness and readiness probes configured
- Distroless or minimal base image (no shell, no package manager)
- Container image uses digest pinning, not tags (in Dockerfile runtime stage and `manager.yaml`)
- Multi-stage Dockerfile with `CGO_ENABLED=0` for static binary
- No writable volumes beyond necessary `emptyDir` with `sizeLimit`
- Operator namespace enforces Pod Security Standards (`pod-security.kubernetes.io/enforce: restricted`)

### Dimension 6: Network & TLS Security

- HTTP/2 disabled unless explicitly needed (CVE mitigations: GHSA-qppj-fm5r-hxr3, GHSA-4374-p667-p6c8)
- Metrics endpoint served over HTTPS with authentication/authorization
- Webhook server TLS configured with minimum TLS 1.2, strong cipher suites
- Webhook certificates managed by cert-manager or equivalent (not self-signed manual certs in production)
- Network policies restrict ingress/egress for controller pods:
  - Ingress: only from API server (webhooks) and monitoring (metrics)
  - Egress: only to API server, DNS, and necessary external endpoints
- No wildcard CORS configuration
- Health/readiness probes on separate port from metrics (no auth bypass through probe endpoint)
- `MaxHeaderBytes` and request body size limits set on any custom HTTP servers

### Dimension 7: Architecture & Trust Boundaries

Apply when scope is project-wide or covers a significant subsystem.

**Trust boundary identification**:
- Map where privilege levels change: unauthenticated → API server → admission webhook → controller → managed resources
- Controller's ServiceAccount privileges vs. end-user RBAC — controller must not become a privilege escalation vector
- Cross-namespace boundaries: controller managing resources in namespaces other than its own
- External system boundaries: OCI registries, Flux sources, external APIs

**Confused deputy assessment**:
- Can a user with limited RBAC (only create/update on CRDs) cause the controller to perform privileged operations?
- Can a CRD in namespace A cause the controller to read/write resources in namespace B?
- Can annotation/label values on CRDs influence controller behavior in security-relevant ways (template injection, shell injection, SSRF)?

**STRIDE assessment** — for each major component or data flow crossing a trust boundary:

| Threat | Question |
|--------|----------|
| **Spoofing** | Can an attacker create CRDs that impersonate legitimate resources? Can webhook identity be spoofed? |
| **Tampering** | Can CRD spec be modified between validation and reconciliation? Can artifacts be tampered between fetch and apply? |
| **Repudiation** | Are reconciliation actions auditable? Can a user deny creating a CRD that triggered privileged operations? |
| **Information Disclosure** | Can secrets leak through status, events, logs, or error messages? Can cache contents be exfiltrated? |
| **Denial of Service** | Can CRD creation trigger unbounded resource consumption? Can reconciliation loops be caused? Can finalizers block namespace deletion? |
| **Elevation of Privilege** | Can CRD fields reference privileged ServiceAccounts, cross-namespace resources, or cluster-scoped objects to escalate? |

**Defense in depth**:
- No single control (RBAC alone, webhook alone, or CEL alone) is the sole protection
- Principle of least privilege applied: controller ServiceAccount, CRD field constraints, network policy, container security

---

## Technology-Specific Checks

Apply the relevant subset based on in-scope code.

### Go Controller Code

- `crypto/rand` used instead of `math/rand` for any security-relevant values (token generation, nonces)
- No `text/template` with user-controlled input — use `html/template` for HTML or strict allowlisting
- Label and annotation values from CRDs validated before use in any string interpolation, template rendering, or shell context
- No newline injection via annotation values that end up in generated configs
- Race conditions: no shared mutable state (maps, slices) without mutex or channels across concurrent reconciliations
- All error returns checked in security-sensitive paths — no silent `_` for errors from `Get`, `Create`, `Update`, `Delete`
- No `fmt.Sprintf` for constructing API paths, label selectors, or field selectors from user input
- Unstructured objects validated before `SetUnstructuredContent` — field allowlisting, no arbitrary user maps
- `client.IgnoreNotFound(err)` used only where NotFound is expected and safe to ignore
- Deep copy before mutation when reading from informer cache

### Kubebuilder Markers & Generated Code

- All `+kubebuilder:rbac` markers accurate: match actual API calls in reconciler
- No stale RBAC markers granting permissions for removed functionality
- `+kubebuilder:validation` markers on every user-facing CRD field
- `+kubebuilder:default` values are safe and don't grant unintended access
- `+kubebuilder:subresource:status` present on all CRDs that use status
- `+kubebuilder:printcolumn` does not expose sensitive data in `kubectl get` output
- Generated `zz_generated.deepcopy.go` is current (matches types after `task dev:generate`)

### Kustomize Manifests & Deployment Config

- `config/manager/manager.yaml` security context is restrictive (all 5 hardening fields)
- `config/rbac/role.yaml` matches RBAC markers (regenerate and diff)
- No default namespace overrides that weaken isolation
- ServiceAccount `automountServiceAccountToken` set appropriately
- Webhook configurations use `failurePolicy: Fail`
- Network policies present and enabled (not just scaffolded and commented out)
- Image references in manifests use digests, not tags
- `config/default/kustomization.yaml` enables all security-relevant overlays (RBAC, network policy, metrics auth)

### Dockerfile & Build

- Multi-stage build: builder stage separate from runtime
- Runtime base image is minimal (distroless, scratch, or alpine with no extras)
- `CGO_ENABLED=0` for static Go binary
- No secrets baked into image layers (build args, copied credential files)
- `.dockerignore` excludes `.git`, `.env`, credentials, test data
- Build uses specific Go version tag (not `golang:latest`)
- Final image runs as non-root user

---

## Execution Steps

### Full-Project Audit

1. **Map the attack surface**

   Launch an Explore subagent to identify:
   - All CRD types, their fields, and validation markers
   - All RBAC markers and generated roles
   - All reconciliation entry points and the resources they read/write
   - External integrations (OCI registries, Flux sources, external APIs)
   - Deployment manifests (SecurityContext, NetworkPolicy, image references)
   - Webhook configurations (if any)

2. **Audit each dimension**

   Launch Explore subagents (parallelize where independent) to check each dimension against the relevant code. Each subagent must return findings with: **file path**, **line number(s)**, **what the issue is**, **why it matters**, and **severity** (CRITICAL / WARNING / SUGGESTION).

3. **Apply technology-specific checks**

   Check Go code patterns, Kubebuilder markers, Kustomize manifests, and Dockerfile against the technology-specific checklists.

4. **Deduplicate, rank, and generate report**

### Targeted Audit (Path or Feature)

1. **Identify scope**

   If a path: use that directory directly.
   If a feature keyword: launch an Explore subagent to find all code related to that feature (controllers, types, manifests, tests).

2. **Apply relevant dimensions and technology checks**

   Skip dimensions that don't apply. Apply Dimension 7 (Architecture) only if the target spans a trust boundary.

3. **Generate report**

---

## Severity Classification

| Severity | Definition | Examples |
|----------|-----------|----------|
| **CRITICAL** | Exploitable vulnerability, privilege escalation, data exfiltration, or auth bypass. Must be addressed before deployment. | Cross-namespace secret exfiltration via CRD field, wildcard RBAC on secrets, hardcoded credentials in source, missing auth on webhook endpoint, confused deputy allowing privilege escalation |
| **WARNING** | Security weakness with material impact, or best-practice violation that increases attack surface. Should be addressed in the current cycle. | Missing CRD validation markers, RBAC grants broader than needed, secrets in log output, missing network policy, container running as root, image using tags not digests, missing resource limits |
| **SUGGESTION** | Defense-in-depth improvement, hardening recommendation, or theoretical risk with low current exploitability. Address when convenient. | Missing CEL validation rules, pod security standard labels on namespace, HTTP/2 still enabled, missing seccomp profile, minor RBAC tightening opportunity |

### Classification Heuristics

- **Exploitability**: Can this be exploited by a user with only CRD create/update permissions? Does it require cluster-admin?
- **Impact**: What is the worst-case outcome? (cluster compromise, cross-namespace data leak, DoS, information disclosure)
- **Scope**: How many namespaces, users, or resources are affected?
- **False positives**: When uncertain, prefer SUGGESTION over WARNING, WARNING over CRITICAL
- **Confidence**: Only report findings with >= 80% confidence. If uncertain, state the uncertainty and suggest investigation rather than assert a vulnerability

---

## Report Format

```markdown
## Security Audit Report

### Scope
- **Target**: Full project | `<path>` | Feature: `<name>`
- **Date**: YYYY-MM-DD

### Summary
| Dimension                        | Status              |
|----------------------------------|---------------------|
| RBAC & Least Privilege           | N issues / Clean    |
| CRD Security & Validation        | N issues / Clean    |
| Reconciliation & Controller      | N issues / Clean    |
| Secret & Sensitive Data          | N issues / Clean    |
| Container & Pod Security         | N issues / Clean    |
| Network & TLS Security           | N issues / Clean    |
| Architecture & Trust Boundaries  | N issues / Skipped  |

**Totals**: X CRITICAL · Y WARNING · Z SUGGESTION

### CRITICAL (Must fix)

1. **[Title]** — `file/path:line`
   **Dimension**: (e.g., CRD Security & Validation)
   **Description**: What the issue is and how it could be exploited
   **Evidence**: Code snippet or pattern observed
   **Recommendation**: Specific fix with file/line target

### WARNING (Should fix)

1. **[Title]** — `file/path:line`
   **Dimension**: ...
   **Description**: ...
   **Evidence**: ...
   **Recommendation**: ...

### SUGGESTION (Nice to fix)

1. **[Title]** — `file/path:line`
   **Dimension**: ...
   **Description**: ...
   **Recommendation**: ...

### Positive Observations
- (Security practices done well — always include at least one)

### Skipped / Out of Scope
- (Dimensions or checks skipped and why)

### Final Assessment
- If CRITICAL issues: "X critical issue(s) found. Address before deployment."
- If only warnings: "No critical issues. Y warning(s) to consider."
- If all clear: "All checks passed. No security issues identified in scope."
```

---

## Guardrails

- **NEVER make code changes** — this skill is analysis and reporting only
- **Delegate deep analysis to Explore subagents** — protect the main context window from the volume of file reads and grep operations
- **>= 80% confidence threshold** — if uncertain, state it explicitly and suggest investigation rather than assert a vulnerability
- **Always include Positive Observations** — an audit that only reports negatives erodes trust and misses the value of confirming what works
- **Always include Skipped / Out of Scope** — the requestor needs to know what was NOT checked
- **Include code evidence** — every CRITICAL and WARNING must cite a file:line reference and show the relevant code pattern
- **Be specific in recommendations** — "fix the RBAC" is not actionable; "remove wildcard verb in `+kubebuilder:rbac` marker at `internal/controller/foo_controller.go:42` and replace with `get;list;watch`" is
- **Do not overstate severity** — a theoretical risk with no current exploitability path is a SUGGESTION, not a CRITICAL. Crying wolf undermines the report
- **Respect the target scope** — a targeted audit stays in scope. Note adjacent concerns in "Skipped / Out of Scope" rather than expanding unbounded
- **Actionability** — every issue must have a specific recommendation with file/line references where applicable. No vague "consider reviewing" suggestions

## Graceful Degradation

- If no webhooks in scope: skip webhook-specific checks in Dimensions 2 and 6, note in Skipped
- If target is a single controller file: skip Dimensions 5-7 (Container, Network, Architecture), note in Skipped
- If only manifests in scope: skip Go code and reconciliation checks, note in Skipped
- If no external-facing endpoints: skip CORS and external TLS checks
- If CRDs are cluster-scoped: flag as its own finding (prefer namespace-scoped) and adjust multi-tenancy checks
- Always note which checks were skipped and why
