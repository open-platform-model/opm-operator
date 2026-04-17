package source

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
)

// MaxArtifactSize is the maximum allowed artifact download size (64 MB).
const MaxArtifactSize int64 = 64 << 20

// Fetcher fetches a resolved source artifact into a local directory.
type Fetcher interface {
	Fetch(ctx context.Context, artifactURL, artifactDigest, dir string) error
}

// ArtifactFetcher downloads OCI artifacts via HTTP and extracts them as zip files.
// Implements the Fetcher interface.
//
// Uses direct HTTP fetch with SHA-256 digest verification rather than Flux's
// ArchiveFetcher, because Flux assumes tar.gz format while CUE OCI artifacts
// use zip format.
type ArtifactFetcher struct {
	// HTTPClient is the HTTP client used for downloads. If nil, http.DefaultClient is used.
	HTTPClient *http.Client

	// MaxSize is the maximum artifact size in bytes. Defaults to MaxArtifactSize.
	MaxSize int64
}

// Fetch downloads the artifact from artifactURL, verifies its SHA-256 digest matches
// artifactDigest, extracts the zip contents to dir, and validates the CUE module layout.
//
// The artifact body is expected to be a zip file despite the URL path possibly
// ending in .tar.gz (Flux artifact format quirk confirmed in experiment 001).
func (f *ArtifactFetcher) Fetch(ctx context.Context, artifactURL, artifactDigest, dir string) error {
	client := f.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}

	maxSize := f.MaxSize
	if maxSize == 0 {
		maxSize = MaxArtifactSize
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, artifactURL, nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("downloading artifact: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("downloading artifact: status %d", resp.StatusCode)
	}

	tmpFile, err := os.CreateTemp("", "artifact-*.zip")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	hasher := sha256.New()
	// Read up to maxSize+1 to detect overflow without reading the entire body.
	reader := io.LimitReader(resp.Body, maxSize+1)
	written, err := io.Copy(tmpFile, io.TeeReader(reader, hasher))
	_ = tmpFile.Close()
	if err != nil {
		return fmt.Errorf("downloading artifact: %w", err)
	}
	if written > maxSize {
		return fmt.Errorf("artifact size exceeds limit of %d bytes", maxSize)
	}

	got := fmt.Sprintf("sha256:%x", hasher.Sum(nil))
	if got != artifactDigest {
		return fmt.Errorf("artifact digest mismatch: got %s, want %s", got, artifactDigest)
	}

	if err := extractZip(tmpPath, dir); err != nil {
		return fmt.Errorf("extracting artifact: %w", err)
	}

	if err := ValidateCUEModule(dir); err != nil {
		return err
	}

	return nil
}
