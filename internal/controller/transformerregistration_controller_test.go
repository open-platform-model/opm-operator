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

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	platformstore "github.com/open-platform-model/opm-operator/internal/platform"
	"github.com/open-platform-model/opm-operator/internal/status"
)

// claimCounter names each spec's claim apart. The kind is cluster-scoped, so
// specs sharing a name would share an object.
var claimCounter int

// createClaim applies a well-formed claim from the named instance. The CRD
// requires the dot-joined name, so the object name is derived rather than
// chosen.
func createClaim(ctx context.Context, namespace, name string) *releasesv1alpha1.TransformerRegistration {
	claim := &releasesv1alpha1.TransformerRegistration{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("%s.%s", namespace, name),
		},
		Spec: releasesv1alpha1.TransformerRegistrationSpec{
			Catalog:  "opmodel.dev/catalogs/k8up@v1",
			Version:  "1.0.0",
			Provides: []string{"opmodel.dev/catalogs/opm/traits/backup@v1alpha1"},
			ProviderRef: releasesv1alpha1.ProviderReference{
				Namespace: namespace,
				Name:      name,
			},
		},
	}
	Expect(k8sClient.Create(ctx, claim)).To(Succeed())
	return claim
}

// nextClaimNamespace returns a namespace string unique to the calling spec.
func nextClaimNamespace() string {
	claimCounter++
	return fmt.Sprintf("claim-ns-%d", claimCounter)
}

var _ = Describe("TransformerRegistration Controller", func() {
	Context("When no platform has been generated", func() {
		It("requeues without writing a verdict", func() {
			ctx := context.Background()
			ns := nextClaimNamespace()
			claim := createClaim(ctx, ns, "k8up")

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
		It("patches status and records the generation it observed", func() {
			ctx := context.Background()
			ns := nextClaimNamespace()
			claim := createClaim(ctx, ns, "k8up")

			store := platformstore.NewStore()
			store.SetGenerated(platformstore.Generated{Generation: 1, Dir: "/does-not-matter"})

			reconciler := &TransformerRegistrationReconciler{
				Client:        k8sClient,
				Scheme:        k8sClient.Scheme(),
				EventRecorder: events.NewFakeRecorder(10),
				Store:         store,
			}

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: claim.Name},
			})
			Expect(err).NotTo(HaveOccurred())

			var judged releasesv1alpha1.TransformerRegistration
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: claim.Name}, &judged)).To(Succeed())

			Expect(judged.Status.ObservedGeneration).To(Equal(judged.Generation))
			ready := apimeta.FindStatusCondition(judged.Status.Conditions, status.ReadyCondition)
			Expect(ready).NotTo(BeNil())
			Expect(ready.Reason).To(Equal(status.NotYetJudgedReason))

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
