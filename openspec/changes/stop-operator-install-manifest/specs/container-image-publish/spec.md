## REMOVED Requirements

### Requirement: Release install manifest with digest-pinned image

**Reason**: The install manifest is the operator module's render at default values, and it is published by the module's release (`operator-module-release`, "Every module release publishes the install manifest rendered from it"). An operator release no longer renders `config/default` into an asset. `task operator:installer` stays a development task.

**Migration**: Download `install.yaml` from the newest module release (`opm_operator-vX.Y.Z`), which is GitHub's Latest release, or install with `opm operator install`. A module release names the operator image by tag and digest, so the manifest still pulls the image by digest.
