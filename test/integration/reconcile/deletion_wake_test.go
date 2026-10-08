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

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/config"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/open-platform-model/library/opm/k8s/labels"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	"github.com/open-platform-model/opm-operator/internal/apply"
	opmcontroller "github.com/open-platform-model/opm-operator/internal/controller"
	opmreconcile "github.com/open-platform-model/opm-operator/internal/reconcile"
	"github.com/open-platform-model/opm-operator/internal/status"
)

// A deletion stalled on a missing ServiceAccount requeues after 30 minutes.
// Setting the orphan annotation, a recovery action its message names, does
// not move metadata.generation, so only a predicate on the primary watch
// makes it take effect at once. The spec runs a real manager: a direct
// Reconcile call would pass with no trigger at all.
var _ = Describe("Recovery of a stalled deletion (manager-driven)", func() {
	const wakeWithin = 10 * time.Second

	// stalledDeletion leaves a ModuleInstance in Terminating with a one-entry
	// inventory and a ServiceAccount that does not exist, starts a manager,
	// and returns once the controller has stalled it with DeletionSAMissing.
	stalledDeletion := func(name, saName string) (types.NamespacedName, client.ObjectKey) {
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name + "-owned",
				Namespace: namespace,
				Labels:    map[string]string{labels.ManagedBy: labels.ManagedByController},
			},
			Data: map[string]string{"k": "v"},
		}
		Expect(k8sClient.Create(ctx, cm)).To(Succeed())
		DeferCleanup(func() {
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, cm))).To(Succeed())
		})

		mi := &releasesv1alpha1.ModuleInstance{
			ObjectMeta: metav1.ObjectMeta{
				Name:       name,
				Namespace:  namespace,
				Finalizers: []string{opmreconcile.FinalizerName},
			},
			Spec: releasesv1alpha1.ModuleInstanceSpec{
				Module:             releasesv1alpha1.ModuleReference{Path: "opmodel.dev/test/module", Version: "v0.1.0"},
				Prune:              true,
				ServiceAccountName: saName,
			},
		}
		Expect(k8sClient.Create(ctx, mi)).To(Succeed())
		nn := client.ObjectKeyFromObject(mi)

		var current releasesv1alpha1.ModuleInstance
		Expect(k8sClient.Get(ctx, nn, &current)).To(Succeed())
		current.Status.Inventory = &releasesv1alpha1.Inventory{
			Revision: 1,
			Count:    1,
			Entries: []releasesv1alpha1.InventoryEntry{
				{Kind: "ConfigMap", Name: cm.Name, Namespace: namespace, Version: "v1"},
			},
		}
		Expect(k8sClient.Status().Update(ctx, &current)).To(Succeed())
		Expect(k8sClient.Delete(ctx, &current)).To(Succeed())

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

		reconciler := &opmcontroller.ModuleInstanceReconciler{
			Client:          mgr.GetClient(),
			APIReader:       mgr.GetAPIReader(),
			Scheme:          mgr.GetScheme(),
			RestConfig:      cfg,
			ResourceManager: apply.NewResourceManager(mgr.GetClient(), "opm-controller"),
			EventRecorder:   events.NewFakeRecorder(256),
			Renderer:        &stubRenderer{},
		}
		Expect(reconciler.SetupWithManager(mgr)).To(Succeed())

		go func() {
			defer GinkgoRecover()
			_ = mgr.Start(mgrCtx)
		}()

		Eventually(func(g Gomega) {
			var stalled releasesv1alpha1.ModuleInstance
			g.Expect(k8sClient.Get(ctx, nn, &stalled)).To(Succeed())
			ready := apimeta.FindStatusCondition(stalled.Status.Conditions, status.ReadyCondition)
			g.Expect(ready).NotTo(BeNil())
			g.Expect(ready.Reason).To(Equal(status.DeletionSAMissingReason))
		}).WithTimeout(wakeWithin).WithPolling(100 * time.Millisecond).Should(Succeed())

		return nn, client.ObjectKeyFromObject(cm)
	}

	gone := func(nn types.NamespacedName) {
		GinkgoHelper()
		Eventually(func() bool {
			var mi releasesv1alpha1.ModuleInstance
			return apierrors.IsNotFound(k8sClient.Get(ctx, nn, &mi))
		}).WithTimeout(wakeWithin).WithPolling(100*time.Millisecond).Should(BeTrue(),
			"the recovery action must reconcile the instance without waiting for the stalled recheck")
	}

	It("releases the instance when the orphan annotation is set", func() {
		nn, cmKey := stalledDeletion("wake-orphan-mi", "wake-orphan-absent-sa")

		var stalled releasesv1alpha1.ModuleInstance
		Expect(k8sClient.Get(ctx, nn, &stalled)).To(Succeed())
		base := stalled.DeepCopy()
		stalled.Annotations = map[string]string{releasesv1alpha1.AnnotationForceDeleteOrphan: "true"}
		Expect(k8sClient.Patch(ctx, &stalled, client.MergeFrom(base))).To(Succeed())

		gone(nn)

		var orphaned corev1.ConfigMap
		Expect(k8sClient.Get(ctx, cmKey, &orphaned)).To(Succeed(), "the orphan exit prunes nothing")
	})
})
