# Experiment 001: CUE OCI Validation

## Goal

Prove the smallest thing that matters for controller implementation:

> Can Flux `OCIRepository` resolve a real native CUE OCI module artifact and expose enough artifact data for OPM to reconstruct a valid local CUE module tree?

This experiment intentionally stops before controller reconcile, render, or apply logic.

## Scope

The experiment covers:

- publishing one minimal native CUE module as an OCI artifact
- creating one Flux `OCIRepository` that points at that artifact
- confirming Flux marks the source `Ready`
- fetching the resolved artifact from `status.artifact.url`
- validating that the fetched content can become a usable local CUE module

The experiment does not cover:

- `ModuleRelease` reconciliation
- Kubernetes apply or prune behavior
- bundle evaluation
- health or drift handling

## Success Criteria

The spike is a success when all of the following are true:

1. Flux accepts the OCI reference without format-level rejection.
2. The `OCIRepository` reaches `Ready=True`.
3. `status.artifact` includes revision, digest, and URL.
4. The fetched artifact can be unpacked or recovered into a valid local CUE module tree.
5. The recovered module contains `cue.mod/module.cue` and the expected package file.
6. A trivial `cue` command succeeds against the recovered module.

An acceptable partial success is:

- Flux resolves the source successfully, but OPM must perform an extra unpack step after fetching the artifact.

The spike fails if either of these is true:

- Flux cannot reconcile the native CUE artifact into a ready `OCIRepository`.
- The artifact URL does not preserve enough information for OPM to recover a valid module tree.

## Minimal Test Flow

### 1. Prepare a tiny native CUE module

Use the fixture in `fixtures/minimal-module/` as the source module tree.

It contains:

- `cue.mod/module.cue`
- one trivial package in `main.cue`

### 2. Publish the module with normal CUE-native OCI tooling

Publish the fixture to an OCI registry using the standard CUE module workflow.

Important:

- do not repackage it into a Flux-specific tarball
- do not change the artifact shape to make Flux happy

Record:

- registry/repository
- tag
- optional pinned digest after publish

### 3. Create a Flux `OCIRepository`

Apply `manifests/ocirepository.yaml`, filling in the repository URL and tag first.

This should give source-controller one real native CUE module artifact to reconcile.

Important:

- for native CUE modules, select the `application/zip` layer and use `operation: copy`
- the default Flux extraction behavior is tar+gzip-oriented and is not sufficient for the native CUE zip layer

### 4. Inspect the resolved source

Use `scripts/observe-ocirepository.sh` to print the key conditions and artifact fields.

What to verify:

- `Ready=True`
- `status.artifact.revision` is present
- `status.artifact.digest` is present
- `status.artifact.url` is present

### 5. Fetch and inspect the artifact

Use `scripts/fetch-and-validate.sh <artifact-url> <artifact-digest>`.

The script:

- downloads the resolved artifact
- identifies the downloaded file type
- attempts a tar extraction first
- if a zip payload is found, unpacks it
- validates the recovered tree
- runs a trivial `cue export` against the recovered module

### 6. Record the outcome

Capture the following in your notes or shell transcript:

- `OCIRepository` status output
- artifact URL, revision, digest
- downloaded file type
- resulting extracted tree
- `cue export` output or error

## Expected Outcome Variants

### Best case

- Flux marks the source ready
- the fetched artifact is directly recoverable into a valid module tree
- no special OPM unpack bridge is needed beyond normal fetch/unpack logic

### Acceptable case

- Flux marks the source ready
- the fetched artifact contains an additional wrapper or preserves the native module payload indirectly
- OPM needs explicit zip extraction or content recovery logic

### Failure case

- Flux never reaches `Ready` for the source, or
- the fetched artifact cannot be converted back into a valid CUE module tree

## Prerequisites

- `cue`
- `kubectl`
- access to a Kubernetes cluster with Flux source-controller installed
- access to an OCI registry you can push to
- `curl`, `tar`, `unzip`, `file`

## Suggested Commands

The exact publish command depends on your registry setup, but the high-level order is:

```bash
# 1. Publish the minimal module with native CUE tooling.
# 2. Update manifests/ocirepository.yaml with the repository URL and tag.

kubectl apply -f experiments/001-cue-oci-validation/manifests/ocirepository.yaml

./experiments/001-cue-oci-validation/scripts/observe-ocirepository.sh \
  cue-oci-minimal \
  default

# Copy the artifact URL and digest from the script output.
./experiments/001-cue-oci-validation/scripts/fetch-and-validate.sh \
  "<artifact-url>" \
  "<artifact-digest>"
```

## Notes For Controller Design

If the experiment succeeds, we will know one of two things:

- Flux gives us a directly usable artifact handoff, or
- Flux gives us a source handoff that still requires an OPM-owned unpack/recovery step

Either result is enough to proceed with `ModuleRelease` implementation. The only truly blocking result is a failure to resolve or recover the native CUE module artifact at all.
