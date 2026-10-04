# operator-module-release-check fixtures

`module/` is a minimal tree shaped like `modules/opm_operator` (its `cue.mod`, `identity` and `operator` packages only). `hack/operator-module/test-release-check.sh` copies it into a scratch git repository per case, edits the copy for the case, and runs `hack/operator-module/release-check.sh` there with passing stubs for the release guard, the image tag guard and the drift check.
