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
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"cuelang.org/go/cue/cuecontext"
	"github.com/open-platform-model/library/opm/catalog"
	oerrors "github.com/open-platform-model/library/opm/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	platformstore "github.com/open-platform-model/opm-operator/internal/platform"
	"github.com/open-platform-model/opm-operator/internal/status"
)

const (
	backupTrait  = "opmodel.dev/catalogs/opm/traits/backup@v1alpha1"
	restoreTrait = "opmodel.dev/catalogs/opm/traits/restore@v1alpha1"
)

// stubCatalogs is a test CatalogAcquirer returning a pre-built catalog or an
// error, without touching an OCI registry.
type stubCatalogs struct {
	cat *catalog.Catalog
	err error
}

func (s *stubCatalogs) AcquireCatalogFromRegistry(_ context.Context, _, _ string) (*catalog.Catalog, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.cat, nil
}

// providerCatalog builds a catalog whose transformers require each named
// contract with provider fulfilment, so Provides derives exactly that set.
func providerCatalog(contracts ...string) *catalog.Catalog {
	var body strings.Builder
	body.WriteString(`kind: "Catalog"
metadata: {
	modulePath: "opmodel.dev/catalogs/k8up@v1"
	version:    "1.0.0"
	fqn:        "opmodel.dev/catalogs/k8up@v1"
}
#transformers: "opmodel.dev/catalogs/k8up/transformers/schedule@1.0.0": requiredTraits: {
`)
	for _, c := range contracts {
		fmt.Fprintf(&body, "\t%q: fulfilment: \"provider\"\n", c)
	}
	body.WriteString("}\n")

	v := cuecontext.New().CompileString(body.String())
	Expect(v.Err()).NotTo(HaveOccurred())
	cat, err := catalog.NewCatalogFromValue(v)
	Expect(err).NotTo(HaveOccurred())

	// Every acquired catalog carries its committed module file, which the
	// build-compatibility check reads. This one requires nothing, so it is
	// compatible with any platform; the 0015:D8 specs supply their own.
	cat.Source = catalogSourceRequiring(nil)
	return cat
}

// catalogSourceRequiring builds the overlay source of a catalog whose
// committed cue.mod/module.cue declares the given dependencies.
func catalogSourceRequiring(deps map[string]string) *catalog.Source {
	const root = "/synthetic/catalog"
	return &catalog.Source{
		Root: root,
		Overlay: map[string][]byte{
			filepath.Join(root, "cue.mod", "module.cue"): []byte(
				modFile("opmodel.dev/catalogs/k8up@v1", deps)),
		},
	}
}

// acceptanceReconciler returns a reconciler with a platform in its store, so
// the specs below reach the checks rather than parking on PlatformNotReady.
// The platform resolves core, which the default provider catalog does not
// require, so nothing here refuses on build compatibility, and its enabled
// subscriptions provide no contract, so nothing here refuses on 0015:D2 either.
func acceptanceReconciler(catalogs CatalogAcquirer) *TransformerRegistrationReconciler {
	store := platformstore.NewStore()
	store.SetGenerated(platformstore.Generated{
		Identity: platformIdentity(1),
		Dir:      platformDirWith(map[string]string{"opmodel.dev/core@v2": "v2.0.0"}),
		Platform: platformProviding(nil),
	})
	return &TransformerRegistrationReconciler{
		Client:        k8sClient,
		Scheme:        k8sClient.Scheme(),
		EventRecorder: events.NewFakeRecorder(20),
		Catalogs:      catalogs,
		Store:         store,
	}
}

// judge reconciles the claim once and returns it as stored.
func judge(ctx context.Context, r *TransformerRegistrationReconciler, name string) releasesv1alpha1.TransformerRegistration {
	_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: name}})
	Expect(err).NotTo(HaveOccurred())

	var judged releasesv1alpha1.TransformerRegistration
	Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name}, &judged)).To(Succeed())
	return judged
}

// readyOf returns the claim's Ready condition, which carries the verdict.
func readyOf(claim releasesv1alpha1.TransformerRegistration) *metav1.Condition {
	ready := apimeta.FindStatusCondition(claim.Status.Conditions, status.ReadyCondition)
	Expect(ready).NotTo(BeNil())
	return ready
}

// ownProvidedInventory gives the instance an inventory owning the named
// claim, which is what the provider-identity check reads.
func ownProvidedInventory(ctx context.Context, namespace, claimName string) {
	const name = providerInstanceName
	instance := &releasesv1alpha1.ModuleInstance{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: releasesv1alpha1.ModuleInstanceSpec{
			Module: releasesv1alpha1.ModuleReference{
				Path:    "opmodel.dev/modules/k8up",
				Version: "1.0.0",
			},
		},
	}
	Expect(k8sClient.Create(ctx, instance)).To(Succeed())

	instance.Status.Inventory = &releasesv1alpha1.Inventory{
		Count: 1,
		Entries: []releasesv1alpha1.InventoryEntry{{
			Group:   "opmodel.dev",
			Kind:    "TransformerRegistration",
			Name:    claimName,
			Version: "v1alpha1",
		}},
	}
	Expect(k8sClient.Status().Update(ctx, instance)).To(Succeed())
}

var _ = Describe("TransformerRegistration acceptance: the catalog checks", func() {
	Context("D10 — the claimed artifact must be a catalog", func() {
		It("refuses a wrong-kind artifact, naming the kind found", func() {
			ctx := context.Background()
			ns := nextClaimNamespace()
			claim := createClaim(ctx, ns)

			r := acceptanceReconciler(&stubCatalogs{
				err: fmt.Errorf(`expected kind "Catalog", got "Module": %w`, oerrors.ErrWrongKind),
			})
			judged := judge(ctx, r, claim.Name)

			Expect(judged.Status.Accepted).To(BeFalse())
			ready := readyOf(judged)
			Expect(ready.Status).To(Equal(metav1.ConditionFalse))
			Expect(ready.Reason).To(Equal(status.CatalogWrongKindReason))
			Expect(ready.Message).To(ContainSubstring(`got "Module"`))
		})

		It("refuses an unresolvable coordinate with a different reason, naming the coordinate", func() {
			ctx := context.Background()
			ns := nextClaimNamespace()
			claim := createClaim(ctx, ns)

			r := acceptanceReconciler(&stubCatalogs{
				err: fmt.Errorf("module opmodel.dev/catalogs/k8up@v1: version 1.0.0 not found"),
			})
			judged := judge(ctx, r, claim.Name)

			Expect(judged.Status.Accepted).To(BeFalse())
			ready := readyOf(judged)
			Expect(ready.Reason).To(Equal(status.CatalogUnresolvedReason))
			Expect(ready.Reason).NotTo(Equal(status.CatalogWrongKindReason),
				"a registry problem and an authoring problem send the claimant to different fixes")
			Expect(ready.Message).To(ContainSubstring(claim.Spec.Catalog))
			Expect(ready.Message).To(ContainSubstring("1.0.0"))
		})

		It("classifies on the sentinel, not on the words in the message", func() {
			ctx := context.Background()
			ns := nextClaimNamespace()
			claim := createClaim(ctx, ns)

			// A fetch failure whose text talks about kinds. A message match
			// would call this a wrong-kind refusal; errors.Is does not,
			// because nothing here wraps the library's sentinel.
			r := acceptanceReconciler(&stubCatalogs{
				err: fmt.Errorf(`expected kind "Catalog": registry unreachable`),
			})
			judged := judge(ctx, r, claim.Name)

			Expect(readyOf(judged).Reason).To(Equal(status.CatalogUnresolvedReason))
		})
	})

	Context("D11 — provides is re-derived and compared for exact equality", func() {
		It("refuses a claim listing a contract the catalog does not implement", func() {
			ctx := context.Background()
			ns := nextClaimNamespace()
			claim := createClaim(ctx, ns) // claims backupTrait

			r := acceptanceReconciler(&stubCatalogs{cat: providerCatalog()})
			judged := judge(ctx, r, claim.Name)

			Expect(judged.Status.Accepted).To(BeFalse())
			ready := readyOf(judged)
			Expect(ready.Reason).To(Equal(status.ProvidesMismatchReason))
			Expect(ready.Message).To(ContainSubstring(backupTrait), "the claimed list is named")
		})

		It("refuses a claim omitting a contract the catalog does implement", func() {
			ctx := context.Background()
			ns := nextClaimNamespace()
			claim := createClaim(ctx, ns) // claims backupTrait only

			r := acceptanceReconciler(&stubCatalogs{cat: providerCatalog(backupTrait, restoreTrait)})
			judged := judge(ctx, r, claim.Name)

			Expect(judged.Status.Accepted).To(BeFalse())
			ready := readyOf(judged)
			Expect(ready.Reason).To(Equal(status.ProvidesMismatchReason))
			Expect(ready.Message).To(ContainSubstring(restoreTrait), "the derived list is named")
			Expect(ready.Message).To(ContainSubstring(backupTrait))
		})

		It("accepts an exactly matching claim whatever order the lists came in", func() {
			ctx := context.Background()
			ns := nextClaimNamespace()

			// The claim lists restore before backup; Provides derives them
			// sorted, so the two lists arrive in opposite orders.
			claim := createClaim(ctx, ns)
			claim.Spec.Provides = []string{restoreTrait, backupTrait}
			Expect(k8sClient.Update(ctx, claim)).To(Succeed())
			claimName := claim.Name
			ownProvidedInventory(ctx, ns, claimName)

			r := acceptanceReconciler(&stubCatalogs{cat: providerCatalog(backupTrait, restoreTrait)})
			judged := judge(ctx, r, claimName)

			Expect(judged.Status.Accepted).To(BeTrue())
			Expect(readyOf(judged).Reason).To(Equal(status.AcceptedReason))

			// Acceptance does not activate.
			Expect(judged.Status.Active).To(BeFalse())
		})
	})

	Context("D11's deferred check — the claim must come from the instance it names", func() {
		It("accepts a claim its named instance's inventory owns", func() {
			ctx := context.Background()
			ns := nextClaimNamespace()
			claim := createClaim(ctx, ns)
			ownProvidedInventory(ctx, ns, claim.Name)

			r := acceptanceReconciler(&stubCatalogs{cat: providerCatalog(backupTrait)})
			judged := judge(ctx, r, claim.Name)

			Expect(judged.Status.Accepted).To(BeTrue())
			Expect(readyOf(judged).Reason).To(Equal(status.AcceptedReason))
		})

		It("refuses a claim whose providerRef names an instance that does not own it", func() {
			ctx := context.Background()
			ns := nextClaimNamespace()
			claim := createClaim(ctx, ns)

			// The named instance exists and has a settled inventory, but that
			// inventory owns another instance's claim: a hand-applied stray.
			ownProvidedInventory(ctx, ns, "other-namespace.k8up")

			r := acceptanceReconciler(&stubCatalogs{cat: providerCatalog(backupTrait)})
			judged := judge(ctx, r, claim.Name)

			Expect(judged.Status.Accepted).To(BeFalse())
			ready := readyOf(judged)
			Expect(ready.Reason).To(Equal(status.ProviderMismatchReason))
			Expect(ready.Message).To(ContainSubstring(claim.Name), "the claim identity is named")
			Expect(ready.Message).To(ContainSubstring(ns+"/k8up"), "the provider identity is named")
		})

		It("refuses a claim whose providerRef names no instance at all", func() {
			ctx := context.Background()
			ns := nextClaimNamespace()
			claim := createClaim(ctx, ns)

			r := acceptanceReconciler(&stubCatalogs{cat: providerCatalog(backupTrait)})
			judged := judge(ctx, r, claim.Name)

			Expect(judged.Status.Accepted).To(BeFalse())
			ready := readyOf(judged)
			Expect(ready.Reason).To(Equal(status.ProviderMismatchReason))
			Expect(ready.Message).To(ContainSubstring("does not exist"))
		})

		It("requeues, not refuses, while the named instance has written no inventory", func() {
			ctx := context.Background()
			ns := nextClaimNamespace()
			claim := createClaim(ctx, ns)

			instance := &releasesv1alpha1.ModuleInstance{
				ObjectMeta: metav1.ObjectMeta{Name: "k8up", Namespace: ns},
				Spec: releasesv1alpha1.ModuleInstanceSpec{
					Module: releasesv1alpha1.ModuleReference{
						Path:    "opmodel.dev/modules/k8up",
						Version: "1.0.0",
					},
				},
			}
			Expect(k8sClient.Create(ctx, instance)).To(Succeed())

			r := acceptanceReconciler(&stubCatalogs{cat: providerCatalog(backupTrait)})
			result, err := r.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: claim.Name},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).NotTo(BeZero())

			var judged releasesv1alpha1.TransformerRegistration
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: claim.Name}, &judged)).To(Succeed())

			Expect(judged.Status.Accepted).To(BeFalse())
			ready := readyOf(judged)
			Expect(ready.Status).To(Equal(metav1.ConditionUnknown),
				"a race is not a verdict, so the claim is not refused")
			Expect(ready.Reason).To(Equal(status.ProviderInventoryPendingReason))
		})
	})
})
