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
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"cuelang.org/go/cue/cuecontext"
	appsv1 "k8s.io/api/apps/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/open-platform-model/library/opm/k8s/labels"
	"github.com/open-platform-model/library/opm/k8s/object"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	"github.com/open-platform-model/opm-operator/internal/apply"
	opmreconcile "github.com/open-platform-model/opm-operator/internal/reconcile"
	"github.com/open-platform-model/opm-operator/internal/render"
	opmsource "github.com/open-platform-model/opm-operator/internal/source"
	"github.com/open-platform-model/opm-operator/internal/status"
)

// deploymentResource renders one Deployment that carries the labels of an
// object the stub instance owns.
func deploymentResource(name string) *object.Resource {
	v := cuecontext.New().CompileString(fmt.Sprintf(`{
	apiVersion: "apps/v1"
	kind:       "Deployment"
	metadata: {
		name:      %q
		namespace: %q
		labels: {
			%q: %q
			%q: %q
			%q: %q
		}
	}
	spec: {
		selector: matchLabels: app: %q
		template: {
			metadata: labels: app: %q
			spec: containers: [{name: "app", image: "registry.invalid/app:1"}]
		}
	}
}`, name, namespace,
		labels.ManagedBy, labels.ManagedByController,
		labels.ModuleInstanceNamespace, namespace,
		labels.ModuleInstanceUUID, stubInstanceUUID,
		name, name))
	if v.Err() != nil {
		panic(fmt.Sprintf("compiling stub Deployment: %v", v.Err()))
	}
	return &object.Resource{Value: v, Instance: name, Component: name, Transformer: "kubernetes#simple"}
}

// withDeployment renders the named ConfigMap and, when deployment is set, a
// Deployment of that name.
func withDeployment(configMap, deployment string) *render.RenderResult {
	result := namedConfigMapRenderResult(configMap)
	if deployment != "" {
		result.Resources = append(result.Resources, deploymentResource(deployment))
	}
	return result
}

// expectTerminatingInForeground asserts that the Deployment still exists and
// is being deleted with Foreground propagation: it carries a
// deletionTimestamp and the foregroundDeletion finalizer.
func expectTerminatingInForeground(name string) {
	GinkgoHelper()
	var deployment appsv1.Deployment
	Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, &deployment)).To(Succeed(),
		"the Deployment must still exist while nothing collects it")
	Expect(deployment.DeletionTimestamp.IsZero()).To(BeFalse(), "the Deployment must be terminating")
	Expect(deployment.Finalizers).To(ContainElement(metav1.FinalizerDeleteDependents),
		"the delete must carry Foreground propagation")
}

// A stale prune deletes with Foreground propagation and does not wait: the
// reconcile ends Ready while the object still terminates, and the inventory
// no longer lists it.
var _ = Describe("Stale prune with Foreground propagation", func() {
	removeDeployment := func(name string) {
		GinkgoHelper()
		deployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace}}
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, deployment))).To(Succeed())
		expectGone(client.ObjectKeyFromObject(deployment), &appsv1.Deployment{})
	}

	It("deletes a stale Deployment of a ModuleInstance and ends Ready while it terminates", func() {
		params := reconcileParams()
		params.EventRecorder = &actionRecorder{}
		nn := createClaimInstance("fg-prune-instance", true, "", params)
		DeferCleanup(func() {
			removeDeployment("fgi-web")
			removeConfigMaps("fgi-config")
			cleanupInstance(nn)
		})

		params.Renderer = &stubRenderer{result: withDeployment("fgi-config", "fgi-web")}
		Expect(reconcileInstance(params, nn)).To(Succeed())
		var mi releasesv1alpha1.ModuleInstance
		Expect(k8sClient.Get(ctx, nn, &mi)).To(Succeed())
		Expect(inventoryNames(mi.Status.Inventory)).To(ConsistOf("ConfigMap/fgi-config", "Deployment/fgi-web"))

		By("holding the namespace's terminating objects, as dependents that are not gone yet would")
		resume := gc.Pause(namespace)
		DeferCleanup(resume)

		By("the render drops the Deployment")
		params.Renderer = &stubRenderer{result: withDeployment("fgi-config", "")}
		Expect(reconcileInstance(params, nn)).To(Succeed())

		expectTerminatingInForeground("fgi-web")
		Expect(k8sClient.Get(ctx, nn, &mi)).To(Succeed())
		ready := apimeta.FindStatusCondition(mi.Status.Conditions, status.ReadyCondition)
		Expect(ready).NotTo(BeNil())
		Expect(ready.Status).To(Equal(metav1.ConditionTrue), "reason=%s message=%s", ready.Reason, ready.Message)
		Expect(inventoryNames(mi.Status.Inventory)).To(ConsistOf("ConfigMap/fgi-config"),
			"the stale entry leaves the inventory when its delete is accepted")

		By("a further reconcile changes nothing: the prune does not wait")
		Expect(reconcileInstance(params, nn)).To(Succeed())
		expectTerminatingInForeground("fgi-web")

		By("the collector runs again")
		resume()
		expectGone(types.NamespacedName{Name: "fgi-web", Namespace: namespace}, &appsv1.Deployment{})
	})

	It("deletes a stale Deployment of a ModulePackage and ends Ready while it terminates", func() {
		const sourceName = "fg-prune-src"
		realClient, err := client.NewWithWatch(cfg, client.Options{Scheme: k8sClient.Scheme()})
		Expect(err).NotTo(HaveOccurred())
		params := &opmreconcile.ModulePackageParams{
			Client:          readySource(realClient, sourceName),
			ResourceManager: apply.NewResourceManager(k8sClient, "opm-controller"),
			EventRecorder:   &actionRecorder{},
			Fetcher:         packageDirFetcher{path: "releases/app"},
		}
		nn := types.NamespacedName{Name: "fg-prune-package", Namespace: namespace}
		reconcilePackage := func(r *render.RenderResult) ctrl.Result {
			GinkgoHelper()
			params.Renderer = &stubPackageRenderer{result: r}
			result, err := opmreconcile.ReconcileModulePackage(ctx, params, ctrl.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			return result
		}

		pkg := &releasesv1alpha1.ModulePackage{
			ObjectMeta: metav1.ObjectMeta{Name: nn.Name, Namespace: nn.Namespace},
			Spec: releasesv1alpha1.ModulePackageSpec{
				SourceRef: releasesv1alpha1.SourceReference{Kind: opmsource.SourceKindOCIRepository, Name: sourceName},
				Path:      "releases/app",
				Interval:  metav1.Duration{Duration: time.Minute},
				Prune:     true,
			},
		}
		Expect(k8sClient.Create(ctx, pkg)).To(Succeed())
		DeferCleanup(func() {
			removeDeployment("fgp-web")
			removeConfigMaps("fgp-config")
			var current releasesv1alpha1.ModulePackage
			Expect(k8sClient.Get(ctx, nn, &current)).To(Succeed())
			current.Finalizers = nil
			Expect(k8sClient.Update(ctx, &current)).To(Succeed())
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &current))).To(Succeed())
		})
		Expect(reconcilePackage(withDeployment("fgp-config", "fgp-web"))).To(Equal(ctrl.Result{Requeue: true}),
			"the first reconcile registers the finalizer")

		reconcilePackage(withDeployment("fgp-config", "fgp-web"))
		var current releasesv1alpha1.ModulePackage
		Expect(k8sClient.Get(ctx, nn, &current)).To(Succeed())
		Expect(inventoryNames(current.Status.Inventory)).To(ConsistOf("ConfigMap/fgp-config", "Deployment/fgp-web"))

		By("holding the namespace's terminating objects, as dependents that are not gone yet would")
		resume := gc.Pause(namespace)
		DeferCleanup(resume)

		By("the render drops the Deployment")
		reconcilePackage(withDeployment("fgp-config", ""))

		expectTerminatingInForeground("fgp-web")
		Expect(k8sClient.Get(ctx, nn, &current)).To(Succeed())
		ready := apimeta.FindStatusCondition(current.Status.Conditions, status.ReadyCondition)
		Expect(ready).NotTo(BeNil())
		Expect(ready.Status).To(Equal(metav1.ConditionTrue), "reason=%s message=%s", ready.Reason, ready.Message)
		Expect(inventoryNames(current.Status.Inventory)).To(ConsistOf("ConfigMap/fgp-config"),
			"the stale entry leaves the inventory when its delete is accepted")

		By("the collector runs again")
		resume()
		expectGone(types.NamespacedName{Name: "fgp-web", Namespace: namespace}, &appsv1.Deployment{})
	})
})
