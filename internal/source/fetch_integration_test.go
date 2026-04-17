package source

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFetchIntegration(t *testing.T) {
	fixtureDir := filepath.Join("testdata", "minimal-module")

	// Build zip from fixture.
	zipDir := t.TempDir()
	zipPath := filepath.Join(zipDir, "module.zip")
	createZipFromDir(t, fixtureDir, zipPath)

	// Compute digest.
	zipData, err := os.ReadFile(zipPath)
	if err != nil {
		t.Fatalf("reading zip: %v", err)
	}
	h := sha256.Sum256(zipData)
	digest := fmt.Sprintf("sha256:%x", h)

	// Serve via httptest.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(zipData)
	}))
	defer srv.Close()

	// Fetch into temp dir.
	dest := t.TempDir()
	fetcher := &ArtifactFetcher{HTTPClient: srv.Client()}
	if err := fetcher.Fetch(context.Background(), srv.URL+"/artifact.tar.gz", digest, dest, FetchOptions{}); err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	// Assert cue.mod/module.cue exists with correct content.
	moduleCuePath := filepath.Join(dest, "cue.mod", "module.cue")
	moduleCue, err := os.ReadFile(moduleCuePath)
	if err != nil {
		t.Fatalf("reading cue.mod/module.cue: %v", err)
	}
	if !strings.Contains(string(moduleCue), "opmodel.dev/experiments/minimal") {
		t.Errorf("module.cue does not contain expected module path, got: %s", moduleCue)
	}

	// Assert main.cue exists with correct content.
	mainCuePath := filepath.Join(dest, "main.cue")
	mainCue, err := os.ReadFile(mainCuePath)
	if err != nil {
		t.Fatalf("reading main.cue: %v", err)
	}
	if !strings.Contains(string(mainCue), "hello from native cue oci") {
		t.Errorf("main.cue does not contain expected content, got: %s", mainCue)
	}

	// Validate the module tree is structurally valid.
	if err := ValidateCUEModule(dest); err != nil {
		t.Fatalf("ValidateCUEModule failed on fetched module: %v", err)
	}
}
