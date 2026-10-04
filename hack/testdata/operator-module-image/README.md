# operator-module-image fixtures

Inputs for `hack/operator-module/test-image.sh`, the offline test of `hack/operator-module/image.sh`: a CRD and the same CRD with `v1alpha1` no longer served, and two operator CHANGELOG excerpts whose newest section (`1.0.0-beta.2`) carries no breaking change or one. `changelog-plain.md` keeps a breaking change in the older `1.0.0-beta.1` section, so the test also proves that only the sections after the module's previous operator tag count. The module tree comes from `../operator-module-release-check/module`.
