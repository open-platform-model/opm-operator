// Package moduleacquire loads a ModuleInstance's target module from the OCI
// registry into a library *module.Module. A ModuleInstance references its module
// by registry path and version; the library kernel owns the registry fetch and
// in-memory load (Kernel.AcquireModuleFromRegistry), so this package is a thin
// adapter over the kernel's source-carrying acquisition.
package moduleacquire

import (
	"context"
	"fmt"

	"github.com/open-platform-model/library/opm/kernel"
	"github.com/open-platform-model/library/opm/module"
)

// Acquire loads the module at the given registry path and version into a
// library *module.Module using the shared kernel. It delegates to
// Kernel.AcquireModuleFromRegistry, which fetches the module's source via CUE's
// native module machinery and stages it as the main module — so the module's
// own cue.mod/module.cue resolves transitive dependencies and its kind/metadata
// are evaluated at the package root, with the registry applied per-call (no
// process-environment mutation, safe to call concurrently from reconcilers).
//
// Unlike the older LoadModuleFromRegistry + NewModuleFromValue pair, the
// returned *module.Module carries its staged source (module.HasSource() is
// true). That source is REQUIRED downstream: Kernel.SynthesizeInstance now
// builds the instance inside the module's own staged root so the module's
// tidied dependency closure (including catalog subpackages) drives transitive
// resolution. A source-free module would be rejected with synth.ErrMissingSource
// (library#31, library v1.0.0-alpha.3 migration).
//
// The registry parameter is retained for signature stability; the kernel
// resolves the registry it was constructed with (see kernel.WithRegistry).
func Acquire(ctx context.Context, k *kernel.Kernel, path, version, registry string) (*module.Module, error) {
	mod, err := k.AcquireModuleFromRegistry(ctx, path, version)
	if err != nil {
		return nil, fmt.Errorf("acquiring module %q@%q: %w", path, version, err)
	}

	return mod, nil
}
