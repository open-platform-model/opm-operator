package source

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExtractZip(t *testing.T) {
	fixtureDir := filepath.Join("testdata", "minimal-module")

	t.Run("valid zip extracts files at expected paths", func(t *testing.T) {
		tmp := t.TempDir()
		zipPath := filepath.Join(tmp, "module.zip")
		createZipFromDir(t, fixtureDir, zipPath)

		destDir := filepath.Join(tmp, "out")
		if err := os.MkdirAll(destDir, 0o755); err != nil {
			t.Fatalf("creating dest dir: %v", err)
		}

		if err := extractZip(zipPath, destDir); err != nil {
			t.Fatalf("extractZip returned error: %v", err)
		}

		// Assert expected files exist with correct content.
		moduleCue := filepath.Join(destDir, "cue.mod", "module.cue")
		data, err := os.ReadFile(moduleCue)
		if err != nil {
			t.Fatalf("reading cue.mod/module.cue: %v", err)
		}
		if len(data) == 0 {
			t.Error("cue.mod/module.cue is empty")
		}

		mainCue := filepath.Join(destDir, "main.cue")
		data, err = os.ReadFile(mainCue)
		if err != nil {
			t.Fatalf("reading main.cue: %v", err)
		}
		if len(data) == 0 {
			t.Error("main.cue is empty")
		}
	})

	t.Run("path traversal returns error", func(t *testing.T) {
		tmp := t.TempDir()
		zipPath := filepath.Join(tmp, "traversal.zip")
		createTraversalZip(t, zipPath)

		destDir := filepath.Join(tmp, "out")
		if err := os.MkdirAll(destDir, 0o755); err != nil {
			t.Fatalf("creating dest dir: %v", err)
		}

		err := extractZip(zipPath, destDir)
		if err == nil {
			t.Fatal("expected error for path traversal zip, got nil")
		}

		// Verify no files escaped outside destDir.
		escaped := filepath.Join(tmp, "escape.txt")
		if _, err := os.Stat(escaped); err == nil {
			t.Error("file escaped destination directory")
		}
	})

	t.Run("empty archive extracts without error", func(t *testing.T) {
		tmp := t.TempDir()
		zipPath := filepath.Join(tmp, "empty.zip")
		createEmptyZip(t, zipPath)

		destDir := filepath.Join(tmp, "out")
		if err := os.MkdirAll(destDir, 0o755); err != nil {
			t.Fatalf("creating dest dir: %v", err)
		}

		if err := extractZip(zipPath, destDir); err != nil {
			t.Fatalf("extractZip returned error for empty zip: %v", err)
		}

		entries, err := os.ReadDir(destDir)
		if err != nil {
			t.Fatalf("reading dest dir: %v", err)
		}
		if len(entries) != 0 {
			t.Errorf("expected empty dest dir, got %d entries", len(entries))
		}
	})

	t.Run("corrupt data returns error", func(t *testing.T) {
		tmp := t.TempDir()
		badPath := filepath.Join(tmp, "corrupt.zip")
		if err := os.WriteFile(badPath, []byte("this is not a zip file"), 0o644); err != nil {
			t.Fatalf("writing corrupt file: %v", err)
		}

		destDir := filepath.Join(tmp, "out")
		if err := os.MkdirAll(destDir, 0o755); err != nil {
			t.Fatalf("creating dest dir: %v", err)
		}

		err := extractZip(badPath, destDir)
		if err == nil {
			t.Fatal("expected error for corrupt zip, got nil")
		}
	})

	t.Run("file count limit exceeded returns error", func(t *testing.T) {
		tmp := t.TempDir()
		zipPath := filepath.Join(tmp, "oversized.zip")
		createOversizedZip(t, zipPath, MaxZipFiles+1)

		destDir := filepath.Join(tmp, "out")
		if err := os.MkdirAll(destDir, 0o755); err != nil {
			t.Fatalf("creating dest dir: %v", err)
		}

		err := extractZip(zipPath, destDir)
		if err == nil {
			t.Fatal("expected error for oversized zip, got nil")
		}
	})
}
