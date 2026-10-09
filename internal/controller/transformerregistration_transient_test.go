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
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	oerrors "github.com/open-platform-model/library/opm/errors"
	"github.com/open-platform-model/library/opm/kernel"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	platformstore "github.com/open-platform-model/opm-operator/internal/platform"
	opmreconcile "github.com/open-platform-model/opm-operator/internal/reconcile"
	"github.com/open-platform-model/opm-operator/internal/status"
)

// unreachableErr is the error shape the library returns when the registry
// gives no HTTP response, wrapped the way the fetch site wraps it.
func unreachableErr() error {
	return fmt.Errorf("fetching catalog: %w", &oerrors.FetchError{
		Kind: oerrors.FetchUnreachable,
		Err:  errors.New("dial tcp 127.0.0.1:1: connect: connection refused"),
	})
}

// useColdCUECache points the process CUE module cache at a new, empty
// directory for the calling spec and restores it afterwards. The library
// serves a catalog version it already fetched from that cache with no
// registry call, so the defect these specs cover shows only when the cache
// is cold: the state of a new operator pod, whose cache is an emptyDir.
func useColdCUECache() {
	orig, had := os.LookupEnv("CUE_CACHE_DIR")
	DeferCleanup(func() {
		if had {
			Expect(os.Setenv("CUE_CACHE_DIR", orig)).To(Succeed())
			return
		}
		Expect(os.Unsetenv("CUE_CACHE_DIR")).To(Succeed())
	})
	Expect(os.Setenv("CUE_CACHE_DIR", GinkgoT().TempDir())).To(Succeed())
}

// rejudge reconciles the claim once with the given acquirer, against a
// platform that refuses nothing, and returns the result, the claim as stored
// and the recorder that received the events.
func rejudge(
	ctx context.Context,
	catalogs CatalogAcquirer,
	name string,
) (reconcile.Result, releasesv1alpha1.TransformerRegistration, *events.FakeRecorder) {
	r := acceptanceReconciler(catalogs)
	recorder := events.NewFakeRecorder(20)
	r.EventRecorder = recorder

	result, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: name}})
	Expect(err).NotTo(HaveOccurred())

	var judged releasesv1alpha1.TransformerRegistration
	Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name}, &judged)).To(Succeed())
	return result, judged, recorder
}

// expectVerdictKept asserts that every field that records the verdict is as
// the accepted, active claim left it, and that the hold is reported.
func expectVerdictKept(before, after releasesv1alpha1.TransformerRegistration) {
	GinkgoHelper()

	Expect(after.Status.Accepted).To(BeTrue(), "the claim stays accepted")
	Expect(after.Status.Active).To(BeTrue(), "the claim stays active")
	Expect(after.Status.ObservedGeneration).To(Equal(before.Status.ObservedGeneration))

	ready := readyOf(after)
	Expect(ready.Status).To(Equal(metav1.ConditionTrue))
	Expect(ready.Reason).To(Equal(status.AcceptedReason))
	Expect(*ready).To(Equal(*readyOf(before)), "the Ready condition is untouched")
	Expect(*activeOf(after)).To(Equal(*activeOf(before)), "the Active condition is untouched")

	Expect(apimeta.FindStatusCondition(after.Status.Conditions, status.StalledCondition)).To(BeNil())
	reconciling := apimeta.FindStatusCondition(after.Status.Conditions, status.ReconcilingCondition)
	Expect(reconciling).NotTo(BeNil(), "the failure is visible on the claim")
	Expect(reconciling.Status).To(Equal(metav1.ConditionTrue))
	Expect(reconciling.Reason).To(Equal(status.CatalogUnresolvedReason))
	Expect(reconciling.Message).To(ContainSubstring(after.Spec.Catalog))
}

// expectRefused asserts the refusal every non-transient acquisition failure
// gets, accepted claim or not.
func expectRefused(result reconcile.Result, after releasesv1alpha1.TransformerRegistration) {
	GinkgoHelper()

	Expect(after.Status.Accepted).To(BeFalse(), "the claim is un-accepted")
	ready := readyOf(after)
	Expect(ready.Status).To(Equal(metav1.ConditionFalse))
	Expect(ready.Reason).To(Equal(status.CatalogUnresolvedReason))
	Expect(apimeta.IsStatusConditionTrue(after.Status.Conditions, status.StalledCondition)).To(BeTrue())
	Expect(result.RequeueAfter).To(Equal(opmreconcile.StalledRecheckInterval))
}

// backdateHold moves the start of the claim's held state into the past, as
// if the registry had been failing for that long.
func backdateHold(ctx context.Context, name string, age time.Duration) {
	GinkgoHelper()

	var claim releasesv1alpha1.TransformerRegistration
	Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name}, &claim)).To(Succeed())
	reconciling := apimeta.FindStatusCondition(claim.Status.Conditions, status.ReconcilingCondition)
	Expect(reconciling).NotTo(BeNil())
	reconciling.LastTransitionTime = metav1.NewTime(time.Now().Add(-age))
	Expect(k8sClient.Status().Update(ctx, &claim)).To(Succeed())
}

var _ = Describe("TransformerRegistration acceptance: a transient registry failure", func() {
	Context("an accepted, active claim", func() {
		It("keeps its verdict when the registry cannot be reached", func() {
			ctx := context.Background()
			ns := nextClaimNamespace()
			before := activatedClaim(ctx, ns, claimContract(ns))

			result, after, recorder := rejudge(ctx, &stubCatalogs{err: unreachableErr()}, before.Name)

			expectVerdictKept(before, after)
			Expect(result.RequeueAfter).To(BeNumerically(">=", opmreconcile.BackoffBaseDelay))
			Expect(result.RequeueAfter).To(BeNumerically("<", 2*opmreconcile.BackoffBaseDelay),
				"the first retry is the base delay plus jitter, not the 30-minute recheck")

			var event string
			Expect(recorder.Events).To(Receive(&event))
			Expect(event).To(ContainSubstring("Warning"))
			Expect(event).To(ContainSubstring(status.CatalogUnresolvedReason))
			Expect(event).To(ContainSubstring("connection refused"), "the event carries the registry error")
		})

		It("keeps its verdict when the registry answers 5xx", func() {
			ctx := context.Background()
			ns := nextClaimNamespace()
			before := activatedClaim(ctx, ns, claimContract(ns))

			_, after, _ := rejudge(ctx, &stubCatalogs{err: fmt.Errorf("fetching catalog: %w", &oerrors.FetchError{
				Kind:   oerrors.FetchOther,
				Status: http.StatusBadGateway,
				Err:    errors.New("502 Bad Gateway"),
			})}, before.Name)

			expectVerdictKept(before, after)
		})

		It("keeps its verdict when the registry answers 429", func() {
			ctx := context.Background()
			ns := nextClaimNamespace()
			before := activatedClaim(ctx, ns, claimContract(ns))

			result, after, _ := rejudge(ctx, &stubCatalogs{err: fmt.Errorf("fetching catalog: %w", &oerrors.FetchError{
				Kind:   oerrors.FetchOther,
				Status: http.StatusTooManyRequests,
				Err:    errors.New("429 Too Many Requests"),
			})}, before.Name)

			expectVerdictKept(before, after)
			Expect(result.RequeueAfter).To(BeNumerically("<", 2*opmreconcile.BackoffBaseDelay))
		})

		It("writes no status and no second event on a repeated attempt", func() {
			ctx := context.Background()
			ns := nextClaimNamespace()
			before := activatedClaim(ctx, ns, claimContract(ns))

			_, first, _ := rejudge(ctx, &stubCatalogs{err: unreachableErr()}, before.Name)
			_, second, recorder := rejudge(ctx, &stubCatalogs{err: unreachableErr()}, before.Name)

			expectVerdictKept(before, second)
			Expect(second.ResourceVersion).To(Equal(first.ResourceVersion), "a repeated hold is an empty patch")
			Expect(recorder.Events).NotTo(Receive(), "the event marks the entry into the state only")
		})

		It("backs off for as long as the failure has lasted, up to the cap", func() {
			ctx := context.Background()
			ns := nextClaimNamespace()
			before := activatedClaim(ctx, ns, claimContract(ns))
			rejudge(ctx, &stubCatalogs{err: unreachableErr()}, before.Name)

			backdateHold(ctx, before.Name, 40*time.Second)
			result, _, _ := rejudge(ctx, &stubCatalogs{err: unreachableErr()}, before.Name)
			Expect(result.RequeueAfter).To(BeNumerically(">=", 40*time.Second))
			Expect(result.RequeueAfter).To(BeNumerically("<", 55*time.Second))

			backdateHold(ctx, before.Name, time.Hour)
			result, _, _ = rejudge(ctx, &stubCatalogs{err: unreachableErr()}, before.Name)
			Expect(result.RequeueAfter).To(BeNumerically(">=", opmreconcile.BackoffMaxDelay))
			Expect(result.RequeueAfter).To(BeNumerically("<=", opmreconcile.BackoffMaxDelay+opmreconcile.BackoffMaxDelay/10))
		})

		It("is judged again, and stops reporting the failure, when the registry answers", func() {
			ctx := context.Background()
			ns := nextClaimNamespace()
			contract := claimContract(ns)
			before := activatedClaim(ctx, ns, contract)
			rejudge(ctx, &stubCatalogs{err: unreachableErr()}, before.Name)

			_, after, _ := rejudge(ctx, &stubCatalogs{cat: providerCatalog(contract)}, before.Name)

			Expect(after.Status.Accepted).To(BeTrue())
			Expect(after.Status.Active).To(BeTrue())
			Expect(readyOf(after).Reason).To(Equal(status.AcceptedReason))
			Expect(apimeta.FindStatusCondition(after.Status.Conditions, status.ReconcilingCondition)).To(BeNil())
		})

		It("is un-accepted when the registry answers that the catalog is gone, after a hold", func() {
			ctx := context.Background()
			ns := nextClaimNamespace()
			before := activatedClaim(ctx, ns, claimContract(ns))
			rejudge(ctx, &stubCatalogs{err: unreachableErr()}, before.Name)

			result, after, _ := rejudge(ctx, &stubCatalogs{err: fmt.Errorf("fetching catalog: %w", &oerrors.FetchError{
				Kind: oerrors.FetchNotFound,
				Err:  errors.New("module not found"),
			})}, before.Name)

			expectRefused(result, after)
			Expect(apimeta.FindStatusCondition(after.Status.Conditions, status.ReconcilingCondition)).To(BeNil())
		})

		DescribeTable("is un-accepted by a failure that is not transient",
			func(cause error) {
				ctx := context.Background()
				ns := nextClaimNamespace()
				before := activatedClaim(ctx, ns, claimContract(ns))

				result, after, _ := rejudge(ctx, &stubCatalogs{err: cause}, before.Name)

				expectRefused(result, after)
			},
			Entry("the registry does not hold the catalog",
				&oerrors.FetchError{Kind: oerrors.FetchNotFound, Err: errors.New("module not found")}),
			Entry("the registry refuses the credentials",
				&oerrors.FetchError{Kind: oerrors.FetchUnauthorized, Status: http.StatusUnauthorized, Err: errors.New("401")}),
			Entry("the library did not classify the failure",
				errors.New("registry unreachable, said in words only")),
			Entry("a terminal cause is joined to an unreachable registry",
				errors.Join(unreachableErr(), oerrors.ErrInvalidPackage)),
		)
	})

	Context("an accepted claim that has not activated", func() {
		It("keeps its verdict and still says it waits on its provider", func() {
			ctx := context.Background()
			ns := nextClaimNamespace()
			contract := claimContract(ns)
			claim := createClaimProviding(ctx, ns, contract)
			ownProvidedInventory(ctx, ns, claim.Name)
			before := judge(ctx, acceptanceReconciler(&stubCatalogs{cat: providerCatalog(contract)}), claim.Name)
			Expect(before.Status.Accepted).To(BeTrue())
			Expect(before.Status.Active).To(BeFalse())

			_, after, _ := rejudge(ctx, &stubCatalogs{err: unreachableErr()}, claim.Name)

			Expect(after.Status.Accepted).To(BeTrue())
			Expect(after.Status.Active).To(BeFalse())
			Expect(*readyOf(after)).To(Equal(*readyOf(before)))
			Expect(*activeOf(after)).To(Equal(*activeOf(before)))
			Expect(activeOf(after).Reason).To(Equal(status.ProviderNotReadyReason))
		})
	})

	Context("an accepted claim after an operator restart", func() {
		It("stays accepted and active, and Ready names the registry failure", func() {
			ctx := context.Background()
			ns := nextClaimNamespace()
			before := activatedClaim(ctx, ns, claimContract(ns))

			// A new process has an empty platform store, so the first
			// reconcile defers: Ready goes Unknown and accepted is untouched.
			restarted := &TransformerRegistrationReconciler{
				Client:        k8sClient,
				Scheme:        k8sClient.Scheme(),
				EventRecorder: events.NewFakeRecorder(10),
				Store:         platformstore.NewStore(),
			}
			deferred := judge(ctx, restarted, before.Name)
			Expect(deferred.Status.Accepted).To(BeTrue())
			Expect(readyOf(deferred).Reason).To(Equal(status.PlatformNotReadyReason))

			// The platform is back and the registry is not.
			_, after, _ := rejudge(ctx, &stubCatalogs{err: unreachableErr()}, before.Name)

			Expect(after.Status.Accepted).To(BeTrue())
			Expect(after.Status.Active).To(BeTrue())
			Expect(*activeOf(after)).To(Equal(*activeOf(before)))
			ready := readyOf(after)
			Expect(ready.Status).To(Equal(metav1.ConditionUnknown))
			Expect(ready.Reason).To(Equal(status.CatalogUnresolvedReason),
				"a Ready that carried no verdict must not keep naming the platform")
		})
	})

	Context("an accepted claim whose spec was edited after the verdict", func() {
		It("is refused, because the verdict it holds is for another spec", func() {
			ctx := context.Background()
			ns := nextClaimNamespace()
			before := activatedClaim(ctx, ns, claimContract(ns))

			edited := before.DeepCopy()
			edited.Spec.Version = "9.9.9"
			Expect(k8sClient.Update(ctx, edited)).To(Succeed())
			Expect(edited.Generation).To(BeNumerically(">", before.Status.ObservedGeneration))

			result, after, _ := rejudge(ctx, &stubCatalogs{err: unreachableErr()}, before.Name)

			expectRefused(result, after)
			Expect(readyOf(after).Message).To(ContainSubstring("9.9.9"))
		})
	})

	Context("a claim that is not accepted", func() {
		It("is refused as before", func() {
			ctx := context.Background()
			ns := nextClaimNamespace()
			claim := createClaim(ctx, ns)

			result, after, _ := rejudge(ctx, &stubCatalogs{err: unreachableErr()}, claim.Name)

			expectRefused(result, after)
		})
	})

	// These specs pass the real library Kernel as the acquirer, so the error
	// the reconciler classifies is the library's own. Each one runs on a cold
	// module cache (useColdCUECache), which is what makes the fetch go to the
	// registry at all.
	Context("through the library, on a cold module cache", func() {
		It("keeps an accepted, active claim when nothing listens at the registry", func() {
			ctx := context.Background()
			ns := nextClaimNamespace()
			before := activatedClaim(ctx, ns, claimContract(ns))
			useColdCUECache()

			result, after, _ := rejudge(ctx, kernel.New(kernel.WithRegistry(closedRegistry)), before.Name)

			expectVerdictKept(before, after)
			Expect(result.RequeueAfter).To(BeNumerically("<", 2*opmreconcile.BackoffBaseDelay))
		})

		It("keeps an accepted, active claim when the registry answers 503", func() {
			ctx := context.Background()
			ns := nextClaimNamespace()
			before := activatedClaim(ctx, ns, claimContract(ns))
			useColdCUECache()
			registry, stop := statusRegistry(http.StatusServiceUnavailable)
			DeferCleanup(stop)

			_, after, _ := rejudge(ctx, kernel.New(kernel.WithRegistry(registry)), before.Name)

			expectVerdictKept(before, after)
		})

		It("un-accepts an accepted, active claim when the registry answers 404", func() {
			ctx := context.Background()
			ns := nextClaimNamespace()
			before := activatedClaim(ctx, ns, claimContract(ns))
			useColdCUECache()
			registry, stop := statusRegistry(http.StatusNotFound)
			DeferCleanup(stop)

			result, after, _ := rejudge(ctx, kernel.New(kernel.WithRegistry(registry)), before.Name)

			expectRefused(result, after)
		})

		// A registry that uses token authentication, as GHCR does: the
		// answer comes from its token endpoint.
		DescribeTable("judges an accepted, active claim by the token endpoint's answer",
			func(tokenStatus int, kept bool) {
				ctx := context.Background()
				ns := nextClaimNamespace()
				before := activatedClaim(ctx, ns, claimContract(ns))
				useColdCUECache()
				registry, stop := tokenRegistry(tokenStatus)
				DeferCleanup(stop)

				result, after, _ := rejudge(ctx, kernel.New(kernel.WithRegistry(registry)), before.Name)

				if kept {
					expectVerdictKept(before, after)
					return
				}
				expectRefused(result, after)
			},
			Entry("a refused token (401) un-accepts", http.StatusUnauthorized, false),
			Entry("a rate limit (429) holds", http.StatusTooManyRequests, true),
			Entry("a token endpoint that is down (503) holds", http.StatusServiceUnavailable, true),
		)
	})
})

var _ = DescribeTable("holdBackoff waits as long as the hold has lasted, between the base delay and the cap",
	func(held, want time.Duration) {
		now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
		Expect(holdBackoff(now.Add(-held), now)).To(Equal(want))
	},
	Entry("a new hold", time.Duration(0), opmreconcile.BackoffBaseDelay),
	Entry("a hold shorter than the base delay", 3*time.Second, opmreconcile.BackoffBaseDelay),
	Entry("a start in the future (clock skew)", -time.Minute, opmreconcile.BackoffBaseDelay),
	Entry("ten seconds", 10*time.Second, 10*time.Second),
	Entry("two minutes", 2*time.Minute, 2*time.Minute),
	Entry("the cap", opmreconcile.BackoffMaxDelay, opmreconcile.BackoffMaxDelay),
	Entry("past the cap", time.Hour, opmreconcile.BackoffMaxDelay),
)
