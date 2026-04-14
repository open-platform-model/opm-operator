## 1. Apply Samples Target

- [x] 1.1 Add `apply-samples` Makefile target: `kubectl apply -f config/samples/source_v1_ocirepository.yaml -f config/samples/releases_v1alpha1_modulerelease.yaml`

## 2. Orchestration Targets

- [x] 2.1 Add `local-run` Makefile target composing: `setup-test-e2e`, `start-registry`, `connect-registry`, `install-flux`, `publish-test-module`, `kind-load`, `deploy`, `apply-samples`
- [x] 2.2 Add `local-clean` Makefile target composing: `undeploy` (ignore-not-found=true), `uninstall-flux`, `cleanup-test-e2e`

## 3. Taskfile Aliases

- [x] 3.1 Add `local-run`, `local-clean`, and `apply-samples` tasks in `Taskfile.yml`

## 4. Validation

- [x] 4.1 Run `make local-run` from scratch — verify Kind cluster created, Flux running, controller running, ModuleRelease reconciled
- [x] 4.2 Verify the test ConfigMap is created in the cluster (`kubectl get configmap` in default namespace)
- [x] 4.3 Run `make local-clean` and verify cluster deleted
