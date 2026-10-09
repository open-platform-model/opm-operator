/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

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

	opmreconcile "github.com/open-platform-model/opm-operator/internal/reconcile"
)

// tokenRegistry starts a registry that uses token authentication, as GHCR
// does: it answers every registry request 401 with a Bearer challenge that
// names its own /token endpoint, and that endpoint answers every request
// with tokenStatus. It returns the CUE_REGISTRY mapping and the function that
// stops the server.
func tokenRegistry(tokenStatus int) (string, func()) {
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
	return strings.TrimPrefix(srv.URL, "http://") + "+insecure", srv.Close
}

// dependentCatalogDir writes a catalog source tree whose module file declares
// one dependency, which only the registry can serve. Loading it fails at the
// dependency, the way a fetched catalog's own load does.
func dependentCatalogDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "cue.mod"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "cue.mod", "module.cue"), []byte(
		"module: \""+probeCatalog+"\"\n"+
			"language: version: \"v0.17.0\"\n"+
			"deps: \"example.test/fetch/dep@v0\": v: \"v0.1.0\"\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "catalog.cue"), []byte(
		"package probe\n\nimport \"example.test/fetch/dep\"\n\n_dep: dep\n"), 0o644))
	return dir
}

// TestCatalogDependencyLoadTokenEndpoint pins what an answer from the
// registry's token endpoint does to a claim when it arrives through a
// catalog's dependency load. cue/load flattens the cause of a failed import
// into text, and until library v1.0.0-beta.8 that text read as an unreachable
// registry, so a refused token held an accepted claim as a registry outage.
// A refusal (401) is an answer about the claim and must un-accept. A rate
// limit (429) and a 5xx answer say nothing about the claim: they are what the
// hold exists for. The 429 is told by the typed status, never by text.
//
// No test reaches a real registry, and every case starts on a cold cache.
func TestCatalogDependencyLoadTokenEndpoint(t *testing.T) {
	cases := []struct {
		name   string
		status int
		kind   oerrors.FetchKind
		keeps  bool
	}{
		{name: "a refused token un-accepts", status: http.StatusUnauthorized, kind: oerrors.FetchUnauthorized},
		{name: "a rate-limited token endpoint holds the claim", status: http.StatusTooManyRequests, kind: oerrors.FetchOther, keeps: true},
		{name: "a token endpoint that is down holds the claim", status: http.StatusServiceUnavailable, kind: oerrors.FetchOther, keeps: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("CUE_CACHE_DIR", t.TempDir())

			registry, stop := tokenRegistry(tc.status)
			t.Cleanup(stop)

			_, err := kernel.New(kernel.WithRegistry(registry)).
				AcquireCatalogFromDir(context.Background(), dependentCatalogDir(t))
			require.Error(t, err)

			fe, typed := errors.AsType[*oerrors.FetchError](err)
			require.True(t, typed, "want a *FetchError, got %T: %v", err, err)
			assert.Equal(t, tc.kind, fe.Kind, "kind %s: %v", fe.Kind, err)
			assert.Equal(t, tc.status, fe.Status, "status: %v", err)
			assert.True(t, opmreconcile.IsTransientFailure(err), "every typed fetch failure retries: %v", err)
			assert.Equal(t, tc.keeps, keepsVerdict(err), "keepsVerdict: %v", err)
		})
	}
}
