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
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	"github.com/open-platform-model/opm-operator/internal/apply"
	"github.com/open-platform-model/opm-operator/internal/render"
)

// The instance side of the removal guard (0015:D3, D16):
// status.requiredContracts records what the instance's components demand, so
// a TransformerRegistration can count its dependents without re-rendering
// anything at deletion time.
var _ = Describe("ModuleInstance contract demand", func() {
	const namespace = "default"

	newInstance := func(ctx context.Context, name string) types.NamespacedName {
		mi := &releasesv1alpha1.ModuleInstance{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
			Spec: releasesv1alpha1.ModuleInstanceSpec{
				Module: releasesv1alpha1.ModuleReference{
					Path:    "opmodel.dev/test/module",
					Version: "v0.1.0",
				},
				Values: &releasesv1alpha1.RawValues{},
			},
		}
		mi.Spec.Values.Raw = []byte(`{"message": "hello"}`)
		Expect(k8sClient.Create(ctx, mi)).To(Succeed())
		return types.NamespacedName{Name: name, Namespace: namespace}
	}

	newReconciler := func(renderer render.ModuleRenderer) *ModuleInstanceReconciler {
		return &ModuleInstanceReconciler{
			Client:          k8sClient,
			Scheme:          k8sClient.Scheme(),
			ResourceManager: apply.NewResourceManager(k8sClient, "opm-controller"),
			EventRecorder:   events.NewFakeRecorder(10),
			Renderer:        renderer,
		}
	}

	// reconcileTwice runs the finalizer reconcile and then the pipeline one.
	reconcileTwice := func(ctx context.Context, r *ModuleInstanceReconciler, nn types.NamespacedName) {
		GinkgoHelper()
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())
		_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())
	}

	read := func(ctx context.Context, nn types.NamespacedName) *releasesv1alpha1.ModuleInstance {
		GinkgoHelper()
		var mi releasesv1alpha1.ModuleInstance
		Expect(k8sClient.Get(ctx, nn, &mi)).To(Succeed())
		return &mi
	}

	It("records what the render reported", func() {
		ctx := context.Background()
		nn := newInstance(ctx, "demand-recorded")

		reconcileTwice(ctx, newReconciler(&stubRenderer{}), nn)

		Expect(read(ctx, nn).Status.RequiredContracts).To(Equal([]string{stubContract}))
	})

	It("keeps the previous demand when a render fails", func() {
		ctx := context.Background()
		nn := newInstance(ctx, "demand-survives-failure")

		reconcileTwice(ctx, newReconciler(&stubRenderer{}), nn)
		Expect(read(ctx, nn).Status.RequiredContracts).To(Equal([]string{stubContract}))

		// A failed render returns before the write. The stale value
		// over-reports demand, which blocks a claim deletion that could have
		// proceeded — the direction a guard should fail in.
		failing := newReconciler(&stubRenderer{err: fmt.Errorf("rendering module instance: boom")})
		_, err := failing.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		Expect(read(ctx, nn).Status.RequiredContracts).To(Equal([]string{stubContract}),
			"a failed render must not clear the last known demand")
	})

	It("refreshes the demand on a no-op reconcile", func() {
		ctx := context.Background()
		nn := newInstance(ctx, "demand-follows-render")

		reconcileTwice(ctx, newReconciler(&stubRenderer{}), nn)
		Expect(read(ctx, nn).Status.RequiredContracts).To(Equal([]string{stubContract}))
		attemptedBefore := read(ctx, nn).Status.LastAttemptedAt
		Expect(attemptedBefore).NotTo(BeNil())

		// The same instance, re-rendered against a platform whose catalogs
		// moved: a no-op for apply (the rendered objects are identical) but
		// the only evidence that the demand changed.
		moved := stubRenderResult(namespace, nil)
		moved.RequiredContracts = []string{
			"opmodel.dev/catalogs/opm/traits/backup@v1alpha1",
			"opmodel.dev/catalogs/opm/traits/scaling@v1beta1",
		}
		shifted := newReconciler(&stubRenderer{result: moved})
		_, err := shifted.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		after := read(ctx, nn)
		// Pin the path: the NoOp branch of the deferred patch is bounded and
		// leaves lastAttempted* alone, so an unchanged timestamp is what
		// proves this reconcile went through it. Without this the spec would
		// still pass from the apply path if a digest ever moved, and the
		// scenario it exists for would go uncovered silently.
		Expect(after.Status.LastAttemptedAt).To(Equal(attemptedBefore),
			"this reconcile must be the no-op path, not a re-apply")
		Expect(after.Status.RequiredContracts).To(Equal(moved.RequiredContracts),
			"a no-op reconcile still refreshes the demand the render reported")
		Expect(after.Status.RequiredContracts).NotTo(ContainElement(stubContract),
			"a contract the instance stopped demanding leaves the status")
	})

	It("follows the spec when the instance's module changes", func() {
		ctx := context.Background()
		nn := newInstance(ctx, "demand-follows-spec")

		reconcileTwice(ctx, newReconciler(&stubRenderer{}), nn)
		Expect(read(ctx, nn).Status.RequiredContracts).To(Equal([]string{stubContract}))

		// A real spec edit, not a swapped stub: the instance is pointed at a
		// module whose components declare a different contract set, which is
		// the shape the requirement's scenario names.
		mi := read(ctx, nn)
		mi.Spec.Module.Version = "v0.2.0"
		Expect(k8sClient.Update(ctx, mi)).To(Succeed())

		rewritten := stubRenderResult(namespace, nil)
		rewritten.RequiredContracts = []string{
			"opmodel.dev/catalogs/opm/resources/secrets@v1beta1",
		}
		_, err := newReconciler(&stubRenderer{result: rewritten}).
			Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		after := read(ctx, nn)
		Expect(after.Status.RequiredContracts).To(Equal(rewritten.RequiredContracts))
		Expect(after.Status.RequiredContracts).NotTo(ContainElement(stubContract),
			"the previous module's contracts no longer appear")
	})

	It("leaves the demand untouched on a suspended instance", func() {
		ctx := context.Background()
		nn := newInstance(ctx, "demand-suspended")

		reconcileTwice(ctx, newReconciler(&stubRenderer{}), nn)
		Expect(read(ctx, nn).Status.RequiredContracts).To(Equal([]string{stubContract}))

		mi := read(ctx, nn)
		mi.Spec.Suspend = true
		Expect(k8sClient.Update(ctx, mi)).To(Succeed())

		// A renderer that would report nothing, to prove the suspend path
		// never reaches it.
		empty := stubRenderResult(namespace, nil)
		empty.RequiredContracts = nil
		_, err := newReconciler(&stubRenderer{result: empty}).
			Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		Expect(read(ctx, nn).Status.RequiredContracts).To(Equal([]string{stubContract}),
			"a suspended instance returns before the render and keeps its demand")
	})
})
