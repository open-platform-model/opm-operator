// Package moduleacquire loads a ModuleRelease's target module from the OCI
// registry into a library *module.Module. A ModuleRelease references its module
// by registry path and version; the library kernel owns the registry fetch and
// in-memory load (Kernel.LoadModuleFromRegistry), so this package is a thin
// adapter that decodes the loaded value to the operator's *module.Module.
package moduleacquire

import (
	"context"
	"fmt"

	"github.com/open-platform-model/library/opm/kernel"
	"github.com/open-platform-model/library/opm/module"
)

// Acquire loads the module at the given registry path and version into a
// library *module.Module using the shared kernel. It delegates to
// Kernel.LoadModuleFromRegistry, which fetches the module's source via CUE's
// native module machinery and loads it in memory as the main module — so the
// module's own cue.mod/module.cue resolves transitive dependencies and its
// kind/metadata are evaluated at the package root, with the registry applied
// per-call (no process-environment mutation, safe to call concurrently from
// reconcilers). The loaded value is decoded via Kernel.NewModuleFromValue.
//
// The registry parameter is retained for signature stability; the kernel
// resolves the registry it was constructed with (see kernel.WithRegistry).
func Acquire(ctx context.Context, k *kernel.Kernel, path, version, registry string) (*module.Module, error) {
	val, err := k.LoadModuleFromRegistry(ctx, path, version)
	if err != nil {
		return nil, fmt.Errorf("acquiring module %q@%q: %w", path, version, err)
	}

	mod, err := k.NewModuleFromValue(val)
	if err != nil {
		return nil, fmt.Errorf("acquiring module %q@%q: %w", path, version, err)
	}

	return mod, nil
}
