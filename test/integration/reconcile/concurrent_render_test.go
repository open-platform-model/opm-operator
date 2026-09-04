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
	"fmt"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"cuelang.org/go/cue/cuecontext"
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
	"github.com/open-platform-model/opm-operator/internal/inventory"
	"github.com/open-platform-model/opm-operator/internal/render"
	"github.com/open-platform-model/opm-operator/internal/status"
	"github.com/open-platform-model/opm-operator/pkg/core"
)

// rendezvousRenderer proves two renders overlap: each RenderModule call
// arrives at a barrier sized for two and only proceeds once the other call
// has arrived too. Under MaxConcurrentReconciles: 1 the second call could
// never arrive while the first waits, so the barrier would time out and both
// renders would fail; under 2 both pass. Each instance renders a ConfigMap
// named after itself so the two inventories do not overlap.
type rendezvousRenderer struct {
	barrier sync.WaitGroup
	timeout time.Duration
}

func newRendezvousRenderer(parties int, timeout time.Duration) *rendezvousRenderer {
	r := &rendezvousRenderer{timeout: timeout}
	r.barrier.Add(parties)
	return r
}

func (r *rendezvousRenderer) RenderModule(
	ctx context.Context,
	name, ns, _, _ string,
	_ *releasesv1alpha1.RawValues,
) (*render.RenderResult, error) {
	r.barrier.Done()
	arrived := make(chan struct{})
	go func() { r.barrier.Wait(); close(arrived) }()
	select {
	case <-arrived:
	case <-time.After(r.timeout):
		return nil, fmt.Errorf("render of %s waited %s for a concurrent render that never arrived", name, r.timeout)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return namedConfigMapResult(name, ns), nil
}

// namedConfigMapResult is a stub render result whose ConfigMap is named after
// the instance, carrying the instance's own uuid label.
func namedConfigMapResult(name, namespace string) *render.RenderResult {
	cm := cuecontext.New().CompileString(fmt.Sprintf(`{
	apiVersion: "v1"
	kind:       "ConfigMap"
	metadata: {
		name:      %q
		namespace: %q
		labels: {
			%q: %q
			%q: %q
			%q: %q
		}
	}
	data: instance: %q
}`, name, namespace,
		core.LabelManagedBy, core.LabelManagedByControllerValue,
		core.LabelModuleInstanceNamespace, namespace,
		core.LabelModuleInstanceUUID, "00000000-0000-0000-0000-0000000"+name[len(name)-5:],
		name))
	Expect(cm.Err()).NotTo(HaveOccurred())
	resource := &core.Resource{Value: cm, Instance: name, Component: "hello", Transformer: "kubernetes#simple"}
	u, err := resource.ToUnstructured()
	Expect(err).NotTo(HaveOccurred())
	return &render.RenderResult{
		Resources:        []*core.Resource{resource},
		InventoryEntries: []releasesv1alpha1.InventoryEntry{inventory.NewEntryFromResource(u)},
	}
}

// The manager-driven proof of --max-concurrent-renders (0019 D8, spec
// library-kernel-runtime "Two renders overlap"): with MaxConcurrentRenders 2
// two ModuleInstances render at the same time and both reach Ready. The
// renderer is a barrier, so the spec fails rather than passes vacuously if
// the option stops reaching the controller.
var _ = Describe("Concurrent renders (manager-driven)", func() {
	const (
		concurrentNamespace = "concurrent-render-ns"
		instanceA           = "concurrent-mi-00001"
		instanceB           = "concurrent-mi-00002"
	)

	It("renders two ModuleInstances at once under MaxConcurrentRenders 2 and both become Ready", func() {
		mgrCtx, cancelMgr := context.WithCancel(ctx)
		defer cancelMgr()

		Expect(client.IgnoreAlreadyExists(k8sClient.Create(ctx, &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: concurrentNamespace},
		}))).To(Succeed())

		// Sibling specs also run a manager with a controller named
		// "moduleinstance"; controller-runtime enforces process-global name
		// uniqueness, so skip that check for this test-only manager.
		skipNameValidation := true
		mgr, err := ctrl.NewManager(cfg, ctrl.Options{
			Scheme:                 scheme.Scheme,
			LeaderElection:         false,
			Metrics:                metricsserver.Options{BindAddress: "0"},
			HealthProbeBindAddress: "0",
			Controller:             config.Controller{SkipNameValidation: &skipNameValidation},
		})
		Expect(err).NotTo(HaveOccurred())

		reconciler := &opmcontroller.ModuleInstanceReconciler{
			Client:               mgr.GetClient(),
			APIReader:            mgr.GetAPIReader(),
			Scheme:               mgr.GetScheme(),
			RestConfig:           cfg,
			ResourceManager:      apply.NewResourceManager(mgr.GetClient(), "opm-controller"),
			EventRecorder:        events.NewFakeRecorder(64),
			Renderer:             newRendezvousRenderer(2, 15*time.Second),
			MaxConcurrentRenders: 2,
		}
		Expect(reconciler.SetupWithManager(mgr)).To(Succeed())

		go func() {
			defer GinkgoRecover()
			_ = mgr.Start(mgrCtx)
		}()

		for _, name := range []string{instanceA, instanceB} {
			mi := &releasesv1alpha1.ModuleInstance{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: concurrentNamespace},
				Spec: releasesv1alpha1.ModuleInstanceSpec{
					Module: releasesv1alpha1.ModuleReference{Path: "opmodel.dev/test/module", Version: "v0.1.0"},
				},
			}
			Expect(k8sClient.Create(ctx, mi)).To(Succeed())
		}

		for _, name := range []string{instanceA, instanceB} {
			nn := types.NamespacedName{Name: name, Namespace: concurrentNamespace}
			Eventually(func(g Gomega) {
				var current releasesv1alpha1.ModuleInstance
				g.Expect(k8sClient.Get(ctx, nn, &current)).To(Succeed())
				ready := meta.FindStatusCondition(current.Status.Conditions, status.ReadyCondition)
				g.Expect(ready).NotTo(BeNil())
				g.Expect(ready.Status).To(Equal(metav1.ConditionTrue), "reason=%s message=%s", ready.Reason, ready.Message)
			}).WithTimeout(30 * time.Second).WithPolling(250 * time.Millisecond).Should(Succeed())
			Expect(k8sClient.Get(ctx, nn, &corev1.ConfigMap{})).To(Succeed(), "%s's ConfigMap is applied", name)
		}

		// Cleanup: delete both instances (the finalizer prunes their ConfigMaps).
		for _, name := range []string{instanceA, instanceB} {
			nn := types.NamespacedName{Name: name, Namespace: concurrentNamespace}
			Expect(k8sClient.Delete(ctx, &releasesv1alpha1.ModuleInstance{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: concurrentNamespace},
			})).To(Succeed())
			Eventually(func() bool {
				var current releasesv1alpha1.ModuleInstance
				return k8sClient.Get(ctx, nn, &current) != nil
			}).WithTimeout(15 * time.Second).WithPolling(250 * time.Millisecond).Should(BeTrue())
		}
	})
})
