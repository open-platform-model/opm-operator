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
	"strings"

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

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	"github.com/open-platform-model/opm-operator/internal/inventory"
	opmreconcile "github.com/open-platform-model/opm-operator/internal/reconcile"
	"github.com/open-platform-model/opm-operator/internal/render"
	"github.com/open-platform-model/opm-operator/internal/status"
	"github.com/open-platform-model/opm-operator/pkg/core"
)

const (
	contractStorage = "opmodel.dev/contract.Storage"
	contractBackup  = "opmodel.dev/contract.Backup"
)

// claimResource builds the TransformerRegistration a provider module renders,
// carrying the ownership labels the prune guard reads.
func claimResource(
	claimName, providerName, version string,
	provides ...string,
) (*core.Resource, releasesv1alpha1.InventoryEntry) {
	quoted := make([]string, 0, len(provides))
	for _, fqn := range provides {
		quoted = append(quoted, fmt.Sprintf("%q", fqn))
	}

	cueCtx := cuecontext.New()
	claim := cueCtx.CompileString(fmt.Sprintf(`{
	apiVersion: "opmodel.dev/v1alpha1"
	kind:       "TransformerRegistration"
	metadata: {
		name: %q
		labels: {
			%q: %q
			%q: %q
			%q: %q
		}
	}
	spec: {
		catalog: "opmodel.dev/catalogs/example@v1"
		version: %q
		provides: [%s]
		providerRef: {
			namespace: %q
			name:      %q
		}
	}
}`, claimName,
		core.LabelManagedBy, core.LabelManagedByControllerValue,
		core.LabelModuleInstanceNamespace, namespace,
		core.LabelModuleInstanceUUID, stubInstanceUUID,
		version, strings.Join(quoted, ", "), namespace, providerName))
	if claim.Err() != nil {
		panic(fmt.Sprintf("compiling stub claim: %v", claim.Err()))
	}

	resource := &core.Resource{
		Value:       claim,
		Instance:    providerName,
		Component:   "registration",
		Transformer: "kubernetes#simple",
	}
	u, err := resource.ToUnstructured()
	if err != nil {
		panic(fmt.Sprintf("converting stub claim: %v", err))
	}
	return resource, inventory.NewEntryFromResource(u)
}

// providerRenderResult is what a provider module renders: its claim plus one
// ordinary resource, so a refusal can be shown to withhold the claim alone.
func providerRenderResult(claimName, providerName, version, payload string, provides ...string) *render.RenderResult {
	claim, claimEntry := claimResource(claimName, providerName, version, provides...)
	sidecar, sidecarEntry := configMapResource(providerName+"-cm", payload)
	return &render.RenderResult{
		Resources:        []*core.Resource{claim, sidecar},
		InventoryEntries: []releasesv1alpha1.InventoryEntry{claimEntry, sidecarEntry},
	}
}

// storeClaim creates the accepted claim the cluster already holds, with a
// verdict on it so the test can prove the refusal leaves it alone.
func storeClaim(claimName, providerName string, provides ...string) *releasesv1alpha1.TransformerRegistration {
	stored := &releasesv1alpha1.TransformerRegistration{
		ObjectMeta: metav1.ObjectMeta{Name: claimName},
		Spec: releasesv1alpha1.TransformerRegistrationSpec{
			Catalog:  "opmodel.dev/catalogs/example@v1",
			Version:  "1.0.0",
			Provides: provides,
			ProviderRef: releasesv1alpha1.ProviderReference{
				Namespace: namespace,
				Name:      providerName,
			},
		},
	}
	Expect(k8sClient.Create(ctx, stored)).To(Succeed())

	stored.Status.Accepted = true
	stored.Status.Active = true
	apimeta.SetStatusCondition(&stored.Status.Conditions, metav1.Condition{
		Type:    status.ReadyCondition,
		Status:  metav1.ConditionTrue,
		Reason:  status.AcceptedReason,
		Message: "Claim accepted",
	})
	Expect(k8sClient.Status().Update(ctx, stored)).To(Succeed())
	return stored
}

// demandContracts creates an instance in another namespace whose recorded
// demand is what the refusal protects.
func demandContracts(name string, contracts ...string) types.NamespacedName {
	instance := &releasesv1alpha1.ModuleInstance{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: releasesv1alpha1.ModuleInstanceSpec{
			Module: releasesv1alpha1.ModuleReference{
				Path:    "opmodel.dev/test/consumer",
				Version: "v0.1.0",
			},
			Suspend: true,
		},
	}
	Expect(k8sClient.Create(ctx, instance)).To(Succeed())
	instance.Status.RequiredContracts = contracts
	Expect(k8sClient.Status().Update(ctx, instance)).To(Succeed())
	return types.NamespacedName{Name: name, Namespace: namespace}
}

var _ = Describe("Reconcile Provides Shrink Refusal", func() {
	// registration-shrink-refusal section 2 (enhancement 0015 D16): a provider
	// upgrade that drops a still-demanded contract is withheld from apply
	// while the accepted claim keeps serving.
	It("withholds the shrinking claim, applies everything else and reports why", func() {
		providerName := "shrink-provider-mr"
		claimName := namespace + "." + providerName
		consumer := demandContracts("shrink-consumer-mr", contractBackup)

		stored := storeClaim(claimName, providerName, contractStorage, contractBackup)
		storedBefore := stored.DeepCopy()

		createModuleInstance(providerName)
		nn := types.NamespacedName{Name: providerName, Namespace: namespace}

		rec := events.NewFakeRecorder(30)
		params := reconcileParams()
		params.EventRecorder = rec
		params.APIReader = k8sClient
		params.Renderer = &stubRenderer{result: providerRenderResult(
			claimName, providerName, "2.0.0", "upgraded", contractStorage,
		)}
		ensureFinalizer(params, nn)

		By("the reconcile refuses the shrink and asks to be retried")
		result, err := opmreconcile.ReconcileModuleInstance(ctx, params, ctrl.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(BeNumerically(">", 0))

		By("the stored claim is untouched, down to its resourceVersion")
		var after releasesv1alpha1.TransformerRegistration
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: claimName}, &after)).To(Succeed())
		Expect(after.Spec).To(Equal(storedBefore.Spec))
		Expect(after.ResourceVersion).To(Equal(storedBefore.ResourceVersion))

		By("the claim's own verdict is unchanged: acceptance owns it, not the refusal")
		Expect(after.Status.Accepted).To(BeTrue())
		Expect(after.Status.Active).To(BeTrue())
		Expect(after.Status.Conditions).To(Equal(storedBefore.Status.Conditions))

		By("every other rendered resource applied")
		var cm corev1.ConfigMap
		Expect(k8sClient.Get(ctx,
			types.NamespacedName{Name: providerName + "-cm", Namespace: namespace}, &cm)).To(Succeed())
		Expect(cm.Data).To(HaveKeyWithValue("payload", "upgraded"))

		By("the instance reports not ready, naming the claim, the contract and the count")
		var mi releasesv1alpha1.ModuleInstance
		Expect(k8sClient.Get(ctx, nn, &mi)).To(Succeed())
		ready := apimeta.FindStatusCondition(mi.Status.Conditions, status.ReadyCondition)
		Expect(ready).NotTo(BeNil())
		Expect(ready.Status).To(Equal(metav1.ConditionFalse))
		Expect(ready.Reason).To(Equal(status.DependentsRemainReason))
		Expect(ready.Message).To(ContainSubstring(claimName))
		Expect(ready.Message).To(ContainSubstring(contractBackup))
		Expect(ready.Message).To(ContainSubstring("1 module instance(s)"))
		Expect(ready.Message).To(ContainSubstring(consumer.Namespace + "/" + consumer.Name))
		Expect(ready.Message).NotTo(ContainSubstring(contractStorage))
		Expect(countBufferedEvents(rec, status.DependentsRemainReason)).To(Equal(1))

		By("the refusal is not mistaken for a converged instance")
		Expect(mi.Status.LastAppliedRenderDigest).To(BeEmpty())
		Expect(mi.Status.Inventory).To(BeNil())

		cleanupShrinkFixtures(claimName, providerName, nn, consumer)
	})

	// The upgrade must release without anyone touching the claim.
	It("applies the upgrade once the dependents stop demanding the contract", func() {
		providerName := "shrink-release-mr"
		claimName := namespace + "." + providerName
		consumer := demandContracts("shrink-release-consumer-mr", contractBackup)

		storeClaim(claimName, providerName, contractStorage, contractBackup)

		createModuleInstance(providerName)
		nn := types.NamespacedName{Name: providerName, Namespace: namespace}

		params := reconcileParams()
		params.EventRecorder = events.NewFakeRecorder(30)
		params.APIReader = k8sClient
		params.Renderer = &stubRenderer{result: providerRenderResult(
			claimName, providerName, "2.0.0", "upgraded", contractStorage,
		)}
		ensureFinalizer(params, nn)

		By("the first reconcile refuses")
		result, err := opmreconcile.ReconcileModuleInstance(ctx, params, ctrl.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(BeNumerically(">", 0))

		By("the dependent stops demanding the contract, with no action on the claim")
		var dependent releasesv1alpha1.ModuleInstance
		Expect(k8sClient.Get(ctx, consumer, &dependent)).To(Succeed())
		dependent.Status.RequiredContracts = nil
		Expect(k8sClient.Status().Update(ctx, &dependent)).To(Succeed())

		By("the next reconcile applies the upgrade and the instance converges")
		result, err = opmreconcile.ReconcileModuleInstance(ctx, params, ctrl.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(BeZero())

		var after releasesv1alpha1.TransformerRegistration
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: claimName}, &after)).To(Succeed())
		Expect(after.Spec.Provides).To(ConsistOf(contractStorage))
		Expect(after.Spec.Version).To(Equal("2.0.0"))

		var mi releasesv1alpha1.ModuleInstance
		Expect(k8sClient.Get(ctx, nn, &mi)).To(Succeed())
		ready := apimeta.FindStatusCondition(mi.Status.Conditions, status.ReadyCondition)
		Expect(ready).NotTo(BeNil())
		Expect(ready.Status).To(Equal(metav1.ConditionTrue))
		Expect(mi.Status.Inventory).NotTo(BeNil())
		Expect(mi.Status.Inventory.Count).To(Equal(int64(2)))

		cleanupShrinkFixtures(claimName, providerName, nn, consumer)
	})

	// A provider upgrade that keeps its contract set is the common case and
	// must pass through untouched.
	It("applies an upgrade that keeps every demanded contract", func() {
		providerName := "shrink-nochange-mr"
		claimName := namespace + "." + providerName
		consumer := demandContracts("shrink-nochange-consumer-mr", contractBackup)

		storeClaim(claimName, providerName, contractStorage, contractBackup)

		createModuleInstance(providerName)
		nn := types.NamespacedName{Name: providerName, Namespace: namespace}

		params := reconcileParams()
		params.EventRecorder = events.NewFakeRecorder(30)
		params.APIReader = k8sClient
		params.Renderer = &stubRenderer{result: providerRenderResult(
			claimName, providerName, "2.0.0", "upgraded", contractStorage, contractBackup,
		)}
		ensureFinalizer(params, nn)

		result, err := opmreconcile.ReconcileModuleInstance(ctx, params, ctrl.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(BeZero())

		var after releasesv1alpha1.TransformerRegistration
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: claimName}, &after)).To(Succeed())
		Expect(after.Spec.Version).To(Equal("2.0.0"))
		Expect(after.Spec.Provides).To(ConsistOf(contractStorage, contractBackup))

		var mi releasesv1alpha1.ModuleInstance
		Expect(k8sClient.Get(ctx, nn, &mi)).To(Succeed())
		ready := apimeta.FindStatusCondition(mi.Status.Conditions, status.ReadyCondition)
		Expect(ready).NotTo(BeNil())
		Expect(ready.Status).To(Equal(metav1.ConditionTrue))

		cleanupShrinkFixtures(claimName, providerName, nn, consumer)
	})
})

func cleanupShrinkFixtures(claimName, providerName string, provider, consumer types.NamespacedName) {
	Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: providerName + "-cm", Namespace: namespace},
	}))).To(Succeed())
	Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &releasesv1alpha1.TransformerRegistration{
		ObjectMeta: metav1.ObjectMeta{Name: claimName},
	}))).To(Succeed())
	Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &releasesv1alpha1.ModuleInstance{
		ObjectMeta: metav1.ObjectMeta{Name: consumer.Name, Namespace: consumer.Namespace},
	}))).To(Succeed())
	cleanupInstance(provider)
}
