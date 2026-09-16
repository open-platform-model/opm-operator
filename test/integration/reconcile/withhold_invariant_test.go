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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	"github.com/open-platform-model/opm-operator/internal/inventory"
	opmreconcile "github.com/open-platform-model/opm-operator/internal/reconcile"
	"github.com/open-platform-model/opm-operator/internal/render"
	"github.com/open-platform-model/opm-operator/pkg/core"
)

// stubResource describes one rendered ConfigMap for withheldRenderResult.
// Withheld resources are rendered — they reach the inventory — but are kept
// out of the apply list, which is the shape the refusal produces.
type stubResource struct {
	name     string
	payload  string
	withheld bool
}

// configMapResource builds one owned ConfigMap resource carrying the given
// data payload, plus the labels the prune ownership guard reads.
func configMapResource(name, payload string) (*core.Resource, releasesv1alpha1.InventoryEntry) {
	cueCtx := cuecontext.New()
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
		payload: %q
	}
}`, name, namespace,
		core.LabelManagedBy, core.LabelManagedByControllerValue,
		core.LabelModuleInstanceNamespace, namespace,
		core.LabelModuleInstanceUUID, stubInstanceUUID,
		payload))
	if cm.Err() != nil {
		panic(fmt.Sprintf("compiling withhold stub ConfigMap: %v", cm.Err()))
	}

	resource := &core.Resource{
		Value:       cm,
		Instance:    name,
		Component:   name,
		Transformer: "kubernetes#simple",
	}
	u, err := resource.ToUnstructured()
	if err != nil {
		panic(fmt.Sprintf("converting withhold stub resource: %v", err))
	}
	return resource, inventory.NewEntryFromResource(u)
}

// withheldRenderResult models what the refusal will produce: the inventory is
// built from the full rendered set while the apply list carries only the
// resources that were not withheld. A withheld resource is rendered with a
// changed payload, so the test proves the inventory entry is indifferent to
// content rather than merely that nothing moved.
func withheldRenderResult(resources ...stubResource) *render.RenderResult {
	result := &render.RenderResult{}
	for _, spec := range resources {
		resource, entry := configMapResource(spec.name, spec.payload)
		if !spec.withheld {
			result.Resources = append(result.Resources, resource)
		}
		result.InventoryEntries = append(result.InventoryEntries, entry)
	}
	return result
}

var _ = Describe("Reconcile Withheld Apply Invariant", func() {
	// registration-shrink-refusal section 1: the one assumption the refusal
	// rests on. Withholding a resource from the apply list must leave the
	// inventory and the stale set untouched, so no prune fires and the
	// previously applied object survives. Asserted against the real reconcile
	// — the inventory it commits and the stale set it computes — not against
	// inventory.NewEntryFromResource in isolation.
	It("keeps a withheld resource owned, unpruned and out of the stale set", func() {
		mrName := "withhold-invariant-mr"
		createModuleInstance(mrName) // Prune: true
		nn := types.NamespacedName{Name: mrName, Namespace: namespace}

		params := reconcileParams()
		params.EventRecorder = events.NewFakeRecorder(30)
		params.Renderer = &stubRenderer{result: withheldRenderResult(
			stubResource{name: "withhold-a", payload: "v1"},
			stubResource{name: "withhold-b", payload: "v1"},
		)}
		ensureFinalizer(params, nn)

		By("first reconcile applies A and B and records both in the inventory")
		result, err := opmreconcile.ReconcileModuleInstance(ctx, params, ctrl.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(BeZero())

		var mi releasesv1alpha1.ModuleInstance
		Expect(k8sClient.Get(ctx, nn, &mi)).To(Succeed())
		Expect(mi.Status.Inventory).NotTo(BeNil())
		Expect(mi.Status.Inventory.Entries).To(HaveLen(2))
		entriesAfterFullApply := mi.Status.Inventory.Entries

		By("second reconcile renders A and B but withholds B from the apply list")
		withheld := withheldRenderResult(
			stubResource{name: "withhold-a", payload: "v2"},
			stubResource{name: "withhold-b", payload: "v2", withheld: true},
		)
		Expect(withheld.Resources).To(HaveLen(1))
		Expect(withheld.InventoryEntries).To(HaveLen(2))

		By("the stale set computed from the withheld render is empty")
		Expect(inventory.ComputeStaleSet(entriesAfterFullApply, withheld.InventoryEntries)).To(BeEmpty())

		params.Renderer = &stubRenderer{result: withheld}
		result, err = opmreconcile.ReconcileModuleInstance(ctx, params, ctrl.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(BeZero())

		By("the committed inventory still owns B, with entries identical to the full apply's")
		Expect(k8sClient.Get(ctx, nn, &mi)).To(Succeed())
		Expect(mi.Status.Inventory.Entries).To(HaveLen(2))
		Expect(mi.Status.Inventory.Count).To(Equal(int64(2)))
		Expect(mi.Status.Inventory.Entries).To(ConsistOf(entriesAfterFullApply))

		By("B survives the prune and still holds what the first apply wrote")
		var cm corev1.ConfigMap
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "withhold-b", Namespace: namespace}, &cm)).To(Succeed())
		Expect(cm.Data).To(HaveKeyWithValue("payload", "v1"))

		By("A, which was not withheld, received the new render")
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "withhold-a", Namespace: namespace}, &cm)).To(Succeed())
		Expect(cm.Data).To(HaveKeyWithValue("payload", "v2"))

		for _, name := range []string{"withhold-a", "withhold-b"} {
			Expect(k8sClient.Delete(ctx, &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
			})).To(Succeed())
		}
		cleanupInstance(nn)
	})
})
