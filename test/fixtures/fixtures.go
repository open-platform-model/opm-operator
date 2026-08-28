// Package fixtures reads the declared coordinate of this repo's published
// test-fixture modules, so tests pin the version the tree carries instead of a
// literal that has to be edited on every bump.
//
// IDENTICAL COPY in cli/tests/fixtures/fixtures.go and
// opm-operator/test/fixtures/fixtures.go. The workspace root `task
// fixtures:lint` fails when the two drift; edit both.
//
// A fixture lives at <this dir>/modules/<name>/ and its identity package
// (identity/identity.cue) is the single source of ModulePath and Version. The
// identity package is import-free by design, so loading it needs no registry
// and no CUE_REGISTRY mapping. hack/fixtures.sh reads the same two fields with
// `cue eval ./identity`; this is the Go spelling of that read.
package fixtures

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/load"
)

// Coordinate is a fixture's declared module path and bare SemVer version.
type Coordinate struct {
	// ModulePath is the major-suffixed CUE module path, e.g.
	// testing.opmodel.dev/modules/operator/hello@v0.
	ModulePath string
	// Version is the bare SemVer, e.g. 0.0.6.
	Version string
}

// Tag is the registry tag for the version: "v" + Version.
func (c Coordinate) Tag() string { return "v" + c.Version }

// Dir returns the absolute path of the fixture modules directory. FIXTURES_DIR
// overrides the location next to this file (for trimmed builds or an
// out-of-tree checkout).
func Dir() (string, error) {
	if d := os.Getenv("FIXTURES_DIR"); d != "" {
		return filepath.Abs(d)
	}
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("fixtures: cannot locate this source file; set FIXTURES_DIR")
	}
	d := filepath.Join(filepath.Dir(file), "modules")
	if st, err := os.Stat(d); err != nil || !st.IsDir() {
		return "", fmt.Errorf("fixtures: %s is not a directory; set FIXTURES_DIR", d)
	}
	return d, nil
}

// Load reads the coordinate of the fixture module <Dir()>/<name>.
func Load(name string) (Coordinate, error) {
	root, err := Dir()
	if err != nil {
		return Coordinate{}, err
	}
	moduleDir := filepath.Join(root, name)
	if _, err := os.Stat(filepath.Join(moduleDir, "identity")); err != nil {
		return Coordinate{}, fmt.Errorf("fixtures: %s has no identity/ package: %w", name, err)
	}
	insts := load.Instances([]string{"./identity"}, &load.Config{Dir: moduleDir})
	if len(insts) != 1 {
		return Coordinate{}, fmt.Errorf("fixtures: %s: expected one identity instance, got %d", name, len(insts))
	}
	if insts[0].Err != nil {
		return Coordinate{}, fmt.Errorf("fixtures: %s: loading identity: %w", name, insts[0].Err)
	}
	v := cuecontext.New().BuildInstance(insts[0])
	if v.Err() != nil {
		return Coordinate{}, fmt.Errorf("fixtures: %s: building identity: %w", name, v.Err())
	}
	var c Coordinate
	for field, dst := range map[string]*string{"ModulePath": &c.ModulePath, "Version": &c.Version} {
		fv := v.LookupPath(cue.ParsePath(field))
		if fv.Err() != nil {
			return Coordinate{}, fmt.Errorf("fixtures: %s: %s: %w", name, field, fv.Err())
		}
		// Both fields are plain literals (core #IdentityPackage; the kernel's
		// loader gate rejects a defaulted disjunction), so no Default() fallback:
		// a non-concrete value is an error here, as it is at build time.
		s, err := fv.String()
		if err != nil {
			return Coordinate{}, fmt.Errorf("fixtures: %s: %s is not a concrete string: %w", name, field, err)
		}
		if s == "" {
			return Coordinate{}, fmt.Errorf("fixtures: %s: %s is empty", name, field)
		}
		*dst = s
	}
	return c, nil
}

// Failer is the slice of *testing.T (and Ginkgo's GinkgoT()) that Must needs.
type Failer interface {
	Helper()
	Fatal(args ...any)
}

// Must is Load for tests: a fixture that cannot be read fails the test.
func Must(t Failer, name string) Coordinate {
	t.Helper()
	c, err := Load(name)
	if err != nil {
		t.Fatal(err)
	}
	return c
}
