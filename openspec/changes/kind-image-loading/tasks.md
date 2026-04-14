## 1. Makefile Target

- [x] 1.1 Add `kind-load` target: run `make docker-build`, then `kind load docker-image $(IMG) --name $(KIND_CLUSTER)`

## 2. Manager Deployment

- [x] 2.1 Add `imagePullPolicy: IfNotPresent` to the manager container in `config/manager/manager.yaml`

## 3. Taskfile Alias

- [x] 3.1 Add `kind-load` task in `Taskfile.yml` delegating to `make kind-load`

## 4. Validation

- [ ] 4.1 Run `make kind-load` against a Kind cluster and verify image appears in `docker exec <node> crictl images`
