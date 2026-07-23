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
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"cuelang.org/go/cue/cuecontext"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	"github.com/open-platform-model/opm-operator/internal/inventory"
	opmreconcile "github.com/open-platform-model/opm-operator/internal/reconcile"
	"github.com/open-platform-model/opm-operator/internal/render"
	"github.com/open-platform-model/opm-operator/internal/status"
	"github.com/open-platform-model/opm-operator/pkg/core"
)

// namedConfigMapRenderResult builds a render result of one ConfigMap per name
// in the suite namespace,
// each carrying the controller manager label and the stub instance UUID so the
// prune ownership guard treats them as owned by the reconciling instance.
func namedConfigMapRenderResult(names ...string) *render.RenderResult {
	cueCtx := cuecontext.New()
	result := &render.RenderResult{}
	for _, name := range names {
		cm := cueCtx.CompileString(fmt.Sprintf(`{
	apiVersion: "v1"
	kind:       "ConfigMap"
	metadata: {
		name:      %q
		namespace: %q
		labels: {
			%q: %q
			%q: %q
			%q: %q
		}
	}
	data: {
		name: %q
	}
}`, name, namespace,
			core.LabelManagedBy, core.LabelManagedByControllerValue,
			core.LabelModuleInstanceNamespace, namespace,
			core.LabelModuleInstanceUUID, stubInstanceUUID,
			name))
		if cm.Err() != nil {
			panic(fmt.Sprintf("compiling named stub ConfigMap: %v", cm.Err()))
		}

		resource := &core.Resource{
			Value:       cm,
			Instance:    name,
			Component:   name,
			Transformer: "kubernetes#simple",
		}
		u, err := resource.ToUnstructured()
		if err != nil {
			panic(fmt.Sprintf("converting named stub resource: %v", err))
		}

		result.Resources = append(result.Resources, resource)
		result.InventoryEntries = append(result.InventoryEntries, inventory.NewEntryFromResource(u))
	}
	return result
}

// cleanupInstance force-removes the cleanup finalizer and deletes the
// ModuleInstance without running the deletion prune path.
func cleanupInstance(nn types.NamespacedName) {
	var mi releasesv1alpha1.ModuleInstance
	Expect(k8sClient.Get(ctx, nn, &mi)).To(Succeed())
	if controllerutil.ContainsFinalizer(&mi, opmreconcile.FinalizerName) {
		controllerutil.RemoveFinalizer(&mi, opmreconcile.FinalizerName)
		Expect(k8sClient.Update(ctx, &mi)).To(Succeed())
	}
	Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &mi))).To(Succeed())
}

var _ = Describe("Reconcile Stale Pruning", func() {
	// Validates Phase 4 stale-set computation + Phase 6 prune during normal
	// reconcile (design 2.1).
	It("should prune a resource removed from the render output", func() {
		mrName := "stale-prune-mr"
		createModuleInstance(mrName) // Prune: true
		nn := types.NamespacedName{Name: mrName, Namespace: namespace}

		rec := events.NewFakeRecorder(30)
		params := reconcileParams()
		params.EventRecorder = rec
		params.Renderer = &stubRenderer{result: namedConfigMapRenderResult("stale-a", "stale-b")}
		ensureFinalizer(params, nn)

		By("first reconcile applies A and B")
		result, err := opmreconcile.ReconcileModuleInstance(ctx, params, ctrl.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(BeZero())

		var cm corev1.ConfigMap
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "stale-a", Namespace: namespace}, &cm)).To(Succeed())
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "stale-b", Namespace: namespace}, &cm)).To(Succeed())

		var mi releasesv1alpha1.ModuleInstance
		Expect(k8sClient.Get(ctx, nn, &mi)).To(Succeed())
		Expect(mi.Status.Inventory).NotTo(BeNil())
		Expect(mi.Status.Inventory.Count).To(Equal(int64(2)))

		By("second reconcile renders only A — B moves to the stale set and is pruned")
		params.Renderer = &stubRenderer{result: namedConfigMapRenderResult("stale-a")}
		result, err = opmreconcile.ReconcileModuleInstance(ctx, params, ctrl.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(BeZero())

		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "stale-a", Namespace: namespace}, &cm)).To(Succeed())
		err = k8sClient.Get(ctx, types.NamespacedName{Name: "stale-b", Namespace: namespace}, &cm)
		Expect(err).To(HaveOccurred())
		Expect(client.IgnoreNotFound(err)).To(Succeed())

		By("inventory contains only A and the Pruned event was emitted (AppliedAndPruned outcome)")
		Expect(k8sClient.Get(ctx, nn, &mi)).To(Succeed())
		Expect(mi.Status.Inventory.Count).To(Equal(int64(1)))
		Expect(mi.Status.Inventory.Entries).To(HaveLen(1))
		Expect(mi.Status.Inventory.Entries[0].Name).To(Equal("stale-a"))
		Expect(countBufferedEvents(rec, status.PrunedReason)).To(Equal(1))

		ready := apimeta.FindStatusCondition(mi.Status.Conditions, status.ReadyCondition)
		Expect(ready).NotTo(BeNil())
		Expect(ready.Status).To(Equal(metav1.ConditionTrue))

		// Cleanup
		Expect(k8sClient.Delete(ctx, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "stale-a", Namespace: namespace},
		})).To(Succeed())
		cleanupInstance(nn)
	})

	// Validates the Phase 6 prune gate when spec.prune=false (design 2.2).
	It("should retain stale resources when prune is disabled", func() {
		mrName := "noprune-mr"
		mr := &releasesv1alpha1.ModuleInstance{
			ObjectMeta: metav1.ObjectMeta{
				Name:      mrName,
				Namespace: namespace,
			},
			Spec: releasesv1alpha1.ModuleInstanceSpec{
				Module: releasesv1alpha1.ModuleReference{
					Path:    "opmodel.dev/test/module",
					Version: "v0.1.0",
				},
				Prune:  false,
				Values: &releasesv1alpha1.RawValues{},
			},
		}
		mr.Spec.Values.Raw = []byte(`{"message": "hello"}`)
		Expect(k8sClient.Create(ctx, mr)).To(Succeed())
		nn := types.NamespacedName{Name: mrName, Namespace: namespace}

		rec := events.NewFakeRecorder(30)
		params := reconcileParams()
		params.EventRecorder = rec
		params.Renderer = &stubRenderer{result: namedConfigMapRenderResult("noprune-a", "noprune-b")}
		ensureFinalizer(params, nn)

		By("first reconcile applies A and B")
		_, err := opmreconcile.ReconcileModuleInstance(ctx, params, ctrl.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		By("second reconcile renders only A — B is stale but prune is disabled")
		params.Renderer = &stubRenderer{result: namedConfigMapRenderResult("noprune-a")}
		result, err := opmreconcile.ReconcileModuleInstance(ctx, params, ctrl.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(BeZero())

		By("B remains in the cluster (orphaned) and no Pruned event was emitted")
		var cm corev1.ConfigMap
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "noprune-b", Namespace: namespace}, &cm)).To(Succeed())
		Expect(countBufferedEvents(rec, status.PrunedReason)).To(BeZero())

		// The committed inventory tracks the current render (A only) — the
		// orphaned B leaves inventory. Outcome is Applied, not
		// AppliedAndPruned, and Ready=True.
		var mi releasesv1alpha1.ModuleInstance
		Expect(k8sClient.Get(ctx, nn, &mi)).To(Succeed())
		Expect(mi.Status.Inventory).NotTo(BeNil())
		Expect(mi.Status.Inventory.Count).To(Equal(int64(1)))
		Expect(mi.Status.Inventory.Entries[0].Name).To(Equal("noprune-a"))
		ready := apimeta.FindStatusCondition(mi.Status.Conditions, status.ReadyCondition)
		Expect(ready).NotTo(BeNil())
		Expect(ready.Status).To(Equal(metav1.ConditionTrue))

		// Cleanup
		for _, name := range []string{"noprune-a", "noprune-b"} {
			Expect(k8sClient.Delete(ctx, &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
			})).To(Succeed())
		}
		cleanupInstance(nn)
	})

	// Validates selective pruning via ComputeStaleSet identity comparison
	// across multiple resources (design 2.3).
	It("should prune only the removed resource when multiple exist", func() {
		mrName := "selective-prune-mr"
		createModuleInstance(mrName) // Prune: true
		nn := types.NamespacedName{Name: mrName, Namespace: namespace}

		params := reconcileParams()
		params.Renderer = &stubRenderer{result: namedConfigMapRenderResult("sel-a", "sel-b", "sel-c")}
		ensureFinalizer(params, nn)

		By("first reconcile applies A, B, and C")
		_, err := opmreconcile.ReconcileModuleInstance(ctx, params, ctrl.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		By("second reconcile renders A and C — only B is pruned")
		params.Renderer = &stubRenderer{result: namedConfigMapRenderResult("sel-a", "sel-c")}
		result, err := opmreconcile.ReconcileModuleInstance(ctx, params, ctrl.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(BeZero())

		var cm corev1.ConfigMap
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "sel-a", Namespace: namespace}, &cm)).To(Succeed())
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "sel-c", Namespace: namespace}, &cm)).To(Succeed())
		err = k8sClient.Get(ctx, types.NamespacedName{Name: "sel-b", Namespace: namespace}, &cm)
		Expect(err).To(HaveOccurred())
		Expect(client.IgnoreNotFound(err)).To(Succeed())

		By("inventory reflects A and C")
		var mi releasesv1alpha1.ModuleInstance
		Expect(k8sClient.Get(ctx, nn, &mi)).To(Succeed())
		Expect(mi.Status.Inventory).NotTo(BeNil())
		Expect(mi.Status.Inventory.Count).To(Equal(int64(2)))
		names := []string{mi.Status.Inventory.Entries[0].Name, mi.Status.Inventory.Entries[1].Name}
		Expect(names).To(ConsistOf("sel-a", "sel-c"))

		// Cleanup
		for _, name := range []string{"sel-a", "sel-c"} {
			Expect(k8sClient.Delete(ctx, &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
			})).To(Succeed())
		}
		cleanupInstance(nn)
	})
})
