## 1. Makefile Targets

- [x] 1.1 Add `install-flux` target: check for `flux` CLI, run `flux install --components=source-controller`
- [x] 1.2 Add `uninstall-flux` target: run `flux uninstall --silent`

## 2. Taskfile Aliases

- [x] 2.1 Add `install-flux` task in `Taskfile.yml` delegating to `make install-flux`
- [x] 2.2 Add `uninstall-flux` task in `Taskfile.yml` delegating to `make uninstall-flux`

## 3. Validation

- [x] 3.1 Run `make install-flux` against a Kind cluster and verify Flux source-controller Pod is running in `flux-system`
- [x] 3.2 Verify `OCIRepository` CRD exists after install (`kubectl get crd ocirepositories.source.toolkit.fluxcd.io`)
