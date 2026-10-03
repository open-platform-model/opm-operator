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

package reconcile_test

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/config"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	"github.com/open-platform-model/opm-operator/internal/apply"
	opmcontroller "github.com/open-platform-model/opm-operator/internal/controller"
	opmreconcile "github.com/open-platform-model/opm-operator/internal/reconcile"
	"github.com/open-platform-model/opm-operator/internal/render"
	"github.com/open-platform-model/opm-operator/internal/status"
)

// platformWatchStub reports PlatformNotReady until ready is set, then renders
// the stub ConfigMap. It records when each call happened, so a spec can tell
// a render the Platform watch caused from one the transient backoff caused.
type platformWatchStub struct {
	ready     atomic.Bool
	mu        sync.Mutex
	notReady  []time.Time
	successes []time.Time
}

func (s *platformWatchStub) RenderModule(
	_ context.Context,
	_, ns, _, _ string,
	values *releasesv1alpha1.RawValues,
) (*render.RenderResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.ready.Load() {
		s.notReady = append(s.notReady, time.Now())
		return nil, render.ErrPlatformNotReady
	}
	s.successes = append(s.successes, time.Now())
	return stubRenderResult(ns, values), nil
}

func (s *platformWatchStub) calls() (notReady, successes []time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]time.Time(nil), s.notReady...), append([]time.Time(nil), s.successes...)
}

func (s *platformWatchStub) renders() int {
	n, ok := s.calls()
	return len(n) + len(ok)
}

// getCountingClient counts Get calls per key, so a spec can see whether the
// reconciler ever looked at an instance. It embeds the manager's cached
// client, so the field index still answers List.
type getCountingClient struct {
	client.Client
	mu   sync.Mutex
	gets map[client.ObjectKey]int
}

func (c *getCountingClient) Get(
	ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption,
) error {
	c.mu.Lock()
	c.gets[key]++
	c.mu.Unlock()
	return c.Client.Get(ctx, key, obj, opts...)
}

func (c *getCountingClient) getsOf(key client.ObjectKey) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.gets[key]
}

// The manager-driven proof of the filtered Platform watch: an instance
// blocked on PlatformNotReady still recovers through the watch, a Platform
// status write that changes nothing an instance consumes renders nothing, a
// pin-set change renders again, and a CLI-owned instance is never enqueued.
// No Platform reconciler runs, so the spec owns every Platform status write
// and needs no registry.
var _ = Describe("filtered Platform watch (manager-driven)", func() {
	const (
		watchNamespace = "platform-watch-filter-ns"
		managedName    = "watch-filter-managed"
		cliName        = "watch-filter-cli"
		platformName   = "cluster"
	)

	It("recovers PlatformNotReady through the watch and drops writes that change no consumed field", func() {
		Expect(client.IgnoreAlreadyExists(k8sClient.Create(ctx, &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: watchNamespace},
		}))).To(Succeed())

		// Registered first so it runs last (DeferCleanup is LIFO): the
		// instances and the Platform are deleted while the manager runs.
		mgrCtx, cancelMgr := context.WithCancel(ctx)
		DeferCleanup(cancelMgr)

		skipNameValidation := true
		mgr, err := ctrl.NewManager(cfg, ctrl.Options{
			Scheme:                 scheme.Scheme,
			LeaderElection:         false,
			Metrics:                metricsserver.Options{BindAddress: "0"},
			HealthProbeBindAddress: "0",
			Controller:             config.Controller{SkipNameValidation: &skipNameValidation},
		})
		Expect(err).NotTo(HaveOccurred())

		stub := &platformWatchStub{}
		counting := &getCountingClient{Client: mgr.GetClient(), gets: map[client.ObjectKey]int{}}
		reconciler := &opmcontroller.ModuleInstanceReconciler{
			Client:          counting,
			APIReader:       mgr.GetAPIReader(),
			Scheme:          mgr.GetScheme(),
			RestConfig:      cfg,
			ResourceManager: apply.NewResourceManager(mgr.GetClient(), "opm-controller"),
			EventRecorder:   events.NewFakeRecorder(256),
			Renderer:        stub,
		}
		Expect(reconciler.SetupWithManager(mgr)).To(Succeed())

		go func() {
			defer GinkgoRecover()
			_ = mgr.Start(mgrCtx)
		}()

		managedKey := types.NamespacedName{Name: managedName, Namespace: watchNamespace}
		cliKey := types.NamespacedName{Name: cliName, Namespace: watchNamespace}
		for _, mi := range []*releasesv1alpha1.ModuleInstance{
			{
				ObjectMeta: metav1.ObjectMeta{Name: managedName, Namespace: watchNamespace},
				Spec: releasesv1alpha1.ModuleInstanceSpec{
					Module: releasesv1alpha1.ModuleReference{Path: "opmodel.dev/test/module", Version: "v0.1.0"},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: cliName, Namespace: watchNamespace},
				Spec: releasesv1alpha1.ModuleInstanceSpec{
					Module: releasesv1alpha1.ModuleReference{Path: "opmodel.dev/test/module", Version: "v0.1.0"},
					Owner:  releasesv1alpha1.OwnerCLI,
				},
			},
		} {
			Expect(k8sClient.Create(ctx, mi)).To(Succeed())
			key := client.ObjectKeyFromObject(mi)
			DeferCleanup(func(ctx context.Context) {
				Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &releasesv1alpha1.ModuleInstance{
					ObjectMeta: metav1.ObjectMeta{Name: key.Name, Namespace: key.Namespace},
				}))).To(Succeed())
				Eventually(func() bool {
					return k8sClient.Get(ctx, key, &releasesv1alpha1.ModuleInstance{}) != nil
				}).WithTimeout(15 * time.Second).WithPolling(250 * time.Millisecond).Should(BeTrue())
			})
		}

		readyOf := func(g Gomega, key types.NamespacedName) *metav1.Condition {
			var current releasesv1alpha1.ModuleInstance
			g.Expect(k8sClient.Get(ctx, key, &current)).To(Succeed())
			ready := meta.FindStatusCondition(current.Status.Conditions, status.ReadyCondition)
			g.Expect(ready).NotTo(BeNil())
			return ready
		}

		// (a) With no Platform the managed instance blocks on PlatformNotReady;
		// the CLI-owned one is acknowledged and never renders.
		Eventually(func(g Gomega) {
			g.Expect(readyOf(g, managedKey).Reason).To(Equal(status.PlatformNotReadyReason))
			g.Expect(readyOf(g, cliKey).Reason).To(Equal(status.ManagedExternallyReason))
		}).WithTimeout(10 * time.Second).WithPolling(50 * time.Millisecond).Should(Succeed())
		notReady, _ := stub.calls()
		Expect(notReady).NotTo(BeEmpty())
		// Every backoff requeue is scheduled by a NotReady render, at least
		// BackoffBaseDelay after it, and every NotReady render happens at or
		// after the first. A render before this instant therefore came from
		// a watch event, never from the backoff.
		backoffFloor := notReady[0].Add(opmreconcile.BackoffBaseDelay)
		cliGets := counting.getsOf(cliKey)
		Expect(cliGets).To(BeNumerically(">", 0), "the CLI-owned instance was reconciled once on create")

		// (b) The Platform appears, not yet generated: its create and status
		// events render the managed instance again, still blocked.
		plat := &releasesv1alpha1.Platform{
			ObjectMeta: metav1.ObjectMeta{Name: platformName},
			Spec:       releasesv1alpha1.PlatformSpec{Type: "kubernetes"},
		}
		Expect(k8sClient.Create(ctx, plat)).To(Succeed())
		DeferCleanup(func(ctx context.Context) {
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &releasesv1alpha1.Platform{
				ObjectMeta: metav1.ObjectMeta{Name: platformName},
			}))).To(Succeed())
		})
		writePlatformStatus := func(mutate func(*releasesv1alpha1.Platform)) {
			Eventually(func(g Gomega) {
				var current releasesv1alpha1.Platform
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: platformName}, &current)).To(Succeed())
				mutate(&current)
				g.Expect(k8sClient.Status().Update(ctx, &current)).To(Succeed())
			}).WithTimeout(5 * time.Second).WithPolling(50 * time.Millisecond).Should(Succeed())
		}
		setReady := func(p *releasesv1alpha1.Platform, s metav1.ConditionStatus, reason, msg string) {
			meta.SetStatusCondition(&p.Status.Conditions, metav1.Condition{
				Type: status.ReadyCondition, Status: s, Reason: reason, Message: msg, ObservedGeneration: p.Generation,
			})
		}

		before := len(notReady)
		writePlatformStatus(func(p *releasesv1alpha1.Platform) {
			setReady(p, metav1.ConditionFalse, status.BuildFailedReason, "building platform module: not yet")
		})
		Eventually(func() int {
			n, _ := stub.calls()
			return len(n)
		}).WithTimeout(5*time.Second).WithPolling(20*time.Millisecond).Should(BeNumerically(">", before),
			"the Platform's create and status events re-render the blocked instance")

		// The platform is generated: the status write the Platform reconciler
		// makes after recording the package.
		stub.ready.Store(true)
		writePlatformStatus(func(p *releasesv1alpha1.Platform) {
			p.Status.ObservedGeneration = p.Generation
			p.Status.PackageIdentity = "g1"
			p.Status.OperatorVersion = "v1.0.0-beta.1"
			setReady(p, metav1.ConditionTrue, status.GeneratedReason, "Platform module generated and built for generation 1")
		})
		Eventually(func(g Gomega) {
			g.Expect(readyOf(g, managedKey).Status).To(Equal(metav1.ConditionTrue))
		}).WithTimeout(10 * time.Second).WithPolling(50 * time.Millisecond).Should(Succeed())
		_, successes := stub.calls()
		Expect(successes).NotTo(BeEmpty())
		Expect(successes[0]).To(BeTemporally("<", backoffFloor),
			"the recovering render must come from the Platform watch, not the transient backoff")

		// (c) Let any backoff requeue scheduled while blocked fire and
		// settle, so the success path (which does not requeue) is quiet.
		time.Sleep(time.Until(backoffFloor.Add(opmreconcile.BackoffBaseDelay)))
		var settled int
		Eventually(func() bool {
			first := stub.renders()
			time.Sleep(time.Second)
			settled = stub.renders()
			return first == settled
		}).WithTimeout(15 * time.Second).Should(BeTrue())

		// A message-only Ready rewrite and a ContractsFulfilled update change
		// nothing an instance renders against: no render.
		writePlatformStatus(func(p *releasesv1alpha1.Platform) {
			setReady(p, metav1.ConditionTrue, status.GeneratedReason,
				"Platform module generated and built for generation 1 (refreshed)")
			meta.SetStatusCondition(&p.Status.Conditions, metav1.Condition{
				Type: status.ContractsFulfilledCondition, Status: metav1.ConditionFalse,
				Reason: status.UnfulfilledContractsReason, Message: "one contract has no provider",
			})
		})
		Consistently(stub.renders).WithTimeout(3*time.Second).WithPolling(100*time.Millisecond).Should(Equal(settled),
			"a Platform status write that changes no consumed field renders nothing")

		// A new pin set renders the instance again, once.
		writePlatformStatus(func(p *releasesv1alpha1.Platform) {
			p.Status.PackageIdentity = "g1+claims"
		})
		Eventually(stub.renders).WithTimeout(5*time.Second).WithPolling(50*time.Millisecond).
			Should(BeNumerically(">=", settled+1),
				"a pin-set change re-enqueues the instance")
		afterPin := stub.renders()
		Consistently(stub.renders).WithTimeout(2 * time.Second).WithPolling(100 * time.Millisecond).Should(Equal(afterPin))

		// The CLI-owned instance was never enqueued by a Platform event.
		Expect(counting.getsOf(cliKey)).To(Equal(cliGets),
			"no Platform event reached the CLI-owned instance")
	})
})
