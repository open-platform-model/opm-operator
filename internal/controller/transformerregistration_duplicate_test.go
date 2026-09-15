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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/open-platform-model/opm-operator/internal/status"
)

var _ = Describe("TransformerRegistration acceptance: D12 — one claim per provider", func() {
	It("accepts the older claim and refuses the newer one naming the holder", func() {
		ctx := context.Background()

		// Two instances of one provider module, in different namespaces.
		// The dot-joined name guarantees two distinct CRs; both name the
		// same provider catalog, so only one can hold it.
		const shared = "opmodel.dev/catalogs/duplicate-a@v1"

		firstNS := nextClaimNamespace()
		first := createClaimFor(ctx, firstNS, shared)
		ownProvidedInventory(ctx, firstNS, first.Name)

		secondNS := nextClaimNamespace()
		second := createClaimFor(ctx, secondNS, shared)
		ownProvidedInventory(ctx, secondNS, second.Name)

		r := acceptanceReconciler(&stubCatalogs{cat: providerCatalog(backupTrait)})

		judgedFirst := judge(ctx, r, first.Name)
		Expect(judgedFirst.Status.Accepted).To(BeTrue())
		Expect(readyOf(judgedFirst).Reason).To(Equal(status.AcceptedReason))

		judgedSecond := judge(ctx, r, second.Name)
		Expect(judgedSecond.Status.Accepted).To(BeFalse())
		ready := readyOf(judgedSecond)
		Expect(ready.Reason).To(Equal(status.DuplicateClaimReason))
		Expect(ready.Message).To(ContainSubstring(first.Name),
			"an operator needs the holder's name to know which object to remove")
	})

	It("keeps the holder across repeated reconciles in either order", func() {
		ctx := context.Background()

		const shared = "opmodel.dev/catalogs/duplicate-b@v1"

		firstNS := nextClaimNamespace()
		first := createClaimFor(ctx, firstNS, shared)
		ownProvidedInventory(ctx, firstNS, first.Name)

		secondNS := nextClaimNamespace()
		second := createClaimFor(ctx, secondNS, shared)
		ownProvidedInventory(ctx, secondNS, second.Name)

		r := acceptanceReconciler(&stubCatalogs{cat: providerCatalog(backupTrait)})

		// Judge the newer claim first: reconcile order must not decide the
		// holder, so this must still refuse rather than take acceptance.
		Expect(judge(ctx, r, second.Name).Status.Accepted).To(BeFalse())
		Expect(judge(ctx, r, first.Name).Status.Accepted).To(BeTrue())

		// Re-reconcile both, repeatedly, with no spec change.
		for range 3 {
			Expect(judge(ctx, r, first.Name).Status.Accepted).To(BeTrue())
			Expect(judge(ctx, r, second.Name).Status.Accepted).To(BeFalse())
		}
	})

	It("does not refuse a claim whose only competitor names another catalog", func() {
		ctx := context.Background()

		// An older claim exists, for a different provider catalog. It is not
		// a competitor, so it must not cost the newer claim its acceptance.
		otherNS := nextClaimNamespace()
		other := createClaimFor(ctx, otherNS, "opmodel.dev/catalogs/velero@v1")
		ownProvidedInventory(ctx, otherNS, other.Name)

		ns := nextClaimNamespace()
		claim := createClaimFor(ctx, ns, "opmodel.dev/catalogs/restic@v1")
		ownProvidedInventory(ctx, ns, claim.Name)

		r := acceptanceReconciler(&stubCatalogs{cat: providerCatalog(backupTrait)})

		judged := judge(ctx, r, claim.Name)
		Expect(judged.Status.Accepted).To(BeTrue())
		Expect(readyOf(judged).Reason).To(Equal(status.AcceptedReason))
	})
})
