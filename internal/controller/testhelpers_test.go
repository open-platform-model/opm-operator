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
	"fmt"

	"cuelang.org/go/cue/cuecontext"

	releasesv1alpha1 "github.com/open-platform-model/poc-controller/api/v1alpha1"
	"github.com/open-platform-model/poc-controller/internal/inventory"
	"github.com/open-platform-model/poc-controller/internal/render"
	"github.com/open-platform-model/poc-controller/pkg/core"
	"github.com/open-platform-model/poc-controller/pkg/provider"
)

// testProvider builds a minimal provider for controller tests.
// Produces a ConfigMap from each component's data.message field.
func testProvider() *provider.Provider {
	cueCtx := cuecontext.New()
	data := cueCtx.CompileString(`{
	metadata: {
		name:        "kubernetes"
		description: "Test provider"
		version:     "0.1.0"
	}
	#transformers: {
		"simple": {
			#transform: {
				#component: _
				#context: _
				output: {
					apiVersion: "v1"
					kind:       "ConfigMap"
					metadata: {
						name:      #context.#moduleReleaseMetadata.name
						namespace: #context.#moduleReleaseMetadata.namespace
						labels:    #context.#runtimeLabels
					}
					data: {
						message: #component.data.message
					}
				}
			}
		}
	}
}`)
	if data.Err() != nil {
		panic(fmt.Sprintf("compiling test provider: %v", data.Err()))
	}
	return &provider.Provider{
		Metadata: &provider.ProviderMetadata{
			Name:    "kubernetes",
			Version: "0.1.0",
		},
		Data: data,
	}
}

// stubRenderer is a test ModuleRenderer that returns a pre-built result or
// an error without touching an OCI registry.
type stubRenderer struct {
	result *render.RenderResult
	err    error
}

func (s *stubRenderer) RenderModule(
	_ context.Context,
	_, namespace, _, _ string,
	values *releasesv1alpha1.RawValues,
	_ *provider.Provider,
) (*render.RenderResult, error) {
	if s.err != nil {
		return nil, s.err
	}
	if s.result != nil {
		return s.result, nil
	}
	return stubRenderResult(namespace, values), nil
}

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
		}
	}
	data: {
		message: %q
	}
}`, namespace,
		core.LabelManagedBy, core.LabelManagedByControllerValue,
		core.LabelModuleReleaseNamespace, namespace,
		message))
	if cm.Err() != nil {
		panic(fmt.Sprintf("compiling stub ConfigMap: %v", cm.Err()))
	}

	resource := &core.Resource{
		Value:       cm,
		Release:     "test-module",
		Component:   "hello",
		Transformer: "kubernetes#simple",
	}

	u, err := resource.ToUnstructured()
	if err != nil {
		panic(fmt.Sprintf("converting stub resource: %v", err))
	}

	return &render.RenderResult{
		Resources:        []*core.Resource{resource},
		InventoryEntries: []releasesv1alpha1.InventoryEntry{inventory.NewEntryFromResource(u)},
	}
}

// resolutionErrorRenderer returns a stub whose error is classified by
// isResolutionError() as a ResolutionFailed outcome.
func resolutionErrorRenderer() *stubRenderer {
	return &stubRenderer{
		err: fmt.Errorf("loading synthesized release: module not found in registry"),
	}
}
