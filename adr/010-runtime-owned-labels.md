# ADR-010: Runtime-Owned Labels and Ownership Metadata

## Status

Accepted

## Context

Rendered Kubernetes resources carry a mix of metadata concerns: application identity, release identity, component identity, and operational runtime identity. Previously, `app.kubernetes.io/managed-by` was hardcoded to `open-platform-model` as a static catalog constant in `#TransformerContext.controllerLabels`.

This became inadequate when OPM gained two distinct runtime actors:

- `opm-cli` — the CLI applying releases directly
- `opm-controller` — the Kubernetes controller reconciling releases

With a single static value, the cluster could not distinguish CLI-managed from controller-managed resources. This matters for debugging, interoperability, and future automation. `managed-by` is operational metadata — it answers "which runtime actor currently manages this resource?" — not domain metadata about what the resource is.

If module or release authors could freely override `managed-by`, the label becomes untrustworthy as operational metadata.

## Decision

`app.kubernetes.io/managed-by` is runtime-owned metadata. It is set by the executing runtime actor, not by the module or release author.

- CLI sets: `app.kubernetes.io/managed-by=opm-cli`
- Controller sets: `app.kubernetes.io/managed-by=opm-controller`

The label is injected via `#runtimeLabels`, a hidden input field on `#TransformerContext` in the CUE catalog. The runtime actor supplies `#runtimeLabels` during CUE evaluation. CUE unification enforces that if a module or component label attempts to set a key present in `#runtimeLabels` to a different value, evaluation fails — the conflict surfaces as an error rather than being silently overridden.

The metadata ownership model distinguishes three classes:

1. **Domain metadata** — module, release, and component identity. Examples: `app.kubernetes.io/name`, `module-release.opmodel.dev/name`, `component.opmodel.dev/name`.
2. **Runtime-owned metadata** — actor identity. Currently: `app.kubernetes.io/managed-by`, `module-release.opmodel.dev/namespace`.
3. **Reserved metadata** — keys that OPM treats as operationally significant and must not be overridden by arbitrary module labels.

`managed-by` is not a permanent birthmark. If a resource previously managed by the CLI is later reconciled by the controller, the controller updates the label to reflect the current actor. Labels remain supportive metadata for observability — `status.inventory` is still the authoritative ownership record.

## Consequences

**Positive:** Operators can distinguish CLI-managed from controller-managed resources using standard `kubectl` label selectors. This is valuable for debugging and migration workflows.

**Positive:** CUE unification provides compile-time enforcement of reserved labels. No post-render filtering or stripping pass is needed — conflicts fail at evaluation time.

**Positive:** Both runtimes share the same domain metadata conventions (release identity, component identity, module labels) while maintaining distinct operational identity.

**Negative:** The legacy value `open-platform-model` is no longer stamped on new resources. Existing resources carrying the old value are recognized for backward compatibility but may show a label change on first reconcile under the new model.

**Trade-off:** Making `managed-by` runtime-owned means module authors cannot set it to custom values (e.g., for integration with other tools that inspect this label). This is accepted because trustworthy operational metadata is more valuable than arbitrary customization.

Related: [runtime-owned-labels-and-ownership-metadata.md](../docs/design/runtime-owned-labels-and-ownership-metadata.md), [naming-taxonomy.md](../docs/design/naming-taxonomy.md)
