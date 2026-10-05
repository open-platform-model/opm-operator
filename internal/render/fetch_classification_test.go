package render

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
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

// These tests pin what library v1.0.0-beta.6 returns at the operator's two
// acquisition wrap sites when a registry fetch fails. The
// reconcile loops decide retry by the library's *oerrors.FetchError alone,
// so a library change that drops the classification at one of these sites
// fails here, on the bump, rather than as a 30-minute stall in a cluster.
// No test reaches a real registry: a closed local port stands for an
// unreachable one and an httptest server answers 404 to everything.

// closedRegistry is a CUE_REGISTRY mapping to a local port nothing listens
// on, so every fetch fails with a refused connection.
const closedRegistry = "127.0.0.1:1+insecure"

// isolateCUECache points the CUE module cache at an empty directory, so no
// module cached by an earlier run lets a fetch succeed without the registry.
func isolateCUECache(t *testing.T) {
	t.Helper()
	t.Setenv("CUE_CACHE_DIR", t.TempDir())
}

// notFoundRegistry starts a registry that answers 404 to every request and
// returns its CUE_REGISTRY mapping.
func notFoundRegistry(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.NotFound(w, nil)
	}))
	t.Cleanup(srv.Close)
	return strings.TrimPrefix(srv.URL, "http://") + "+insecure"
}

func moduleRendererFor(registry string) *KernelModuleRenderer {
	store := platformstore.NewStore()
	store.SetGenerated(platformstore.Generated{})
	return &KernelModuleRenderer{
		Kernel:      kernel.New(kernel.WithRegistry(registry)),
		Store:       store,
		Registry:    registry,
		RuntimeName: "opm-controller",
	}
}

// An unreachable registry at module acquisition is an unreachable,
// transient *FetchError under the acquisition mark and prefix.
func TestKernelModuleRenderer_AcquireFetchUnreachable(t *testing.T) {
	isolateCUECache(t)

	_, err := moduleRendererFor(closedRegistry).RenderModule(context.Background(),
		"web", "default", "example.test/fetch/web", "v0.1.0", nil)
	require.Error(t, err)

	fe, ok := errors.AsType[*oerrors.FetchError](err)
	require.True(t, ok, "want a *FetchError, got %T: %v", err, err)
	assert.Equal(t, oerrors.FetchUnreachable, fe.Kind, "kind %s: %v", fe.Kind, err)
	assert.ErrorIs(t, err, oerrors.ErrTransient)
	assert.ErrorIs(t, err, ErrAcquire)
	assert.True(t, strings.HasPrefix(err.Error(), "acquiring module: "), "message: %s", err)
}

// A registry that does not hold the module gives a not-found *FetchError at
// module acquisition, which is not ErrTransient (the operator still retries
// it on the backoff: a late publish recovers on the next attempt).
func TestKernelModuleRenderer_AcquireFetchNotFound(t *testing.T) {
	isolateCUECache(t)

	_, err := moduleRendererFor(notFoundRegistry(t)).RenderModule(context.Background(),
		"web", "default", "example.test/fetch/web", "v0.1.0", nil)
	require.Error(t, err)

	fe, ok := errors.AsType[*oerrors.FetchError](err)
	require.True(t, ok, "want a *FetchError, got %T: %v", err, err)
	assert.Equal(t, oerrors.FetchNotFound, fe.Kind, "kind %s: %v", fe.Kind, err)
	assert.NotErrorIs(t, err, oerrors.ErrTransient)
	assert.ErrorIs(t, err, ErrAcquire)
	assert.True(t, strings.HasPrefix(err.Error(), "acquiring module: "), "message: %s", err)
}

// A package whose cue.mod/module.cue declares a dependency the registry
// cannot serve fails its load with a *FetchError under the package load
// prefix.
func TestKernelPackageRenderer_LoadFetchUnreachable(t *testing.T) {
	isolateCUECache(t)

	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "cue.mod"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "cue.mod", "module.cue"), []byte(
		"module: \"test.example/release@v0\"\n"+
			"language: version: \"v0.17.0\"\n"+
			"deps: \"example.test/fetch/dep@v0\": v: \"v0.1.0\"\n"), 0o644))
	pkg := "package instance\n\nimport \"example.test/fetch/dep\"\n\n" +
		"kind: \"" + KindModuleInstance + "\"\n" +
		"metadata: {\n\tname:      \"test-instance\"\n\tnamespace: \"default\"\n}\n" +
		"#module: kind: \"Module\"\n" +
		"_dep: dep\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "instance.cue"), []byte(pkg), 0o644))

	r := &KernelPackageRenderer{
		Kernel:      kernel.New(kernel.WithRegistry(closedRegistry)),
		Store:       platformstore.NewStore(),
		RuntimeName: "opm-controller",
	}
	_, _, err := r.Render(context.Background(), dir)
	require.Error(t, err)

	fe, ok := errors.AsType[*oerrors.FetchError](err)
	require.True(t, ok, "want a *FetchError, got %T: %v", err, err)
	assert.Equal(t, oerrors.FetchUnreachable, fe.Kind, "kind %s: %v", fe.Kind, err)
	assert.ErrorIs(t, err, ErrAcquire)
	assert.True(t, strings.HasPrefix(err.Error(), "loading package: "), "message: %s", err)
}
