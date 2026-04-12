# ADR-008: Naming Taxonomy

## Status

Accepted

## Context

The OPM controller ecosystem uses several naming layers that serve different roles: Kubernetes API groups, resource labels and annotations, inventory and status field names, and CUE module paths. All of these use the `opmodel.dev` domain, making it easy to conflate them.

Without an explicit taxonomy:

- API group names could be mistakenly used as label prefixes on workloads.
- Label families for release identity, component identity, and runtime actor identity could be mixed or overloaded.
- Status field names could mirror label prefix conventions unnecessarily.
- CUE module paths could be confused with Kubernetes API groups.

Each naming layer has distinct semantics and consumers. They need clear boundaries.

## Decision

The controller ecosystem uses four distinct naming layers with explicit rules:

**1. Kubernetes API groups** — for CRDs only.
- `releases.opmodel.dev` is the API group for `ModuleRelease` and `BundleRelease`.
- This namespace is not used as a workload label prefix.

**2. Resource labels and annotations** — split into families by purpose:
- `app.kubernetes.io/*` — Kubernetes conventional labels (`name`, `instance`, `managed-by`).
- `module-release.opmodel.dev/*` — release identity on rendered resources (`name`, `namespace`).
- `component.opmodel.dev/*` — component identity (`name`).
- `opmodel.dev/*` — OPM internal infrastructure classification only.

**3. Inventory and status field names** — plain schema-oriented names.
- Use `sourceDigest`, `configDigest`, `renderDigest` — not label-prefixed field names.
- Status fields describe controller state cleanly without inheriting label conventions.

**4. CUE module and registry naming** — separate from Kubernetes API naming.
- `opmodel.dev/modules/jellyfin` is a CUE module path.
- `releases.opmodel.dev/v1alpha1` is a Kubernetes API group/version.
- They share a parent domain but are not the same namespace.

Five governing rules:

1. API groups are for CRDs only.
2. Workload labels use their own families (not API group prefixes).
3. Runtime actor identity (`managed-by`) is distinct from release identity (`module-release.opmodel.dev/*`).
4. Status field names are not forced to mirror label prefixes.
5. CUE module paths stay separate from Kubernetes CRD naming.

## Consequences

**Positive:** Each naming layer has a clear owner and purpose. Developers and operators can look at a label, field name, or API version and know immediately what namespace it belongs to and what it means.

**Positive:** Prevents future confusion as the project grows. New labels, status fields, or API groups can be placed in the right namespace by consulting the taxonomy.

**Negative:** Requires discipline to maintain. Contributors must understand which naming layer a new identifier belongs to. The taxonomy document serves as the reference.

**Trade-off:** Some naming layers look similar (e.g., `opmodel.dev/modules/*` vs `releases.opmodel.dev`). The similarity is intentional — they share a domain — but the distinction must be documented and maintained.

Related: [naming-taxonomy.md](../docs/design/naming-taxonomy.md)
