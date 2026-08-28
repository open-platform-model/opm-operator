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
}

// TestIdentityIsLiteral: every fixture's identity package declares Version
// as a plain string literal, with no default arm and no local #VersionType.
// The kernel's loader gate rejects a defaulted disjunction as non-concrete.
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
