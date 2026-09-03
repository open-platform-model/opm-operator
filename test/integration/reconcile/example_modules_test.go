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
	"github.com/open-platform-model/opm-operator/test/fixtures"
)

// These specs render the published example modules (redis here) through the
// real KernelModuleRenderer against the local OCI registry and assert the
// modelled workload/storage/probe contract, complementing the podinfo e2e
// (which proves the probes actually pass on a live cluster). They skip
// automatically in CI (the example modules + the catalog version they pin live
// on the local registry); run with `task dev:test:local`.
//
// The platform subscription names exactly one published catalog build (0010
// D14) — matching the cluster Platform sample and avoiding catalog-version
// skew. Resource and transformer FQNs embed the catalog version, so the
// platform MUST materialize the same catalog build the modules pin; any other
// build yields "no matching transformer".
var _ = Describe("Example module rendering", func() {
	var (
		k        *kernel.Kernel
		registry string
		store    *platformstore.Store
	)

	BeforeEach(func() {
		skipIfNoTestRegistry()
		registry = os.Getenv("CUE_REGISTRY")
		k = materializeKernel(registry)

		plat, err := k.SynthesizePlatform(ctx, synth.PlatformInput{
			Name: "cluster",
			Type: "kubernetes",
			Subscriptions: map[string]synth.SubscriptionSpec{
				defaultTestCatalogPath: {Version: testCatalogVersion()},
			},
		})
		if err != nil {
			registrySkip("synthesizing platform failed (registry/schema unreachable): " + err.Error())
		}
		mp, err := k.Materialize(ctx, plat)
		if err != nil {
			registrySkip("materializing platform failed (catalog unreachable): " + err.Error())
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
		redis := fixtures.Must(GinkgoT(), "redis")
		res, err := renderer.RenderModule(ctx,
			"redis", "default",
			redis.ModulePath, redis.Tag(),
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
