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

	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	platformstore "github.com/open-platform-model/opm-operator/internal/platform"
	"github.com/open-platform-model/opm-operator/internal/status"
)

// providerInstanceName is the instance every spec's claim is rendered by. The
// namespace is what varies between specs, so the dot-joined claim names stay
// distinct on a cluster-scoped kind.
const providerInstanceName = "k8up"

// claimCounter names each spec's claim apart. The kind is cluster-scoped, so
// specs sharing a name would share an object.
var claimCounter int

// createClaim applies a well-formed claim naming a provider catalog of its
// own. The specs share one API server, so claims outlive the spec that made
// them; a shared catalog path would make every acceptance after the first a
// 0015:D12 duplicate. A spec that wants two claims competing passes the same path
// to createClaimFor.
func createClaim(ctx context.Context, namespace string) *releasesv1alpha1.TransformerRegistration {
	return createClaimFor(ctx, namespace, claimCatalogFor(namespace))
}

// createClaimFor applies a well-formed claim for the named provider catalog,
// listing the contract every spec that does not care about contracts uses.
func createClaimFor(ctx context.Context, namespace, catalogPath string) *releasesv1alpha1.TransformerRegistration {
	return createClaimListing(ctx, namespace, catalogPath, backupTrait)
}

// createClaimProviding applies a well-formed claim naming a provider catalog
// of its own and listing the given contracts.
//
// A spec that leaves an ACTIVE claim behind must list contracts of its own
// (claimContract): the suite shares one API server, 0015:D2 gives a contract
// exactly one provider cluster-wide, and an active claim holding the shared
// contract would refuse every later spec that claims it.
func createClaimProviding(ctx context.Context, namespace string, provides ...string) *releasesv1alpha1.TransformerRegistration {
	return createClaimListing(ctx, namespace, claimCatalogFor(namespace), provides...)
}

// claimCatalogFor is the provider catalog path a spec's own claim names.
func claimCatalogFor(namespace string) string {
	return fmt.Sprintf("opmodel.dev/catalogs/%s@v1", namespace)
}

// claimContract is a provider-fulfilled contract FQN unique to the calling
// spec's namespace. See createClaimProviding for why that matters.
func claimContract(namespace string) string {
	return fmt.Sprintf("opmodel.dev/catalogs/opm/traits/%s@v1alpha1", namespace)
}

// createClaimListing applies a well-formed claim for the named provider
// catalog listing the given contracts. The CRD requires the dot-joined name,
// so the object name is derived from the provider instance rather than
// chosen.
func createClaimListing(
	ctx context.Context,
	namespace, catalogPath string,
	provides ...string,
) *releasesv1alpha1.TransformerRegistration {
	const name = providerInstanceName
	claim := &releasesv1alpha1.TransformerRegistration{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("%s.%s", namespace, name),
		},
		Spec: releasesv1alpha1.TransformerRegistrationSpec{
			Catalog:  catalogPath,
			Version:  "1.0.0",
			Provides: provides,
			ProviderRef: releasesv1alpha1.ProviderReference{
				Namespace: namespace,
				Name:      name,
			},
		},
	}
	Expect(k8sClient.Create(ctx, claim)).To(Succeed())
	return claim
}

// nextClaimNamespace creates and returns a namespace unique to the calling
// spec, so a claim's providerRef can name an instance that really lives
// there. The counter is zero-padded so namespaces sort in creation order:
// creationTimestamp has one-second granularity, so two claims made in one
// spec usually tie and 0015:D12's holder falls to the name tie-break.
func nextClaimNamespace() string {
	claimCounter++
	name := fmt.Sprintf("claim-ns-%03d", claimCounter)
	Expect(k8sClient.Create(context.Background(), &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: name},
	})).To(Succeed())
	return name
}

var _ = Describe("TransformerRegistration Controller", func() {
	Context("When no platform has been generated", func() {
		It("requeues without writing a verdict", func() {
			ctx := context.Background()
			ns := nextClaimNamespace()
			claim := createClaim(ctx, ns)

			reconciler := &TransformerRegistrationReconciler{
				Client:        k8sClient,
				Scheme:        k8sClient.Scheme(),
				EventRecorder: events.NewFakeRecorder(10),
				Store:         platformstore.NewStore(),
			}

			result, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: claim.Name},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).NotTo(BeZero(), "the claim is retried, not abandoned")

			var judged releasesv1alpha1.TransformerRegistration
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: claim.Name}, &judged)).To(Succeed())

			// No verdict: not accepted, and not refused either.
			Expect(judged.Status.Accepted).To(BeFalse())
			ready := apimeta.FindStatusCondition(judged.Status.Conditions, status.ReadyCondition)
			Expect(ready).NotTo(BeNil())
			Expect(ready.Status).To(Equal(metav1.ConditionUnknown),
				"Ready=False would read as a refusal the claim never earned")
			Expect(ready.Reason).To(Equal(status.PlatformNotReadyReason))
		})
	})

	Context("When a platform has been generated", func() {
		It("records the generation the verdict was reached for", func() {
			ctx := context.Background()
			ns := nextClaimNamespace()
			claim := createClaim(ctx, ns)
			ownProvidedInventory(ctx, ns, claim.Name)

			r := acceptanceReconciler(&stubCatalogs{cat: providerCatalog(backupTrait)})
			judged := judge(ctx, r, claim.Name)

			Expect(judged.Status.ObservedGeneration).To(Equal(judged.Generation))
			ready := apimeta.FindStatusCondition(judged.Status.Conditions, status.ReadyCondition)
			Expect(ready).NotTo(BeNil())
			Expect(ready.ObservedGeneration).To(Equal(judged.Generation))

			// Acceptance does not activate.
			Expect(judged.Status.Active).To(BeFalse())
		})
	})

	Context("When the claim does not exist", func() {
		It("reconciles without error", func() {
			ctx := context.Background()
			reconciler := &TransformerRegistrationReconciler{
				Client:        k8sClient,
				Scheme:        k8sClient.Scheme(),
				EventRecorder: events.NewFakeRecorder(10),
				Store:         platformstore.NewStore(),
			}

			result, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "gone.claim"},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))
		})
	})
})
