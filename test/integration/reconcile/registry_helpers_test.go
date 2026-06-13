/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package reconcile_test

import (
	"os"
	"os/exec"
	"strings"

	. "github.com/onsi/ginkgo/v2"
)

// skipIfNoTestRegistry skips the current spec when the local OCI registry is
// not available. Tests that load the fixture module (testing.opmodel.dev/modules/hello)
// require a local registry with the module published — the fixture is not
// available on remote registries like ghcr.
//
// Requirements:
//   - CUE_REGISTRY env var with a testing.opmodel.dev mapping pointing at
//     localhost (see `task registry:start && task module:publish`)
//   - the kernel-era deps published to the same local registry: the fixture
//     imports opmodel.dev/core@v0 and opmodel.dev/catalogs/opm@v0, and the
//     kernel enumerates catalog versions from the registry at materialize time.
//     Publish them from the core/ and catalog_opm/ repos at the versions pinned
//     in test/fixtures/modules/hello/cue.mod/module.cue (see the prerequisites
//     block in .tasks/module.yaml). Requires a cue CLI at the kernel's CUE
//     language version (v0.17.x).
//   - a container tool (docker or podman) on PATH
//
// When running against ghcr (CI default via `task dev:test`) these tests skip
// automatically; use `task dev:test:local` to run them.
func skipIfNoTestRegistry() {
	reg := os.Getenv("CUE_REGISTRY")
	if reg == "" {
		Skip("CUE_REGISTRY not set — run `task registry:start && task module:publish` first")
	}
	if !strings.Contains(reg, "testing.opmodel.dev=localhost") {
		Skip("CUE_REGISTRY does not map testing.opmodel.dev to localhost — " +
			"fixture module only available on local registry (use task dev:test:local)")
	}
	if !containerToolAvailable() {
		Skip("no container tool (docker/podman) on PATH — cannot validate local registry")
	}
}

// containerToolAvailable reports whether docker or podman is installed.
func containerToolAvailable() bool {
	for _, tool := range []string{"docker", "podman"} {
		if _, err := exec.LookPath(tool); err == nil {
			return true
		}
	}
	return false
}
