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
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	fluxmeta "github.com/fluxcd/pkg/apis/meta"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	k8sinventory "github.com/open-platform-model/library/opm/k8s/inventory"
	"github.com/open-platform-model/library/opm/k8s/object"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	"github.com/open-platform-model/opm-operator/internal/apply"
	"github.com/open-platform-model/opm-operator/internal/render"
	opmsource "github.com/open-platform-model/opm-operator/internal/source"
	"github.com/open-platform-model/opm-operator/internal/status"
)

// upgradedOperatorVersion is the operator version the upgrade specs move to
// from testOperatorVersion.
const upgradedOperatorVersion = "v1.0.1-test"

// callCountingRenderer counts the module renders a reconcile asks for.
type callCountingRenderer struct {
	stubRenderer
	calls atomic.Int32
}

func (c *callCountingRenderer) RenderModule(
	ctx context.Context,
	name, namespace, path, version string,
	values *releasesv1alpha1.RawValues,
) (*render.RenderResult, error) {
	c.calls.Add(1)
	return c.stubRenderer.RenderModule(ctx, name, namespace, path, version, values)
}

// callCountingPackageRenderer counts the package renders a reconcile asks for.
type callCountingPackageRenderer struct {
	stubPackageRenderer
	calls atomic.Int32
}

func (c *callCountingPackageRenderer) Render(ctx context.Context, dir string) (string, *render.RenderResult, error) {
	c.calls.Add(1)
	return c.stubPackageRenderer.Render(ctx, dir)
}

// callCountingFetcher counts the artifact fetches a reconcile makes.
type callCountingFetcher struct {
	stubFetcher
	calls atomic.Int32
}

func (c *callCountingFetcher) Fetch(ctx context.Context, url, digest, dir string, opts opmsource.FetchOptions) error {
	c.calls.Add(1)
	return c.stubFetcher.Fetch(ctx, url, digest, dir, opts)
}

// patchCountingClient wraps the envtest API server in a client that counts
// every write a reconcile sends: patches and updates of the object and of
// its status. A resourceVersion check cannot see a patch whose content
// matches the stored object; this counter can.
func patchCountingClient() (client.Client, *atomic.Int32) {
	base, err := client.NewWithWatch(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	var writes atomic.Int32
	return interceptor.NewClient(base, interceptor.Funcs{
		Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, p client.Patch, opts ...client.PatchOption) error {
			writes.Add(1)
			return c.Patch(ctx, obj, p, opts...)
		},
		Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
			writes.Add(1)
			return c.Update(ctx, obj, opts...)
		},
		SubResourcePatch: func(ctx context.Context, c client.Client, sub string, obj client.Object, p client.Patch, opts ...client.SubResourcePatchOption) error {
			writes.Add(1)
			return c.SubResource(sub).Patch(ctx, obj, p, opts...)
		},
		SubResourceUpdate: func(ctx context.Context, c client.Client, sub string, obj client.Object, opts ...client.SubResourceUpdateOption) error {
			writes.Add(1)
			return c.SubResource(sub).Update(ctx, obj, opts...)
		},
	}), &writes
}

// createSkipPlatform creates the cluster Platform with identity as its
// status.packageIdentity and deletes it when the spec ends.
func createSkipPlatform(ctx context.Context, identity string) {
	plat := &releasesv1alpha1.Platform{
		ObjectMeta: metav1.ObjectMeta{Name: platformSingletonName},
		Spec:       releasesv1alpha1.PlatformSpec{Type: "kubernetes"},
	}
	Expect(k8sClient.Create(ctx, plat)).To(Succeed())
	DeferCleanup(deletePlatform)
	setPackageIdentity(ctx, identity)
}

// setPackageIdentity writes Platform.status.packageIdentity.
func setPackageIdentity(ctx context.Context, identity string) {
	Eventually(func() error {
		var plat releasesv1alpha1.Platform
		if err := k8sClient.Get(ctx, client.ObjectKey{Name: platformSingletonName}, &plat); err != nil {
			return err
		}
		plat.Status.PackageIdentity = identity
		return k8sClient.Status().Update(ctx, &plat)
	}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
}

// preUpgradeInventoryDigest is the inventory digest the operator recorded
// before it adopted the library's opm/k8s/inventory: the entries sorted by
// group, kind, namespace, name, component and version, hashed as their JSON.
// It is kept here only to seed status with a pre-upgrade value.
func preUpgradeInventoryDigest(entries []releasesv1alpha1.InventoryEntry) string {
	sorted := slices.Clone(entries)
	if len(sorted) == 0 {
		return fmt.Sprintf("sha256:%x", sha256.Sum256(nil))
	}
	sort.SliceStable(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]
		for _, pair := range [][2]string{
			{a.Group, b.Group}, {a.Kind, b.Kind}, {a.Namespace, b.Namespace},
			{a.Name, b.Name}, {a.Component, b.Component},
		} {
			if pair[0] != pair[1] {
				return pair[0] < pair[1]
			}
		}
		return a.Version < b.Version
	})
	b, err := json.Marshal(sorted)
	Expect(err).NotTo(HaveOccurred())
	return fmt.Sprintf("sha256:%x", sha256.Sum256(b))
}

// preUpgradeRenderDigest is the render digest the operator recorded before it
// adopted the library's opm/k8s/inventory: the exported objects sorted by
// group, kind, namespace and name, their JSON hashed in that order, with the
// managed-by value included. It is kept here only to seed status with a
// pre-upgrade value.
func preUpgradeRenderDigest(exported []object.Exported) string {
	group := func(apiVersion string) string {
		if idx := strings.LastIndex(apiVersion, "/"); idx >= 0 {
			return apiVersion[:idx]
		}
		return ""
	}
	order := make([]int, len(exported))
	for i := range order {
		order[i] = i
	}
	key := func(i int) [4]string {
		u := exported[i].Object
		return [4]string{group(u.GetAPIVersion()), u.GetKind(), u.GetNamespace(), u.GetName()}
	}
	sort.SliceStable(order, func(a, b int) bool {
		ka, kb := key(order[a]), key(order[b])
		return slices.Compare(ka[:], kb[:]) < 0
	})
	h := sha256.New()
	for _, i := range order {
		h.Write(exported[i].JSON)
	}
	return fmt.Sprintf("sha256:%x", h.Sum(nil))
}

// libraryDigestsOf returns the render digest and the inventory digest the
// operator now records for result: the library's digests over one export of
// its resources and over the entries built from that export.
func libraryDigestsOf(result *render.RenderResult) (renderDigest, inventoryDigest string, exported []object.Exported) {
	exported, err := object.Export(result.Resources)
	Expect(err).NotTo(HaveOccurred())
	renderDigest, err = k8sinventory.RenderDigest(exported)
	Expect(err).NotTo(HaveOccurred())
	entries := make([]k8sinventory.Entry, 0, len(exported))
	for _, e := range exported {
		entries = append(entries, k8sinventory.NewEntry(e.Object))
	}
	return renderDigest, k8sinventory.Digest(entries), exported
}

// The render skip (render-input-key): a reconcile whose render input key
// matches status.lastAppliedInputs, whose last confirming render is younger
// than the drift render interval, that is Ready and has observed its
// generation, renders nothing and patches nothing.
var _ = Describe("Render skip on unchanged inputs", func() {
	const (
		namespace = "default"
		interval  = 30 * time.Minute
	)

	Context("ModuleInstance", func() {
		newReconciler := func(renderer render.ModuleRenderer, drift time.Duration) *ModuleInstanceReconciler {
			return &ModuleInstanceReconciler{
				Client:              k8sClient,
				Scheme:              k8sClient.Scheme(),
				ResourceManager:     apply.NewResourceManager(k8sClient, "opm-controller"),
				EventRecorder:       events.NewFakeRecorder(100),
				Renderer:            renderer,
				OperatorVersion:     testOperatorVersion,
				LibraryVersion:      testLibraryVersion,
				DriftRenderInterval: drift,
			}
		}

		get := func(ctx context.Context, nn types.NamespacedName) *releasesv1alpha1.ModuleInstance {
			var mi releasesv1alpha1.ModuleInstance
			Expect(k8sClient.Get(ctx, nn, &mi)).To(Succeed())
			return &mi
		}

		reconcileOnce := func(ctx context.Context, r *ModuleInstanceReconciler, nn types.NamespacedName) reconcile.Result {
			res, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			return res
		}

		// appliedInstance creates an instance against a gen-1 Platform and
		// reconciles it to a first successful apply, which records its key.
		appliedInstance := func(ctx context.Context, name string, renderer *callCountingRenderer) types.NamespacedName {
			createSkipPlatform(ctx, stubPlatformIdentity)
			mi := &releasesv1alpha1.ModuleInstance{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
				Spec: releasesv1alpha1.ModuleInstanceSpec{
					Module: releasesv1alpha1.ModuleReference{Path: "opmodel.dev/test/module", Version: "v0.1.0"},
					Values: &releasesv1alpha1.RawValues{},
				},
			}
			mi.Spec.Values.Raw = []byte(`{"message": "hello"}`)
			Expect(k8sClient.Create(ctx, mi)).To(Succeed())
			nn := types.NamespacedName{Name: name, Namespace: namespace}
			DeferCleanup(func() {
				Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{Name: "test-module", Namespace: namespace},
				}))).To(Succeed())
				Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &releasesv1alpha1.ModuleInstance{
					ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
				}))).To(Succeed())
			})
			r := newReconciler(renderer, interval)
			reconcileOnce(ctx, r, nn) // finalizer
			reconcileOnce(ctx, r, nn) // apply
			applied := get(ctx, nn)
			Expect(applied.Status.LastAppliedInputs).NotTo(BeNil(), "the apply records the key")
			return nn
		}

		updateStatus := func(ctx context.Context, nn types.NamespacedName, mutate func(*releasesv1alpha1.ModuleInstanceStatus)) {
			Eventually(func() error {
				var mi releasesv1alpha1.ModuleInstance
				if err := k8sClient.Get(ctx, nn, &mi); err != nil {
					return err
				}
				mutate(&mi.Status)
				return k8sClient.Status().Update(ctx, &mi)
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
		}

		// seedPreUpgradeDigests overwrites the stored inventory and render
		// digests with the values the earlier encoding gives for the same
		// entries and render, as an instance applied by the earlier operator
		// holds them. The key stays the one appliedInstance recorded, under
		// testOperatorVersion.
		seedPreUpgradeDigests := func(ctx context.Context, nn types.NamespacedName) {
			mi := get(ctx, nn)
			Expect(mi.Status.LastAppliedInputs).NotTo(BeNil())
			_, _, exported := libraryDigestsOf(stubRenderResult(namespace, mi.Spec.Values))
			oldRender := preUpgradeRenderDigest(exported)
			oldInventory := preUpgradeInventoryDigest(mi.Status.Inventory.Entries)
			Expect(oldRender).NotTo(Equal(mi.Status.LastAppliedRenderDigest), "the encodings differ")
			Expect(oldInventory).NotTo(Equal(mi.Status.Inventory.Digest), "the encodings differ")
			updateStatus(ctx, nn, func(s *releasesv1alpha1.ModuleInstanceStatus) {
				s.LastAppliedRenderDigest = oldRender
				s.LastAttemptedRenderDigest = oldRender
				s.Inventory.Digest = oldInventory
			})
		}

		It("skips the render and patches nothing when the inputs are unchanged", func() {
			ctx := context.Background()
			renderer := &callCountingRenderer{}
			nn := appliedInstance(ctx, "skip-unchanged-mi", renderer)
			before := get(ctx, nn)
			calls := renderer.calls.Load()

			r := newReconciler(renderer, interval)
			counting, writes := patchCountingClient()
			r.Client = counting
			res := reconcileOnce(ctx, r, nn)

			Expect(res).To(Equal(reconcile.Result{}), "a skipped instance does not requeue")
			Expect(renderer.calls.Load()).To(Equal(calls), "the renderer is not called")
			Expect(writes.Load()).To(BeZero(), "no patch or status patch is sent")
			after := get(ctx, nn)
			Expect(after.ResourceVersion).To(Equal(before.ResourceVersion), "no status patch is sent")
			Expect(after.Status.LastAppliedVersion).To(Equal(before.Status.LastAppliedVersion))
			Expect(after.Status.RequiredContracts).To(Equal(before.Status.RequiredContracts))
			Expect(after.Status.History).To(Equal(before.Status.History))
			Expect(apimeta.FindStatusCondition(after.Status.Conditions, status.DriftedCondition)).
				To(Equal(apimeta.FindStatusCondition(before.Status.Conditions, status.DriftedCondition)))
			Expect(after.Status.LastAppliedInputs.RenderedAt.Equal(&before.Status.LastAppliedInputs.RenderedAt)).
				To(BeTrue(), "a skip does not move renderedAt")
		})

		It("renders when the Platform's package identity moves", func() {
			ctx := context.Background()
			renderer := &callCountingRenderer{}
			nn := appliedInstance(ctx, "skip-identity-mi", renderer)
			calls := renderer.calls.Load()

			setPackageIdentity(ctx, "gen-2")
			reconcileOnce(ctx, newReconciler(renderer, interval), nn)

			Expect(renderer.calls.Load()).To(Equal(calls + 1))
			// The stub render reports it leased gen-1, so the recorded key
			// names gen-1, never the gen-2 read before the render.
			after := get(ctx, nn)
			leased := status.RenderInputKey{
				Source:          after.Status.LastAppliedSourceDigest,
				Config:          after.Status.LastAppliedConfigDigest,
				PackageIdentity: stubPlatformIdentity,
				SkewPolicy:      stubSkewPolicy,
				OperatorVersion: testOperatorVersion,
				LibraryVersion:  testLibraryVersion,
			}
			Expect(after.Status.LastAppliedInputs.Digest).To(Equal(leased.Digest()),
				"the recorded identity is the one the render leased")
		})

		It("renders once the last confirming render is older than the interval, and moves renderedAt", func() {
			ctx := context.Background()
			renderer := &callCountingRenderer{}
			nn := appliedInstance(ctx, "skip-interval-mi", renderer)
			old := metav1.NewTime(time.Now().Add(-31 * time.Minute).Truncate(time.Second))
			updateStatus(ctx, nn, func(s *releasesv1alpha1.ModuleInstanceStatus) { s.LastAppliedInputs.RenderedAt = old })
			calls := renderer.calls.Load()
			historyBefore := len(get(ctx, nn).Status.History)

			reconcileOnce(ctx, newReconciler(renderer, interval), nn)

			Expect(renderer.calls.Load()).To(Equal(calls + 1))
			after := get(ctx, nn)
			Expect(after.Status.History).To(HaveLen(historyBefore), "the render ended NoOp")
			Expect(after.Status.LastAppliedInputs.RenderedAt.After(old.Time)).To(BeTrue(), "the NoOp re-proves the key")
		})

		It("renders when the instance is not Ready", func() {
			ctx := context.Background()
			renderer := &callCountingRenderer{}
			nn := appliedInstance(ctx, "skip-notready-mi", renderer)
			// The state a failed apply leaves once the spec is back at the
			// recorded inputs: same generation observed, key unchanged,
			// Ready=False.
			updateStatus(ctx, nn, func(s *releasesv1alpha1.ModuleInstanceStatus) {
				apimeta.SetStatusCondition(&s.Conditions, metav1.Condition{
					Type: status.ReadyCondition, Status: metav1.ConditionFalse,
					Reason: status.ApplyFailedReason, Message: "injected",
				})
			})
			calls := renderer.calls.Load()

			reconcileOnce(ctx, newReconciler(renderer, interval), nn)

			Expect(renderer.calls.Load()).To(Equal(calls + 1))
			ready := apimeta.FindStatusCondition(get(ctx, nn).Status.Conditions, status.ReadyCondition)
			Expect(ready.Status).To(Equal(metav1.ConditionTrue), "the render found the cluster in line and recovered Ready")
		})

		It("renders a failed apply's revert to the recorded inputs", func() {
			ctx := context.Background()
			renderer := &callCountingRenderer{}
			nn := appliedInstance(ctx, "skip-failed-revert-mi", renderer)

			setValues := func(msg string) {
				Eventually(func() error {
					var mi releasesv1alpha1.ModuleInstance
					if err := k8sClient.Get(ctx, nn, &mi); err != nil {
						return err
					}
					mi.Spec.Values.Raw = fmt.Appendf(nil, `{"message": %q}`, msg)
					return k8sClient.Update(ctx, &mi)
				}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
			}
			setValues("changed")
			realWithWatch, err := client.NewWithWatch(cfg, client.Options{Scheme: scheme.Scheme})
			Expect(err).NotTo(HaveOccurred())
			failing := newReconciler(renderer, interval)
			failing.ResourceManager = apply.NewResourceManager(interceptor.NewClient(realWithWatch, interceptor.Funcs{
				Patch: func(_ context.Context, _ client.WithWatch, _ client.Object, _ client.Patch, _ ...client.PatchOption) error {
					return fmt.Errorf("injected apply failure")
				},
			}), "opm-controller")
			reconcileOnce(ctx, failing, nn)
			Expect(apimeta.FindStatusCondition(get(ctx, nn).Status.Conditions, status.ReadyCondition).Status).
				To(Equal(metav1.ConditionFalse))

			setValues("hello")
			calls := renderer.calls.Load()
			reconcileOnce(ctx, newReconciler(renderer, interval), nn)
			Expect(renderer.calls.Load()).To(Equal(calls + 1))
		})

		It("renders a spec edit outside the key and writes observedGeneration", func() {
			ctx := context.Background()
			renderer := &callCountingRenderer{}
			nn := appliedInstance(ctx, "skip-specedit-mi", renderer)
			Eventually(func() error {
				var mi releasesv1alpha1.ModuleInstance
				if err := k8sClient.Get(ctx, nn, &mi); err != nil {
					return err
				}
				mi.Spec.Prune = !mi.Spec.Prune
				return k8sClient.Update(ctx, &mi)
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
			edited := get(ctx, nn)
			Expect(edited.Status.ObservedGeneration).NotTo(Equal(edited.Generation))
			calls := renderer.calls.Load()

			reconcileOnce(ctx, newReconciler(renderer, interval), nn)

			Expect(renderer.calls.Load()).To(Equal(calls + 1))
			after := get(ctx, nn)
			Expect(after.Status.ObservedGeneration).To(Equal(after.Generation))
		})

		It("renders and applies once after an operator upgrade, and records the new key", func() {
			ctx := context.Background()
			renderer := &callCountingRenderer{}
			nn := appliedInstance(ctx, "skip-upgrade-mi", renderer)
			before := get(ctx, nn)
			calls := renderer.calls.Load()

			// The new operator computes a different render digest for the
			// same inputs, as a change to how a stored digest is computed would.
			values := &releasesv1alpha1.RawValues{}
			values.Raw = []byte(`{"message": "re-digested"}`)
			upgraded := &callCountingRenderer{stubRenderer: stubRenderer{result: stubRenderResult(namespace, values)}}
			r := newReconciler(upgraded, interval)
			r.OperatorVersion = upgradedOperatorVersion
			reconcileOnce(ctx, r, nn)

			Expect(renderer.calls.Load()).To(Equal(calls))
			Expect(upgraded.calls.Load()).To(Equal(int32(1)), "the upgrade renders")
			after := get(ctx, nn)
			Expect(after.Status.LastAppliedRenderDigest).NotTo(Equal(before.Status.LastAppliedRenderDigest), "and applies")
			Expect(after.Status.History).To(HaveLen(len(before.Status.History) + 1))
			want := status.RenderInputKey{
				Source:          after.Status.LastAppliedSourceDigest,
				Config:          after.Status.LastAppliedConfigDigest,
				PackageIdentity: stubPlatformIdentity,
				SkewPolicy:      stubSkewPolicy,
				OperatorVersion: upgradedOperatorVersion,
				LibraryVersion:  testLibraryVersion,
			}
			Expect(after.Status.LastAppliedInputs.Digest).To(Equal(want.Digest()))
		})

		// The one-time digest change of adopting the library's
		// opm/k8s/inventory, pinned against the render skip: an instance
		// whose stored digests are in the earlier encoding renders once and
		// applies once under the next operator version, then converges.
		It("applies the library's digests once after an upgrade, then converges", func() {
			ctx := context.Background()
			renderer := &callCountingRenderer{}
			nn := appliedInstance(ctx, "skip-inventory-upgrade-mi", renderer)
			seedPreUpgradeDigests(ctx, nn)
			before := get(ctx, nn)
			calls := renderer.calls.Load()

			By("the first reconcile under the new operator renders and applies once")
			r := newReconciler(renderer, interval)
			r.OperatorVersion = upgradedOperatorVersion
			reconcileOnce(ctx, r, nn)
			Expect(renderer.calls.Load()).To(Equal(calls+1), "the key moved with the operator version")
			after := get(ctx, nn)
			Expect(after.Status.History).To(HaveLen(len(before.Status.History)+1), "one apply")
			Expect(after.Status.Inventory.Revision).To(Equal(before.Status.Inventory.Revision + 1))
			wantRender, wantInventory, _ := libraryDigestsOf(stubRenderResult(namespace, before.Spec.Values))
			Expect(after.Status.LastAppliedRenderDigest).To(Equal(wantRender))
			Expect(after.Status.Inventory.Digest).To(Equal(wantInventory))

			By("the next reconcile under the same operator skips and patches nothing")
			calls = renderer.calls.Load()
			counting, writes := patchCountingClient()
			r.Client = counting
			reconcileOnce(ctx, r, nn)
			Expect(renderer.calls.Load()).To(Equal(calls), "skipped")
			Expect(writes.Load()).To(BeZero(), "nothing patched")

			By("a render after the interval ends NoOp without a second apply")
			old := metav1.NewTime(time.Now().Add(-31 * time.Minute).Truncate(time.Second))
			updateStatus(ctx, nn, func(s *releasesv1alpha1.ModuleInstanceStatus) { s.LastAppliedInputs.RenderedAt = old })
			r.Client = k8sClient
			reconcileOnce(ctx, r, nn)
			Expect(renderer.calls.Load()).To(Equal(calls + 1))
			converged := get(ctx, nn)
			Expect(converged.Status.History).To(HaveLen(len(after.Status.History)), "NoOp: no second apply")
			Expect(converged.Status.Inventory.Revision).To(Equal(after.Status.Inventory.Revision))
		})

		// The documented boundary: the key holds no digest, so a key recorded
		// by the running operator version hides the earlier encoding until
		// the drift render interval passes. Release images always move the
		// version, so this costs only a development image the interval.
		It("keeps the earlier digests under a key from the same version until the interval", func() {
			ctx := context.Background()
			renderer := &callCountingRenderer{}
			nn := appliedInstance(ctx, "skip-inventory-same-version-mi", renderer)
			seedPreUpgradeDigests(ctx, nn)
			before := get(ctx, nn)
			calls := renderer.calls.Load()

			reconcileOnce(ctx, newReconciler(renderer, interval), nn)
			Expect(renderer.calls.Load()).To(Equal(calls), "the key matches, so the render is skipped")

			old := metav1.NewTime(time.Now().Add(-31 * time.Minute).Truncate(time.Second))
			updateStatus(ctx, nn, func(s *releasesv1alpha1.ModuleInstanceStatus) { s.LastAppliedInputs.RenderedAt = old })
			reconcileOnce(ctx, newReconciler(renderer, interval), nn)
			Expect(renderer.calls.Load()).To(Equal(calls + 1))
			after := get(ctx, nn)
			Expect(after.Status.History).To(HaveLen(len(before.Status.History)+1), "the render applies once")
			wantRender, wantInventory, _ := libraryDigestsOf(stubRenderResult(namespace, before.Spec.Values))
			Expect(after.Status.LastAppliedRenderDigest).To(Equal(wantRender))
			Expect(after.Status.Inventory.Digest).To(Equal(wantInventory))
		})

		It("renders when no Platform exists", func() {
			ctx := context.Background()
			renderer := &callCountingRenderer{}
			nn := appliedInstance(ctx, "skip-noplatform-mi", renderer)
			deletePlatform()
			calls := renderer.calls.Load()

			reconcileOnce(ctx, newReconciler(renderer, interval), nn)

			Expect(renderer.calls.Load()).To(Equal(calls + 1))
		})

		It("renders every reconcile when the interval is zero", func() {
			ctx := context.Background()
			renderer := &callCountingRenderer{}
			nn := appliedInstance(ctx, "skip-disabled-mi", renderer)
			before := get(ctx, nn)
			calls := renderer.calls.Load()

			reconcileOnce(ctx, newReconciler(renderer, 0), nn)

			Expect(renderer.calls.Load()).To(Equal(calls + 1))
			after := get(ctx, nn)
			Expect(after.Status.LastAppliedInputs.RenderedAt.Equal(&before.Status.LastAppliedInputs.RenderedAt)).
				To(BeTrue(), "with the skip disabled a NoOp does not move renderedAt")
		})
	})

	Context("ModulePackage", func() {
		const artifactURL = "http://source-controller/artifact.tar.gz"

		setArtifact := func(ctx context.Context, src *sourcev1.OCIRepository, rev, digest string) {
			Eventually(func() error {
				var latest sourcev1.OCIRepository
				if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(src), &latest); err != nil {
					return err
				}
				latest.Status.Conditions = []metav1.Condition{{
					Type: fluxmeta.ReadyCondition, Status: metav1.ConditionTrue,
					Reason: "Succeeded", Message: "ready", LastTransitionTime: metav1.Now(),
				}}
				latest.Status.Artifact = &fluxmeta.Artifact{
					URL: artifactURL, Revision: rev, Digest: digest,
					Path:           "ocirepository/default/" + src.Name + "/" + digest + ".tar.gz",
					LastUpdateTime: metav1.Now(),
				}
				return k8sClient.Status().Update(ctx, &latest)
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
		}

		// appliedPackage creates a package against a gen-1 Platform and an
		// artifact at revision main@sha256:skip, reconciled to a first apply.
		appliedPackage := func(ctx context.Context, name string, fetcher *callCountingFetcher, renderer *callCountingPackageRenderer) (types.NamespacedName, *sourcev1.OCIRepository, *ModulePackageReconciler) {
			createSkipPlatform(ctx, stubPlatformIdentity)
			src := &sourcev1.OCIRepository{
				ObjectMeta: metav1.ObjectMeta{Name: name + "-src", Namespace: namespace},
				Spec: sourcev1.OCIRepositorySpec{
					URL:      "oci://example.com/repo",
					Interval: metav1.Duration{Duration: time.Minute},
				},
			}
			Expect(k8sClient.Create(ctx, src)).To(Succeed())
			setArtifact(ctx, src, "main@sha256:skip", "sha256:skip")
			pkg := &releasesv1alpha1.ModulePackage{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
				Spec: releasesv1alpha1.ModulePackageSpec{
					SourceRef: releasesv1alpha1.SourceReference{Kind: opmsource.SourceKindOCIRepository, Name: name + "-src"},
					Path:      "releases/app",
					Interval:  metav1.Duration{Duration: time.Minute},
					Prune:     true,
				},
			}
			Expect(k8sClient.Create(ctx, pkg)).To(Succeed())
			DeferCleanup(func() {
				Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{Name: "test-module", Namespace: namespace},
				}))).To(Succeed())
				Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, pkg))).To(Succeed())
				Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, src))).To(Succeed())
			})
			r := &ModulePackageReconciler{
				Client:              k8sClient,
				Scheme:              k8sClient.Scheme(),
				ResourceManager:     apply.NewResourceManager(k8sClient, "opm-controller"),
				EventRecorder:       events.NewFakeRecorder(100),
				Fetcher:             fetcher,
				Renderer:            renderer,
				OperatorVersion:     testOperatorVersion,
				LibraryVersion:      testLibraryVersion,
				DriftRenderInterval: interval,
			}
			nn := types.NamespacedName{Name: name, Namespace: namespace}
			for range 2 { // finalizer, then apply
				_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
				Expect(err).NotTo(HaveOccurred())
			}
			var got releasesv1alpha1.ModulePackage
			Expect(k8sClient.Get(ctx, nn, &got)).To(Succeed())
			Expect(got.Status.LastAppliedInputs).NotTo(BeNil(), "the apply records the key")
			return nn, src, r
		}

		newCounters := func() (*callCountingFetcher, *callCountingPackageRenderer) {
			return &callCountingFetcher{stubFetcher: stubFetcher{pathInArtifact: "releases/app"}},
				&callCountingPackageRenderer{stubPackageRenderer: stubPackageRenderer{result: stubRenderResult(namespace, nil)}}
		}

		It("fetches, renders and patches nothing on an unchanged interval requeue", func() {
			ctx := context.Background()
			fetcher, renderer := newCounters()
			nn, _, r := appliedPackage(ctx, "skip-unchanged-pkg", fetcher, renderer)
			var before releasesv1alpha1.ModulePackage
			Expect(k8sClient.Get(ctx, nn, &before)).To(Succeed())
			fetches, renders := fetcher.calls.Load(), renderer.calls.Load()
			counting, writes := patchCountingClient()
			r.Client = counting

			res, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})

			Expect(err).NotTo(HaveOccurred())
			Expect(writes.Load()).To(BeZero(), "no patch or status patch is sent")
			Expect(res.RequeueAfter).To(Equal(time.Minute), "the skip keeps spec.interval")
			Expect(fetcher.calls.Load()).To(Equal(fetches), "no artifact fetch")
			Expect(renderer.calls.Load()).To(Equal(renders), "no render")
			var after releasesv1alpha1.ModulePackage
			Expect(k8sClient.Get(ctx, nn, &after)).To(Succeed())
			Expect(after.ResourceVersion).To(Equal(before.ResourceVersion), "no status patch")
		})

		It("renders when the artifact digest changes", func() {
			ctx := context.Background()
			fetcher, renderer := newCounters()
			nn, src, r := appliedPackage(ctx, "skip-digest-pkg", fetcher, renderer)
			setArtifact(ctx, src, "main@sha256:next", "sha256:next")
			renders := renderer.calls.Load()

			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})

			Expect(err).NotTo(HaveOccurred())
			Expect(renderer.calls.Load()).To(Equal(renders + 1))
		})

		It("renders once and records a new revision whose digest is unchanged", func() {
			ctx := context.Background()
			fetcher, renderer := newCounters()
			nn, src, r := appliedPackage(ctx, "skip-revision-pkg", fetcher, renderer)
			setArtifact(ctx, src, "main@sha256:ignored-paths-only", "sha256:skip")
			var before releasesv1alpha1.ModulePackage
			Expect(k8sClient.Get(ctx, nn, &before)).To(Succeed())
			renders := renderer.calls.Load()

			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			Expect(renderer.calls.Load()).To(Equal(renders + 1))
			var after releasesv1alpha1.ModulePackage
			Expect(k8sClient.Get(ctx, nn, &after)).To(Succeed())
			Expect(after.Status.History).To(HaveLen(len(before.Status.History)), "the render ended NoOp")
			Expect(after.Status.Source.ArtifactRevision).To(Equal("main@sha256:ignored-paths-only"))

			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			Expect(renderer.calls.Load()).To(Equal(renders+1), "once recorded, the next requeue skips")
		})

		It("applies the library's digests once after an upgrade, then converges", func() {
			ctx := context.Background()
			fetcher, renderer := newCounters()
			nn, _, r := appliedPackage(ctx, "skip-inventory-upgrade-pkg", fetcher, renderer)
			var before releasesv1alpha1.ModulePackage
			Expect(k8sClient.Get(ctx, nn, &before)).To(Succeed())
			wantRender, wantInventory, exported := libraryDigestsOf(stubRenderResult(namespace, nil))
			Expect(before.Status.LastAppliedRenderDigest).To(Equal(wantRender))
			Expect(before.Status.Inventory.Digest).To(Equal(wantInventory))
			oldRender := preUpgradeRenderDigest(exported)
			oldInventory := preUpgradeInventoryDigest(before.Status.Inventory.Entries)
			Eventually(func() error {
				var pkg releasesv1alpha1.ModulePackage
				if err := k8sClient.Get(ctx, nn, &pkg); err != nil {
					return err
				}
				pkg.Status.LastAppliedRenderDigest = oldRender
				pkg.Status.LastAttemptedRenderDigest = oldRender
				pkg.Status.Inventory.Digest = oldInventory
				return k8sClient.Status().Update(ctx, &pkg)
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
			Expect(k8sClient.Get(ctx, nn, &before)).To(Succeed())
			renders := renderer.calls.Load()

			By("the first reconcile under the new operator renders and applies once")
			r.OperatorVersion = upgradedOperatorVersion
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			Expect(renderer.calls.Load()).To(Equal(renders + 1))
			var after releasesv1alpha1.ModulePackage
			Expect(k8sClient.Get(ctx, nn, &after)).To(Succeed())
			Expect(after.Status.History).To(HaveLen(len(before.Status.History)+1), "one apply")
			Expect(after.Status.Inventory.Revision).To(Equal(before.Status.Inventory.Revision + 1))
			Expect(after.Status.LastAppliedRenderDigest).To(Equal(wantRender))
			Expect(after.Status.Inventory.Digest).To(Equal(wantInventory))

			By("the next reconcile under the same operator skips and patches nothing")
			counting, writes := patchCountingClient()
			r.Client = counting
			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			Expect(renderer.calls.Load()).To(Equal(renders+1), "skipped")
			Expect(writes.Load()).To(BeZero(), "nothing patched")

			By("a render after the interval ends NoOp without a second apply")
			old := metav1.NewTime(time.Now().Add(-31 * time.Minute).Truncate(time.Second))
			Eventually(func() error {
				var pkg releasesv1alpha1.ModulePackage
				if err := k8sClient.Get(ctx, nn, &pkg); err != nil {
					return err
				}
				pkg.Status.LastAppliedInputs.RenderedAt = old
				return k8sClient.Status().Update(ctx, &pkg)
			}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
			r.Client = k8sClient
			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			Expect(renderer.calls.Load()).To(Equal(renders + 2))
			var converged releasesv1alpha1.ModulePackage
			Expect(k8sClient.Get(ctx, nn, &converged)).To(Succeed())
			Expect(converged.Status.History).To(HaveLen(len(after.Status.History)), "NoOp: no second apply")
			Expect(converged.Status.Inventory.Revision).To(Equal(after.Status.Inventory.Revision))
		})
	})
})
