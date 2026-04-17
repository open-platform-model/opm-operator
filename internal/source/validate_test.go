package source

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestValidateCUEModule(t *testing.T) {
	t.Run("valid layout passes", func(t *testing.T) {
		dir := filepath.Join("testdata", "minimal-module")
		if err := ValidateCUEModule(dir); err != nil {
			t.Fatalf("expected no error for valid module, got: %v", err)
		}
	})

	t.Run("missing cue.mod returns ErrMissingCUEModule", func(t *testing.T) {
		dir := t.TempDir()
		// Write a file but no cue.mod directory.
		if err := os.WriteFile(filepath.Join(dir, "main.cue"), []byte("package x"), 0o644); err != nil {
			t.Fatal(err)
		}

		err := ValidateCUEModule(dir)
		if err == nil {
			t.Fatal("expected error for missing cue.mod, got nil")
		}
		if !errors.Is(err, ErrMissingCUEModule) {
			t.Fatalf("expected errors.Is(err, ErrMissingCUEModule), got: %v", err)
		}
	})

	t.Run("empty module.cue returns error", func(t *testing.T) {
		dir := t.TempDir()
		modDir := filepath.Join(dir, "cue.mod")
		if err := os.MkdirAll(modDir, 0o755); err != nil {
			t.Fatal(err)
		}
		// Create empty module.cue.
		if err := os.WriteFile(filepath.Join(modDir, "module.cue"), nil, 0o644); err != nil {
			t.Fatal(err)
		}

		err := ValidateCUEModule(dir)
		if err == nil {
			t.Fatal("expected error for empty module.cue, got nil")
		}
	})

	t.Run("cue.mod is a file not a directory returns error", func(t *testing.T) {
		dir := t.TempDir()
		// Create cue.mod as a regular file.
		if err := os.WriteFile(filepath.Join(dir, "cue.mod"), []byte("not a dir"), 0o644); err != nil {
			t.Fatal(err)
		}

		err := ValidateCUEModule(dir)
		if err == nil {
			t.Fatal("expected error when cue.mod is a file, got nil")
		}
	})
}
