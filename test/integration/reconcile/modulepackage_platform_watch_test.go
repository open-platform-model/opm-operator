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
	"sync/atomic"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/config"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	opmcontroller "github.com/open-platform-model/opm-operator/internal/controller"
	"github.com/open-platform-model/opm-operator/internal/status"
)

// platformListCountingClient counts the cluster-wide ModulePackageList calls.
// mapPlatformToModulePackages is the only caller that lists ModulePackages
// across all namespaces (the Flux source mapper lists in the source's
// namespace), so with no ModulePackage in the cluster each count is one
// Platform event that reached the mapper.
type platformListCountingClient struct {
	client.Client
	lists atomic.Int64
}

func (c *platformListCountingClient) List(
	ctx context.Context, list client.ObjectList, opts ...client.ListOption,
) error {
	if _, ok := list.(*releasesv1alpha1.ModulePackageList); ok {
		lo := &client.ListOptions{}
		lo.ApplyOptions(opts)
		if lo.Namespace == "" {
			c.lists.Add(1)
		}
	}
	return c.Client.List(ctx, list, opts...)
}

func (c *platformListCountingClient) count() int64 { return c.lists.Load() }

// The manager-driven proof that the ModulePackage controller's Platform watch
// carries the shared consumed-fields predicate: the Ready edge, a pin-set
// change and an operatorVersion-only write reach the mapper, and a status
// write that changes only a message or a report does not. No ModulePackage
// and no Platform reconciler run, so the spec needs no registry, source or
// render; the field-by-field table lives in the predicate's unit test.
var _ = Describe("ModulePackage Platform watch (manager-driven)", func() {
	const platformName = "cluster"

	It("re-enqueues packages only when a consumed Platform field changes", func() {
		// Registered first so it runs last (DeferCleanup is LIFO): the
		// Platform is deleted while the manager runs.
		mgrCtx, cancelMgr := context.WithCancel(ctx)
		DeferCleanup(cancelMgr)

		skipNameValidation := true
		mgr, err := ctrl.NewManager(cfg, ctrl.Options{
			Scheme:                 scheme.Scheme,
			LeaderElection:         false,
			Metrics:                metricsserver.Options{BindAddress: "0"},
			HealthProbeBindAddress: "0",
			Controller:             config.Controller{SkipNameValidation: &skipNameValidation},
		})
		Expect(err).NotTo(HaveOccurred())

		counting := &platformListCountingClient{Client: mgr.GetClient()}
		reconciler := &opmcontroller.ModulePackageReconciler{
			Client:        counting,
			APIReader:     mgr.GetAPIReader(),
			Scheme:        mgr.GetScheme(),
			RestConfig:    cfg,
			EventRecorder: events.NewFakeRecorder(64),
		}
		Expect(reconciler.SetupWithManager(mgr)).To(Succeed())

		go func() {
			defer GinkgoRecover()
			_ = mgr.Start(mgrCtx)
		}()

		plat := &releasesv1alpha1.Platform{
			ObjectMeta: metav1.ObjectMeta{Name: platformName},
			Spec:       releasesv1alpha1.PlatformSpec{Type: "kubernetes"},
		}
		Expect(k8sClient.Create(ctx, plat)).To(Succeed())
		DeferCleanup(func(ctx context.Context) {
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &releasesv1alpha1.Platform{
				ObjectMeta: metav1.ObjectMeta{Name: platformName},
			}))).To(Succeed())
		})

		writePlatformStatus := func(mutate func(*releasesv1alpha1.Platform)) {
			Eventually(func(g Gomega) {
				var current releasesv1alpha1.Platform
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: platformName}, &current)).To(Succeed())
				mutate(&current)
				g.Expect(k8sClient.Status().Update(ctx, &current)).To(Succeed())
			}).WithTimeout(5 * time.Second).WithPolling(50 * time.Millisecond).Should(Succeed())
		}
		setReady := func(p *releasesv1alpha1.Platform, s metav1.ConditionStatus, reason, msg string) {
			meta.SetStatusCondition(&p.Status.Conditions, metav1.Condition{
				Type: status.ReadyCondition, Status: s, Reason: reason, Message: msg, ObservedGeneration: p.Generation,
			})
		}

		// quiesce waits until the count holds still for window and returns it,
		// so the next write is the only Platform event in flight.
		quiesce := func(window time.Duration) int64 {
			var n int64
			Eventually(func(g Gomega) {
				n = counting.count()
				g.Consistently(counting.count).WithTimeout(window).WithPolling(25 * time.Millisecond).Should(Equal(n))
			}).WithTimeout(20 * time.Second).Should(Succeed())
			return n
		}
		grows := func(from int64, why string) {
			Eventually(counting.count).WithTimeout(5*time.Second).WithPolling(20*time.Millisecond).
				Should(BeNumerically(">", from), why)
		}

		// The Platform's create event passes; wait until the mapper has run
		// for it, then write a not-yet-generated status.
		grows(0, "the Platform create event reaches the mapper")
		writePlatformStatus(func(p *releasesv1alpha1.Platform) {
			setReady(p, metav1.ConditionFalse, status.BuildFailedReason, "building platform module: not yet")
		})
		blocked := quiesce(300 * time.Millisecond)

		// The recovery edge: Ready moves False -> True.
		writePlatformStatus(func(p *releasesv1alpha1.Platform) {
			p.Status.ObservedGeneration = p.Generation
			p.Status.PackageIdentity = "g1"
			p.Status.OperatorVersion = "v1.0.0-beta.1"
			setReady(p, metav1.ConditionTrue, status.GeneratedReason, "Platform module generated and built for generation 1")
		})
		grows(blocked, "the Ready=True write re-enqueues packages blocked on PlatformNotReady")
		settled := quiesce(300 * time.Millisecond)

		// A message-only Ready rewrite and a ContractsFulfilled update change
		// nothing a package renders against: the mapper does not run.
		writePlatformStatus(func(p *releasesv1alpha1.Platform) {
			setReady(p, metav1.ConditionTrue, status.GeneratedReason,
				"Platform module generated and built for generation 1 (refreshed)")
			meta.SetStatusCondition(&p.Status.Conditions, metav1.Condition{
				Type: status.ContractsFulfilledCondition, Status: metav1.ConditionFalse,
				Reason: status.UnfulfilledContractsReason, Message: "one contract has no provider",
			})
		})
		Consistently(counting.count).WithTimeout(3*time.Second).WithPolling(100*time.Millisecond).Should(Equal(settled),
			"a Platform status write that changes no consumed field enqueues no package")

		// A new pin set re-enqueues packages.
		writePlatformStatus(func(p *releasesv1alpha1.Platform) {
			p.Status.PackageIdentity = "g1+claims"
		})
		grows(settled, "a pin-set change re-enqueues packages")
		afterPin := quiesce(300 * time.Millisecond)

		// An operator upgrade into an already-Ready Platform: the regenerated
		// Platform's status write moves only status.operatorVersion, and that
		// alone must re-enqueue packages blocked on the empty store.
		writePlatformStatus(func(p *releasesv1alpha1.Platform) {
			p.Status.OperatorVersion = "v1.0.0-beta.2"
		})
		grows(afterPin, "an operatorVersion-only write re-enqueues packages")
	})
})
