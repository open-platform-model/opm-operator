package source

import (
	"archive/zip"
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
