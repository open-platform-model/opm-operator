package source

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// MaxZipFiles is the maximum number of files allowed in a zip archive.
const MaxZipFiles = 10000

// MaxTarFiles is the maximum number of files allowed in a tar.gz archive.
const MaxTarFiles = 10000

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

// extractTarGz extracts the gzipped tar archive at tarPath into destDir.
// Rejects entries with path traversal components ("..") to prevent tar slip
// attacks. Enforces MaxTarFiles limit. Only regular files, directories, and
// symlinks are extracted; symlink targets are validated to stay within destDir.
func extractTarGz(tarPath, destDir string) error {
	f, err := os.Open(tarPath)
	if err != nil {
		return fmt.Errorf("opening tar.gz archive: %w", err)
	}
	defer func() { _ = f.Close() }()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("opening gzip stream: %w", err)
	}
	defer func() { _ = gz.Close() }()

	tr := tar.NewReader(gz)
	count := 0
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("reading tar entry: %w", err)
		}

		count++
		if count > MaxTarFiles {
			return fmt.Errorf("tar archive contains more than %d files", MaxTarFiles)
		}

		name := filepath.Clean(hdr.Name)
		if filepath.IsAbs(name) || strings.HasPrefix(name, "..") || name == ".." {
			return fmt.Errorf("tar entry %q contains path traversal", hdr.Name)
		}

		target := filepath.Join(destDir, name)

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("creating directory %s: %w", target, err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("creating parent directory for %s: %w", target, err)
			}
			if err := extractTarFile(tr, target, hdr.Mode); err != nil {
				return err
			}
		case tar.TypeSymlink:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("creating parent directory for %s: %w", target, err)
			}
			linkTarget := filepath.Clean(filepath.Join(filepath.Dir(target), hdr.Linkname))
			if !strings.HasPrefix(linkTarget, filepath.Clean(destDir)+string(os.PathSeparator)) && linkTarget != filepath.Clean(destDir) {
				return fmt.Errorf("tar symlink %q escapes destination", hdr.Name)
			}
			if err := os.Symlink(hdr.Linkname, target); err != nil {
				return fmt.Errorf("creating symlink %s: %w", target, err)
			}
		default:
			// Skip unsupported types (block/char devices, fifos, etc.).
			continue
		}
	}

	return nil
}

func extractTarFile(tr *tar.Reader, target string, mode int64) error {
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, os.FileMode(mode)&0o777)
	if err != nil {
		return fmt.Errorf("creating file %s: %w", target, err)
	}
	defer func() { _ = out.Close() }()

	if _, err := io.Copy(out, tr); err != nil {
		return fmt.Errorf("extracting %s: %w", target, err)
	}
	return nil
}
