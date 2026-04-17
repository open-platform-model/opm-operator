package source

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ErrSourceNotFound indicates the referenced OCIRepository does not exist.
var ErrSourceNotFound = errors.New("source not found")

// ErrSourceNotReady indicates the OCIRepository exists but is not ready.
var ErrSourceNotReady = errors.New("source not ready")

// ErrMissingCUEModule indicates the fetched artifact does not contain a CUE module.
var ErrMissingCUEModule = errors.New("artifact does not contain a cue module")

// ValidateCUEModule checks that dir contains a valid CUE module layout.
// Currently validates that cue.mod/module.cue exists and is non-empty.
// Returns ErrMissingCUEModule on failure.
func ValidateCUEModule(dir string) error {
	modDir := filepath.Join(dir, "cue.mod")
	info, err := os.Stat(modDir)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("cue.mod directory missing: %w", ErrMissingCUEModule)
		}
		return fmt.Errorf("checking cue.mod: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("cue.mod is not a directory: %w", ErrMissingCUEModule)
	}

	moduleCue := filepath.Join(modDir, "module.cue")
	fi, err := os.Stat(moduleCue)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("cue.mod/module.cue missing: %w", ErrMissingCUEModule)
		}
		return fmt.Errorf("checking cue.mod/module.cue: %w", err)
	}
	if fi.Size() == 0 {
		return fmt.Errorf("cue.mod/module.cue is empty: %w", ErrMissingCUEModule)
	}

	return nil
}
