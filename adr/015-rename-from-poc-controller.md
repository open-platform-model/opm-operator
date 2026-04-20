# ADR-015: Rename Project From `poc-controller` to `opm-operator`

## Status

Accepted

## Context

ADR-014 standardized the controller's published container image on `ghcr.io/open-platform-model/opm-operator` and explicitly deferred the rename of the project's other identifiers — the GitHub repository name, the Go module path, the CUE catalog module path, the kustomize namespace (`poc-controller-system`), the `app.kubernetes.io/name` label, and the name prefix applied to every manifest — as non-blocking follow-up work. That follow-up has now been picked up.

At the point this ADR is written, the project presents a contradictory identity to operators and contributors:

- `kubectl apply -f install.yaml` installs an image named `opm-operator` into a namespace named `poc-controller-system`, controlled by a Deployment named `poc-controller-controller-manager`.
- Consumers running `go get github.com/open-platform-model/poc-controller` receive a module that publishes its container as `opm-operator`.
- The catalog module at `opmodel.dev/poc-controller/catalog@v1` is bundled with a controller that is advertised publicly as `opm-operator`.

The name `poc-controller` originates from the kubebuilder scaffold when this was a proof-of-concept. The project is past that stage. `opm-operator` was chosen as the public identifier during the image-publishing rollout in ADR-014 because:

- It matches the OPM (Open Platform Model) ecosystem naming pattern used by sibling repos and the CUE catalog domain `opmodel.dev`.
- "Operator" is the correct term of art for a Kubernetes custom controller that ships CRDs (narrower than "controller", which is also used for leaf controller-runtime reconcilers within this codebase).
- It is short, distinct, and free of "POC" connotations.

This ADR records the decision to propagate that name across the remaining surfaces.

## Decision

The project is renamed from `poc-controller` to `opm-operator` across all identifiers that are not immutable history.

**In scope:**

- Go module path: `github.com/open-platform-model/poc-controller` → `github.com/open-platform-model/opm-operator`.
- GitHub repository name: `open-platform-model/poc-controller` → `open-platform-model/opm-operator`, performed manually via the GitHub UI.
- CUE catalog module path: `opmodel.dev/poc-controller/catalog@v1` → `opmodel.dev/opm-operator/catalog@v1`.
- Kustomize namespace: `poc-controller-system` → `opm-operator-system`.
- Kustomize name prefix: `poc-controller-` → `opm-operator-`.
- `app.kubernetes.io/name` label value on every manager manifest.
- Build and task plumbing: Kind cluster default, buildx builder name, fixture source paths.
- Release-please `package-name`.
- Repo-local documentation and the workspace root's path map.

**Out of scope:**

- The CRD API group `releases.opmodel.dev`. This is the stable public API surface.
- The SSA field manager `opm-controller` and the `app.kubernetes.io/managed-by: opm-controller` label applied to rendered user resources. These were decoupled from the project name in ADR-010 and already carry the ecosystem-level identifier, not the repo identifier.
- User-applied `ModuleRelease` and `BundleRelease` resources.
- Archived artifacts: git tags `v0.4.x`, `CHANGELOG.md` entries, everything under `openspec/changes/archive/`.

**Alternatives considered and rejected:**

- *Keep `poc-controller` everywhere and only rename the image.* Rejected: leaves the project permanently divergent from its published artifact and from the ecosystem domain `opmodel.dev`.
- *Rename to `opm-controller` instead of `opm-operator`.* Rejected: `opm-controller` is already the runtime actor identity stamped in managed-by labels and the SSA field manager. Reusing the same name for the project would re-couple what ADR-010 intentionally separated.
- *Phase the rename across multiple releases.* Rejected: intermediate states have no operator audience, and every phased variant leaves contradictory identifiers live in some clusters longer than necessary.
- *Publish a bridging release under the old Go module path with a `retract` directive.* Rejected: no external Go consumers are known; the bridging release would be pure overhead.

## Consequences

**Positive:** Every identifier a human encounters — repo URL, Go import path, CUE catalog path, installed namespace, deployment name, manifest labels — agrees with the published image. The project's name reflects its maturity and its ecosystem role. The workspace-level documentation map becomes internally consistent.

**Positive:** The release cut as part of this rename bumps from `v0.4.4` to `v0.5.0`, a clean signal to operators that an upgrade is required. Pre-1.0 semver permits the bump without publishing a `/v2` module path.

**Negative:** In-place upgrade of a running controller is not possible. The manager Deployment's `matchLabels` selector is immutable and includes `app.kubernetes.io/name: poc-controller`. Operators upgrading from `v0.4.x` must delete the old namespace and associated cluster-scoped RBAC before applying the new `install.yaml`. The reconcile model tolerates this: CRDs are cluster-scoped and unaffected, user CRs already carry the runtime-owned managed-by label rather than the project-owned name label, and the controller is stateless beyond `status`. The cost is visible but bounded and one-time.

**Negative:** Git tags and release notes under the old name remain reachable (GitHub preserves the redirect), but they do not move forward. A user searching for "poc-controller" on GitHub after the rename is redirected to the new repo, which is the correct behavior but depends on GitHub's redirect remaining in place.

**Trade-off:** `PROJECT` is hand-edited rather than regenerated via `kubebuilder edit`. The alternative would re-scaffold layout metadata, resource lists, and markers that are stable in this repo. Hand-editing is surgical and acceptable because the file is re-read only when new resources are scaffolded, which is not an active workflow.

**Trade-off:** The catalog CUE module is renamed and will need to be republished under the new path. No workspace consumer references the old path, so the old OCI artifact is abandoned in place rather than migrated.

**Trade-off:** ADR-014 contains a paragraph noting "reconciling those [identifiers] is a separate, non-blocking change — captured as follow-up work, not scope creep here." That paragraph is now historically inaccurate in the sense that the follow-up is done. It is deliberately left unchanged: an ADR records the state of thinking when the decision was made, and rewriting it would erase the judgment that landing the image rename first was worth the divergence.
