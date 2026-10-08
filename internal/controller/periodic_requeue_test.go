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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	"github.com/open-platform-model/opm-operator/internal/apply"
)

// periodicInterval is the instance reconcile interval of the periodic specs,
// and periodicUpper the longest requeue it may give with its jitter.
const (
	periodicInterval = 10 * time.Minute
	periodicUpper    = 11 * time.Minute
)

// periodicNamespace is the namespace of the periodic and restore specs.
const periodicNamespace = "default"

// periodicReconciler is a ModuleInstance reconciler with the instance
// reconcile interval set. drift is its drift render interval: zero makes
// every reconcile render.
func periodicReconciler(renderer *callCountingRenderer, drift time.Duration) *ModuleInstanceReconciler {
	return &ModuleInstanceReconciler{
		Client:              k8sClient,
		Scheme:              k8sClient.Scheme(),
		ResourceManager:     apply.NewResourceManager(k8sClient, "opm-controller"),
		EventRecorder:       events.NewFakeRecorder(100),
		Renderer:            renderer,
		OperatorVersion:     testOperatorVersion,
		LibraryVersion:      testLibraryVersion,
		DriftRenderInterval: drift,
		ReconcileInterval:   periodicInterval,
	}
}

// createPeriodicInstance creates an operator-owned ModuleInstance whose stub
// render is the ConfigMap "test-module", and removes both when the spec ends.
func createPeriodicInstance(ctx context.Context, name string) types.NamespacedName {
	const namespace = periodicNamespace
	mi := &releasesv1alpha1.ModuleInstance{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: releasesv1alpha1.ModuleInstanceSpec{
			Module: releasesv1alpha1.ModuleReference{Path: "opmodel.dev/test/module", Version: "v0.1.0"},
			Values: &releasesv1alpha1.RawValues{},
		},
	}
	mi.Spec.Values.Raw = []byte(`{"message": "hello"}`)
	Expect(k8sClient.Create(ctx, mi)).To(Succeed())
	DeferCleanup(func() {
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "test-module", Namespace: namespace},
		}))).To(Succeed())
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &releasesv1alpha1.ModuleInstance{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		}))).To(Succeed())
	})
	return types.NamespacedName{Name: name, Namespace: namespace}
}

// beThePeriodicRequeue matches a requeue of the instance reconcile interval
// with its jitter.
func beThePeriodicRequeue() OmegaMatcher {
	return And(BeNumerically(">=", periodicInterval), BeNumerically("<=", periodicUpper))
}

// A healthy ModuleInstance is reconciled again on the operator's instance
// reconcile interval; suspended and CLI-owned instances are not.
var _ = Describe("Periodic reconcile of a healthy ModuleInstance", func() {
	reconcileOnce := func(ctx context.Context, r *ModuleInstanceReconciler, nn types.NamespacedName) reconcile.Result {
		res, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())
		return res
	}

	It("requeues on the interval after an apply, a NoOp and a skipped render", func() {
		ctx := context.Background()
		createSkipPlatform(ctx)
		nn := createPeriodicInstance(ctx, "periodic-healthy-mi")
		renderer := &callCountingRenderer{}

		// Every reconcile renders: the apply, then a NoOp.
		rendering := periodicReconciler(renderer, 0)
		reconcileOnce(ctx, rendering, nn) // finalizer
		Expect(reconcileOnce(ctx, rendering, nn).RequeueAfter).To(beThePeriodicRequeue(), "after the apply")
		Expect(renderer.calls.Load()).To(Equal(int32(1)))

		Expect(reconcileOnce(ctx, rendering, nn).RequeueAfter).To(beThePeriodicRequeue(), "after the NoOp")
		Expect(renderer.calls.Load()).To(Equal(int32(2)), "the NoOp rendered")

		var mi releasesv1alpha1.ModuleInstance
		Expect(k8sClient.Get(ctx, nn, &mi)).To(Succeed())
		Expect(mi.Status.NextRetryAt).To(BeNil(), "a periodic requeue is not a retry")
		Expect(mi.Status.History).To(HaveLen(1), "the NoOp recorded no history entry")

		// With the render skip enabled, the next periodic reconcile finds the
		// key the apply recorded, skips its render and writes nothing.
		skipping := periodicReconciler(renderer, 30*time.Minute)
		counting, writes := patchCountingClient()
		skipping.Client = counting
		Expect(reconcileOnce(ctx, skipping, nn).RequeueAfter).To(beThePeriodicRequeue(), "after the skipped render")
		Expect(renderer.calls.Load()).To(Equal(int32(2)), "the periodic reconcile did not render")
		Expect(writes.Load()).To(BeZero(), "the periodic reconcile of an unchanged instance sends no write")
	})

	It("does not requeue when the interval is zero", func() {
		ctx := context.Background()
		nn := createPeriodicInstance(ctx, "periodic-disabled-mi")
		r := periodicReconciler(&callCountingRenderer{}, 0)
		r.ReconcileInterval = 0

		reconcileOnce(ctx, r, nn) // finalizer
		Expect(reconcileOnce(ctx, r, nn)).To(Equal(reconcile.Result{}), "after the apply")
		Expect(reconcileOnce(ctx, r, nn)).To(Equal(reconcile.Result{}), "after the NoOp")
	})

	It("does not requeue a suspended instance", func() {
		ctx := context.Background()
		nn := createPeriodicInstance(ctx, "periodic-suspended-mi")
		var mi releasesv1alpha1.ModuleInstance
		Expect(k8sClient.Get(ctx, nn, &mi)).To(Succeed())
		mi.Spec.Suspend = true
		Expect(k8sClient.Update(ctx, &mi)).To(Succeed())
		renderer := &callCountingRenderer{}
		r := periodicReconciler(renderer, 0)

		reconcileOnce(ctx, r, nn) // finalizer
		Expect(reconcileOnce(ctx, r, nn)).To(Equal(reconcile.Result{}))
		Expect(renderer.calls.Load()).To(BeZero())
	})

	It("does not requeue a CLI-owned instance", func() {
		ctx := context.Background()
		nn := createPeriodicInstance(ctx, "periodic-cli-mi")
		var mi releasesv1alpha1.ModuleInstance
		Expect(k8sClient.Get(ctx, nn, &mi)).To(Succeed())
		mi.Spec.Owner = releasesv1alpha1.OwnerCLI
		Expect(k8sClient.Update(ctx, &mi)).To(Succeed())
		renderer := &callCountingRenderer{}
		r := periodicReconciler(renderer, 0)

		Expect(reconcileOnce(ctx, r, nn)).To(Equal(reconcile.Result{}))
		Expect(renderer.calls.Load()).To(BeZero())
	})
})
