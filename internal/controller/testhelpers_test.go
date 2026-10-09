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
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"cuelang.org/go/cue/cuecontext"

	oerrors "github.com/open-platform-model/library/opm/errors"
	"github.com/open-platform-model/library/opm/k8s/labels"
	"github.com/open-platform-model/library/opm/k8s/object"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	"github.com/open-platform-model/opm-operator/internal/render"
)

// stubRenderer is a test ModuleRenderer that returns a pre-built result or
// an error without touching an OCI registry. Each call returns a copy of
// result, as a real render returns a fresh one, because the reconcile drops
// the rendered resources from the result it was handed; the last result
// returned is kept for specs to inspect.
type stubRenderer struct {
	result *render.RenderResult
	err    error

	mu   sync.Mutex
	last *render.RenderResult
}

func (s *stubRenderer) RenderModule(
	_ context.Context,
	_, namespace, _, _ string,
	values *releasesv1alpha1.RawValues,
) (*render.RenderResult, error) {
	if s.err != nil {
		return nil, s.err
	}
	var out *render.RenderResult
	if s.result != nil {
		r := *s.result
		out = &r
	} else {
		out = stubRenderResult(namespace, values)
	}
	s.mu.Lock()
	s.last = out
	s.mu.Unlock()
	return out, nil
}

// lastResult is the result the most recent successful call returned.
func (s *stubRenderer) lastResult() *render.RenderResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.last
}

// stubContract is the contract FQN the stub render result reports as its
// component's demand, in the shape TransformerRegistration.spec.provides
// carries.
const stubContract = "opmodel.dev/catalogs/opm/resources/config-maps@v1beta1"

// stubModuleVersion is the module version the stub render result reports, as
// a module declares it in metadata.version.
const stubModuleVersion = "0.1.0"

// stubPlatformIdentity and stubSkewPolicy are the platform the stub render
// result reports it rendered against, as a kernel render reports the record
// it leased.
const (
	stubPlatformIdentity = "gen-1"
	stubSkewPolicy       = releasesv1alpha1.SkewPolicyWarn
)

// testOperatorVersion and testLibraryVersion stand in for version.Full() and
// version.Library(), which a test binary cannot report for the library.
const (
	testOperatorVersion = "v1.0.0-test"
	testLibraryVersion  = "v1.0.0-test.library"
)

// stubRenderResult builds a ConfigMap render result named "test-module" in the
// given namespace, with data.message from values (default "hello").
func stubRenderResult(namespace string, values *releasesv1alpha1.RawValues) *render.RenderResult {
	message := "hello"
	if values != nil && len(values.Raw) > 0 {
		var parsed map[string]any
		if err := json.Unmarshal(values.Raw, &parsed); err == nil {
			if m, ok := parsed["message"].(string); ok {
				message = m
			}
		}
	}

	cueCtx := cuecontext.New()
	cm := cueCtx.CompileString(fmt.Sprintf(`{
	apiVersion: "v1"
	kind:       "ConfigMap"
	metadata: {
		name:      "test-module"
		namespace: %q
		labels: {
			%q: %q
			%q: %q
			%q: %q
		}
	}
	data: {
		message: %q
	}
}`, namespace,
		labels.ManagedBy, labels.ManagedByController,
		labels.ModuleInstanceNamespace, namespace,
		labels.ModuleInstanceUUID, stubRenderUUID,
		message))
	if cm.Err() != nil {
		panic(fmt.Sprintf("compiling stub ConfigMap: %v", cm.Err()))
	}

	resource := &object.Resource{
		Value:       cm,
		Instance:    "test-module",
		Component:   "hello",
		Transformer: "kubernetes#simple",
	}

	return &render.RenderResult{
		Resources:         []*object.Resource{resource},
		RequiredContracts: []string{stubContract},
		ModuleVersion:     stubModuleVersion,
		PlatformIdentity:  stubPlatformIdentity,
		SkewPolicy:        stubSkewPolicy,
	}
}

// acquireErr marks cause as an acquisition failure (render.ErrAcquire), the
// way the module renderer does when moduleacquire.Acquire fails.
func acquireErr(cause error) error {
	return fmt.Errorf("acquiring module: %w: %w", cause, render.ErrAcquire)
}

// acquireFailureRenderer returns a stub whose error is a registry outage at
// module acquisition as the library returns it: the typed *oerrors.FetchError
// (unreachable) under the acquisition mark. The reconcile loop
// classifies it as a transient ResolutionFailed retried on the bounded
// backoff.
func acquireFailureRenderer() *stubRenderer {
	return &stubRenderer{
		err: acquireErr(&oerrors.FetchError{
			Kind:       oerrors.FetchUnreachable,
			Coordinate: "opmodel.dev/test@v0.1.0",
			Err:        errors.New("fetching opmodel.dev/test@v0.1.0: dial tcp registry.example:443: connection refused"),
		}),
	}
}

// stubRenderUUID is the instance identity the stub renders of this suite
// carry in the UUID label, as every real render does: the apply guard asks
// the ownership verdict with it and refuses to ask without one.
const stubRenderUUID = "00000000-0000-0000-0000-00000000c0de"
