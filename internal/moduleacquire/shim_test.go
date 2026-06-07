package moduleacquire

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	testModulePath    = "testing.opmodel.dev/modules/hello@v0"
	testModuleVersion = "v0.0.1"
)

func TestWriteShim_ModuleFile(t *testing.T) {
	dir, err := writeShim(testModulePath, testModuleVersion)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	modContent, err := os.ReadFile(filepath.Join(dir, "cue.mod", "module.cue"))
	if err != nil {
		t.Fatalf("reading module.cue: %v", err)
	}
	modStr := string(modContent)

	if !strings.Contains(modStr, `module: "`+shimModule+`"`) {
		t.Errorf("module.cue missing shim module path %q:\n%s", shimModule, modStr)
	}
	if !strings.Contains(modStr, cueLanguageVersion) {
		t.Errorf("module.cue missing language version %q:\n%s", cueLanguageVersion, modStr)
	}
	if !strings.Contains(modStr, `"`+testModulePath+`": v: "`+testModuleVersion+`"`) {
		t.Errorf("module.cue missing target module dependency:\n%s", modStr)
	}

	// The shim depends only on the target module: exactly one dep entry, and
	// no catalog path or catalog version pin appears anywhere.
	if got := strings.Count(modStr, `: v: "`); got != 1 {
		t.Errorf("expected exactly one dependency, found %d:\n%s", got, modStr)
	}
	for _, forbidden := range []string{"catalog", "opmodel.dev/core", "opmodel.dev/opm", "v1.3.4"} {
		if strings.Contains(modStr, forbidden) {
			t.Errorf("module.cue must not reference %q:\n%s", forbidden, modStr)
		}
	}
}

func TestWriteShim_PackageFile(t *testing.T) {
	dir, err := writeShim(testModulePath, testModuleVersion)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	pkgContent, err := os.ReadFile(filepath.Join(dir, "acquire.cue"))
	if err != nil {
		t.Fatalf("reading acquire.cue: %v", err)
	}
	pkgStr := string(pkgContent)

	if !strings.Contains(pkgStr, "package "+shimPackage) {
		t.Errorf("acquire.cue missing package declaration:\n%s", pkgStr)
	}
	if !strings.Contains(pkgStr, `import mod "`+testModulePath+`"`) {
		t.Errorf("acquire.cue missing target module import:\n%s", pkgStr)
	}
	// The module is embedded at the package root via a bare reference.
	if !strings.Contains(pkgStr, "\nmod\n") {
		t.Errorf("acquire.cue must embed the module at the root:\n%s", pkgStr)
	}
	for _, forbidden := range []string{"catalog", "#ModuleRelease", "opmodel.dev/core"} {
		if strings.Contains(pkgStr, forbidden) {
			t.Errorf("acquire.cue must not reference %q:\n%s", forbidden, pkgStr)
		}
	}
}

func TestWriteShim_DirectoryStructure(t *testing.T) {
	dir, err := writeShim(testModulePath, testModuleVersion)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	for _, path := range []string{
		"cue.mod/module.cue",
		"acquire.cue",
	} {
		if _, err := os.Stat(filepath.Join(dir, path)); err != nil {
			t.Errorf("expected file %q to exist: %v", path, err)
		}
	}
}

func TestWriteShim_Validation(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		version string
		errMsg  string
	}{
		{name: "empty path", path: "", version: "v0.1.0", errMsg: "module path is required"},
		{name: "empty version", path: "m@v0", version: "", errMsg: "module version is required"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := writeShim(tt.path, tt.version)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.errMsg) {
				t.Errorf("expected error containing %q, got: %v", tt.errMsg, err)
			}
		})
	}
}
