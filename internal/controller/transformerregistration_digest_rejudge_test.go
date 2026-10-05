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
	"sync/atomic"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/event"

	"github.com/open-platform-model/library/opm/catalog"
	k8sinventory "github.com/open-platform-model/library/opm/k8s/inventory"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	"github.com/open-platform-model/opm-operator/internal/inventory"
)

// countingCatalogs counts the registry acquisitions a verdict makes.
type countingCatalogs struct {
	stubCatalogs
	calls atomic.Int32
}

func (c *countingCatalogs) AcquireCatalogFromRegistry(ctx context.Context, path, version string) (*catalog.Catalog, error) {
	c.calls.Add(1)
	return c.stubCatalogs.AcquireCatalogFromRegistry(ctx, path, version)
}

// Adopting the library's inventory digest moves status.inventory.digest once
// on every operator-managed provider instance while its entries stay the
// same. The provider watch passes that update, so each claim is judged once
// more and re-acquires its catalog; the verdict must not change.
var _ = Describe("TransformerRegistration: a provider's one-time inventory digest change", func() {
	It("re-judges the claim once and leaves its verdict unchanged", func() {
		ctx := context.Background()
		ns := nextClaimNamespace()
		contract := claimContract(ns)
		claim := createClaimProviding(ctx, ns, contract)
		ownProvidedInventory(ctx, ns, claim.Name)
		setProviderReadiness(ctx, ns, true)
		providerKey := types.NamespacedName{Namespace: ns, Name: providerInstanceName}

		setDigest := func(digest func([]releasesv1alpha1.InventoryEntry) string) (old, updated *releasesv1alpha1.ModuleInstance) {
			var instance releasesv1alpha1.ModuleInstance
			Expect(k8sClient.Get(ctx, providerKey, &instance)).To(Succeed())
			old = instance.DeepCopy()
			instance.Status.Inventory.Digest = digest(instance.Status.Inventory.Entries)
			Expect(k8sClient.Status().Update(ctx, &instance)).To(Succeed())
			return old, &instance
		}

		By("the provider holds the digest the earlier operator recorded")
		setDigest(preUpgradeInventoryDigest)
		catalogs := &countingCatalogs{stubCatalogs: stubCatalogs{cat: providerCatalog(contract)}}
		r := acceptanceReconciler(catalogs)
		before := judge(ctx, r, claim.Name)
		Expect(before.Status.Accepted).To(BeTrue())
		Expect(before.Status.Active).To(BeTrue())
		acquires := catalogs.calls.Load()

		By("the upgrade rewrites only the digest, which the watch passes")
		old, updated := setDigest(func(entries []releasesv1alpha1.InventoryEntry) string {
			return k8sinventory.Digest(inventory.ToEntries(entries))
		})
		Expect(updated.Status.Inventory.Digest).NotTo(Equal(old.Status.Inventory.Digest))
		Expect(updated.Status.Inventory.Entries).To(Equal(old.Status.Inventory.Entries))
		Expect(providerFactsChanged().Update(event.UpdateEvent{ObjectOld: old, ObjectNew: updated})).
			To(BeTrue(), "a moved inventory digest re-enqueues the claim")

		By("the re-judge acquires the catalog once more and keeps the verdict")
		after := judge(ctx, r, claim.Name)
		Expect(catalogs.calls.Load()).To(Equal(acquires+1), "one registry fetch for the re-judge")
		Expect(after.Status.Accepted).To(Equal(before.Status.Accepted))
		Expect(after.Status.Active).To(Equal(before.Status.Active))
		Expect(readyOf(after).Status).To(Equal(readyOf(before).Status))
		Expect(readyOf(after).Reason).To(Equal(readyOf(before).Reason))

		By("the provider's later status writes leave the digest alone and are dropped")
		Expect(providerFactsChanged().Update(event.UpdateEvent{ObjectOld: updated, ObjectNew: updated.DeepCopy()})).
			To(BeFalse(), "the digest moves once, so the claim is re-judged once")
	})
})
