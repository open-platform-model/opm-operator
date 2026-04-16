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
// not available. End-to-end tests that invoke the real RegistryRenderer
// require:
//
//   - CUE_REGISTRY env var pointing at a reachable registry (see Makefile
//     `make start-registry && make publish-test-module`)
//   - a container tool (docker or podman) on PATH for connectivity checks
//
// When either is missing, the test skips with a clear message rather than
// failing, so stub-based tests still run in bare environments (including CI
// without container support).
func skipIfNoTestRegistry() {
	reg := os.Getenv("CUE_REGISTRY")
	if reg == "" {
		Skip("CUE_REGISTRY not set — run `make start-registry && make publish-test-module` first")
	}
	if !strings.Contains(reg, "testing.opmodel.dev=") {
		Skip("CUE_REGISTRY missing testing.opmodel.dev mapping — fixture module cannot be resolved")
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
