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

// ArchiveFormat identifies the encoding of a Flux source artifact.
type ArchiveFormat int

const (
	// ArchiveFormatZip is used by Flux OCIRepository artifacts.
	ArchiveFormatZip ArchiveFormat = iota
	// ArchiveFormatTarGz is used by Flux GitRepository and Bucket artifacts.
	ArchiveFormatTarGz
)

// FetchOptions controls extraction behavior for ArtifactFetcher.Fetch.
type FetchOptions struct {
	// Format selects the extraction format. Defaults to ArchiveFormatZip.
	Format ArchiveFormat

	// SkipRootCUEModuleValidation skips the post-extraction check that
	// cue.mod/module.cue exists at the destination root. Callers that place
	// the CUE module at a subdirectory (Release CR) should enable this.
	SkipRootCUEModuleValidation bool
}

// FormatForKind returns the archive format used by the given Flux source kind
// when consumed by the Release reconciler.
//
// All three kinds produce tar.gz: GitRepository/Bucket bundle the repo tree as
// tar.gz natively, and OCIRepository artifacts for Release are expected to be
// published via `flux push artifact`, which emits
// `application/vnd.cncf.flux.content.v1.tar+gzip`.
//
// The legacy zip format (used by `cue mod publish`) belongs to the
// ModuleRelease cue-native path and does NOT flow through this fetcher.
func FormatForKind(_ string) ArchiveFormat {
	return ArchiveFormatTarGz
}

// Fetcher fetches a resolved source artifact into a local directory.
type Fetcher interface {
	Fetch(ctx context.Context, artifactURL, artifactDigest, dir string, opts FetchOptions) error
}

// ArtifactFetcher downloads Flux source artifacts via HTTP and extracts them
// as either zip or tar.gz. Implements the Fetcher interface.
//
// Zip is used by CUE OCI artifacts (despite URLs often ending in .tar.gz);
// tar.gz is used by GitRepository and Bucket artifacts. The caller selects
// the format via FetchOptions.
type ArtifactFetcher struct {
	// HTTPClient is the HTTP client used for downloads. If nil, http.DefaultClient is used.
	HTTPClient *http.Client

	// MaxSize is the maximum artifact size in bytes. Defaults to MaxArtifactSize.
	MaxSize int64
}

// Fetch downloads the artifact from artifactURL, verifies its SHA-256 digest
// matches artifactDigest, and extracts it to dir using the format selected in
// opts. When opts.SkipRootCUEModuleValidation is false, ValidateCUEModule is
// called on dir after extraction.
func (f *ArtifactFetcher) Fetch(ctx context.Context, artifactURL, artifactDigest, dir string, opts FetchOptions) error {
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

	tmpFile, err := os.CreateTemp("", "artifact-*")
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

	switch opts.Format {
	case ArchiveFormatZip:
		if err := extractZip(tmpPath, dir); err != nil {
			return fmt.Errorf("extracting artifact: %w", err)
		}
	case ArchiveFormatTarGz:
		if err := extractTarGz(tmpPath, dir); err != nil {
			return fmt.Errorf("extracting artifact: %w", err)
		}
	default:
		return fmt.Errorf("unknown archive format: %d", opts.Format)
	}

	if opts.SkipRootCUEModuleValidation {
		return nil
	}

	return ValidateCUEModule(dir)
}
