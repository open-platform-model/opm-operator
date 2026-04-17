# Documentation Style Guide — poc-controller

Inherits all rules from the [workspace STYLE.md](../../STYLE.md). This file adds controller-specific conventions.

## Audience

**Kubernetes administrators and platform operators.** Readers operate clusters, manage CRDs, and run controllers in production. They are comfortable with `kubectl`, YAML manifests, and Kubernetes concepts.

## Tone

- **Operational and procedure-oriented.** Tell the reader exactly what to do and in what order.
- **Direct.** No motivational framing. Assume the reader has a task to accomplish.
- Use imperative steps: "Apply the CRD manifest", "Verify the controller is running", "Check the status condition".
- Warn explicitly before destructive or irreversible operations.

## Document Types in This Repo

| Type | Location | Purpose |
|------|----------|---------|
| Design docs | `docs/design/` | Controller architecture and reconciliation design |

## Procedure Format

Operational procedures must use numbered steps:

1. Apply the CRD manifests:

   ```bash
   kubectl apply -f config/crd/bases/
   ```

2. Verify the CRDs are registered:

   ```bash
   kubectl get crds | grep opmodel.dev
   ```

3. Deploy the controller:

   ```bash
   task operator:controller:install IMG=ghcr.io/open-platform-model/poc-controller:latest
   ```

- Number every step. Do not use prose paragraphs for multi-step procedures.
- Separate each step with a blank line.
- Include the expected output or a verification command after steps that can fail silently.

## Status and Conditions

When documenting controller behavior, use a table to show conditions:

| Condition | Status | Meaning |
|-----------|--------|---------|
| `Ready` | `True` | Reconciliation succeeded |
| `Ready` | `False` | Reconciliation failed; see `message` |
| `Progressing` | `True` | Controller is working |

## YAML Examples

- All YAML examples must be complete and valid (no `...` placeholders unless explicitly noted as partial).
- Include `apiVersion`, `kind`, `metadata`, and `spec` in every manifest example.
- Use ` ```yaml ` fencing.

```yaml
apiVersion: opmodel.dev/v1alpha1
kind: ModuleRelease
metadata:
  name: my-app-production
  namespace: default
spec:
  module: "opmodel.dev/my-app@1.0.0"
  values:
    replicas: 3
```

## Warnings

Always use `> **Warning:**` before steps that:
- Delete or overwrite resources
- Require cluster-admin permissions
- Are irreversible

## Glossary

Canonical glossary: [`opm/docs/glossary.md`](../../opm/docs/glossary.md).

## What to Omit

- CUE authoring guidance (belongs in `catalog/docs/`).
- CLI command walkthroughs (belongs in `cli/docs/`).
- End-user quickstarts (belongs in `opm/docs/`).
