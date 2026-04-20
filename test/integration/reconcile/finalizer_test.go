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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	"github.com/open-platform-model/opm-operator/internal/apply"
	opmcontroller "github.com/open-platform-model/opm-operator/internal/controller"
	opmreconcile "github.com/open-platform-model/opm-operator/internal/reconcile"
	"github.com/open-platform-model/opm-operator/internal/status"
)

// Regression test for the finalizer-add requeue: when the first reconcile
// adds the finalizer, the resulting watch event is filtered by
// GenerationChangedPredicate (finalizer patches do not bump
// metadata.generation). Without Requeue: true on the first return, the
// manager never re-enqueues the request and the MR sits without status
// conditions until the 10h periodic resync. This test exercises the path
// through a real manager (not a direct Reconcile call); otherwise the
// predicate never runs and the regression is invisible.
var _ = Describe("Finalizer-add requeue (manager-driven)", func() {
	It("populates status conditions within 10 seconds of MR creation", func() {
		mgrCtx, cancelMgr := context.WithCancel(ctx)
		defer cancelMgr()

		mgr, err := ctrl.NewManager(cfg, ctrl.Options{
			Scheme:                 scheme.Scheme,
			LeaderElection:         false,
			Metrics:                metricsserver.Options{BindAddress: "0"},
			HealthProbeBindAddress: "0",
		})
		Expect(err).NotTo(HaveOccurred())

		reconciler := &opmcontroller.ModuleReleaseReconciler{
			Client:          mgr.GetClient(),
			APIReader:       mgr.GetAPIReader(),
			Scheme:          mgr.GetScheme(),
			RestConfig:      cfg,
			Provider:        testProvider(),
			ResourceManager: apply.NewResourceManager(mgr.GetClient(), "opm-controller"),
			EventRecorder:   events.NewFakeRecorder(32),
			Renderer:        &stubRenderer{},
		}
		Expect(reconciler.SetupWithManager(mgr)).To(Succeed())

		go func() {
			defer GinkgoRecover()
			_ = mgr.Start(mgrCtx)
		}()

		mrName := "finalizer-requeue-mr"
		mr := &releasesv1alpha1.ModuleRelease{
			ObjectMeta: metav1.ObjectMeta{
				Name:      mrName,
				Namespace: namespace,
			},
			Spec: releasesv1alpha1.ModuleReleaseSpec{
				Module: releasesv1alpha1.ModuleReference{
					Path:    "opmodel.dev/test/module",
					Version: "v0.1.0",
				},
				Prune:  true,
				Values: &releasesv1alpha1.RawValues{},
			},
		}
		mr.Spec.Values.Raw = []byte(`{"message":"finalizer"}`)
		Expect(k8sClient.Create(ctx, mr)).To(Succeed())

		nn := types.NamespacedName{Name: mrName, Namespace: namespace}
		Eventually(func(g Gomega) {
			var current releasesv1alpha1.ModuleRelease
			g.Expect(k8sClient.Get(ctx, nn, &current)).To(Succeed())

			g.Expect(current.Finalizers).To(ContainElement(opmreconcile.FinalizerName),
				"finalizer must be added by the first reconcile")

			// The second reconcile (driven by the explicit Requeue, not by a
			// watch event the predicate would filter) must populate at least
			// one terminal/progress condition.
			g.Expect(current.Status.Conditions).NotTo(BeEmpty(),
				"status.conditions must be populated after the finalizer-add requeue")
			foundAny := false
			for _, typ := range []string{
				status.ReadyCondition,
				status.ReconcilingCondition,
				status.StalledCondition,
			} {
				if meta.FindStatusCondition(current.Status.Conditions, typ) != nil {
					foundAny = true
					break
				}
			}
			g.Expect(foundAny).To(BeTrue(),
				"at least one of Ready/Reconciling/Stalled must be set")
		}).WithTimeout(10 * time.Second).WithPolling(250 * time.Millisecond).Should(Succeed())

		// Cleanup: delete the MR (finalizer removed by handleDeletion once the
		// reconciler processes the deletion event).
		Expect(k8sClient.Delete(ctx, &releasesv1alpha1.ModuleRelease{
			ObjectMeta: metav1.ObjectMeta{Name: mrName, Namespace: namespace},
		})).To(Succeed())
		Eventually(func() bool {
			var current releasesv1alpha1.ModuleRelease
			err := k8sClient.Get(ctx, nn, &current)
			return err != nil
		}).WithTimeout(10 * time.Second).WithPolling(250 * time.Millisecond).Should(BeTrue())
	})
})
