package moduleacquire

import (
	"context"
	"fmt"
	"os"

	loaderfile "github.com/open-platform-model/library/opm/helper/loader/file"
	"github.com/open-platform-model/library/opm/kernel"
	"github.com/open-platform-model/library/opm/module"
)

// Acquire loads the module at the given registry path and version into a
// library *module.Module using the shared kernel. It writes a shim package
// (see writeShim), loads it through Kernel.LoadModulePackage with the registry
// applied per-call (no process-environment mutation, so it is safe to call
// concurrently from reconcilers), and decodes the loaded value via
// Kernel.NewModuleFromValue.
//
// The temporary shim directory is removed before Acquire returns, on both the
// success and error paths.
func Acquire(ctx context.Context, k *kernel.Kernel, path, version, registry string) (*module.Module, error) {
	dir, err := writeShim(path, version)
	if err != nil {
		return nil, fmt.Errorf("acquiring module %q@%q: %w", path, version, err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	val, err := k.LoadModulePackage(ctx, dir, loaderfile.LoadOptions{Registry: registry})
	if err != nil {
		return nil, fmt.Errorf("acquiring module %q@%q: %w", path, version, err)
	}

	mod, err := k.NewModuleFromValue(val)
	if err != nil {
		return nil, fmt.Errorf("acquiring module %q@%q: %w", path, version, err)
	}

	return mod, nil
}
