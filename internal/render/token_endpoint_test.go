package render

import (
	"context"
	"errors"
	"fmt"
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

// These tests pin how the pinned library classifies an answer from a
// registry's token endpoint when a dependency load carries it. cue/load
// flattens the cause of a failed import into text, so no typed status is
// left; until library v1.0.0-beta.8 that text read as an unreachable
// registry, which is transient. The reconcile loops retry every typed fetch
// failure, so a ModulePackage behaves the same either way, but the claim
// reconciler keeps an accepted claim on oerrors.ErrTransient alone: a refused
// token must not read as a registry outage there.
//
// No test reaches a real registry.

// tokenRegistry starts a registry that uses token authentication, as GHCR
// does: it answers every registry request 401 with a Bearer challenge that
// names its own /token endpoint, and that endpoint answers every request
// with tokenStatus. It returns the CUE_REGISTRY mapping.
func tokenRegistry(t *testing.T, tokenStatus int) string {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			w.WriteHeader(tokenStatus)
			return
		}
		w.Header().Set("Www-Authenticate", fmt.Sprintf(`Bearer realm=%q,service="test"`, srv.URL+"/token"))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = fmt.Fprint(w, `{"errors":[{"code":"UNAUTHORIZED","message":"token required"}]}`)
	}))
	t.Cleanup(srv.Close)
	return strings.TrimPrefix(srv.URL, "http://") + "+insecure"
}

// dependentInstancePackage writes an instance package whose module file
// declares one dependency, which only the registry can serve.
func dependentInstancePackage(t *testing.T) string {
	t.Helper()
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
	return dir
}

// A package whose dependency load meets a token endpoint's answer fails with
// a fetch failure of that answer's status: a refusal and a 429 are not
// transient, a 5xx is.
func TestKernelPackageRenderer_LoadTokenEndpointAnswer(t *testing.T) {
	cases := []struct {
		name      string
		status    int
		kind      oerrors.FetchKind
		transient bool
	}{
		{name: "a refused token is unauthorized", status: http.StatusUnauthorized, kind: oerrors.FetchUnauthorized},
		{name: "a rate-limited token endpoint is not transient", status: http.StatusTooManyRequests, kind: oerrors.FetchOther},
		{name: "a token endpoint that is down is transient", status: http.StatusServiceUnavailable, kind: oerrors.FetchOther, transient: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			isolateCUECache(t)
			r := &KernelPackageRenderer{
				Kernel:      kernel.New(kernel.WithRegistry(tokenRegistry(t, tc.status))),
				Store:       platformstore.NewStore(),
				RuntimeName: "opm-controller",
			}

			_, _, err := r.Render(context.Background(), dependentInstancePackage(t))
			require.Error(t, err)

			fe, ok := errors.AsType[*oerrors.FetchError](err)
			require.True(t, ok, "want a *FetchError, got %T: %v", err, err)
			assert.Equal(t, tc.kind, fe.Kind, "kind %s: %v", fe.Kind, err)
			assert.Equal(t, tc.status, fe.Status, "status: %v", err)
			assert.Equal(t, tc.transient, errors.Is(err, oerrors.ErrTransient), "ErrTransient: %v", err)
			assert.ErrorIs(t, err, ErrAcquire)
			assert.True(t, strings.HasPrefix(err.Error(), "loading package: "), "message: %s", err)
			assert.True(t, strings.HasSuffix(err.Error(), fmt.Sprintf(": %d %s", tc.status, http.StatusText(tc.status))),
				"the message ends in the token endpoint's status: %s", err)
		})
	}
}
