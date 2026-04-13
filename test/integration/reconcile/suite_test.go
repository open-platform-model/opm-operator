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
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"cuelang.org/go/cue/cuecontext"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	releasesv1alpha1 "github.com/open-platform-model/poc-controller/api/v1alpha1"
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
	Expect(sourcev1.AddToScheme(scheme.Scheme)).To(Succeed())

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "..", "config", "crd", "bases"),
			filepath.Join("..", "..", "..", "internal", "controller", "testdata", "crds"),
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

// stubFetcher returns a fixed error on every Fetch call.
type stubFetcher struct {
	err error
}

func (f *stubFetcher) Fetch(_ context.Context, _, _, _ string) error {
	if f.err != nil {
		return f.err
	}
	return fmt.Errorf("stubFetcher: not implemented")
}

// copyDirFetcher copies a source directory into the fetch target.
type copyDirFetcher struct {
	sourceDir string
}

func (f *copyDirFetcher) Fetch(_ context.Context, _, _, dir string) error {
	return copyDir(f.sourceDir, dir)
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)

		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}

		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer func() { _ = in.Close() }()

		out, err := os.Create(target)
		if err != nil {
			return err
		}
		defer func() { _ = out.Close() }()

		_, err = io.Copy(out, in)
		return err
	})
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

func renderTestdataDir(name string) string {
	return filepath.Join("..", "..", "..", "internal", "render", "testdata", name)
}
