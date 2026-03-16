## Why

After source resolution (change 2) provides the artifact URL and digest, the controller must download the artifact and recover a valid CUE module tree. Experiment 001 proved that Flux stores native CUE OCI artifacts as zip files despite a `.tar.gz` path suffix. The controller needs its own fetch-and-unzip path since Flux's default tar extraction fails on CUE's native zip format.

## What Changes

- Implement the `Fetcher` interface in `internal/source` to download artifacts via HTTP with digest verification.
- Add zip extraction logic that handles the Flux artifact format quirk (zip body, `.tar.gz` path).
- Add CUE module layout validation (`cue.mod/module.cue` exists and is parseable).
- Return a clean temp directory containing the extracted CUE module tree.

## Capabilities

### New Capabilities
- `artifact-fetch`: Download a Flux artifact by URL, verify its digest, extract the zip payload, validate CUE module layout, and return a usable directory.

### Modified Capabilities

## Impact

- `internal/source/` — new `Fetcher` implementation, zip extraction, CUE module validation.
- Depends on: change 2 (source resolution) for `ArtifactRef`.
- SemVer: MINOR — new capability.
