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

package apply_test

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	fluxssa "github.com/fluxcd/pkg/ssa"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/open-platform-model/opm-operator/internal/apply"
	"github.com/open-platform-model/opm-operator/test/collector"
)

var (
	ctx       context.Context
	cancel    context.CancelFunc
	testEnv   *envtest.Environment
	cfg       *rest.Config
	k8sClient client.Client

	// gc plays the garbage collector, which envtest does not run.
	gc *collector.Collector
)

func TestApplyIntegration(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Apply Integration Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	ctx, cancel = context.WithCancel(context.TODO())

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{}

	if dir := getFirstFoundEnvTestBinaryDir(); dir != "" {
		testEnv.BinaryAssetsDirectory = dir
	}

	var err error
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	k8sClient, err = client.New(cfg, client.Options{})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	gc, err = collector.Start(cfg)
	Expect(err).NotTo(HaveOccurred())
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	gc.Stop()
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

// laggingMapper simulates API discovery lagging a CustomResourceDefinition's
// Established condition: for one GroupKind, RESTMapping and RESTMappings
// report a no-match until lag has passed since the first lookup of that kind,
// the error a client sees when it resolves a custom resource whose CRD the
// API server has established but discovery does not serve yet. Every other
// lookup goes to the wrapped mapper.
type laggingMapper struct {
	meta.RESTMapper

	gk  schema.GroupKind
	lag time.Duration

	mu    sync.Mutex
	first time.Time
}

// lagging reports whether a lookup of gk falls inside the lag window, and
// starts the window on the first lookup of the lagged kind.
func (m *laggingMapper) lagging(gk schema.GroupKind) bool {
	if gk != m.gk {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.first.IsZero() {
		m.first = time.Now()
	}
	return time.Since(m.first) < m.lag
}

// RESTMapping reports a no-match for the lagged kind inside the lag window.
func (m *laggingMapper) RESTMapping(gk schema.GroupKind, versions ...string) (*meta.RESTMapping, error) {
	if m.lagging(gk) {
		return nil, &meta.NoKindMatchError{GroupKind: gk, SearchedVersions: versions}
	}
	return m.RESTMapper.RESTMapping(gk, versions...)
}

// RESTMappings reports a no-match for the lagged kind inside the lag window.
func (m *laggingMapper) RESTMappings(gk schema.GroupKind, versions ...string) ([]*meta.RESTMapping, error) {
	if m.lagging(gk) {
		return nil, &meta.NoKindMatchError{GroupKind: gk, SearchedVersions: versions}
	}
	return m.RESTMapper.RESTMappings(gk, versions...)
}

// newLaggingResourceManager returns a ResourceManager whose client resolves
// kinds through a fresh dynamic RESTMapper that simulates discovery serving
// gk only lag after its first lookup, as if the API server's discovery lagged
// the Established condition of gk's CustomResourceDefinition.
func newLaggingResourceManager(gk schema.GroupKind, lag time.Duration) *fluxssa.ResourceManager {
	httpClient, err := rest.HTTPClientFor(cfg)
	Expect(err).NotTo(HaveOccurred())
	mapper, err := apiutil.NewDynamicRESTMapper(cfg, httpClient)
	Expect(err).NotTo(HaveOccurred())
	c, err := client.New(cfg, client.Options{
		HTTPClient: httpClient,
		Mapper:     &laggingMapper{RESTMapper: mapper, gk: gk, lag: lag},
	})
	Expect(err).NotTo(HaveOccurred())
	return apply.NewResourceManager(c, "test-owner")
}
