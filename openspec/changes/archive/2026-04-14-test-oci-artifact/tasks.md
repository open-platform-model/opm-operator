## 1. Test Module

- [x] 1.1 Create `test/fixtures/modules/hello/cue.mod/module.cue` with module path `opmodel.dev/test/hello@v0`, deps on `core/v1alpha1` and `opm/v1alpha1`
- [x] 1.2 Create `test/fixtures/modules/hello/module.cue` with OPM module metadata, `#config: { message: string | *"hello from opm" }`, and debugValues
- [x] 1.3 Create `test/fixtures/modules/hello/components.cue` with `#components` rendering a single ConfigMap with `data.message` from `#config.message`
- [x] 1.4 Run `cue vet ./...` from the test module directory to validate

## 2. Registry Connectivity

- [x] 2.1 Add `connect-registry` Makefile target: `docker network connect kind opm-registry` (ignore already-connected error)
- [x] 2.2 Add `start-registry` Makefile target: start `opm-registry` container if not running (same pattern as `catalog/.tasks/registry/docker.yml`)

## 3. Publish Target

- [x] 3.1 Add `publish-test-module` Makefile target: set CUE_REGISTRY, run `cue mod tidy` + `cue mod publish v0.0.1` from `test/fixtures/modules/hello/`

## 4. Taskfile Aliases

- [x] 4.1 Add `connect-registry`, `start-registry`, and `publish-test-module` tasks in `Taskfile.yml`

## 5. Validation

- [x] 5.1 Run `make start-registry && make publish-test-module` and verify module appears in registry (`curl http://localhost:5000/v2/_catalog`)
- [x] 5.2 Run `make connect-registry` with a Kind cluster and verify `docker exec <kind-node> nslookup opm-registry` resolves
