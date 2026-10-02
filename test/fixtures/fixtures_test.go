package fixtures

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// IDENTICAL COPY in both repos (see fixtures.go); both carry a podinfo fixture.

func TestLoadPodinfo(t *testing.T) {
	c := Must(t, "podinfo")
	wellFormed := strings.HasPrefix(c.ModulePath, "testing.opmodel.dev/modules/") &&
		strings.HasSuffix(c.ModulePath, "/podinfo@v0")
	if !wellFormed {
		t.Fatalf("ModulePath = %q, want testing.opmodel.dev/modules/<repo>/podinfo@v0", c.ModulePath)
	}
	if !regexp.MustCompile(`^\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$`).MatchString(c.Version) {
		t.Fatalf("Version = %q, want bare SemVer", c.Version)
	}
	if c.Tag() != "v"+c.Version {
		t.Fatalf("Tag() = %q", c.Tag())
	}
}

func TestLoadMissing(t *testing.T) {
	if _, err := Load("does-not-exist"); err == nil {
		t.Fatal("Load of a missing fixture must fail")
	}
	if _, err := LoadCatalog("does-not-exist"); err == nil {
		t.Fatal("LoadCatalog of a missing fixture must fail")
	}
}

// TestLoadCatalogs: every catalog fixture, where a repo has any, loads and
// sits on the testing domain's catalogs segment.
func TestLoadCatalogs(t *testing.T) {
	root, err := CatalogDir()
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		t.Skipf("no catalog fixtures under %s", root)
	}
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		c := MustCatalog(t, e.Name())
		if !strings.HasPrefix(c.ModulePath, "testing.opmodel.dev/catalogs/") {
			t.Errorf("%s: ModulePath = %q, want testing.opmodel.dev/catalogs/<repo>/%s@vN", e.Name(), c.ModulePath, e.Name())
		}
	}
}

// TestIdentityIsLiteral: every fixture's identity package, module or
// catalog, declares Version as a plain string literal, with no default arm and
// no local #VersionType. The kernel's loader gate rejects a defaulted
// disjunction as non-concrete.
func TestIdentityIsLiteral(t *testing.T) {
	root, err := Dir()
	if err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join(root, "*", "identity", "identity.cue"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatalf("no identity.cue under %s", root)
	}
	catalogRoot, err := CatalogDir()
	if err != nil {
		t.Fatal(err)
	}
	catalogFiles, err := filepath.Glob(filepath.Join(catalogRoot, "*", "identity", "identity.cue"))
	if err != nil {
		t.Fatal(err)
	}
	files = append(files, catalogFiles...)
	versionLine := regexp.MustCompile(`(?m)^Version:\s*"\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?"\s*$`)
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		src := string(data)
		if !versionLine.MatchString(src) {
			t.Errorf("%s: Version must be a plain SemVer literal (`Version: \"X.Y.Z\"`)", f)
		}
		if strings.Contains(src, "#VersionType") {
			t.Errorf("%s: declares a local #VersionType; core #IdentityPackage already constrains Version", f)
		}
	}
}
