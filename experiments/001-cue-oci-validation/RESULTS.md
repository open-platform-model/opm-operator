# Experiment 001 Results

## Outcome

The experiment succeeded with one important caveat:

- a native CUE OCI module does not reconcile successfully with the default Flux `OCIRepository` layer handling
- the same module does reconcile successfully when `spec.layerSelector` selects `application/zip` with `operation: copy`
- the resulting Flux artifact can be fetched and recovered into a valid local CUE module tree

This means the controller can proceed with Flux as the source mechanism, but it must treat the native CUE zip payload as an OPM-owned handoff rather than relying on Flux's default tar+gzip extraction behavior.

## Environment Used

- Kubernetes context: `kind-opm-dev`
- Flux CLI: `2.8.1`
- CUE CLI: `v0.16.0`
- Local OCI registry container: `opm-registry`
- Registry address used by source-controller: `172.18.0.3:5000`

## Module Published

- module path: `opmodel.dev/experiments/minimal`
- version: `v0.0.1`
- published to: `localhost:5000/opmodel.dev/experiments/minimal:v0.0.1`

## Key Observation

The generated OCI manifest for the CUE module used these layer types:

- config: `application/vnd.cue.module.v1+json`
- layer 1: `application/zip`
- layer 2: `application/vnd.cue.modulefile.v1`

That matches the design concern in `docs/design/cue-oci-artifacts-and-flux-source-controller.md`.

## Failed Configuration

Using the default `OCIRepository` behavior failed with:

`failed to extract layer contents from artifact: requires gzip-compressed body: gzip: invalid header`

This happened because Flux attempted extraction using its normal tar+gzip-oriented path against a native CUE zip layer.

## Working Configuration

The following source settings worked:

```yaml
apiVersion: source.toolkit.fluxcd.io/v1
kind: OCIRepository
metadata:
  name: cue-oci-minimal
  namespace: default
spec:
  interval: 5m
  url: oci://172.18.0.3:5000/opmodel.dev/experiments/minimal
  ref:
    tag: v0.0.1
  insecure: true
  layerSelector:
    mediaType: application/zip
    operation: copy
  timeout: 1m
```

## Flux Result

After applying the working configuration, the `OCIRepository` reached `Ready=True` and reported:

- revision: `v0.0.1@sha256:0df9f44c26e5807ea0770ba514013d85d15ce96a12e6064d01080c0ce069011f`
- artifact digest: `sha256:c109d327cf437b4754c3f81bd3f95783ac9159a020cd2c587233c2474918636c`
- artifact path: `ocirepository/default/cue-oci-minimal/sha256:0df9f44c26e5807ea0770ba514013d85d15ce96a12e6064d01080c0ce069011f.tar.gz`

Important detail:

- despite the `.tar.gz` artifact path suffix, the downloaded artifact body was actually a zip archive containing the module tree

## Validation Result

After fetching the artifact through a local port-forward to `source-controller`:

- the downloaded file type was detected as zip
- the recovered tree contained `cue.mod/module.cue`
- the recovered tree contained `main.cue`
- `cue export` succeeded against the recovered module

The exported output was:

```json
{
  "output": {
    "kind": "ConfigMap",
    "apiVersion": "v1",
    "metadata": {
      "name": "cue-oci-minimal",
      "namespace": "default"
    },
    "data": {
      "message": "hello from native cue oci"
    }
  }
}
```

## Conclusion

The risky integration seam is now clarified:

- Flux can resolve and track a real native CUE OCI artifact
- Flux default layer extraction is not sufficient for the native CUE zip payload
- Flux `layerSelector.operation=copy` is enough to preserve the native payload
- OPM can fetch that preserved payload and reconstruct a valid local module tree

## Implication For Implementation

The controller should assume:

- `OCIRepository` must be configured to preserve the native CUE zip layer
- OPM owns the final recovery/unpack step for that payload
- `status.artifact.digest` in this mode refers to the copied zip payload digest, while revision still identifies the full OCI manifest digest
