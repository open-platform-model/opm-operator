## Why

The raw Kubernetes catalog `opmodel.dev/catalogs/k8s@v1` is retired (owner decision 2026-10-02):
only `opmodel.dev/catalogs/opm@v4` is first-party, and nothing first-party may depend on the
retired catalog. Its published builds stay on GHCR. The release-automation spec still lists it as a
prerelease line on the path to GA, and the sample Platform, the install guide, AGENTS.md,
CONSTITUTION.md and the OpenSpec config still subscribe to it or name it.

## What Changes

- Drop `opmodel.dev/catalogs/k8s@v1` from the beta-line list in the release-automation
  requirement "Beta prerelease line"; the requirement and its scenarios are otherwise unchanged.
- Drop the k8s subscription from `config/samples/opmodel.dev_v1alpha1_platform.yaml`, the
  regenerated resource reference and the install guide.
- Drop it from the matching beta-line sentence in AGENTS.md, CONSTITUTION.md and
  `openspec/config.yaml`.

## Capabilities

### Modified Capabilities

- `release-automation`: "Beta prerelease line" no longer lists the retired k8s catalog.

## Impact

No code change. The controller and e2e specs that apply the sample Platform now subscribe to
opm@v4 only. Specs that still name `opmodel.dev/catalogs/k8s@v1` as a second real catalog
(platform claims, platform module generation) are left for a separate change that picks a
replacement test catalog.
