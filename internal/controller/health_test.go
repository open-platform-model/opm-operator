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
	"time"

	"cuelang.org/go/cue/cuecontext"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/open-platform-model/library/opm/k8s/labels"
	"github.com/open-platform-model/library/opm/k8s/object"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	"github.com/open-platform-model/opm-operator/internal/apply"
	opmreconcile "github.com/open-platform-model/opm-operator/internal/reconcile"
	"github.com/open-platform-model/opm-operator/internal/render"
	"github.com/open-platform-model/opm-operator/internal/status"
)

// healthDeploymentName is the Deployment deploymentRenderResult renders.
const healthDeploymentName = "health-web"

// deploymentRenderResult is stubRenderResult's ConfigMap plus a one-replica
// Deployment named healthDeploymentName, so a render has an object whose
// rollout the Healthy condition follows. Envtest runs no workload
// controller: the specs write the Deployment's status by hand.
func deploymentRenderResult(namespace string) *render.RenderResult {
	result := stubRenderResult(namespace, nil)
	dep := cuecontext.New().CompileString(fmt.Sprintf(`{
	apiVersion: "apps/v1"
	kind:       "Deployment"
	metadata: {
		name:      %q
		namespace: %q
		labels: {
			%q: %q
			%q: %q
		}
	}
	spec: {
		replicas: 1
		selector: matchLabels: app: %q
		template: {
			metadata: labels: app: %q
			spec: containers: [{name: "web", image: "example.invalid/web:1"}]
		}
	}
}`, healthDeploymentName, namespace,
		labels.ManagedBy, labels.ManagedByController,
		labels.ModuleInstanceNamespace, namespace,
		healthDeploymentName, healthDeploymentName))
	if dep.Err() != nil {
		panic(fmt.Sprintf("compiling stub Deployment: %v", dep.Err()))
	}
	result.Resources = append(result.Resources, &object.Resource{
		Value:       dep,
		Instance:    "test-module",
		Component:   "web",
		Transformer: "kubernetes#simple",
	})
	return result
}

// setDeploymentStatus writes the status of the health-web Deployment in the
// default namespace as its controller would: rolledOut reports every replica
// updated and available at the observed generation; stalled reports
// Progressing with reason ProgressDeadlineExceeded at the observed
// generation.
func setDeploymentStatus(ctx context.Context, rolledOut, stalled bool) {
	GinkgoHelper()
	Eventually(func() error {
		var dep appsv1.Deployment
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: healthDeploymentName, Namespace: "default"}, &dep); err != nil {
			return err
		}
		dep.Status = appsv1.DeploymentStatus{ObservedGeneration: dep.Generation}
		if rolledOut {
			dep.Status.Replicas, dep.Status.UpdatedReplicas = 1, 1
			dep.Status.ReadyReplicas, dep.Status.AvailableReplicas = 1, 1
		}
		if stalled {
			dep.Status.Conditions = []appsv1.DeploymentCondition{{
				Type: appsv1.DeploymentProgressing, Status: corev1.ConditionFalse,
				Reason: "ProgressDeadlineExceeded", Message: "stalled",
			}}
		}
		return k8sClient.Status().Update(ctx, &dep)
	}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
}

// The Healthy condition (instance-health): judged by the library's
// opm/k8s/health from uncached reads taken after the apply, on every
// reconcile that leaves the instance Ready, and requeued until rolled out.
var _ = Describe("ModuleInstance Healthy condition", func() {
	const (
		namespace = "default"
		interval  = 30 * time.Minute
	)

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
		GinkgoHelper()
		var mi releasesv1alpha1.ModuleInstance
		Expect(k8sClient.Get(ctx, nn, &mi)).To(Succeed())
		return &mi
	}

	healthy := func(mi *releasesv1alpha1.ModuleInstance) *metav1.Condition {
		return apimeta.FindStatusCondition(mi.Status.Conditions, status.HealthyCondition)
	}

	reconcileOnce := func(ctx context.Context, r *ModuleInstanceReconciler, nn types.NamespacedName) reconcile.Result {
		GinkgoHelper()
		res, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())
		return res
	}

	// newInstance creates an instance against a gen-1 Platform, registers
	// its cleanup, and reconciles the finalizer on.
	newInstance := func(ctx context.Context, name string, r *ModuleInstanceReconciler) types.NamespacedName {
		GinkgoHelper()
		createSkipPlatform(ctx, stubPlatformIdentity)
		mi := &releasesv1alpha1.ModuleInstance{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
			Spec: releasesv1alpha1.ModuleInstanceSpec{
				Module: releasesv1alpha1.ModuleReference{Path: "opmodel.dev/test/module", Version: "v0.1.0"},
			},
		}
		Expect(k8sClient.Create(ctx, mi)).To(Succeed())
		nn := client.ObjectKeyFromObject(mi)
		DeferCleanup(func() {
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "test-module", Namespace: namespace},
			}))).To(Succeed())
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Name: healthDeploymentName, Namespace: namespace},
			}))).To(Succeed())
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &releasesv1alpha1.ModuleInstance{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
			}))).To(Succeed())
		})
		reconcileOnce(ctx, r, nn) // finalizer
		return nn
	}

	It("reports a ConfigMap-only instance RolledOut without a requeue", func() {
		ctx := context.Background()
		r := newReconciler(&stubRenderer{}, interval)
		nn := newInstance(ctx, "health-configmap-mi", r)

		res := reconcileOnce(ctx, r, nn)

		Expect(res).To(Equal(reconcile.Result{}))
		mi := get(ctx, nn)
		Expect(apimeta.IsStatusConditionTrue(mi.Status.Conditions, status.ReadyCondition)).To(BeTrue())
		h := healthy(mi)
		Expect(h).NotTo(BeNil())
		Expect(h.Status).To(Equal(metav1.ConditionTrue))
		Expect(h.Reason).To(Equal(status.RolledOutReason))
		Expect(h.Message).To(Equal("1/1 objects ready"))
	})

	It("requeues a rollout in progress and records the rollout on a skipped render", func() {
		ctx := context.Background()
		renderer := &callCountingRenderer{stubRenderer: stubRenderer{result: deploymentRenderResult(namespace)}}
		r := newReconciler(renderer, interval)
		nn := newInstance(ctx, "health-rollout-mi", r)

		By("the apply leaves Ready=True and Healthy=False while the Deployment rolls out")
		res := reconcileOnce(ctx, r, nn)
		Expect(res.RequeueAfter).To(Equal(5 * time.Second))
		applied := get(ctx, nn)
		ready := apimeta.FindStatusCondition(applied.Status.Conditions, status.ReadyCondition)
		Expect(ready.Status).To(Equal(metav1.ConditionTrue))
		Expect(ready.Reason).To(Equal(status.ReconciliationSucceededReason))
		h := healthy(applied)
		Expect(h.Status).To(Equal(metav1.ConditionFalse))
		Expect(h.Reason).To(Equal(status.NotRolledOutReason))
		Expect(h.Message).To(Equal(fmt.Sprintf("1/2 objects ready: Deployment %s/%s (NotReady)", namespace, healthDeploymentName)))
		Expect(applied.Status.NextRetryAt).To(BeNil(), "a health requeue is not a failure")

		By("once the Deployment has rolled out, the skipped render patches only Healthy")
		setDeploymentStatus(ctx, true, false)
		calls := renderer.calls.Load()
		counting, writes := patchCountingClient()
		r.Client = counting
		res = reconcileOnce(ctx, r, nn)
		r.Client = k8sClient

		Expect(renderer.calls.Load()).To(Equal(calls), "the render is skipped")
		Expect(res).To(Equal(reconcile.Result{}), "a rolled-out instance does not requeue")
		// The patch helper writes conditions and the rest of status in
		// separate requests; what was written is checked field by field below.
		Expect(writes.Load()).To(BeNumerically(">", 0), "the changed condition is patched")
		after := get(ctx, nn)
		h = healthy(after)
		Expect(h.Status).To(Equal(metav1.ConditionTrue))
		Expect(h.Reason).To(Equal(status.RolledOutReason))
		Expect(after.Status.ObservedGeneration).To(Equal(applied.Status.ObservedGeneration))
		Expect(after.Status.LastAttemptedAt).To(Equal(applied.Status.LastAttemptedAt))
		Expect(after.Status.History).To(Equal(applied.Status.History))
		Expect(after.Status.LastAppliedInputs.RenderedAt.Equal(&applied.Status.LastAppliedInputs.RenderedAt)).To(BeTrue())
		Expect(apimeta.FindStatusCondition(after.Status.Conditions, status.ReadyCondition)).To(Equal(ready))

		By("judging the same state again patches nothing")
		counting, writes = patchCountingClient()
		r.Client = counting
		reconcileOnce(ctx, r, nn)
		r.Client = k8sClient
		Expect(writes.Load()).To(BeZero())
	})

	It("stops the fast requeue for a stalled Deployment", func() {
		ctx := context.Background()
		renderer := &callCountingRenderer{stubRenderer: stubRenderer{result: deploymentRenderResult(namespace)}}
		r := newReconciler(renderer, interval)
		nn := newInstance(ctx, "health-stalled-mi", r)
		reconcileOnce(ctx, r, nn)

		setDeploymentStatus(ctx, false, true)
		res := reconcileOnce(ctx, r, nn)

		Expect(res.RequeueAfter).To(Equal(opmreconcile.StalledRecheckInterval))
		h := healthy(get(ctx, nn))
		Expect(h.Status).To(Equal(metav1.ConditionFalse))
		Expect(h.Reason).To(Equal(status.ProgressDeadlineExceededReason))
		Expect(h.Message).To(ContainSubstring("Deployment " + namespace + "/" + healthDeploymentName))
	})

	It("re-judges on a NoOp", func() {
		ctx := context.Background()
		renderer := &callCountingRenderer{stubRenderer: stubRenderer{result: deploymentRenderResult(namespace)}}
		// The skip is disabled, so every reconcile renders and ends NoOp.
		r := newReconciler(renderer, 0)
		nn := newInstance(ctx, "health-noop-mi", r)
		reconcileOnce(ctx, r, nn)
		setDeploymentStatus(ctx, true, false)
		reconcileOnce(ctx, r, nn)
		Expect(healthy(get(ctx, nn)).Reason).To(Equal(status.RolledOutReason))
		historyBefore := len(get(ctx, nn).Status.History)

		By("an available replica is lost")
		setDeploymentStatus(ctx, false, false)
		calls := renderer.calls.Load()
		res := reconcileOnce(ctx, r, nn)

		Expect(renderer.calls.Load()).To(Equal(calls + 1))
		after := get(ctx, nn)
		Expect(after.Status.History).To(HaveLen(historyBefore), "the render ended NoOp")
		Expect(healthy(after).Reason).To(Equal(status.NotRolledOutReason))
		Expect(res.RequeueAfter).To(BeNumerically(">=", 5*time.Second))
		Expect(res.RequeueAfter).To(BeNumerically("<=", 2*time.Minute))
	})

	It("reports an object deleted out of band as Missing, across a NoOp", func() {
		ctx := context.Background()
		r := newReconciler(&stubRenderer{}, 0)
		nn := newInstance(ctx, "health-missing-mi", r)
		reconcileOnce(ctx, r, nn)
		Expect(healthy(get(ctx, nn)).Reason).To(Equal(status.RolledOutReason))
		historyBefore := len(get(ctx, nn).Status.History)

		Expect(k8sClient.Delete(ctx, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "test-module", Namespace: namespace},
		})).To(Succeed())
		reconcileOnce(ctx, r, nn)

		after := get(ctx, nn)
		Expect(after.Status.History).To(HaveLen(historyBefore), "the render ended NoOp and re-applied nothing")
		h := healthy(after)
		Expect(h.Status).To(Equal(metav1.ConditionFalse))
		Expect(h.Reason).To(Equal(status.NotRolledOutReason))
		Expect(h.Message).To(Equal("0/1 objects ready: ConfigMap " + namespace + "/test-module (Missing)"))
	})

	It("leaves Healthy as it was when a render fails", func() {
		ctx := context.Background()
		renderer := &stubRenderer{}
		r := newReconciler(renderer, 0)
		nn := newInstance(ctx, "health-failed-mi", r)
		reconcileOnce(ctx, r, nn)
		before := healthy(get(ctx, nn))
		Expect(before.Reason).To(Equal(status.RolledOutReason))

		renderer.err = errors.New("injected render failure")
		reconcileOnce(ctx, r, nn)

		after := get(ctx, nn)
		Expect(apimeta.IsStatusConditionTrue(after.Status.Conditions, status.ReadyCondition)).To(BeFalse())
		Expect(healthy(after)).To(Equal(before))
	})
})
