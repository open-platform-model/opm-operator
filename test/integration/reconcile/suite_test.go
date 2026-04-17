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

package reconcile_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"cuelang.org/go/cue/cuecontext"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	releasesv1alpha1 "github.com/open-platform-model/poc-controller/api/v1alpha1"
	"github.com/open-platform-model/poc-controller/internal/inventory"
	"github.com/open-platform-model/poc-controller/internal/render"
	"github.com/open-platform-model/poc-controller/pkg/core"
	"github.com/open-platform-model/poc-controller/pkg/provider"
)

var (
	ctx       context.Context
	cancel    context.CancelFunc
	testEnv   *envtest.Environment
	cfg       *rest.Config
	k8sClient client.Client
)

func TestReconcileIntegration(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Reconcile Integration Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	ctx, cancel = context.WithCancel(context.TODO())

	Expect(releasesv1alpha1.AddToScheme(scheme.Scheme)).To(Succeed())

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "..", "config", "crd", "bases"),
		},
		ErrorIfCRDPathMissing: true,
	}

	if dir := getFirstFoundEnvTestBinaryDir(); dir != "" {
		testEnv.BinaryAssetsDirectory = dir
	}

	var err error
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	cancel()
	Eventually(func() error {
		return testEnv.Stop()
	}, time.Minute, time.Second).Should(Succeed())
})

func getFirstFoundEnvTestBinaryDir() string {
	basePath := filepath.Join("..", "..", "..", "bin", "k8s")
	entries, err := os.ReadDir(basePath)
	if err != nil {
		logf.Log.Error(err, "Failed to read directory", "path", basePath)
		return ""
	}
	for _, entry := range entries {
		if entry.IsDir() {
			return filepath.Join(basePath, entry.Name())
		}
	}
	return ""
}

// stubRenderer is a test ModuleRenderer that returns a pre-built result or
// an error without touching an OCI registry. Leave result nil + err nil to
// use the default ConfigMap-based render result.
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
// given namespace, with data.message extracted from values (default "hello").
// Mirrors what testProvider would produce for the test fixture module.
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

// resolutionErrorRenderer returns a stub whose error matches the reconcile
// loop's isResolutionError() classification.
func resolutionErrorRenderer() *stubRenderer {
	return &stubRenderer{
		err: fmt.Errorf("loading synthesized release: module not found in registry"),
	}
}

// renderErrorRenderer returns a stub with a render-style error that the
// reconcile loop classifies as RenderFailed (not ResolutionFailed).
func renderErrorRenderer(msg string) *stubRenderer {
	return &stubRenderer{err: fmt.Errorf("%s", msg)}
}

// testProvider builds a minimal provider that produces a ConfigMap.
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
