## 1. Manager Deployment

- [x] 1.1 Add `emptyDir: {}` volume named `tmp` to `config/manager/manager.yaml` spec.volumes
- [x] 1.2 Add volumeMount for `/tmp` on the manager container in spec.containers[0].volumeMounts

## 2. Validation

- [x] 2.1 Run `make manifests generate` to confirm no regressions
- [x] 2.2 Run `make build-installer` and verify `dist/install.yaml` includes the volume
