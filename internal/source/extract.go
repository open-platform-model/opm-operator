package source

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// MaxZipFiles is the maximum number of files allowed in a zip archive.
const MaxZipFiles = 10000

// extractZip extracts the zip archive at zipPath into destDir.
// Rejects entries with path traversal components ("..") to prevent
// zip slip attacks. Enforces MaxZipFiles limit.
func extractZip(zipPath, destDir string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("opening zip archive: %w", err)
	}
	defer func() { _ = r.Close() }()

	if len(r.File) > MaxZipFiles {
		return fmt.Errorf("zip archive contains %d files, exceeds limit of %d", len(r.File), MaxZipFiles)
	}

	for _, f := range r.File {
		name := filepath.Clean(f.Name)
		if filepath.IsAbs(name) || strings.HasPrefix(name, "..") {
			return fmt.Errorf("zip entry %q contains path traversal", f.Name)
		}

		target := filepath.Join(destDir, name)

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("creating directory %s: %w", target, err)
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf("creating parent directory for %s: %w", target, err)
		}

		if err := extractZipFile(f, target); err != nil {
			return err
		}
	}

	return nil
}

func extractZipFile(f *zip.File, target string) error {
	rc, err := f.Open()
	if err != nil {
		return fmt.Errorf("opening zip entry %s: %w", f.Name, err)
	}
	defer func() { _ = rc.Close() }()

	out, err := os.Create(target)
	if err != nil {
		return fmt.Errorf("creating file %s: %w", target, err)
	}
	defer func() { _ = out.Close() }()

	if _, err := io.Copy(out, rc); err != nil {
		return fmt.Errorf("extracting %s: %w", f.Name, err)
	}

	return nil
}
