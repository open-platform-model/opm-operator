package render

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/open-platform-model/library/opm/kernel"

	platformstore "github.com/open-platform-model/opm-operator/internal/platform"
)

// writeInstancePackage writes a self-contained CUE release package (no registry
// imports) to a temp directory and returns its path. The package is minimal but
// satisfies the loader's shape gate: a concrete kind, concrete metadata
// identity, and an embedded #module whose kind is "Module". Keeping it
// import-free lets these tests reach the renderer's kind-detection and
// platform-readiness gate without a live OCI registry.
func writeInstancePackage(t *testing.T, kind string) string {
	t.Helper()
	dir := t.TempDir()

	modDir := filepath.Join(dir, "cue.mod")
	require.NoError(t, os.MkdirAll(modDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(modDir, "module.cue"),
		[]byte("module: \"test.example/release@v0\"\nlanguage: version: \"v0.16.1\"\n"), 0o644))

	pkg := "package instance\n\nkind: \"" + kind + "\"\n" +
		"metadata: {\n\tname:      \"test-instance\"\n\tnamespace: \"default\"\n}\n" +
		"#module: kind: \"Module\"\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "instance.cue"), []byte(pkg), 0o644))
	return dir
}

// A package whose kind is not #ModuleInstance is rejected with
// ErrUnsupportedKind. Detection rides on the loader's shape gate (ErrWrongKind),
// so no separate kind peek is needed.
func TestKernelPackageRenderer_UnsupportedKind(t *testing.T) {
	dir := writeInstancePackage(t, "SomeOtherKind")

	r := &KernelPackageRenderer{
		Kernel:      kernel.New(),
		Store:       platformstore.NewStore(),
		RuntimeName: "opm-controller",
	}

	kind, result, err := r.Render(context.Background(), dir)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnsupportedKind)
	assert.Empty(t, kind)
	assert.Nil(t, result)
}

// With no materialized platform, a ModuleInstance package is blocked before
// Compile with ErrPlatformNotReady — nothing is rendered.
func TestKernelPackageRenderer_PlatformNotReady(t *testing.T) {
	dir := writeInstancePackage(t, KindModuleInstance)

	r := &KernelPackageRenderer{
		Kernel:      kernel.New(),
		Store:       platformstore.NewStore(), // empty: no materialized platform
		RuntimeName: "opm-controller",
	}

	kind, result, err := r.Render(context.Background(), dir)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrPlatformNotReady)
	assert.Equal(t, KindModuleInstance, kind)
	assert.Nil(t, result)
}
