package source

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

// createZipFromDir creates a zip archive at destPath containing all files
// from srcDir, preserving relative paths.
func createZipFromDir(t *testing.T, srcDir, destPath string) {
	t.Helper()

	out, err := os.Create(destPath)
	if err != nil {
		t.Fatalf("creating zip file: %v", err)
	}
	defer func() { _ = out.Close() }()

	w := zip.NewWriter(out)
	defer func() { _ = w.Close() }()

	err = filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}

		if info.IsDir() {
			_, err := w.Create(rel + "/")
			return err
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		fw, err := w.Create(rel)
		if err != nil {
			return err
		}
		_, err = fw.Write(data)
		return err
	})
	if err != nil {
		t.Fatalf("walking source dir: %v", err)
	}
}

// createTraversalZip creates a zip with a path traversal entry.
func createTraversalZip(t *testing.T, destPath string) {
	t.Helper()

	out, err := os.Create(destPath)
	if err != nil {
		t.Fatalf("creating zip: %v", err)
	}
	defer func() { _ = out.Close() }()

	w := zip.NewWriter(out)
	fw, err := w.Create("../escape.txt")
	if err != nil {
		t.Fatalf("creating zip entry: %v", err)
	}
	_, _ = fw.Write([]byte("escaped"))
	_ = w.Close()
}

// createEmptyZip creates a valid zip archive with zero entries.
func createEmptyZip(t *testing.T, destPath string) {
	t.Helper()

	out, err := os.Create(destPath)
	if err != nil {
		t.Fatalf("creating zip: %v", err)
	}
	defer func() { _ = out.Close() }()

	w := zip.NewWriter(out)
	_ = w.Close()
}

// createTarGzFromDir creates a tar.gz archive at destPath containing all
// files from srcDir, preserving relative paths.
func createTarGzFromDir(t *testing.T, srcDir, destPath string) {
	t.Helper()

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	err := filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}

		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = rel
		if info.IsDir() {
			hdr.Name += "/"
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		_, err = tw.Write(data)
		return err
	})
	if err != nil {
		t.Fatalf("walking source dir: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("closing tar writer: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("closing gzip writer: %v", err)
	}
	if err := os.WriteFile(destPath, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("writing tar.gz: %v", err)
	}
}

// createTraversalTarGz creates a tar.gz with a path traversal entry.
func createTraversalTarGz(t *testing.T, destPath string) {
	t.Helper()

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	hdr := &tar.Header{
		Name:     "../escape.txt",
		Mode:     0o644,
		Size:     int64(len("escaped")),
		Typeflag: tar.TypeReg,
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatalf("writing tar header: %v", err)
	}
	if _, err := tw.Write([]byte("escaped")); err != nil {
		t.Fatalf("writing tar body: %v", err)
	}
	_ = tw.Close()
	_ = gz.Close()
	if err := os.WriteFile(destPath, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("writing tar.gz: %v", err)
	}
}

// createOversizedZip creates a zip with more than maxEntries files.
func createOversizedZip(t *testing.T, destPath string, numEntries int) {
	t.Helper()

	out, err := os.Create(destPath)
	if err != nil {
		t.Fatalf("creating zip: %v", err)
	}
	defer func() { _ = out.Close() }()

	w := zip.NewWriter(out)
	for i := range numEntries {
		name := filepath.Join("files", filepath.Base(destPath)+string(rune('0'+i%10)))
		// Use a short numeric name to keep the zip small
		fw, err := w.Create(name)
		if err != nil {
			t.Fatalf("creating entry %d: %v", i, err)
		}
		_, _ = fw.Write([]byte{0})
	}
	_ = w.Close()
}
