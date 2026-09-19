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

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	"github.com/open-platform-model/opm-operator/internal/status"
)

// Acceptance and regeneration form a loop: claims feed the platform, and the
// platform feeds the already-provided check a claim is judged against
// (regeneration and acceptance form a loop that must not
// oscillate). These specs pin the two properties that keep it from
// oscillating — arbitration that does not depend on event order, and a check
// that cannot refuse a claim against itself.

// burstCounter names each burst's shared catalog apart, since
// TransformerRegistration is cluster-scoped and specs outlive each other.
var burstCounter int

// burstCatalog is a provider catalog path unique to one burst, so two claims
// in that burst contend with each other and with nothing else in the suite.
func burstCatalog() string {
	burstCounter++
	return fmt.Sprintf("opmodel.dev/catalogs/burst-%03d@v1", burstCounter)
}

// storeCompetingClaims stores two claims for one catalog, each providing the
// same contract, with their providers serving. They are created in order, so
// the first is the older: creationTimestamp has one-second granularity, which
// usually ties, and the name tie-break then follows the zero-padded namespace
// counter, so "older" is unambiguous either way.
func storeCompetingClaims(ctx context.Context, catalogPath, contract string) (older, younger string) {
	GinkgoHelper()
	for _, name := range []*string{&older, &younger} {
		ns := nextClaimNamespace()
		claim := createClaimListing(ctx, ns, catalogPath, contract)
		ownProvidedInventory(ctx, ns, claim.Name)
		setProviderReadiness(ctx, ns, true)
		*name = claim.Name
	}
	return older, younger
}

// judgeAll judges the named claims in the order given and returns the stored
// claims by name, so a spec can assert the outcome whatever order it drove.
func judgeAll(ctx context.Context, contract string, names ...string) map[string]releasesv1alpha1.TransformerRegistration {
	GinkgoHelper()
	out := make(map[string]releasesv1alpha1.TransformerRegistration, len(names))
	for _, name := range names {
		r := acceptanceReconciler(&stubCatalogs{cat: providerCatalog(contract)})
		out[name] = judge(ctx, r, name)
	}
	return out
}

var _ = Describe("TransformerRegistration acceptance: the regeneration loop", func() {
	Context("two claims activating in the same burst", func() {
		It("settles on the earliest-created claim whichever order the burst is judged in", func() {
			ctx := context.Background()

			// Repeat the burst, driving the two possible event orders, and in
			// both directions more than once: the outcome must be the same
			// every time rather than a function of who woke the reconciler.
			for _, youngerFirst := range []bool{false, true, true, false} {
				catalogPath := burstCatalog()
				contract := claimContract(nextClaimNamespace())
				older, younger := storeCompetingClaims(ctx, catalogPath, contract)

				order := []string{older, younger}
				if youngerFirst {
					order = []string{younger, older}
				}
				judged := judgeAll(ctx, contract, order...)

				Expect(judged[older].Status.Accepted).To(BeTrue(),
					"the earliest-created claim holds the catalog, judged %v first", order[0])
				Expect(judged[older].Status.Active).To(BeTrue(),
					"its provider is serving, so it activates")
				Expect(judged[younger].Status.Accepted).To(BeFalse())
				Expect(readyOf(judged[younger]).Reason).To(Equal(status.DuplicateClaimReason))
				Expect(readyOf(judged[younger]).Message).To(ContainSubstring(older),
					"the refusal names the holder, not whichever claim was judged first")
			}
		})

		It("converges when two catalogs contend for one contract, however often they are re-judged", func() {
			ctx := context.Background()
			contested := claimContract(nextClaimNamespace())

			firstNS := nextClaimNamespace()
			first := createClaimProviding(ctx, firstNS, contested)
			ownProvidedInventory(ctx, firstNS, first.Name)
			setProviderReadiness(ctx, firstNS, true)

			// A second provider, from a catalog of its own: not a 0015:D12
			// duplicate, and a contract still has exactly one provider.
			secondNS := nextClaimNamespace()
			second := createClaimProviding(ctx, secondNS, contested)
			ownProvidedInventory(ctx, secondNS, second.Name)
			setProviderReadiness(ctx, secondNS, true)

			judged := judgeAll(ctx, contested, first.Name, second.Name)
			Expect(judged[first.Name].Status.Active).To(BeTrue())
			Expect(judged[second.Name].Status.Accepted).To(BeFalse())
			Expect(readyOf(judged[second.Name]).Reason).To(Equal(status.ContractClaimedReason))

			// Every later regeneration re-judges both. Only an ACTIVE claim
			// holds a contract, so the holder keeps holding and the refused
			// claim stays refused: the pair settles instead of trading the
			// contract back and forth.
			for range 3 {
				again := judgeAll(ctx, contested, second.Name, first.Name)
				Expect(again[first.Name].Status.Active).To(BeTrue(),
					"the holder is not displaced by a re-judge")
				Expect(again[second.Name].Status.Accepted).To(BeFalse())
				Expect(readyOf(again[second.Name]).Reason).To(Equal(status.ContractClaimedReason))
			}
		})
	})

	Context("an active claim re-judged after its catalog reached the platform", func() {
		It("stays accepted: a claim's own contracts are not already-provided", func() {
			ctx := context.Background()
			ns := nextClaimNamespace()
			contract := claimContract(ns)
			active := activatedClaim(ctx, ns, contract)
			Expect(active.Status.Active).To(BeTrue())

			// Regeneration has folded the active claim's catalog into the
			// platform, so the built platform now provides the contract — from
			// the very catalog being judged. Counting that as already-provided
			// would refuse the claim, deactivate it, drop its catalog from the
			// next package and accept it again: the oscillation this closes.
			r := contractReconciler(
				&stubCatalogs{cat: providerCatalog(contract)},
				map[string][]string{claimCatalogFor(ns): {contract}},
			)
			rejudged := judge(ctx, r, active.Name)

			Expect(rejudged.Status.Accepted).To(BeTrue(),
				"a claim cannot be refused against the registry entry it put there")
			Expect(readyOf(rejudged).Reason).To(Equal(status.AcceptedReason))
			Expect(rejudged.Status.Active).To(BeTrue())

			// Judging it once more against the same platform is equally
			// stable, so nothing flips on the next regeneration either.
			Expect(judge(ctx, r, active.Name).Status.Accepted).To(BeTrue())
		})

		It("still refuses a claim whose contract another subscribed catalog provides", func() {
			ctx := context.Background()
			ns := nextClaimNamespace()
			contract := claimContract(ns)
			claim := createClaimProviding(ctx, ns, contract)
			ownProvidedInventory(ctx, ns, claim.Name)

			// The claim's own catalog is in the registry beside a different
			// catalog providing the same contract. Excusing the claim's own
			// entry must not excuse the other one.
			const subscribed = "opmodel.dev/catalogs/velero@v2"
			r := contractReconciler(
				&stubCatalogs{cat: providerCatalog(contract)},
				map[string][]string{
					claimCatalogFor(ns): {contract},
					subscribed:          {contract},
				},
			)
			judged := judge(ctx, r, claim.Name)

			Expect(judged.Status.Accepted).To(BeFalse())
			Expect(readyOf(judged).Reason).To(Equal(status.ContractSubscribedReason))
			Expect(readyOf(judged).Message).To(ContainSubstring(subscribed),
				"the refusal names the other provider, never the claim's own catalog")
		})
	})
})
