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
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	loaderfile "github.com/open-platform-model/library/opm/helper/loader/file"
	"github.com/open-platform-model/library/opm/helper/platformmodule"
	"github.com/open-platform-model/library/opm/kernel"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"

	opmcontroller "github.com/open-platform-model/opm-operator/internal/controller"
	platformstore "github.com/open-platform-model/opm-operator/internal/platform"
	"github.com/open-platform-model/opm-operator/test/fixtures"
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

// newTestModFileSource is the module-file source the specs derive closures
// through: the reconciler's own configuration, minus the client type.
func newTestModFileSource(registry string) (platformmodule.ModFileSource, error) {
	return platformmodule.NewRegistry(platformmodule.RegistryConfig{Registry: registry, Env: os.Environ()})
}

// defaultTestCatalogPath is the catalog the registry-backed specs subscribe
// to when OPM_TEST_CATALOG_PATH is unset: the first-party abstraction
// catalog, resolved from GHCR under `task dev:test`.
const defaultTestCatalogPath = "opmodel.dev/catalogs/opm@v4"

// testCatalogPath is the catalog the registry-backed specs subscribe to:
// OPM_TEST_CATALOG_PATH when set (seeded CI), defaultTestCatalogPath otherwise.
func testCatalogPath() string {
	if p := os.Getenv("OPM_TEST_CATALOG_PATH"); p != "" {
		return p
	}
	return defaultTestCatalogPath
}

// testCatalogVersion is the exact catalog build the registry-backed specs
// subscribe to — the shared default lives in fixtures.CatalogVersion so a
// catalog bump edits one place for every suite.
func testCatalogVersion() string {
	return fixtures.CatalogVersion()
}

// skipIfNoTestRegistry skips the current spec when no registry is configured to
// serve the fixture modules (testing.opmodel.dev/modules/operator/*) and their
// deps (opmodel.dev/core, opmodel.dev/catalogs/opm).
//
// The fixtures are published to GHCR, so the ordinary GHCR mapping satisfies
// this and these specs run in plain CI with no local registry — the local
// registry is only needed to iterate on an unpublished fixture. The predicate is
// therefore "is a mapping configured at all", not "does it point at localhost".
//
// It deliberately does NOT test for a localhost mapping any more. The former
// check was wrong twice over: `strings.Contains(reg, "opmodel.dev=localhost")`
// also matches `testing.opmodel.dev=localhost`, so it proved nothing about the
// fixture's own namespace; and once the fixtures moved to a published domain it
// would have skipped these specs in every CI run while claiming the fixtures
// were unavailable.
//
// A local mapping additionally requires a container tool to be running the
// registry, so that check is kept but scoped to the local case. Under
// OPM_TEST_REGISTRY_FORCE=1 a missing prerequisite fails instead of skipping.
func skipIfNoTestRegistry() {
	reg := os.Getenv("CUE_REGISTRY")
	if reg == "" {
		registrySkip("CUE_REGISTRY not set — export the canonical GHCR mapping, " +
			"or run `task registry:start && task module:publish` for a local fixture")
	}
	if !strings.Contains(reg, "opmodel.dev") {
		registrySkip("CUE_REGISTRY maps no opmodel.dev domain — cannot resolve the " +
			"fixture module or its core/catalog deps")
	}
	if strings.Contains(reg, "localhost") && !containerToolAvailable() {
		registrySkip("CUE_REGISTRY maps a localhost registry but no container tool " +
			"(docker/podman) is on PATH — cannot validate it")
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

// generatedPlatformStore seeds a store the way the PlatformReconciler does:
// it derives the dependency closure of one catalog subscription (the exact
// build the fixture modules target, 0010 D14), generates the platform module
// into a per-spec temporary Layout, builds it through the kernel's
// source-carrying platform acquisition (the record Kernel.Render imports the
// platform from) and records it as generation 1 under the given skew policy.
// The registry-backed specs then exercise the same path the reconciler does.
// A registry or catalog that does not resolve skips the spec (or fails under
// OPM_TEST_REGISTRY_FORCE=1).
func generatedPlatformStore(k *kernel.Kernel, registry string, skew kernel.SkewPolicy) *platformstore.Store {
	GinkgoHelper()
	return generatedPlatformStoreAt(k, registry, testCatalogVersion(), skew)
}

// generatedPlatformStoreAt is generatedPlatformStore pinned to an explicit
// catalog build, for the skew specs.
func generatedPlatformStoreAt(
	k *kernel.Kernel,
	registry, catalogVersion string,
	skew kernel.SkewPolicy,
) *platformstore.Store {
	GinkgoHelper()
	src, err := newTestModFileSource(registry)
	Expect(err).NotTo(HaveOccurred())
	entries := []platformmodule.Entry{{Path: testCatalogPath(), Version: catalogVersion, Enable: true}}
	deps, err := platformmodule.Closure(ctx, src, platformmodule.Roots(entries))
	if err != nil {
		registrySkip("core and catalog not resolvable from CUE_REGISTRY: " + err.Error())
	}
	files, err := platformmodule.Generate(platformmodule.Input{
		Name:       "cluster",
		Type:       "kubernetes",
		ModulePath: opmcontroller.PlatformModulePath,
		Entries:    entries,
		Deps:       deps,
	})
	Expect(err).NotTo(HaveOccurred())

	layout := platformstore.Layout{Root: filepath.Join(GinkgoT().TempDir(), "platform")}
	dir, err := layout.Write(1, files)
	Expect(err).NotTo(HaveOccurred())

	plat, err := k.AcquirePlatformFromDir(ctx, dir, loaderfile.LoadOptions{Registry: registry})
	if err != nil {
		registrySkip("building the generated platform module failed (registry/schema unreachable): " + err.Error())
	}
	Expect(plat.Source).NotTo(BeNil(), "the acquired platform must carry its on-disk source")

	store := platformstore.NewStore()
	store.SetGenerated(platformstore.Generated{Generation: 1, Dir: dir, Platform: plat, Skew: skew})
	return store
}

// keyOfObject returns the namespaced name of obj.
func keyOfObject(obj client.Object) types.NamespacedName {
	return types.NamespacedName{Name: obj.GetName(), Namespace: obj.GetNamespace()}
}

// countRecordedEvents drains rec and returns how many of the recorded events
// carry reason.
func countRecordedEvents(rec *events.FakeRecorder, reason string) int {
	n := 0
	for {
		select {
		case e := <-rec.Events:
			if strings.Contains(e, reason) {
				n++
			}
		default:
			return n
		}
	}
}
