package synthesis

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSynthesizeRelease_Success(t *testing.T) {
	dir, err := SynthesizeRelease(ReleaseParams{
		Name:          "cert-manager",
		Namespace:     "cert-manager",
		ModulePath:    "opmodel.dev/modules/cert_manager@v0",
		ModuleVersion: "v0.2.1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	// Verify cue.mod/module.cue exists and has correct content.
	modContent, err := os.ReadFile(filepath.Join(dir, "cue.mod", "module.cue"))
	if err != nil {
		t.Fatalf("reading module.cue: %v", err)
	}

	modStr := string(modContent)
	if !strings.Contains(modStr, SynthesisModule) {
		t.Errorf("module.cue missing synthesis module path %q", SynthesisModule)
	}
	if !strings.Contains(modStr, CUELanguageVersion) {
		t.Errorf("module.cue missing language version %q", CUELanguageVersion)
	}
	if !strings.Contains(modStr, CatalogVersion) {
		t.Errorf("module.cue missing catalog version %q", CatalogVersion)
	}
	if !strings.Contains(modStr, "opmodel.dev/modules/cert_manager@v0") {
		t.Error("module.cue missing target module dependency")
	}
	if !strings.Contains(modStr, `v: "v0.2.1"`) {
		t.Error("module.cue missing target module version")
	}

	// Verify release.cue exists and has correct content.
	relContent, err := os.ReadFile(filepath.Join(dir, "release.cue"))
	if err != nil {
		t.Fatalf("reading release.cue: %v", err)
	}

	relStr := string(relContent)
	if !strings.Contains(relStr, "package release") {
		t.Error("release.cue missing package declaration")
	}
	if !strings.Contains(relStr, `mr "opmodel.dev/core/v1alpha1/modulerelease@v1"`) {
		t.Error("release.cue missing modulerelease import")
	}
	if !strings.Contains(relStr, `mod "opmodel.dev/modules/cert_manager@v0"`) {
		t.Error("release.cue missing target module import")
	}
	if !strings.Contains(relStr, "mr.#ModuleRelease") {
		t.Error("release.cue missing #ModuleRelease embedding")
	}
	if !strings.Contains(relStr, `name:      "cert-manager"`) {
		t.Error("release.cue missing metadata.name")
	}
	if !strings.Contains(relStr, `namespace: "cert-manager"`) {
		t.Error("release.cue missing metadata.namespace")
	}
	if !strings.Contains(relStr, "#module: mod") {
		t.Error("release.cue missing #module binding")
	}
}

func TestSynthesizeRelease_CleanupOnError(t *testing.T) {
	// Verify that no temp dirs leak when validation fails.
	_, err := SynthesizeRelease(ReleaseParams{
		Name:          "",
		Namespace:     "default",
		ModulePath:    "opmodel.dev/modules/test@v0",
		ModuleVersion: "v0.1.0",
	})
	if err == nil {
		t.Fatal("expected error for empty name")
	}
	if !strings.Contains(err.Error(), "name is required") {
		t.Errorf("expected 'name is required' error, got: %v", err)
	}
}

func TestSynthesizeRelease_Validation(t *testing.T) {
	tests := []struct {
		name   string
		params ReleaseParams
		errMsg string
	}{
		{
			name:   "empty name",
			params: ReleaseParams{Namespace: "ns", ModulePath: "m@v0", ModuleVersion: "v0.1.0"},
			errMsg: "name is required",
		},
		{
			name:   "empty namespace",
			params: ReleaseParams{Name: "n", ModulePath: "m@v0", ModuleVersion: "v0.1.0"},
			errMsg: "namespace is required",
		},
		{
			name:   "empty module path",
			params: ReleaseParams{Name: "n", Namespace: "ns", ModuleVersion: "v0.1.0"},
			errMsg: "module path is required",
		},
		{
			name:   "empty module version",
			params: ReleaseParams{Name: "n", Namespace: "ns", ModulePath: "m@v0"},
			errMsg: "module version is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := SynthesizeRelease(tt.params)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.errMsg) {
				t.Errorf("expected error containing %q, got: %v", tt.errMsg, err)
			}
		})
	}
}

func TestSynthesizeRelease_DirectoryStructure(t *testing.T) {
	dir, err := SynthesizeRelease(ReleaseParams{
		Name:          "test",
		Namespace:     "default",
		ModulePath:    "opmodel.dev/modules/hello@v0",
		ModuleVersion: "v0.1.0",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	// Verify expected files exist.
	for _, path := range []string{
		"cue.mod/module.cue",
		"release.cue",
	} {
		if _, err := os.Stat(filepath.Join(dir, path)); err != nil {
			t.Errorf("expected file %q to exist: %v", path, err)
		}
	}
}
