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

// registrySkip skips the current spec for a missing registry prerequisite —
// unless OPM_TEST_REGISTRY_FORCE=1 (set in CI), in which case the missing
// prerequisite is a hard failure. The knob mirrors the library's
// OPM_FLOW_TEST_FORCE idiom: registry-seam coverage must not be able to
// evaporate silently in an environment that promises to provide the registry.
func registrySkip(msg string) {
	if os.Getenv("OPM_TEST_REGISTRY_FORCE") == "1" {
		Fail("OPM_TEST_REGISTRY_FORCE=1 but registry prerequisite missing: " + msg)
	}
	Skip(msg)
}

// skipIfNoTestRegistry skips the current spec when the fixture registry is
// not available. Tests that load the fixture module (opmodel.dev/modules/test/hello)
// require a registry serving the module — the fixture is not published to ghcr.
//
// Two mappings satisfy it:
//   - full local: `opmodel.dev=localhost:5000+insecure` with the fixture AND
//     its deps (core, catalogs/opm) all published locally
//     (`task registry:start && task module:publish` plus the prerequisites
//     block in .tasks/module.yaml) — the historical `task dev:test:local` flow;
//   - mixed (CI): `opmodel.dev/modules/test=localhost:5000+insecure` for the
//     fixture with core/catalogs still resolving from ghcr — only the fixture
//     itself needs publishing (`task module:publish`), no sibling repos.
//
// Also needs a container tool (docker or podman) on PATH. Under
// OPM_TEST_REGISTRY_FORCE=1 a missing prerequisite fails instead of skipping.
func skipIfNoTestRegistry() {
	reg := os.Getenv("CUE_REGISTRY")
	if reg == "" {
		registrySkip("CUE_REGISTRY not set — run `task registry:start && task module:publish` first")
	}
	if !strings.Contains(reg, "opmodel.dev=localhost") &&
		!strings.Contains(reg, "opmodel.dev/modules/test=localhost") {
		registrySkip("CUE_REGISTRY maps neither opmodel.dev nor opmodel.dev/modules/test to localhost — " +
			"fixture module only available on a local registry (use task dev:test:local, " +
			"or the mixed mapping with `task module:publish`)")
	}
	if !containerToolAvailable() {
		registrySkip("no container tool (docker/podman) on PATH — cannot validate local registry")
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
