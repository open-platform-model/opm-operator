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
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	oerrors "github.com/open-platform-model/library/opm/errors"
	"github.com/open-platform-model/library/opm/kernel"

	opmreconcile "github.com/open-platform-model/opm-operator/internal/reconcile"
)

// closedRegistry is a CUE_REGISTRY mapping to a local port nothing listens
// on, so every fetch fails with a refused connection.
const closedRegistry = "127.0.0.1:1+insecure"

// probeCatalog is a catalog coordinate no registry in these tests holds.
const (
	probeCatalog        = "example.test/catalogs/probe@v1"
	probeCatalogVersion = "1.0.0"
)

// statusRegistry starts a registry that answers every request with the given
// HTTP status. It returns the CUE_REGISTRY mapping and the function that
// stops the server.
func statusRegistry(code int) (string, func()) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(code)
	}))
	return strings.TrimPrefix(srv.URL, "http://") + "+insecure", srv.Close
}

// TestCatalogAcquisitionClassification pins what the pinned library returns
// from AcquireCatalogFromRegistry when the fetch fails. The claim reconciler
// keeps an accepted claim on the library's ErrTransient alone, so a library
// change that drops or widens the classification at this call fails here, on
// the bump.
//
// No test reaches a real registry. Every case points CUE_CACHE_DIR at a new,
// empty directory: the library serves a version it already fetched from that
// cache with no registry call, so only a cold cache makes the fetch go out.
func TestCatalogAcquisitionClassification(t *testing.T) {
	unavailable, stopUnavailable := statusRegistry(http.StatusServiceUnavailable)
	t.Cleanup(stopUnavailable)
	notFound, stopNotFound := statusRegistry(http.StatusNotFound)
	t.Cleanup(stopNotFound)

	cases := []struct {
		name      string
		registry  string
		transient bool
	}{
		{name: "an unreachable registry is transient", registry: closedRegistry, transient: true},
		{name: "a 503 answer is transient", registry: unavailable, transient: true},
		{name: "a 404 answer is not transient", registry: notFound, transient: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("CUE_CACHE_DIR", t.TempDir())

			_, err := kernel.New(kernel.WithRegistry(tc.registry)).
				AcquireCatalogFromRegistry(context.Background(), probeCatalog, probeCatalogVersion)
			require.Error(t, err)

			_, typed := errors.AsType[*oerrors.FetchError](err)
			require.True(t, typed, "want a *FetchError, got %T: %v", err, err)
			assert.True(t, opmreconcile.IsTransientFailure(err), "a typed fetch failure with no terminal cause: %v", err)
			assert.Equal(t, tc.transient, errors.Is(err, oerrors.ErrTransient), "ErrTransient: %v", err)
			assert.Equal(t, tc.transient, keepsVerdict(err), "keepsVerdict: %v", err)
		})
	}
}
