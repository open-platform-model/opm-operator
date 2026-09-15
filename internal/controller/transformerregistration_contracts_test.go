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
	"maps"
	"slices"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"cuelang.org/go/cue/cuecontext"
	"github.com/open-platform-model/library/opm/platform"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	platformstore "github.com/open-platform-model/opm-operator/internal/platform"
	"github.com/open-platform-model/opm-operator/internal/status"
)

// platformProviding builds a built-platform value whose composed transformers
// are one per subscribed catalog, each requiring that catalog's contracts with
// provider fulfilment.
//
// #composedTransformers is the shape the D2 check folds, and it is core's fold
// over the ENABLED entries only, so a platform built here carries exactly the
// providers a real one would after the disabled entries dropped out. A nil map
// is a platform whose subscriptions provide nothing, which is what every spec
// that is not about D2 wants.
func platformProviding(byCatalog map[string][]string) *platform.Platform {
	var body strings.Builder
	body.WriteString(`kind: "Platform"
type: "kubernetes"
metadata: name: "cluster"
#composedTransformers: {
`)
	for _, catalogPath := range slices.Sorted(maps.Keys(byCatalog)) {
		fmt.Fprintf(&body, "\t%q: {\n\t\tmetadata: modulePath: %q\n\t\trequiredTraits: {\n",
			catalogPath+"/transformers/adapter@1.0.0", catalogPath)
		for _, contract := range byCatalog[catalogPath] {
			fmt.Fprintf(&body, "\t\t\t%q: fulfilment: \"provider\"\n", contract)
		}
		body.WriteString("\t\t}\n\t}\n")
	}
	body.WriteString("}\n")

	v := cuecontext.New().CompileString(body.String())
	Expect(v.Err()).NotTo(HaveOccurred())
	p, err := platform.NewPlatformFromValue(v)
	Expect(err).NotTo(HaveOccurred())
	return p
}

// contractReconciler returns a reconciler whose built platform's enabled
// subscriptions provide the given contracts.
func contractReconciler(
	catalogs CatalogAcquirer,
	byCatalog map[string][]string,
) *TransformerRegistrationReconciler {
	r := acceptanceReconciler(catalogs)
	store := platformstore.NewStore()
	store.SetGenerated(platformstore.Generated{
		Generation: 1,
		Dir:        platformDirWith(map[string]string{"opmodel.dev/core@v2": "v2.0.0"}),
		Platform:   platformProviding(byCatalog),
	})
	r.Store = store
	return r
}

var _ = Describe("TransformerRegistration acceptance: D2 — one provider per contract", func() {
	Context("against an enabled subscription", func() {
		It("refuses the claim, naming the contract and the subscribed catalog", func() {
			ctx := context.Background()
			ns := nextClaimNamespace()
			contract := claimContract(ns)
			claim := createClaimProviding(ctx, ns, contract)
			ownProvidedInventory(ctx, ns, claim.Name)

			const subscribed = "opmodel.dev/catalogs/velero@v2"
			r := contractReconciler(
				&stubCatalogs{cat: providerCatalog(contract)},
				map[string][]string{subscribed: {contract}},
			)
			judged := judge(ctx, r, claim.Name)

			Expect(judged.Status.Accepted).To(BeFalse())
			ready := readyOf(judged)
			Expect(ready.Status).To(Equal(metav1.ConditionFalse))
			Expect(ready.Reason).To(Equal(status.ContractSubscribedReason))
			Expect(ready.Message).To(ContainSubstring(contract), "the contract is named")
			Expect(ready.Message).To(ContainSubstring(subscribed), "the subscribed catalog is named")
		})

		It("accepts a claim whose contract no subscription provides", func() {
			ctx := context.Background()
			ns := nextClaimNamespace()
			contract := claimContract(ns)
			claim := createClaimProviding(ctx, ns, contract)
			ownProvidedInventory(ctx, ns, claim.Name)

			// The platform subscribes to a catalog that provides something
			// else entirely.
			r := contractReconciler(
				&stubCatalogs{cat: providerCatalog(contract)},
				map[string][]string{"opmodel.dev/catalogs/velero@v2": {restoreTrait}},
			)
			judged := judge(ctx, r, claim.Name)

			Expect(judged.Status.Accepted).To(BeTrue())
			Expect(readyOf(judged).Reason).To(Equal(status.AcceptedReason))
		})

		It("requeues, not refuses, while no platform has been built", func() {
			ctx := context.Background()
			ns := nextClaimNamespace()
			contract := claimContract(ns)
			claim := createClaimProviding(ctx, ns, contract)
			ownProvidedInventory(ctx, ns, claim.Name)

			// A generated module on disk with nothing built from it: the
			// claim cannot be judged against contracts that do not exist yet.
			r := acceptanceReconciler(&stubCatalogs{cat: providerCatalog(contract)})
			store := platformstore.NewStore()
			store.SetGenerated(platformstore.Generated{
				Generation: 1,
				Dir:        platformDirWith(map[string]string{"opmodel.dev/core@v2": "v2.0.0"}),
			})
			r.Store = store

			result, err := r.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: claim.Name},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).NotTo(BeZero(), "the claim is retried, not abandoned")

			var judged releasesv1alpha1.TransformerRegistration
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: claim.Name}, &judged)).To(Succeed())

			Expect(judged.Status.Accepted).To(BeFalse())
			ready := readyOf(judged)
			Expect(ready.Status).To(Equal(metav1.ConditionUnknown),
				"a claim judged before the platform is built is not refused")
			Expect(ready.Reason).To(Equal(status.PlatformNotReadyReason))
		})
	})

	Context("against another claim", func() {
		It("refuses a claim whose contract an active claim already provides", func() {
			ctx := context.Background()
			contested := claimContract(nextClaimNamespace())

			holderNS := nextClaimNamespace()
			activatedClaim(ctx, holderNS, contested)

			// A second provider, from a catalog of its own, for the same
			// contract: not a D12 duplicate, and refused all the same.
			ns := nextClaimNamespace()
			claim := createClaimProviding(ctx, ns, contested)
			ownProvidedInventory(ctx, ns, claim.Name)

			r := acceptanceReconciler(&stubCatalogs{cat: providerCatalog(contested)})
			judged := judge(ctx, r, claim.Name)

			Expect(judged.Status.Accepted).To(BeFalse())
			Expect(readyOf(judged).Reason).To(Equal(status.ContractClaimedReason))
			Expect(readyOf(judged).Reason).NotTo(Equal(status.DuplicateClaimReason),
				"two catalogs providing one contract is not two claims on one catalog")
		})

		It("names the holder, so an operator can see which provider to remove", func() {
			ctx := context.Background()
			contested := claimContract(nextClaimNamespace())

			holderNS := nextClaimNamespace()
			holder := activatedClaim(ctx, holderNS, contested)

			ns := nextClaimNamespace()
			claim := createClaimProviding(ctx, ns, contested)
			ownProvidedInventory(ctx, ns, claim.Name)

			r := acceptanceReconciler(&stubCatalogs{cat: providerCatalog(contested)})
			ready := readyOf(judge(ctx, r, claim.Name))

			Expect(ready.Message).To(ContainSubstring(contested), "the contract is named")
			Expect(ready.Message).To(ContainSubstring(holder.Name), "the holding claim is named")
		})

		It("does not refuse against an accepted but inactive claim", func() {
			ctx := context.Background()
			contested := claimContract(nextClaimNamespace())

			// Accepted, never activated: its provider never reported Ready.
			inactiveNS := nextClaimNamespace()
			inactive := createClaimProviding(ctx, inactiveNS, contested)
			ownProvidedInventory(ctx, inactiveNS, inactive.Name)
			judgedInactive := judge(ctx,
				acceptanceReconciler(&stubCatalogs{cat: providerCatalog(contested)}), inactive.Name)
			Expect(judgedInactive.Status.Accepted).To(BeTrue())
			Expect(judgedInactive.Status.Active).To(BeFalse())

			ns := nextClaimNamespace()
			claim := createClaimProviding(ctx, ns, contested)
			ownProvidedInventory(ctx, ns, claim.Name)

			r := acceptanceReconciler(&stubCatalogs{cat: providerCatalog(contested)})
			judged := judge(ctx, r, claim.Name)

			Expect(judged.Status.Accepted).To(BeTrue(),
				"a claim that has never served has no dependents to protect")
			Expect(readyOf(judged).Reason).To(Equal(status.AcceptedReason))
		})
	})
})
