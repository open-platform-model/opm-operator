package fixtures

import (
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
