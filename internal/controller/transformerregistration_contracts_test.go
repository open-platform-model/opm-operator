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
	"strconv"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"github.com/open-platform-model/library/opm/platform"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	platformstore "github.com/open-platform-model/opm-operator/internal/platform"
	"github.com/open-platform-model/opm-operator/internal/status"
)

// platformProviding builds a built-platform value shaped the way core builds
// one, whose enabled registry entries (keyed by registry key, the catalog
// module path with its major) each carry one transformer requiring that
// entry's contracts with provider fulfilment.
//
// Two parts of it matter, and they are deliberately built independently:
//
//   - #contracts is the inventory the library's Platform.Contracts decodes,
//     complete with every field it reads. providedBy is keyed by contract and
//     lists the providing registry keys, sorted; overSubscribed is its keys
//     with two or more entries and routable its emptiness. This is the count
//     acceptance reads.
//   - #composedTransformers is stamped the way core stamps a transformer's
//     owning catalog (core src/catalog.cue): metadata.modulePath is the
//     registry path WITHOUT its major, suffixed "/transformers". Nothing reads
//     it any more; it is kept so that a regression reading the stamp again
//     fails on the shape core really produces instead of passing on a
//     fixture that flatters it.
//
// A nil map is a platform whose entries provide nothing, which is what every
// spec that is not about 0015:D2 wants.
func platformProviding(byEntry map[string][]string) *platform.Platform {
	return platformFromCUE(corePlatformSource(byEntry, true))
}

// platformProvidingWithoutCount is platformProviding with providedBy left out
// of #contracts: a platform whose provider count the library cannot read.
func platformProvidingWithoutCount(byEntry map[string][]string) *platform.Platform {
	return platformFromCUE(corePlatformSource(byEntry, false))
}

// corePlatformSource renders the CUE of platformProviding's value.
func corePlatformSource(byEntry map[string][]string, withProvidedBy bool) string {
	providedBy := map[string][]string{}
	var body strings.Builder
	body.WriteString(`kind: "Platform"
type: "kubernetes"
metadata: name: "cluster"
#composedTransformers: {
`)
	for _, key := range slices.Sorted(maps.Keys(byEntry)) {
		path, major, _ := strings.Cut(key, "@")
		stamp := path + "/transformers"
		fmt.Fprintf(&body, "\t%q: {\n\t\tmetadata: modulePath: %q\n\t\trequiredTraits: {\n",
			stamp+"/adapter@"+strings.TrimPrefix(major, "v")+".0.0", stamp)
		for _, contract := range byEntry[key] {
			fmt.Fprintf(&body, "\t\t\t%q: fulfilment: \"provider\"\n", contract)
			if !slices.Contains(providedBy[contract], key) {
				providedBy[contract] = append(providedBy[contract], key)
			}
		}
		body.WriteString("\t\t}\n\t}\n")
	}
	body.WriteString("}\n")

	var overSubscribed []string
	body.WriteString("#contracts: {\n")
	if withProvidedBy {
		body.WriteString("\tprovidedBy: {\n")
	}
	for _, contract := range slices.Sorted(maps.Keys(providedBy)) {
		keys := providedBy[contract]
		slices.Sort(keys)
		if len(keys) > 1 {
			overSubscribed = append(overSubscribed, contract)
		}
		if withProvidedBy {
			quoted := make([]string, len(keys))
			for i, k := range keys {
				quoted[i] = strconv.Quote(k)
			}
			fmt.Fprintf(&body, "\t\t%q: [%s]\n", contract, strings.Join(quoted, ", "))
		}
	}
	if withProvidedBy {
		body.WriteString("\t}\n")
	}
	quotedOver := make([]string, len(overSubscribed))
	for i, c := range overSubscribed {
		quotedOver[i] = strconv.Quote(c)
	}
	fmt.Fprintf(&body, `	definedBy: {}
	requiredBy: {}
	unfulfilled: []
	overSubscribed: [%s]
	comparable: []
	fulfilled: true
	routable: %t
	discriminated: true
}
`, strings.Join(quotedOver, ", "), len(overSubscribed) == 0)
	return body.String()
}

// platformFromCUE compiles src into a built-platform value.
func platformFromCUE(src string) *platform.Platform {
	v := cuecontext.New().CompileString(src)
	Expect(v.Err()).NotTo(HaveOccurred(), src)
	p, err := platform.NewPlatformFromValue(v)
	Expect(err).NotTo(HaveOccurred())
	return p
}

var _ = Describe("platformProviding, the core-shaped platform fixture", func() {
	DescribeTable("decodes to the provider count it was built from",
		func(byEntry map[string][]string, wantProvidedBy map[string][]string, wantOverSubscribed []string) {
			inv, err := platformProviding(byEntry).Contracts()
			Expect(err).NotTo(HaveOccurred())
			Expect(inv.ProvidedBy).To(Equal(wantProvidedBy))
			Expect(inv.OverSubscribed).To(Equal(wantOverSubscribed))
			Expect(inv.Routable).To(Equal(len(wantOverSubscribed) == 0))
		},
		Entry("nil provides nothing and is routable", nil, map[string][]string{}, []string{}),
		Entry("one entry is one provider",
			map[string][]string{"opmodel.dev/catalogs/velero@v2": {backupTrait}},
			map[string][]string{backupTrait: {"opmodel.dev/catalogs/velero@v2"}}, []string{}),
		Entry("two majors of one catalog are two providers, sorted",
			map[string][]string{
				"opmodel.dev/catalogs/k8up@v2": {backupTrait},
				"opmodel.dev/catalogs/k8up@v1": {backupTrait},
			},
			map[string][]string{backupTrait: {"opmodel.dev/catalogs/k8up@v1", "opmodel.dev/catalogs/k8up@v2"}},
			[]string{backupTrait}),
	)

	It("stamps transformers the way core does: the registry path without its major", func() {
		p := platformProviding(map[string][]string{"opmodel.dev/catalogs/velero@v2": {backupTrait}})
		iter, err := p.Package.LookupPath(cue.MakePath(cue.Def("composedTransformers"))).Fields()
		Expect(err).NotTo(HaveOccurred())
		Expect(iter.Next()).To(BeTrue())
		stamp, err := iter.Value().LookupPath(cue.ParsePath("metadata.modulePath")).String()
		Expect(err).NotTo(HaveOccurred())
		Expect(stamp).To(Equal("opmodel.dev/catalogs/velero/transformers"))
	})
})

// contractReconciler returns a reconciler whose built platform's enabled
// subscriptions provide the given contracts.
func contractReconciler(
	catalogs CatalogAcquirer,
	byEntry map[string][]string,
) *TransformerRegistrationReconciler {
	return reconcilerOver(catalogs, platformProviding(byEntry))
}

// reconcilerOver returns a reconciler whose store holds the given built
// platform.
func reconcilerOver(catalogs CatalogAcquirer, p *platform.Platform) *TransformerRegistrationReconciler {
	r := acceptanceReconciler(catalogs)
	store := platformstore.NewStore()
	store.SetGenerated(platformstore.Generated{
		Identity: platformIdentity(1),
		Dir:      platformDirWith(map[string]string{"opmodel.dev/core@v2": "v2.0.0"}),
		Platform: p,
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

		It("refuses a claim whose contract another major of its own catalog provides", func() {
			ctx := context.Background()
			ns := nextClaimNamespace()
			contract := claimContract(ns)
			claim := createClaimProviding(ctx, ns, contract)
			ownProvidedInventory(ctx, ns, claim.Name)

			// The claim is for the v1 major of its catalog; the platform
			// already carries the v2 major, which provides the same contract.
			// Two majors are two registry entries, so the v2 entry is another
			// provider, not the claim's own.
			own := claimCatalogFor(ns)
			Expect(own).To(HaveSuffix("@v1"))
			otherMajor := strings.TrimSuffix(own, "@v1") + "@v2"
			r := contractReconciler(
				&stubCatalogs{cat: providerCatalog(contract)},
				map[string][]string{otherMajor: {contract}},
			)
			judged := judge(ctx, r, claim.Name)

			Expect(judged.Status.Accepted).To(BeFalse())
			ready := readyOf(judged)
			Expect(ready.Status).To(Equal(metav1.ConditionFalse))
			Expect(ready.Reason).To(Equal(status.ContractSubscribedReason))
			Expect(ready.Message).To(ContainSubstring(contract), "the contract is named")
			Expect(ready.Message).To(ContainSubstring(otherMajor), "the other major's registry entry is named")
		})

		It("defers the verdict when the provider count cannot be read", func() {
			ctx := context.Background()
			ns := nextClaimNamespace()
			contract := claimContract(ns)
			claim := createClaimProviding(ctx, ns, contract)
			ownProvidedInventory(ctx, ns, claim.Name)

			// A built platform whose inventory carries no providedBy: the
			// library refuses to read it, and acceptance must not read that
			// as "nothing provides the contract". Nothing in it provides the
			// claim's contract either, so only a read of the count can tell.
			r := reconcilerOver(
				&stubCatalogs{cat: providerCatalog(contract)},
				platformProvidingWithoutCount(map[string][]string{"opmodel.dev/catalogs/velero@v2": {restoreTrait}}),
			)

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
				"a claim judged against an unreadable provider count is not refused")
			Expect(ready.Reason).To(Equal(status.PlatformNotReadyReason))
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
				Identity: platformIdentity(1),
				Dir:      platformDirWith(map[string]string{"opmodel.dev/core@v2": "v2.0.0"}),
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
			// contract: not a 0015:D12 duplicate, and refused all the same.
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
