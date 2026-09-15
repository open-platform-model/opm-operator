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

	"cuelang.org/go/cue/cuecontext"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	"github.com/open-platform-model/opm-operator/internal/apply"
	"github.com/open-platform-model/opm-operator/internal/inventory"
	"github.com/open-platform-model/opm-operator/internal/render"
	"github.com/open-platform-model/opm-operator/pkg/core"
)

// This spec is the measurement behind design.md § The rendered claim carries
// no namespace-bearing identity label: it drives a rendered-shape claim
// through the real ModuleInstance pipeline and records what reaches the
// cluster and what reaches the inventory. It stays as the regression that
// pins the finding, so a later change that starts stamping a namespace or
// uuid label on rendered output fails here rather than silently widening the
// identity signal acceptance relies on.
//
// The rendered object is byte-for-byte the golden fixture in catalog_opm's
// opm/transformers/transformer_registration_transformer.cue, whose
// _testTransformerRegistrationLabelCount guard asserts the label count is
// exactly four.
const (
	spikeClaimInstanceName      = "k8up"
	spikeClaimInstanceNamespace = "backup-system"
	spikeClaimName              = spikeClaimInstanceNamespace + "." + spikeClaimInstanceName
)

// renderedClaimResult builds the render result a provider module carrying the
// transformer-registration contract produces: one cluster-scoped
// TransformerRegistration and nothing else.
func renderedClaimResult() *render.RenderResult {
	cueCtx := cuecontext.New()
	claim := cueCtx.CompileString(fmt.Sprintf(`{
	apiVersion: "opmodel.dev/v1alpha1"
	kind:       "TransformerRegistration"
	metadata: {
		name: %q
		labels: {
			"app.kubernetes.io/managed-by":     "opm-controller"
			"app.kubernetes.io/name":           %q
			"app.kubernetes.io/instance":       %q
			"module-instance.opmodel.dev/name": %q
		}
	}
	spec: {
		catalog:  "opmodel.dev/catalogs/k8up@v1"
		version:  "1.0.0"
		provides: ["opmodel.dev/catalogs/opm/traits/backup@v1alpha1"]
		providerRef: {name: %q, namespace: %q}
	}
}`,
		spikeClaimName,
		spikeClaimInstanceName, spikeClaimInstanceName, spikeClaimInstanceName,
		spikeClaimInstanceName, spikeClaimInstanceNamespace))
	Expect(claim.Err()).NotTo(HaveOccurred())

	resource := &core.Resource{
		Value:       claim,
		Instance:    spikeClaimInstanceName,
		Component:   spikeClaimInstanceName,
		Transformer: "opm#transformer-registration-transformer",
	}
	u, err := resource.ToUnstructured()
	Expect(err).NotTo(HaveOccurred())

	return &render.RenderResult{
		Resources:        []*core.Resource{resource},
		InventoryEntries: []releasesv1alpha1.InventoryEntry{inventory.NewEntryFromResource(u)},
	}
}

var _ = Describe("TransformerRegistration claim identity", func() {
	It("records the claim in the instance inventory and carries no namespace-bearing label", func() {
		ctx := context.Background()

		instance := &releasesv1alpha1.ModuleInstance{
			ObjectMeta: metav1.ObjectMeta{
				Name:      spikeClaimInstanceName,
				Namespace: "default",
			},
			Spec: releasesv1alpha1.ModuleInstanceSpec{
				Module: releasesv1alpha1.ModuleReference{
					Path:    "opmodel.dev/modules/k8up",
					Version: "1.0.0",
				},
			},
		}
		Expect(k8sClient.Create(ctx, instance)).To(Succeed())

		reconciler := &ModuleInstanceReconciler{
			Client:          k8sClient,
			Scheme:          k8sClient.Scheme(),
			ResourceManager: apply.NewResourceManager(k8sClient, "opm-controller"),
			EventRecorder:   events.NewFakeRecorder(10),
			Renderer:        &stubRenderer{result: renderedClaimResult()},
		}

		nn := types.NamespacedName{Name: spikeClaimInstanceName, Namespace: "default"}

		By("reconciling twice: the first call only adds the finalizer")
		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())
		_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		By("measuring the labels the applied claim carries")
		var claim releasesv1alpha1.TransformerRegistration
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: spikeClaimName}, &claim)).To(Succeed())

		// The finding: the label set identifies an instance NAME and nothing
		// else. No namespace label, no uuid label, so a label-only owner check
		// cannot tell two same-named instances in different namespaces apart.
		Expect(claim.Labels).To(HaveKey(core.LabelModuleInstanceName))
		Expect(claim.Labels).NotTo(HaveKey(core.LabelModuleInstanceNamespace),
			"a namespace label would make the label-only owner check D11 suggested implementable")
		Expect(claim.Labels).NotTo(HaveKey(core.LabelModuleInstanceUUID))
		Expect(claim.Namespace).To(BeEmpty(), "the claim is cluster-scoped")

		By("measuring what the instance inventory records for the claim")
		var reconciled releasesv1alpha1.ModuleInstance
		Expect(k8sClient.Get(ctx, nn, &reconciled)).To(Succeed())
		Expect(reconciled.Status.Inventory).NotTo(BeNil())
		Expect(reconciled.Status.Inventory.Entries).To(ContainElement(releasesv1alpha1.InventoryEntry{
			Group:     "opmodel.dev",
			Kind:      "TransformerRegistration",
			Namespace: "",
			Name:      spikeClaimName,
			Version:   "v1alpha1",
			Component: "",
		}), "the inventory identifies the claim unambiguously, which the labels cannot")
	})
})
