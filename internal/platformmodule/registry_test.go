package platformmodule

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"cuelang.org/go/cue"
	loaderfile "github.com/open-platform-model/library/opm/helper/loader/file"
	"github.com/open-platform-model/library/opm/kernel"

	"github.com/open-platform-model/opm-operator/test/fixtures"
)

// registryOrSkip returns the CUE registry mapping the registry-backed test
// resolves through, skipping (or failing under OPM_TEST_REGISTRY_FORCE=1,
// the CI posture) when none is configured.
func registryOrSkip(t *testing.T) string {
	t.Helper()
	reg := os.Getenv("CUE_REGISTRY")
	if reg == "" {
		if os.Getenv("OPM_TEST_REGISTRY_FORCE") == "1" {
			t.Fatal("OPM_TEST_REGISTRY_FORCE=1 but CUE_REGISTRY is not set")
		}
		t.Skip("CUE_REGISTRY not set; the closure-and-build test needs a registry serving core and the catalog")
	}
	return reg
}

// TestGenerate_BuildsThroughTheKernel is the end-to-end proof for the
// generated module: the closure derived from the published module files and
// the files Generate emits build through the kernel's shape-gated platform
// loader, the stamped version agrees with the catalog's readout, and the
// catalog's transitive dependency is pinned.
func TestGenerate_BuildsThroughTheKernel(t *testing.T) {
	reg := registryOrSkip(t)
	catalogPath := os.Getenv("OPM_TEST_CATALOG_PATH")
	if catalogPath == "" {
		catalogPath = "opmodel.dev/catalogs/opm@v4"
	}
	ctx := context.Background()

	src, err := NewRegistry(reg)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	entries := []Entry{{Path: catalogPath, Version: fixtures.CatalogVersion(), Enable: true}}
	deps, err := Closure(ctx, src, Roots(entries))
	if err != nil {
		t.Fatalf("Closure: %v", err)
	}
	pinned := map[string]string{}
	for _, d := range deps {
		pinned[d.Path] = d.Version
	}
	if pinned[catalogPath] != "v"+fixtures.CatalogVersion() {
		t.Fatalf("closure pins %s at %q, want v%s", catalogPath, pinned[catalogPath], fixtures.CatalogVersion())
	}
	if _, ok := pinned["cue.dev/x/k8s.io@v0"]; !ok {
		t.Fatalf("closure does not pin the catalog's transitive cue.dev/x/k8s.io dependency: %v", deps)
	}

	files, err := Generate(Input{Name: "cluster", Type: "kubernetes", Entries: entries, Deps: deps})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	dir := t.TempDir()
	for name, data := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	k := kernel.New(kernel.WithRegistry(reg))
	val, err := k.LoadPlatformPackage(ctx, dir, loaderfile.LoadOptions{Registry: reg})
	if err != nil {
		t.Fatalf("generated module does not build:\n%s\n%s\nerror: %v", files[ModuleFileName], files[PlatformFileName], err)
	}
	got, err := val.LookupPath(cue.ParsePath(`#registry."` + catalogPath + `".version`)).String()
	if err != nil {
		t.Fatalf("reading the entry's derived version: %v", err)
	}
	if got != fixtures.CatalogVersion() {
		t.Fatalf("derived version %q, want %q", got, fixtures.CatalogVersion())
	}
}
