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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/open-platform-model/library/opm/helper/synth"
	"github.com/open-platform-model/library/opm/kernel"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	opmcontroller "github.com/open-platform-model/opm-operator/internal/controller"
	platformstore "github.com/open-platform-model/opm-operator/internal/platform"
	"github.com/open-platform-model/opm-operator/internal/status"
)

// recoveryPlatformName is the cluster singleton — the only name the CRD permits.
const recoveryPlatformName = "cluster"

// liveMaterializeKernelOrSkip builds a Kernel from CUE_REGISTRY and skips unless
// it can materialize the cluster platform carrying the configured catalog
// subscription — i.e. the registry is reachable AND the subscription resolves.
// The recovery spec needs a subscription that actually pulls from the registry
// (so a dead endpoint fails and a live one succeeds), which is why it requires a
// resolvable catalog fixture rather than the trivial no-subscription platform.
func liveMaterializeKernelOrSkip() (*kernel.Kernel, string) {
	reg := os.Getenv("CUE_REGISTRY")
	if reg == "" {
		Skip("CUE_REGISTRY not set — platform recovery spec needs a reachable registry with opmodel.dev/core@v0")
	}
	catalogPath := os.Getenv("OPM_TEST_CATALOG_PATH")
	if catalogPath == "" {
		Skip("OPM_TEST_CATALOG_PATH not set — recovery spec needs a resolvable catalog subscription to materialize")
	}
	k := kernel.New(kernel.WithRegistry(reg))
	in := synth.PlatformInput{
		Name:          recoveryPlatformName,
		Type:          "kubernetes",
		Subscriptions: map[string]synth.SubscriptionSpec{catalogPath: {}},
	}
	p, err := k.SynthesizePlatform(ctx, in)
	if err != nil {
		Skip("opmodel.dev/core schema not resolvable from CUE_REGISTRY: " + err.Error())
	}
	if _, err := k.Materialize(ctx, p); err != nil {
		Skip("catalog subscription did not materialize from CUE_REGISTRY: " + err.Error())
	}
	return k, catalogPath
}

// This is the registry-backed counterpart to the unit-level failure specs in
// internal/controller: it proves the spec.md "Recovery without a spec change"
// scenario end-to-end. The same Platform CR (same generation) reconciles to
// MaterializeFailed against an unreachable registry, then — with no edit to the
// CR — reconciles to Ready once the registry condition clears. The reconcile
// loop self-heals on its own requeue; nothing re-triggers the generation
// predicate.
//
// The "registry condition clears" is modelled by swapping the reconciler's
// Kernel from one pointed at a dead endpoint to one pointed at the working
// registry. WithRegistry is construction-only, so a fresh Kernel is the
// reconciler's only window onto a recovered registry; the Platform CR and its
// generation stay untouched throughout, which is the property the scenario
// asserts.
var _ = Describe("Platform materialize recovery (registry-backed)", func() {
	It("recovers a MaterializeFailed platform without a spec change once the registry clears", func() {
		// Gate on a reachable registry + resolvable catalog first, so the spec
		// skips cleanly in the ghcr CI path (same posture as the other
		// registry-backed specs in this suite).
		liveKernel, catalogPath := liveMaterializeKernelOrSkip()

		// Defensive: drop any cluster Platform a sibling spec may have left.
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &releasesv1alpha1.Platform{
			ObjectMeta: metav1.ObjectMeta{Name: recoveryPlatformName},
		}))).To(Succeed())
		Eventually(func() bool {
			err := k8sClient.Get(ctx, client.ObjectKey{Name: recoveryPlatformName}, &releasesv1alpha1.Platform{})
			return err != nil && client.IgnoreNotFound(err) == nil
		}).WithTimeout(10 * time.Second).WithPolling(200 * time.Millisecond).Should(BeTrue())

		plat := &releasesv1alpha1.Platform{
			ObjectMeta: metav1.ObjectMeta{Name: recoveryPlatformName},
			Spec: releasesv1alpha1.PlatformSpec{
				Type:     "kubernetes",
				Registry: map[string]releasesv1alpha1.Subscription{catalogPath: {}},
			},
		}
		Expect(k8sClient.Create(ctx, plat)).To(Succeed())
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(plat), plat)).To(Succeed())
		generation := plat.Generation
		Expect(generation).NotTo(BeZero())
		DeferCleanup(func() {
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &releasesv1alpha1.Platform{
				ObjectMeta: metav1.ObjectMeta{Name: recoveryPlatformName},
			}))).To(Succeed())
		})

		store := platformstore.NewStore()
		// A Kernel pointed at a closed port: catalog resolution gets a connection
		// failure, modelling an unreachable registry.
		deadKernel := kernel.New(kernel.WithRegistry(
			"opmodel.dev=localhost:1+insecure,testing.opmodel.dev=localhost:1+insecure"))
		r := &opmcontroller.PlatformReconciler{
			Client:        k8sClient,
			Scheme:        scheme.Scheme,
			EventRecorder: events.NewFakeRecorder(10),
			Kernel:        deadKernel,
			Store:         store,
		}
		req := ctrl.Request{NamespacedName: client.ObjectKey{Name: recoveryPlatformName}}

		// Phase 1: registry unreachable → MaterializeFailed, requeued, store empty.
		res, err := r.Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())
		Expect(res.RequeueAfter).To(BeNumerically(">", 0),
			"a failed materialize must requeue rather than stall indefinitely")

		failed := &releasesv1alpha1.Platform{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(plat), failed)).To(Succeed())
		ready := apimeta.FindStatusCondition(failed.Status.Conditions, status.ReadyCondition)
		Expect(ready).NotTo(BeNil())
		Expect(ready.Status).To(Equal(metav1.ConditionFalse))
		Expect(ready.Reason).To(Equal(status.MaterializeFailedReason))
		Expect(failed.Status.ObservedGeneration).To(Equal(generation),
			"observedGeneration must be recorded on the failure path")
		_, held := store.Get()
		Expect(held).To(BeFalse(), "nothing should be stored while materialize is failing")

		// Phase 2: the registry condition clears — no edit to the Platform CR.
		// Swap in the working Kernel and reconcile the same object again.
		r.Kernel = liveKernel

		res, err = r.Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())
		Expect(res.RequeueAfter).To(BeZero(), "a successful materialize does not requeue")

		recovered := &releasesv1alpha1.Platform{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(plat), recovered)).To(Succeed())
		Expect(recovered.Generation).To(Equal(generation),
			"recovery must happen without a spec change (generation unchanged)")
		ready = apimeta.FindStatusCondition(recovered.Status.Conditions, status.ReadyCondition)
		Expect(ready).NotTo(BeNil())
		Expect(ready.Status).To(Equal(metav1.ConditionTrue))
		Expect(ready.Reason).To(Equal(status.MaterializedReason))
		Expect(recovered.Status.ObservedGeneration).To(Equal(generation))

		got, held := store.Get()
		Expect(held).To(BeTrue(), "the recovered platform must be held in the store")
		Expect(got).NotTo(BeNil())
		Expect(store.Generation()).To(Equal(generation))
	})
})
