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

// serveZip creates an httptest.Server that serves the given zip file at any path.
func serveZip(t *testing.T, zipPath string) *httptest.Server {
	t.Helper()
	data, err := os.ReadFile(zipPath)
	if err != nil {
		t.Fatalf("reading zip for test server: %v", err)
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(data)
	}))
}

// digestOf computes the sha256 digest of a file in "sha256:<hex>" format.
func digestOf(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading file for digest: %v", err)
	}
	h := sha256.Sum256(data)
	return fmt.Sprintf("sha256:%x", h)
}

func TestArtifactFetcherFetch(t *testing.T) {
	fixtureDir := filepath.Join("testdata", "minimal-module")

	// Build fixture zip once for multiple subtests.
	zipDir := t.TempDir()
	zipPath := filepath.Join(zipDir, "module.zip")
	createZipFromDir(t, fixtureDir, zipPath)
	correctDigest := digestOf(t, zipPath)

	t.Run("digest match succeeds", func(t *testing.T) {
		srv := serveZip(t, zipPath)
		defer srv.Close()

		dest := t.TempDir()
		fetcher := &ArtifactFetcher{HTTPClient: srv.Client()}
		err := fetcher.Fetch(context.Background(), srv.URL+"/artifact.tar.gz", correctDigest, dest, FetchOptions{})
		if err != nil {
			t.Fatalf("Fetch returned error: %v", err)
		}

		// Assert extracted files present.
		if _, err := os.Stat(filepath.Join(dest, "cue.mod", "module.cue")); err != nil {
			t.Error("cue.mod/module.cue not found after fetch")
		}
		if _, err := os.Stat(filepath.Join(dest, "main.cue")); err != nil {
			t.Error("main.cue not found after fetch")
		}
	})

	t.Run("digest mismatch returns error", func(t *testing.T) {
		srv := serveZip(t, zipPath)
		defer srv.Close()

		dest := t.TempDir()
		fetcher := &ArtifactFetcher{HTTPClient: srv.Client()}
		err := fetcher.Fetch(context.Background(), srv.URL, "sha256:0000000000000000000000000000000000000000000000000000000000000000", dest, FetchOptions{})
		if err == nil {
			t.Fatal("expected error for digest mismatch, got nil")
		}
		if !strings.Contains(err.Error(), "digest mismatch") {
			t.Fatalf("expected digest mismatch error, got: %v", err)
		}

		// Assert no extracted files.
		entries, _ := os.ReadDir(dest)
		if len(entries) != 0 {
			t.Errorf("expected empty dest dir after digest mismatch, got %d entries", len(entries))
		}
	})

	t.Run("non-200 response returns error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer srv.Close()

		dest := t.TempDir()
		fetcher := &ArtifactFetcher{HTTPClient: srv.Client()}
		err := fetcher.Fetch(context.Background(), srv.URL, correctDigest, dest, FetchOptions{})
		if err == nil {
			t.Fatal("expected error for non-200 response, got nil")
		}
		if !strings.Contains(err.Error(), "404") {
			t.Fatalf("expected status 404 in error, got: %v", err)
		}
	})

	t.Run("size limit exceeded returns error", func(t *testing.T) {
		srv := serveZip(t, zipPath)
		defer srv.Close()

		dest := t.TempDir()
		fetcher := &ArtifactFetcher{
			HTTPClient: srv.Client(),
			MaxSize:    10, // Tiny limit to trigger overflow.
		}
		err := fetcher.Fetch(context.Background(), srv.URL, correctDigest, dest, FetchOptions{})
		if err == nil {
			t.Fatal("expected error for size limit exceeded, got nil")
		}
		if !strings.Contains(err.Error(), "exceeds limit") {
			t.Fatalf("expected size limit error, got: %v", err)
		}
	})

	t.Run("tar.gz format extracts correctly", func(t *testing.T) {
		tarDir := t.TempDir()
		tarPath := filepath.Join(tarDir, "module.tar.gz")
		createTarGzFromDir(t, fixtureDir, tarPath)
		tarDigest := digestOf(t, tarPath)

		srv := serveZip(t, tarPath) // serves bytes; filename irrelevant
		defer srv.Close()

		dest := t.TempDir()
		fetcher := &ArtifactFetcher{HTTPClient: srv.Client()}
		err := fetcher.Fetch(context.Background(), srv.URL+"/artifact.tar.gz", tarDigest, dest, FetchOptions{Format: ArchiveFormatTarGz})
		if err != nil {
			t.Fatalf("tar.gz Fetch returned error: %v", err)
		}
		if _, err := os.Stat(filepath.Join(dest, "cue.mod", "module.cue")); err != nil {
			t.Error("cue.mod/module.cue not found after tar.gz fetch")
		}
	})

	t.Run("skip root CUE module validation", func(t *testing.T) {
		// Build a tar.gz with no cue.mod at the root.
		tarDir := t.TempDir()
		src := filepath.Join(tarDir, "src")
		if err := os.MkdirAll(filepath.Join(src, "releases", "prod"), 0o755); err != nil {
			t.Fatalf("prep src: %v", err)
		}
		if err := os.WriteFile(filepath.Join(src, "releases", "prod", "release.cue"), []byte("package release\n"), 0o644); err != nil {
			t.Fatalf("prep file: %v", err)
		}
		tarPath := filepath.Join(tarDir, "bare.tar.gz")
		createTarGzFromDir(t, src, tarPath)
		tarDigest := digestOf(t, tarPath)

		srv := serveZip(t, tarPath)
		defer srv.Close()

		dest := t.TempDir()
		fetcher := &ArtifactFetcher{HTTPClient: srv.Client()}
		err := fetcher.Fetch(context.Background(), srv.URL, tarDigest, dest, FetchOptions{
			Format:                      ArchiveFormatTarGz,
			SkipRootCUEModuleValidation: true,
		})
		if err != nil {
			t.Fatalf("Fetch with skip validation returned error: %v", err)
		}
		if _, err := os.Stat(filepath.Join(dest, "releases", "prod", "release.cue")); err != nil {
			t.Error("release.cue not found after fetch")
		}
	})

	t.Run("Flux path quirk: zip served from .tar.gz URL", func(t *testing.T) {
		srv := serveZip(t, zipPath)
		defer srv.Close()

		dest := t.TempDir()
		fetcher := &ArtifactFetcher{HTTPClient: srv.Client()}
		// URL ends in .tar.gz but body is zip — fetcher must handle this.
		err := fetcher.Fetch(context.Background(), srv.URL+"/ocirepository/default/my-repo/sha256:abc123.tar.gz", correctDigest, dest, FetchOptions{})
		if err != nil {
			t.Fatalf("Fetch returned error for .tar.gz URL: %v", err)
		}

		if _, err := os.Stat(filepath.Join(dest, "cue.mod", "module.cue")); err != nil {
			t.Error("cue.mod/module.cue not found after .tar.gz URL fetch")
		}
	})
}
