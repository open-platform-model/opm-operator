## 1. OCIRepository Sample

- [ ] 1.1 Create `config/samples/source_v1_ocirepository.yaml` — OCIRepository pointing to `oci://opm-registry:5000/opmodel.dev/test/hello`, interval 1m, insecure true

## 2. ModuleRelease Sample

- [ ] 2.1 Replace stub in `config/samples/releases_v1alpha1_modulerelease.yaml` with complete spec: sourceRef to OCIRepository, module.path, values with message, prune true

## 3. BundleRelease Sample

- [ ] 3.1 Update `config/samples/releases_v1alpha1_bundlerelease.yaml` with valid sourceRef and prune fields, add comment noting controller is unimplemented

## 4. Validation

- [ ] 4.1 Run `kubectl apply --dry-run=client -f config/samples/source_v1_ocirepository.yaml` to validate YAML syntax
- [ ] 4.2 Run `kubectl apply --dry-run=client -f config/samples/releases_v1alpha1_modulerelease.yaml` to validate YAML syntax
