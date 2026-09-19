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
	"slices"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/event"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	"github.com/open-platform-model/opm-operator/internal/status"
)

// setProviderReadiness sets the Ready condition of the provider instance in
// the given namespace, which is what the activation gate reads. The reason
// varies with the status, so a transition back to ready is a real condition
// change rather than one the flux setter would collapse.
func setProviderReadiness(ctx context.Context, namespace string, ready bool) {
	var instance releasesv1alpha1.ModuleInstance
	Expect(k8sClient.Get(ctx, types.NamespacedName{
		Namespace: namespace, Name: providerInstanceName,
	}, &instance)).To(Succeed())

	condition := metav1.Condition{
		Type:               status.ReadyCondition,
		Status:             metav1.ConditionTrue,
		Reason:             status.ReconciliationSucceededReason,
		Message:            "Reconciliation succeeded",
		ObservedGeneration: instance.Generation,
	}
	if !ready {
		condition.Status = metav1.ConditionFalse
		condition.Reason = status.ApplyFailedReason
		condition.Message = "Apply failed"
	}
	apimeta.SetStatusCondition(&instance.Status.Conditions, condition)
	Expect(k8sClient.Status().Update(ctx, &instance)).To(Succeed())
}

// activeOf returns the claim's Active condition, which carries the activation
// transition. It is absent until the claim has been through acceptance.
func activeOf(claim releasesv1alpha1.TransformerRegistration) *metav1.Condition {
	active := apimeta.FindStatusCondition(claim.Status.Conditions, status.ActiveCondition)
	Expect(active).NotTo(BeNil())
	return active
}

// activatedClaim leaves an accepted, ACTIVE claim in the namespace, providing
// the given contracts, and returns it as stored.
func activatedClaim(
	ctx context.Context,
	namespace string,
	provides ...string,
) releasesv1alpha1.TransformerRegistration {
	claim := createClaimProviding(ctx, namespace, provides...)
	ownProvidedInventory(ctx, namespace, claim.Name)
	setProviderReadiness(ctx, namespace, true)

	activated := judge(ctx, acceptanceReconciler(&stubCatalogs{cat: providerCatalog(provides...)}), claim.Name)
	Expect(activated.Status.Active).To(BeTrue())
	return activated
}

var _ = Describe("TransformerRegistration activation: D3 — the readiness gate", func() {
	Context("the ModuleInstance watch", func() {
		It("enqueues only the claims whose providerRef names the changed instance", func() {
			ctx := context.Background()
			ns := nextClaimNamespace()
			claim := createClaimProviding(ctx, ns, claimContract(ns))
			ownProvidedInventory(ctx, ns, claim.Name)

			var provider releasesv1alpha1.ModuleInstance
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Namespace: ns, Name: providerInstanceName,
			}, &provider)).To(Succeed())

			r := acceptanceReconciler(&stubCatalogs{cat: providerCatalog(claimContract(ns))})
			requests := r.mapInstanceToRegistrations(ctx, &provider)

			Expect(requests).To(HaveLen(1))
			Expect(requests[0].Name).To(Equal(claim.Name))
		})

		It("enqueues no claim for an instance no claim names", func() {
			ctx := context.Background()

			// The suite's other specs have left claims all over the cluster,
			// so a list-all-and-enqueue map func would return every one here.
			unrelated := &releasesv1alpha1.ModuleInstance{
				ObjectMeta: metav1.ObjectMeta{Name: "not-a-provider", Namespace: "default"},
			}

			r := acceptanceReconciler(&stubCatalogs{cat: providerCatalog()})
			Expect(r.mapInstanceToRegistrations(ctx, unrelated)).To(BeEmpty())
		})
	})

	Context("an accepted claim", func() {
		It("stays inactive while its provider is not ready, saying what it waits on", func() {
			ctx := context.Background()
			ns := nextClaimNamespace()
			contract := claimContract(ns)
			claim := createClaimProviding(ctx, ns, contract)
			ownProvidedInventory(ctx, ns, claim.Name)

			r := acceptanceReconciler(&stubCatalogs{cat: providerCatalog(contract)})
			judged := judge(ctx, r, claim.Name)

			Expect(judged.Status.Accepted).To(BeTrue())
			Expect(judged.Status.Active).To(BeFalse())
			Expect(readyOf(judged).Reason).To(Equal(status.AcceptedReason),
				"waiting on the provider is not a refusal of the claim")

			active := activeOf(judged)
			Expect(active.Status).To(Equal(metav1.ConditionFalse))
			Expect(active.Reason).To(Equal(status.ProviderNotReadyReason))
			Expect(active.Message).To(ContainSubstring(ns + "/" + providerInstanceName))
		})

		It("activates when its provider becomes ready, with no change to its own spec", func() {
			ctx := context.Background()
			ns := nextClaimNamespace()
			contract := claimContract(ns)
			claim := createClaimProviding(ctx, ns, contract)
			ownProvidedInventory(ctx, ns, claim.Name)

			r := acceptanceReconciler(&stubCatalogs{cat: providerCatalog(contract)})

			inactive := judge(ctx, r, claim.Name)
			Expect(inactive.Status.Active).To(BeFalse())
			generation := inactive.Generation

			setProviderReadiness(ctx, ns, true)
			activated := judge(ctx, r, claim.Name)

			Expect(activated.Status.Active).To(BeTrue())
			Expect(activated.Generation).To(Equal(generation),
				"the claim activated without being edited")

			active := activeOf(activated)
			Expect(active.Status).To(Equal(metav1.ConditionTrue))
			Expect(active.Reason).To(Equal(status.ProviderReadyReason))
		})
	})

	Context("a claim that is not accepted", func() {
		It("stays inactive even though its provider is ready", func() {
			ctx := context.Background()
			ns := nextClaimNamespace()
			claim := createClaimProviding(ctx, ns, claimContract(ns))
			ownProvidedInventory(ctx, ns, claim.Name)
			setProviderReadiness(ctx, ns, true)

			// A catalog implementing nothing: the claim is refused on 0015:D11,
			// long before the gate.
			r := acceptanceReconciler(&stubCatalogs{cat: providerCatalog()})
			judged := judge(ctx, r, claim.Name)

			Expect(judged.Status.Accepted).To(BeFalse())
			Expect(judged.Status.Active).To(BeFalse())
			Expect(readyOf(judged).Reason).To(Equal(status.ProvidesMismatchReason))
			Expect(apimeta.FindStatusCondition(judged.Status.Conditions, status.ActiveCondition)).To(BeNil(),
				"a refused claim never reaches the gate, so it records no activation state")
		})
	})

	Context("the latch", func() {
		It("keeps an active claim active through a provider outage and recovery", func() {
			ctx := context.Background()
			ns := nextClaimNamespace()
			contract := claimContract(ns)
			claim := createClaimProviding(ctx, ns, contract)
			ownProvidedInventory(ctx, ns, claim.Name)
			setProviderReadiness(ctx, ns, true)

			r := acceptanceReconciler(&stubCatalogs{cat: providerCatalog(contract)})

			activated := judge(ctx, r, claim.Name)
			Expect(activated.Status.Active).To(BeTrue())
			transition := activeOf(activated).LastTransitionTime

			// The provider goes away. Its CRDs did not.
			setProviderReadiness(ctx, ns, false)
			duringOutage := judge(ctx, r, claim.Name)
			Expect(duringOutage.Status.Active).To(BeTrue(),
				"a flapping provider must not toggle the active-claim set")
			Expect(activeOf(duringOutage).Reason).To(Equal(status.ProviderReadyReason))
			Expect(activeOf(duringOutage).LastTransitionTime).To(Equal(transition))

			// It recovers. The gate does not run again, so nothing transitions.
			setProviderReadiness(ctx, ns, true)
			afterRecovery := judge(ctx, r, claim.Name)
			Expect(afterRecovery.Status.Active).To(BeTrue())
			Expect(activeOf(afterRecovery).LastTransitionTime).To(Equal(transition),
				"the claim never left the active state, so it never re-entered it")
		})
	})
})

// clusterRevisions snapshots the resourceVersion of every object this
// controller could conceivably write, keyed by kind and name. No manager runs
// in this suite, so nothing moves between two snapshots except what the
// reconcile under test wrote.
func clusterRevisions(ctx context.Context) map[string]string {
	revisions := map[string]string{}

	var claims releasesv1alpha1.TransformerRegistrationList
	Expect(k8sClient.List(ctx, &claims)).To(Succeed())
	for i := range claims.Items {
		revisions["TransformerRegistration/"+claims.Items[i].Name] = claims.Items[i].ResourceVersion
	}

	var instances releasesv1alpha1.ModuleInstanceList
	Expect(k8sClient.List(ctx, &instances)).To(Succeed())
	for i := range instances.Items {
		revisions["ModuleInstance/"+instances.Items[i].Namespace+"/"+instances.Items[i].Name] =
			instances.Items[i].ResourceVersion
	}

	var packages releasesv1alpha1.ModulePackageList
	Expect(k8sClient.List(ctx, &packages)).To(Succeed())
	for i := range packages.Items {
		revisions["ModulePackage/"+packages.Items[i].Namespace+"/"+packages.Items[i].Name] =
			packages.Items[i].ResourceVersion
	}

	var platforms releasesv1alpha1.PlatformList
	Expect(k8sClient.List(ctx, &platforms)).To(Succeed())
	for i := range platforms.Items {
		revisions["Platform/"+platforms.Items[i].Name] = platforms.Items[i].ResourceVersion
	}

	return revisions
}

// changedSince returns the keys whose resourceVersion moved, appeared or
// vanished between two snapshots, sorted so a failure names them stably.
func changedSince(before, after map[string]string) []string {
	var changed []string
	for key, revision := range after {
		if before[key] != revision {
			changed = append(changed, key)
		}
	}
	for key := range before {
		if _, ok := after[key]; !ok {
			changed = append(changed, key+" (deleted)")
		}
	}
	slices.Sort(changed)
	return changed
}

// providerWith builds an unstored ModuleInstance carrying the given provider
// facts, for driving the watch predicate without an API server.
func providerWith(ready metav1.ConditionStatus, inventoryDigest string, history int) *releasesv1alpha1.ModuleInstance {
	instance := &releasesv1alpha1.ModuleInstance{
		ObjectMeta: metav1.ObjectMeta{Name: providerInstanceName, Namespace: "default"},
	}
	if ready != "" {
		apimeta.SetStatusCondition(&instance.Status.Conditions, metav1.Condition{
			Type:    status.ReadyCondition,
			Status:  ready,
			Reason:  status.ReconciliationSucceededReason,
			Message: "Reconciliation succeeded",
		})
	}
	instance.Status.Inventory = &releasesv1alpha1.Inventory{Digest: inventoryDigest}
	for i := range history {
		instance.Status.History = append(instance.Status.History,
			releasesv1alpha1.HistoryEntry{Sequence: int64(i + 1)})
	}
	return instance
}

var _ = Describe("TransformerRegistration activation: activation is inert", func() {
	It("writes nothing but the claim itself when the claim activates", func() {
		ctx := context.Background()
		ns := nextClaimNamespace()
		contract := claimContract(ns)
		claim := createClaimProviding(ctx, ns, contract)
		ownProvidedInventory(ctx, ns, claim.Name)
		setProviderReadiness(ctx, ns, true)

		r := acceptanceReconciler(&stubCatalogs{cat: providerCatalog(contract)})

		before := clusterRevisions(ctx)
		activated := judge(ctx, r, claim.Name)
		Expect(activated.Status.Active).To(BeTrue())

		Expect(changedSince(before, clusterRevisions(ctx))).To(
			ConsistOf("TransformerRegistration/"+claim.Name),
			"activation is reported and inert: no workload renders, no Platform field moves "+
				"and no other claim is written because a claim became active")
	})
})

var _ = Describe("TransformerRegistration activation: the provider watch predicate", func() {
	It("passes an update whose Ready condition moved", func() {
		before := providerWith(metav1.ConditionFalse, "sha256:aaa", 1)
		after := providerWith(metav1.ConditionTrue, "sha256:aaa", 1)

		Expect(providerFactsChanged().Update(event.UpdateEvent{
			ObjectOld: before, ObjectNew: after,
		})).To(BeTrue())
	})

	It("passes an update whose inventory digest moved", func() {
		before := providerWith(metav1.ConditionTrue, "sha256:aaa", 1)
		after := providerWith(metav1.ConditionTrue, "sha256:bbb", 1)

		Expect(providerFactsChanged().Update(event.UpdateEvent{
			ObjectOld: before, ObjectNew: after,
		})).To(BeTrue(), "the provider-identity check reads the inventory, so it can change a verdict")
	})

	It("drops an update that only records reconcile history", func() {
		before := providerWith(metav1.ConditionTrue, "sha256:aaa", 1)
		after := providerWith(metav1.ConditionTrue, "sha256:aaa", 2)

		Expect(providerFactsChanged().Update(event.UpdateEvent{
			ObjectOld: before, ObjectNew: after,
		})).To(BeFalse(), "re-judging costs a registry acquire, and history cannot change a verdict")
	})

	It("passes a create and a delete unfiltered", func() {
		instance := providerWith(metav1.ConditionTrue, "sha256:aaa", 1)

		Expect(providerFactsChanged().Create(event.CreateEvent{Object: instance})).To(BeTrue())
		Expect(providerFactsChanged().Delete(event.DeleteEvent{Object: instance})).To(BeTrue())
	})
})

var _ = Describe("TransformerRegistration activation: a refusal is not an activation state", func() {
	It("drops a stale waiting-on-provider condition when the claim is later refused", func() {
		ctx := context.Background()
		ns := nextClaimNamespace()
		contract := claimContract(ns)
		claim := createClaimProviding(ctx, ns, contract)
		ownProvidedInventory(ctx, ns, claim.Name)

		accepted := judge(ctx, acceptanceReconciler(&stubCatalogs{cat: providerCatalog(contract)}), claim.Name)
		Expect(accepted.Status.Accepted).To(BeTrue())
		Expect(activeOf(accepted).Reason).To(Equal(status.ProviderNotReadyReason))

		// The catalog stops implementing the contract, so the claim is refused
		// on 0015:D11. What it was waiting on is no longer why it is inactive.
		refused := judge(ctx, acceptanceReconciler(&stubCatalogs{cat: providerCatalog()}), claim.Name)
		Expect(refused.Status.Accepted).To(BeFalse())
		Expect(readyOf(refused).Reason).To(Equal(status.ProvidesMismatchReason))
		Expect(apimeta.FindStatusCondition(refused.Status.Conditions, status.ActiveCondition)).To(BeNil(),
			"a refused claim does not report that it is waiting on its provider")
	})

	It("keeps the Active condition on a claim refused while it is already active", func() {
		ctx := context.Background()
		ns := nextClaimNamespace()
		active := activatedClaim(ctx, ns, claimContract(ns))

		refused := judge(ctx, acceptanceReconciler(&stubCatalogs{cat: providerCatalog()}), active.Name)

		Expect(refused.Status.Accepted).To(BeFalse())
		Expect(refused.Status.Active).To(BeTrue(), "the latch says only deletion ends the active state")
		Expect(activeOf(refused).Reason).To(Equal(status.ProviderReadyReason))
	})
})
