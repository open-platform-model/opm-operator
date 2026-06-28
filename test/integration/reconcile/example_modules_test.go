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
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/open-platform-model/library/opm/helper/synth"
	"github.com/open-platform-model/library/opm/kernel"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	platformstore "github.com/open-platform-model/opm-operator/internal/platform"
	"github.com/open-platform-model/opm-operator/internal/render"
	"github.com/open-platform-model/opm-operator/pkg/core"
)

// These specs render the published example modules (redis here) through the
// real KernelModuleRenderer against the local OCI registry and assert the
// modelled workload/storage/probe contract, complementing the podinfo e2e
// (which proves the probes actually pass on a live cluster). They skip
// automatically in CI (the example modules + the catalog version they pin live
// on the local registry); run with `task dev:test:local`.
//
// The example modules are core@v1 and pin opmodel.dev/catalogs/opm@v1
// (v1.0.0-alpha.1, the first catalog line on core@v1 vocabulary), so the
// platform subscription is filtered to the v1 range that resolves it — matching
// the cluster Platform sample and avoiding catalog-version skew. Resource and
// transformer FQNs embed the catalog version, so the platform MUST materialize
// the same catalog version the modules carry; a v0 range (v0.6.0, core@v0)
// yields "no matching transformer". The lower bound is prerelease-inclusive
// because plain ">=1.0.0" excludes -alpha tags.
var _ = Describe("Example module rendering", func() {
	var (
		k        *kernel.Kernel
		registry string
		store    *platformstore.Store
	)

	BeforeEach(func() {
		skipIfNoTestRegistry()
		registry = os.Getenv("CUE_REGISTRY")
		k = kernel.New(kernel.WithRegistry(registry))

		plat, err := k.SynthesizePlatform(ctx, synth.PlatformInput{
			Name: "cluster",
			Type: "kubernetes",
			Subscriptions: map[string]synth.SubscriptionSpec{
				"opmodel.dev/catalogs/opm": {
					Filter: &synth.FilterSpec{Range: ">=1.0.0-alpha.1"},
				},
			},
		})
		if err != nil {
			Skip("synthesizing platform failed (registry/schema unreachable): " + err.Error())
		}
		mp, err := k.Materialize(ctx, plat)
		if err != nil {
			Skip("materializing platform failed (catalog unreachable): " + err.Error())
		}
		store = platformstore.NewStore()
		store.Set(1, mp)
	})

	It("renders the redis module as a StatefulSet + headless Service + PVC with an exec probe", func() {
		renderer := &render.KernelModuleRenderer{
			Kernel:      k,
			Store:       store,
			Registry:    registry,
			RuntimeName: core.LabelManagedByControllerValue,
		}

		values := &releasesv1alpha1.RawValues{}
		values.Raw = []byte(`{}`)
		res, err := renderer.RenderModule(ctx,
			"redis", "default",
			"opmodel.dev/modules/test/redis@v0", "v0.1.6",
			values)
		Expect(err).NotTo(HaveOccurred())
		Expect(res).NotTo(BeNil())

		// Collect the rendered resources by kind.
		byKind := map[string]map[string]any{}
		kinds := make([]string, 0, len(res.Resources))
		for _, r := range res.Resources {
			u, err := r.ToUnstructured()
			Expect(err).NotTo(HaveOccurred())
			byKind[u.GetKind()] = u.Object
			kinds = append(kinds, u.GetKind())
		}

		By("rendering a StatefulSet")
		sts, ok := byKind["StatefulSet"]
		Expect(ok).To(BeTrue(), "expected a StatefulSet among rendered resources, got kinds: %v", kinds)

		By("rendering a headless governing Service (clusterIP: None)")
		svc, ok := byKind["Service"]
		Expect(ok).To(BeTrue(), "expected a Service among rendered resources, got kinds: %v", kinds)
		clusterIP, found, err := unstructured.NestedString(svc, "spec", "clusterIP")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue(), "redis Service must set spec.clusterIP")
		Expect(clusterIP).To(Equal("None"), "redis Service must be headless")

		By("rendering a PersistentVolumeClaim for /data")
		_, ok = byKind["PersistentVolumeClaim"]
		Expect(ok).To(BeTrue(), "expected a PersistentVolumeClaim among rendered resources, got kinds: %v", kinds)

		By("declaring an exec readiness probe (redis-cli ping) on the container")
		containers, found, err := unstructured.NestedSlice(sts, "spec", "template", "spec", "containers")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())
		Expect(containers).NotTo(BeEmpty(), "StatefulSet must declare a container")
		c0, ok := containers[0].(map[string]any)
		Expect(ok).To(BeTrue())
		cmd, found, err := unstructured.NestedStringSlice(c0, "readinessProbe", "exec", "command")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue(), "container must declare an exec readiness probe")
		Expect(cmd).To(Equal([]string{"redis-cli", "ping"}))
	})
})
