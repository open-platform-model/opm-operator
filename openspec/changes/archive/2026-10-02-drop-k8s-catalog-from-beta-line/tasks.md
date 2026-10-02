## 1. Drop the retired k8s catalog from the sample and the beta-line text

- [x] 1.1 Delete the k8s subscription from `config/samples/opmodel.dev_v1alpha1_platform.yaml` and regenerate `docs/site/reference/operator-resources.md` with `task dev:docs:reference`
- [x] 1.2 Delete the k8s paragraph sentence and YAML entry from `docs/site/start/install-the-operator.md`
- [x] 1.3 Drop k8s from the beta-line list in `AGENTS.md`, `CONSTITUTION.md` and `openspec/config.yaml`
- [x] 1.4 Modify the release-automation "Beta prerelease line" requirement via the delta spec
