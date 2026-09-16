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

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	platformstore "github.com/open-platform-model/opm-operator/internal/platform"
	"github.com/open-platform-model/opm-operator/internal/status"
)

// dependentDemanding creates a ModuleInstance in the namespace whose status
// declares the given contract demand — a dependent of any claim providing one
// of them. It is a separate object from the provider instance the claim's
// providerRef names.
func dependentDemanding(ctx context.Context, namespace, name string, contracts ...string) *releasesv1alpha1.ModuleInstance {
	GinkgoHelper()
	instance := &releasesv1alpha1.ModuleInstance{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: releasesv1alpha1.ModuleInstanceSpec{
			Module: releasesv1alpha1.ModuleReference{
				Path:    "opmodel.dev/modules/consumer",
				Version: "1.0.0",
			},
		},
	}
	Expect(k8sClient.Create(ctx, instance)).To(Succeed())

	instance.Status.RequiredContracts = contracts
	Expect(k8sClient.Status().Update(ctx, instance)).To(Succeed())
	return instance
}

// claimExists reports whether the claim is still stored. A blocked deletion
// is exactly the case where the object is gone from the operator's intent and
// still present here.
func claimExists(ctx context.Context, name string) bool {
	GinkgoHelper()
	var claim releasesv1alpha1.TransformerRegistration
	err := k8sClient.Get(ctx, types.NamespacedName{Name: name}, &claim)
	if apierrors.IsNotFound(err) {
		return false
	}
	Expect(err).NotTo(HaveOccurred())
	return true
}

var _ = Describe("TransformerRegistration removal guard: D3 — a claim with dependents cannot be deleted", func() {
	Context("the finalizer", func() {
		It("is on an accepted claim, so a delete is held rather than completed", func() {
			ctx := context.Background()
			ns := nextClaimNamespace()
			contract := claimContract(ns)

			claim := activatedClaim(ctx, ns, contract)
			Expect(controllerutil.ContainsFinalizer(&claim, ClaimFinalizerName)).To(BeTrue())
		})

		It("is absent from a refused claim, which nothing depends on", func() {
			ctx := context.Background()
			ns := nextClaimNamespace()
			claim := createClaimProviding(ctx, ns, claimContract(ns))
			ownProvidedInventory(ctx, ns, claim.Name)

			// A catalog implementing nothing: refused on D11, so the claim is
			// never accepted and never held.
			judged := judge(ctx, acceptanceReconciler(&stubCatalogs{cat: providerCatalog()}), claim.Name)

			Expect(judged.Status.Accepted).To(BeFalse())
			Expect(controllerutil.ContainsFinalizer(&judged, ClaimFinalizerName)).To(BeFalse())
		})
	})

	Context("deleting a claim", func() {
		It("blocks while an instance demands a contract it provides, naming the count", func() {
			ctx := context.Background()
			ns := nextClaimNamespace()
			contract := claimContract(ns)

			claim := activatedClaim(ctx, ns, contract)
			dependentDemanding(ctx, ns, "consumer", contract)

			Expect(k8sClient.Delete(ctx, &claim)).To(Succeed())

			r := acceptanceReconciler(&stubCatalogs{cat: providerCatalog(contract)})
			blocked := judge(ctx, r, claim.Name)

			Expect(blocked.DeletionTimestamp.IsZero()).To(BeFalse(), "the claim is marked for deletion")
			Expect(controllerutil.ContainsFinalizer(&blocked, ClaimFinalizerName)).To(BeTrue(),
				"the block is the finalizer still being held")

			ready := readyOf(blocked)
			Expect(ready.Status).To(Equal(metav1.ConditionFalse))
			Expect(ready.Reason).To(Equal(status.DependentsRemainReason))
			Expect(ready.Message).To(ContainSubstring("1 module instance"))
			Expect(ready.Message).To(ContainSubstring(ns + "/consumer"))

			Expect(blocked.Status.Accepted).To(BeTrue(),
				"a blocked claim has not failed acceptance")
			Expect(blocked.Status.Active).To(BeTrue(),
				"a blocked claim is still serving the dependents the block protects")
		})

		It("completes once the last dependent is gone", func() {
			ctx := context.Background()
			ns := nextClaimNamespace()
			contract := claimContract(ns)

			claim := activatedClaim(ctx, ns, contract)
			dependent := dependentDemanding(ctx, ns, "consumer", contract)

			Expect(k8sClient.Delete(ctx, &claim)).To(Succeed())

			r := acceptanceReconciler(&stubCatalogs{cat: providerCatalog(contract)})
			judge(ctx, r, claim.Name)
			Expect(claimExists(ctx, claim.Name)).To(BeTrue(), "still blocked")

			Expect(k8sClient.Delete(ctx, dependent)).To(Succeed())

			_, err := r.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: claim.Name},
			})
			Expect(err).NotTo(HaveOccurred())

			Expect(claimExists(ctx, claim.Name)).To(BeFalse(),
				"the block released with no operator action on the claim")
		})

		It("completes immediately when nothing depends on it", func() {
			ctx := context.Background()
			ns := nextClaimNamespace()
			contract := claimContract(ns)

			claim := activatedClaim(ctx, ns, contract)

			// An instance demanding some other contract is not a dependent.
			dependentDemanding(ctx, ns, "unrelated", "opmodel.dev/catalogs/opm/traits/scaling@v1beta1")

			Expect(k8sClient.Delete(ctx, &claim)).To(Succeed())

			r := acceptanceReconciler(&stubCatalogs{cat: providerCatalog(contract)})
			_, err := r.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: claim.Name},
			})
			Expect(err).NotTo(HaveOccurred())

			Expect(claimExists(ctx, claim.Name)).To(BeFalse())
		})

		It("reports the same count and verdict when re-evaluated unchanged", func() {
			ctx := context.Background()
			ns := nextClaimNamespace()
			contract := claimContract(ns)

			claim := activatedClaim(ctx, ns, contract)
			dependentDemanding(ctx, ns, "consumer-a", contract)
			dependentDemanding(ctx, ns, "consumer-b", contract)

			Expect(k8sClient.Delete(ctx, &claim)).To(Succeed())

			r := acceptanceReconciler(&stubCatalogs{cat: providerCatalog(contract)})
			first := readyOf(judge(ctx, r, claim.Name))
			second := readyOf(judge(ctx, r, claim.Name))

			Expect(first.Message).To(ContainSubstring("2 module instance"))
			Expect(second.Message).To(Equal(first.Message), "the verdict does not move between reconciles")
			Expect(second.Reason).To(Equal(first.Reason))
		})
	})

	Context("the watches", func() {
		It("wakes the claim when it acquires a deletion timestamp", func() {
			before := &releasesv1alpha1.TransformerRegistration{
				ObjectMeta: metav1.ObjectMeta{Name: "ns.k8up", Generation: 3},
			}
			deleting := before.DeepCopy()
			now := metav1.Now()
			deleting.DeletionTimestamp = &now

			// Stamping a deletion timestamp does not bump generation, so a
			// plain generation filter would swallow the one event the guard
			// has to see.
			Expect(claimSpecOrDeletionChanged().Update(event.UpdateEvent{
				ObjectOld: before, ObjectNew: deleting,
			})).To(BeTrue())
		})

		It("does not wake the claim for an unrelated metadata write", func() {
			before := &releasesv1alpha1.TransformerRegistration{
				ObjectMeta: metav1.ObjectMeta{Name: "ns.k8up", Generation: 3},
			}
			annotated := before.DeepCopy()
			annotated.Annotations = map[string]string{"note": "hello"}

			Expect(claimSpecOrDeletionChanged().Update(event.UpdateEvent{
				ObjectOld: before, ObjectNew: annotated,
			})).To(BeFalse())
		})

		It("enqueues a claim whose contracts a changed instance demands", func() {
			ctx := context.Background()
			ns := nextClaimNamespace()
			contract := claimContract(ns)

			claim := createClaimProviding(ctx, ns, contract)
			ownProvidedInventory(ctx, ns, claim.Name)
			dependent := dependentDemanding(ctx, ns, "consumer", contract)

			r := acceptanceReconciler(&stubCatalogs{cat: providerCatalog(contract)})
			requests := r.mapInstanceToRegistrations(ctx, dependent)

			// The dependent is not the claim's providerRef, so the only way
			// it reaches the claim is through the demand relation.
			names := make([]string, 0, len(requests))
			for _, req := range requests {
				names = append(names, req.Name)
			}
			Expect(names).To(ContainElement(claim.Name))
		})

		It("passes an instance update that only changes the demand", func() {
			before := &releasesv1alpha1.ModuleInstance{}
			after := before.DeepCopy()
			after.Status.RequiredContracts = []string{"opmodel.dev/catalogs/opm/traits/backup@v1alpha1"}

			Expect(providerFactsChanged().Update(event.UpdateEvent{
				ObjectOld: before, ObjectNew: after,
			})).To(BeTrue())
		})
	})

	Context("the platform's active set", func() {
		It("keeps a blocked claim's catalog, so its dependents keep rendering", func() {
			ctx := context.Background()
			ns := nextClaimNamespace()
			contract := claimContract(ns)

			claim := activatedClaim(ctx, ns, contract)
			dependentDemanding(ctx, ns, "consumer", contract)
			Expect(k8sClient.Delete(ctx, &claim)).To(Succeed())

			r := acceptanceReconciler(&stubCatalogs{cat: providerCatalog(contract)})
			blocked := judge(ctx, r, claim.Name)

			Expect(claimContributesAfterDeletion(&blocked)).To(BeTrue())
			_, contributes := claimContribution(&blocked)
			Expect(contributes).To(BeTrue(),
				"a blocked claim still supplies the catalog its dependents render against")

			// The predicate above only decides whether to WAKE a reconcile.
			// activeClaims is the function that decides what the generated
			// package is built from, so the guarantee is asserted there and
			// on the entries it produces.
			p := newPlatformReconciler(platformstore.NewStore(), nil, "")
			active, err := p.activeClaims(ctx)
			Expect(err).NotTo(HaveOccurred())

			names := make([]string, 0, len(active))
			for i := range active {
				names = append(names, active[i].Name)
			}
			Expect(names).To(ContainElement(claim.Name),
				"the blocked claim is still an input to platform generation")

			entries, err := platformEntries(&releasesv1alpha1.Platform{}, active)
			Expect(err).NotTo(HaveOccurred())
			paths := make([]string, 0, len(entries))
			for _, e := range entries {
				paths = append(paths, e.Path)
			}
			Expect(paths).To(ContainElement(claim.Spec.Catalog),
				"the regenerated package still carries the blocked claim's catalog")
		})

		It("drops a terminating claim from activeClaims once the guard released it", func() {
			ctx := context.Background()
			ns := nextClaimNamespace()
			contract := claimContract(ns)

			claim := activatedClaim(ctx, ns, contract)
			// No dependents, so the first deletion reconcile releases it and
			// the object goes. What must not survive is its contribution.
			Expect(k8sClient.Delete(ctx, &claim)).To(Succeed())
			r := acceptanceReconciler(&stubCatalogs{cat: providerCatalog(contract)})
			_, err := r.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: claim.Name},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(claimExists(ctx, claim.Name)).To(BeFalse())

			p := newPlatformReconciler(platformstore.NewStore(), nil, "")
			active, err := p.activeClaims(ctx)
			Expect(err).NotTo(HaveOccurred())
			for i := range active {
				Expect(active[i].Name).NotTo(Equal(claim.Name))
			}
		})

		It("still holds its provider catalog and its contracts while blocked", func() {
			ctx := context.Background()
			ns := nextClaimNamespace()
			contract := claimContract(ns)

			// A blocked claim is in the generated platform, so a second
			// provider arriving now would over-subscribe the contract and
			// refuse generation cluster-wide. Both competing-claim reads
			// therefore treat it as still holding — see design.md for what
			// that costs a provider migration.
			held := activatedClaim(ctx, ns, contract)
			dependentDemanding(ctx, ns, "consumer", contract)
			Expect(k8sClient.Delete(ctx, &held)).To(Succeed())

			r := acceptanceReconciler(&stubCatalogs{cat: providerCatalog(contract)})
			judge(ctx, r, held.Name)

			// D12: another claim for the same catalog.
			rival := nextClaimNamespace()
			sameCatalog := createClaimListing(ctx, rival, held.Spec.Catalog, contract)
			ownProvidedInventory(ctx, rival, sameCatalog.Name)
			refused := judge(ctx, acceptanceReconciler(
				&stubCatalogs{cat: providerCatalog(contract)}), sameCatalog.Name)

			Expect(refused.Status.Accepted).To(BeFalse())
			Expect(readyOf(refused).Reason).To(Equal(status.DuplicateClaimReason))
			Expect(readyOf(refused).Message).To(ContainSubstring(held.Name),
				"the refusal names the blocked claim that still holds the provider")
		})

		It("still holds its contracts against a different catalog while blocked", func() {
			ctx := context.Background()
			ns := nextClaimNamespace()
			contract := claimContract(ns)

			held := activatedClaim(ctx, ns, contract)
			dependentDemanding(ctx, ns, "consumer", contract)
			Expect(k8sClient.Delete(ctx, &held)).To(Succeed())
			judge(ctx, acceptanceReconciler(
				&stubCatalogs{cat: providerCatalog(contract)}), held.Name)

			// D2: a DIFFERENT catalog offering the same contract.
			rival := nextClaimNamespace()
			other := createClaimProviding(ctx, rival, contract)
			ownProvidedInventory(ctx, rival, other.Name)
			refused := judge(ctx, acceptanceReconciler(
				&stubCatalogs{cat: providerCatalog(contract)}), other.Name)

			Expect(refused.Status.Accepted).To(BeFalse())
			Expect(readyOf(refused).Reason).To(Equal(status.ContractClaimedReason))
			Expect(readyOf(refused).Message).To(ContainSubstring(held.Name))
		})

		It("drops the claim once the guard has released it", func() {
			ns := "released-ns"
			released := &releasesv1alpha1.TransformerRegistration{
				ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("%s.%s", ns, providerInstanceName)},
				Spec: releasesv1alpha1.TransformerRegistrationSpec{
					Catalog: claimCatalogFor(ns), Version: "1.0.0",
				},
				Status: releasesv1alpha1.TransformerRegistrationStatus{Accepted: true, Active: true},
			}
			now := metav1.Now()
			released.DeletionTimestamp = &now

			// No finalizer: the guard has let go, so nothing depends on it.
			_, contributes := claimContribution(released)
			Expect(contributes).To(BeFalse())
		})
	})
})
