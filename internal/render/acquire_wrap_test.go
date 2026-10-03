package render

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	oerrors "github.com/open-platform-model/library/opm/errors"
	"github.com/open-platform-model/library/opm/kernel"

	platformstore "github.com/open-platform-model/opm-operator/internal/platform"
)

// These tests drive the production wrap sites without a registry: each
// failure surfaces from acquisition, so the renderer must mark it with
// ErrAcquire and keep its message prefix. Dropping the mark at either site
// reverts the reconcile outcome to a stall, so these are the guard.

func newPackageRenderer() *KernelPackageRenderer {
	return &KernelPackageRenderer{
		Kernel:      kernel.New(),
		Store:       platformstore.NewStore(),
		RuntimeName: "opm-controller",
	}
}

// A package with a CUE syntax error fails to load untyped; the renderer marks
// it as an acquisition failure.
func TestKernelPackageRenderer_LoadFailureIsMarked(t *testing.T) {
	dir := writeInstancePackage(t, KindModuleInstance)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "broken.cue"),
		[]byte("package instance\n\nbroken: {\n"), 0o644))

	_, result, err := newPackageRenderer().Render(context.Background(), dir)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrAcquire)
	assert.True(t, strings.HasPrefix(err.Error(), "loading package: "), "message: %s", err)
	assert.Nil(t, result)
}

// A package missing metadata.name fails the loader's shape gate with the
// typed ErrMissingRequiredField; the mark sits beside the typed cause.
func TestKernelPackageRenderer_MissingRequiredFieldIsMarked(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "cue.mod"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "cue.mod", "module.cue"),
		[]byte("module: \"test.example/release@v0\"\nlanguage: version: \"v0.17.0\"\n"), 0o644))
	pkg := "package instance\n\nkind: \"" + KindModuleInstance + "\"\n" +
		"metadata: namespace: \"default\"\n" +
		"#module: kind: \"Module\"\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "instance.cue"), []byte(pkg), 0o644))

	_, _, err := newPackageRenderer().Render(context.Background(), dir)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrAcquire)
	assert.ErrorIs(t, err, oerrors.ErrMissingRequiredField)
	assert.True(t, strings.HasPrefix(err.Error(), "loading package: "), "message: %s", err)
}

// An unparsable module version fails inside moduleacquire.Acquire before any
// registry I/O; the module renderer marks it as an acquisition failure.
func TestKernelModuleRenderer_AcquireFailureIsMarked(t *testing.T) {
	store := platformstore.NewStore()
	store.SetGenerated(platformstore.Generated{})
	r := &KernelModuleRenderer{
		Kernel:      kernel.New(),
		Store:       store,
		RuntimeName: "opm-controller",
	}

	result, err := r.RenderModule(context.Background(), "web", "default",
		"opmodel.dev/test", "not-a-version", nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrAcquire)
	assert.True(t, strings.HasPrefix(err.Error(), "acquiring module: "), "message: %s", err)
	assert.Nil(t, result)
}
