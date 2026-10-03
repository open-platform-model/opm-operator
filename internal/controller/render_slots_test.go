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
	"sync"
	"sync/atomic"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	"github.com/open-platform-model/opm-operator/internal/apply"
	opmmetrics "github.com/open-platform-model/opm-operator/internal/metrics"
	opmreconcile "github.com/open-platform-model/opm-operator/internal/reconcile"
	"github.com/open-platform-model/opm-operator/internal/render"
	opmsource "github.com/open-platform-model/opm-operator/internal/source"
)

// overlapProbe records how many renders, of either kind, are in flight at
// once.
type overlapProbe struct {
	mu          sync.Mutex
	inFlight    int
	maxInFlight int
}

func (p *overlapProbe) enter() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.inFlight++
	p.maxInFlight = max(p.maxInFlight, p.inFlight)
}

func (p *overlapProbe) leave() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.inFlight--
}

func (p *overlapProbe) max() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.maxInFlight
}

// rendezvous lets a probed render wait a bounded time for its peer to start.
// Without a shared bound the peer arrives and the two overlap; with one slot
// it cannot arrive while this render holds the slot, so the wait times out
// and the render carries on.
type rendezvous struct {
	once    sync.Once
	arrived chan struct{}
}

func newRendezvous() *rendezvous { return &rendezvous{arrived: make(chan struct{})} }

func (r *rendezvous) arrive() { r.once.Do(func() { close(r.arrived) }) }

func waitForPeer(ctx context.Context, peer *rendezvous) {
	select {
	case <-peer.arrived:
	case <-time.After(time.Second):
	case <-ctx.Done():
	}
}

// probedModuleRenderer renders the stub result inside the probe.
type probedModuleRenderer struct {
	probe      *overlapProbe
	self, peer *rendezvous
	inner      *stubRenderer
}

func (r *probedModuleRenderer) RenderModule(
	ctx context.Context,
	name, namespace, modulePath, moduleVersion string,
	values *releasesv1alpha1.RawValues,
) (*render.RenderResult, error) {
	r.probe.enter()
	defer r.probe.leave()
	r.self.arrive()
	waitForPeer(ctx, r.peer)
	return r.inner.RenderModule(ctx, name, namespace, modulePath, moduleVersion, values)
}

// probedPackageRenderer renders the stub result inside the probe.
type probedPackageRenderer struct {
	probe      *overlapProbe
	self, peer *rendezvous
	inner      *stubPackageRenderer
}

func (r *probedPackageRenderer) Render(ctx context.Context, dir string) (string, *render.RenderResult, error) {
	r.probe.enter()
	defer r.probe.leave()
	r.self.arrive()
	waitForPeer(ctx, r.peer)
	return r.inner.Render(ctx, dir)
}

// countingModuleRenderer counts calls and renders the default stub result.
type countingModuleRenderer struct {
	calls atomic.Int32
	inner stubRenderer
}

func (r *countingModuleRenderer) RenderModule(
	ctx context.Context,
	name, namespace, modulePath, moduleVersion string,
	values *releasesv1alpha1.RawValues,
) (*render.RenderResult, error) {
	r.calls.Add(1)
	return r.inner.RenderModule(ctx, name, namespace, modulePath, moduleVersion, values)
}

// countingPackageRenderer counts calls and renders a fixed stub result.
type countingPackageRenderer struct {
	calls atomic.Int32
	inner *stubPackageRenderer
}

func (r *countingPackageRenderer) Render(ctx context.Context, dir string) (string, *render.RenderResult, error) {
	r.calls.Add(1)
	return r.inner.Render(ctx, dir)
}

type panickingModuleRenderer struct{}

func (panickingModuleRenderer) RenderModule(
	context.Context, string, string, string, string, *releasesv1alpha1.RawValues,
) (*render.RenderResult, error) {
	panic("render blew up")
}

type panickingPackageRenderer struct{}

func (panickingPackageRenderer) Render(context.Context, string) (string, *render.RenderResult, error) {
	panic("render blew up")
}

// cancellingFetcher fetches like stubFetcher, then cancels the reconcile's
// context, so the next thing that observes it is the wait for a render slot.
type cancellingFetcher struct {
	stubFetcher
	cancel context.CancelFunc
}

func (f *cancellingFetcher) Fetch(ctx context.Context, url, digest, dir string, opts opmsource.FetchOptions) error {
	err := f.stubFetcher.Fetch(ctx, url, digest, dir, opts)
	f.cancel()
	return err
}

// writeCounter counts every write a reconcile attempts on an object or its
// status, whether or not it lands. A patch made with a cancelled context
// fails on its own, so only the attempt count shows a skipped commit.
type writeCounter struct {
	n atomic.Int32
}

// wrap returns funcs with write counting added; the reads in funcs are kept.
func (w *writeCounter) wrap(funcs interceptor.Funcs) interceptor.Funcs {
	funcs.Patch = func(ctx context.Context, c client.WithWatch, obj client.Object, p client.Patch, opts ...client.PatchOption) error {
		w.n.Add(1)
		return c.Patch(ctx, obj, p, opts...)
	}
	funcs.Update = func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
		w.n.Add(1)
		return c.Update(ctx, obj, opts...)
	}
	funcs.SubResourcePatch = func(ctx context.Context, c client.Client, sub string, obj client.Object, p client.Patch, opts ...client.SubResourcePatchOption) error {
		w.n.Add(1)
		return c.SubResource(sub).Patch(ctx, obj, p, opts...)
	}
	funcs.SubResourceUpdate = func(ctx context.Context, c client.Client, sub string, obj client.Object, opts ...client.SubResourceUpdateOption) error {
		w.n.Add(1)
		return c.SubResource(sub).Update(ctx, obj, opts...)
	}
	return funcs
}

// newWatchClient returns a fresh client of the envtest API server that
// interceptors can wrap.
func newWatchClient() client.WithWatch {
	c, err := client.NewWithWatch(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	return c
}

// expectNoEvents asserts r's fake recorder holds no event.
func expectNoEvents(recorder events.EventRecorder) {
	fake, ok := recorder.(*events.FakeRecorder)
	Expect(ok).To(BeTrue())
	Expect(fake.Events).To(BeEmpty(), "no event is emitted")
}

// expectSlotFree asserts a slot of pool can be taken at once.
func expectSlotFree(pool *render.Slots) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	release, err := pool.Acquire(ctx)
	Expect(err).NotTo(HaveOccurred(), "the render slot was given back")
	release()
}

// statusVersion reads obj's resourceVersion fresh from the API server.
func statusVersion(ctx context.Context, nn types.NamespacedName, obj client.Object) string {
	Expect(k8sClient.Get(ctx, nn, obj)).To(Succeed())
	return obj.GetResourceVersion()
}

var _ = Describe("Render slots", func() {
	newMIReconciler := func(c client.Client, renderer render.ModuleRenderer, pool *render.Slots) *ModuleInstanceReconciler {
		return &ModuleInstanceReconciler{
			Client:          c,
			Scheme:          k8sClient.Scheme(),
			ResourceManager: apply.NewResourceManager(k8sClient, "opm-controller"),
			EventRecorder:   events.NewFakeRecorder(32),
			Renderer:        renderer,
			RenderSlots:     pool,
		}
	}
	newMPReconciler := func(c client.Client, fetcher opmsource.Fetcher, renderer render.PackageRenderer, pool *render.Slots) *ModulePackageReconciler {
		return &ModulePackageReconciler{
			Client:          c,
			Scheme:          k8sClient.Scheme(),
			ResourceManager: apply.NewResourceManager(k8sClient, "opm-controller"),
			EventRecorder:   events.NewFakeRecorder(32),
			Fetcher:         fetcher,
			Renderer:        renderer,
			RenderSlots:     pool,
		}
	}
	addFinalizer := func(ctx context.Context, r reconcile.Reconciler, nn types.NamespacedName) {
		result, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal(reconcile.Result{Requeue: true}), "the first reconcile only adds the finalizer")
	}

	It("renders a ModuleInstance and a ModulePackage one at a time under one shared slot", func() {
		ctx := context.Background()
		miNS := newRenderTestNamespace(ctx, "slots-mi")
		mpNS := newRenderTestNamespace(ctx, "slots-mp")
		miNN := createRenderTestInstance(ctx, miNS, "slots-mi")
		mpNN := createRenderTestPackage(ctx, mpNS, "slots-mp")

		pool := render.NewSlots(1)
		probe := &overlapProbe{}
		miArrive, mpArrive := newRendezvous(), newRendezvous()
		mi := newMIReconciler(k8sClient, &probedModuleRenderer{
			probe: probe, self: miArrive, peer: mpArrive, inner: &stubRenderer{},
		}, pool)
		mp := newMPReconciler(k8sClient, &stubFetcher{pathInArtifact: renderTestPath}, &probedPackageRenderer{
			probe: probe, self: mpArrive, peer: miArrive,
			inner: &stubPackageRenderer{result: stubRenderResult(mpNS, nil)},
		}, pool)
		addFinalizer(ctx, mi, miNN)
		addFinalizer(ctx, mp, mpNN)

		var wg sync.WaitGroup
		errs := make([]error, 2)
		for i, run := range []func() error{
			func() error { _, err := mi.Reconcile(ctx, reconcile.Request{NamespacedName: miNN}); return err },
			func() error { _, err := mp.Reconcile(ctx, reconcile.Request{NamespacedName: mpNN}); return err },
		} {
			wg.Go(func() {
				defer GinkgoRecover()
				errs[i] = run()
			})
		}
		wg.Wait()

		Expect(errs).To(HaveEach(Succeed()))
		Expect(probe.max()).To(Equal(1), "at most one render of either kind is in flight")

		var gotMI releasesv1alpha1.ModuleInstance
		Expect(k8sClient.Get(ctx, miNN, &gotMI)).To(Succeed())
		expectReady(miNN, gotMI.Status.Conditions)
		var gotMP releasesv1alpha1.ModulePackage
		Expect(k8sClient.Get(ctx, mpNN, &gotMP)).To(Succeed())
		expectReady(mpNN, gotMP.Status.Conditions)
	})

	It("commits nothing when a ModuleInstance's wait for a slot is cancelled", func() {
		ctx := context.Background()
		ns := newRenderTestNamespace(ctx, "slots-cancel-mi")
		nn := createRenderTestInstance(ctx, ns, "slots-cancel-mi")

		pool := render.NewSlots(1)
		renderer := &countingModuleRenderer{}
		addFinalizer(ctx, newMIReconciler(k8sClient, renderer, pool), nn)
		before := statusVersion(ctx, nn, &releasesv1alpha1.ModuleInstance{})

		hold, err := pool.Acquire(ctx)
		Expect(err).NotTo(HaveOccurred())
		defer hold()

		// The instance is read with a live context, which is then
		// cancelled: the reconcile meets the cancellation at the slot wait.
		reconcileCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		writes := &writeCounter{}
		cancelAfterGet := interceptor.NewClient(newWatchClient(), writes.wrap(interceptor.Funcs{
			Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				err := c.Get(ctx, key, obj, opts...)
				cancel()
				return err
			},
		}))
		noOps := testutil.ToFloat64(opmmetrics.ReconcileTotal.WithLabelValues(nn.Name, nn.Namespace, opmreconcile.NoOp.MetricLabel()))

		r := newMIReconciler(cancelAfterGet, renderer, pool)
		_, err = r.Reconcile(reconcileCtx, reconcile.Request{NamespacedName: nn})
		Expect(errors.Is(err, context.Canceled)).To(BeTrue(), "got %v", err)
		Expect(renderer.calls.Load()).To(BeZero(), "the renderer is never called")
		Expect(writes.n.Load()).To(BeZero(), "no status patch is attempted")
		Expect(testutil.ToFloat64(opmmetrics.ReconcileTotal.WithLabelValues(nn.Name, nn.Namespace, opmreconcile.NoOp.MetricLabel()))).
			To(Equal(noOps), "no reconcile is recorded")
		expectNoEvents(r.EventRecorder)
		Expect(statusVersion(ctx, nn, &releasesv1alpha1.ModuleInstance{})).To(Equal(before), "status is unchanged")
	})

	It("commits nothing when a ModulePackage's wait for a slot is cancelled", func() {
		ctx := context.Background()
		ns := newRenderTestNamespace(ctx, "slots-cancel-mp")
		nn := createRenderTestPackage(ctx, ns, "slots-cancel-mp")

		pool := render.NewSlots(1)
		renderer := &countingPackageRenderer{inner: &stubPackageRenderer{result: stubRenderResult(ns, nil)}}
		addFinalizer(ctx, newMPReconciler(k8sClient, &stubFetcher{pathInArtifact: renderTestPath}, renderer, pool), nn)
		before := statusVersion(ctx, nn, &releasesv1alpha1.ModulePackage{})

		hold, err := pool.Acquire(ctx)
		Expect(err).NotTo(HaveOccurred())
		defer hold()

		reconcileCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		fetcher := &cancellingFetcher{stubFetcher: stubFetcher{pathInArtifact: renderTestPath}, cancel: cancel}

		writes := &writeCounter{}
		counted := interceptor.NewClient(newWatchClient(), writes.wrap(interceptor.Funcs{}))

		r := newMPReconciler(counted, fetcher, renderer, pool)
		_, err = r.Reconcile(reconcileCtx, reconcile.Request{NamespacedName: nn})
		Expect(errors.Is(err, context.Canceled)).To(BeTrue(), "got %v", err)
		Expect(renderer.calls.Load()).To(BeZero(), "the renderer is never called")
		Expect(writes.n.Load()).To(BeZero(), "no status patch is attempted")
		expectNoEvents(r.EventRecorder)
		Expect(statusVersion(ctx, nn, &releasesv1alpha1.ModulePackage{})).To(Equal(before), "status is unchanged")
	})

	It("frees the slot when a ModuleInstance render panics", func() {
		ctx := context.Background()
		ns := newRenderTestNamespace(ctx, "slots-panic-mi")
		nn := createRenderTestInstance(ctx, ns, "slots-panic-mi")

		pool := render.NewSlots(1)
		r := newMIReconciler(k8sClient, panickingModuleRenderer{}, pool)
		addFinalizer(ctx, r, nn)

		func() {
			defer func() { Expect(recover()).NotTo(BeNil(), "the render panicked") }()
			_, _ = r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		}()
		expectSlotFree(pool)
	})

	It("frees the slot when a ModulePackage render panics", func() {
		ctx := context.Background()
		ns := newRenderTestNamespace(ctx, "slots-panic-mp")
		nn := createRenderTestPackage(ctx, ns, "slots-panic-mp")

		pool := render.NewSlots(1)
		r := newMPReconciler(k8sClient, &stubFetcher{pathInArtifact: renderTestPath}, panickingPackageRenderer{}, pool)
		addFinalizer(ctx, r, nn)

		func() {
			defer func() { Expect(recover()).NotTo(BeNil(), "the render panicked") }()
			_, _ = r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		}()
		expectSlotFree(pool)
	})
})
