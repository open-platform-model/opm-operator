package catalog

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func testdataDir(name string) string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "testdata", name)
}

func TestLoadProvider_Success(t *testing.T) {
	p, err := LoadProvider(testdataDir("valid-catalog"), "kubernetes")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p == nil {
		t.Fatal("expected non-nil provider")
	}
	if p.Metadata == nil {
		t.Fatal("expected non-nil metadata")
	}
	if p.Metadata.Name != "kubernetes" {
		t.Errorf("expected provider name %q, got %q", "kubernetes", p.Metadata.Name)
	}
}

func TestLoadProvider_DefaultProviderName(t *testing.T) {
	// Empty provider name should default to "kubernetes".
	p, err := LoadProvider(testdataDir("valid-catalog"), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Metadata.Name != "kubernetes" {
		t.Errorf("expected default provider name %q, got %q", "kubernetes", p.Metadata.Name)
	}
}

func TestLoadProvider_MissingDirectory(t *testing.T) {
	_, err := LoadProvider(testdataDir("nonexistent"), "kubernetes")
	if err == nil {
		t.Fatal("expected error for missing directory")
	}
}

func TestLoadProvider_MissingCUEModule(t *testing.T) {
	// Without cue.mod/module.cue, CUE load.Instances returns an error.
	_, err := LoadProvider(testdataDir("no-module"), "kubernetes")
	if err == nil {
		t.Fatal("expected error for missing cue.mod/module.cue")
	}
}

func TestLoadProvider_ProviderNotFound(t *testing.T) {
	_, err := LoadProvider(testdataDir("valid-catalog"), "nonexistent-provider")
	if err == nil {
		t.Fatal("expected error for missing provider")
	}
	// Error should mention available providers.
	if !strings.Contains(err.Error(), "kubernetes") {
		t.Errorf("expected error to list available providers, got: %s", err)
	}
}

func TestLoadProvider_InvalidCUE(t *testing.T) {
	_, err := LoadProvider(testdataDir("invalid-catalog"), "kubernetes")
	if err == nil {
		t.Fatal("expected error for invalid CUE module")
	}
}

func TestLoadProvider_EmptyRegistry(t *testing.T) {
	_, err := LoadProvider(testdataDir("empty-registry"), "kubernetes")
	if err == nil {
		t.Fatal("expected error for empty registry")
	}
}

// TODO: Add e2e test for registry-unreachable scenario.
// When CUE_REGISTRY points to an unreachable host, LoadProvider should
// return an error with CUE diagnostic context (registry resolution failure).
// Requires a composition fixture with real imports (not self-contained).
func TestLoadProvider_RegistryUnreachable(t *testing.T) {
	t.Skip("TODO: requires composition fixture with external imports to trigger registry resolution")
}
