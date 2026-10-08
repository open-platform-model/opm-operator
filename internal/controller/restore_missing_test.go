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
	"time"

	"cuelang.org/go/cue/cuecontext"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/open-platform-model/library/opm/k8s/labels"
	"github.com/open-platform-model/library/opm/k8s/object"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	"github.com/open-platform-model/opm-operator/internal/apply"
	opmreconcile "github.com/open-platform-model/opm-operator/internal/reconcile"
	"github.com/open-platform-model/opm-operator/internal/render"
	"github.com/open-platform-model/opm-operator/internal/status"
)

// configMapResource is a rendered ConfigMap named name with data.message.
func configMapResource(name, message string) *object.Resource {
	cm := cuecontext.New().CompileString(fmt.Sprintf(`{
	apiVersion: "v1"
	kind:       "ConfigMap"
	metadata: {
		name:      %q
		namespace: %q
		labels: {
			%q: %q
			%q: %q
		}
	}
	data: message: %q
}`, name, periodicNamespace,
		labels.ManagedBy, labels.ManagedByController,
		labels.ModuleInstanceNamespace, periodicNamespace,
		message))
	Expect(cm.Err()).NotTo(HaveOccurred())
	return &object.Resource{Value: cm, Instance: "test-module", Component: "hello", Transformer: "kubernetes#simple"}
}

// jobResource is a rendered Job named name that sets ttlSecondsAfterFinished.
func jobResource(name string) *object.Resource {
	job := cuecontext.New().CompileString(fmt.Sprintf(`{
	apiVersion: "batch/v1"
	kind:       "Job"
	metadata: {
		name:      %q
		namespace: %q
		labels: {
			%q: %q
			%q: %q
		}
	}
	spec: {
		ttlSecondsAfterFinished: 100
		template: spec: {
			restartPolicy: "Never"
			containers: [{name: "run", image: "busybox"}]
		}
	}
}`, name, periodicNamespace,
		labels.ManagedBy, labels.ManagedByController,
		labels.ModuleInstanceNamespace, periodicNamespace))
	Expect(job.Err()).NotTo(HaveOccurred())
	return &object.Resource{Value: job, Instance: "test-module", Component: "hello", Transformer: "kubernetes#simple"}
}

// A rendering reconcile with unchanged digests creates the rendered objects
// the cluster lacks, and only those.
var _ = Describe("Restore of a missing ModuleInstance object", func() {
	reconcileOnce := func(ctx context.Context, r *ModuleInstanceReconciler, nn types.NamespacedName) reconcile.Result {
		res, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())
		return res
	}

	get := func(ctx context.Context, nn types.NamespacedName) *releasesv1alpha1.ModuleInstance {
		var mi releasesv1alpha1.ModuleInstance
		Expect(k8sClient.Get(ctx, nn, &mi)).To(Succeed())
		return &mi
	}

	configMap := func(ctx context.Context, name string) (*corev1.ConfigMap, error) {
		var cm corev1.ConfigMap
		err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: periodicNamespace}, &cm)
		return &cm, err
	}

	// deleteConfigMap deletes the ConfigMap "test-module" as a user would.
	deleteConfigMap := func(ctx context.Context) {
		Expect(k8sClient.Delete(ctx, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "test-module", Namespace: periodicNamespace},
		})).To(Succeed())
	}

	It("creates a deleted object again and records an apply", func() {
		ctx := context.Background()
		nn := createPeriodicInstance(ctx, "restore-deleted-mi")
		renderer := &callCountingRenderer{}
		r := periodicReconciler(renderer, 0) // every reconcile renders

		reconcileOnce(ctx, r, nn) // finalizer
		reconcileOnce(ctx, r, nn) // apply
		applied := get(ctx, nn)
		Expect(applied.Status.History).To(HaveLen(1))

		deleteConfigMap(ctx)
		res := reconcileOnce(ctx, r, nn)

		cm, err := configMap(ctx, "test-module")
		Expect(err).NotTo(HaveOccurred(), "the deleted ConfigMap exists again")
		Expect(cm.Data).To(HaveKeyWithValue("message", "hello"))

		restored := get(ctx, nn)
		Expect(restored.Status.History).To(HaveLen(2), "a restore is recorded as an apply")
		Expect(restored.Status.LastAppliedAt.Time).NotTo(BeTemporally("<", applied.Status.LastAppliedAt.Time))
		Expect(restored.Status.Inventory.Revision).To(Equal(applied.Status.Inventory.Revision + 1))
		Expect(restored.Status.Inventory.Digest).To(Equal(applied.Status.Inventory.Digest))
		Expect(restored.Status.LastAppliedRenderDigest).To(Equal(applied.Status.LastAppliedRenderDigest))
		ready := apimeta.FindStatusCondition(restored.Status.Conditions, status.ReadyCondition)
		Expect(ready).NotTo(BeNil())
		Expect(ready.Status).To(Equal(metav1.ConditionTrue))
		Expect(res.RequeueAfter).To(beThePeriodicRequeue(), "the restored instance is rolled out")

		// With nothing missing the next reconcile is a NoOp again.
		reconcileOnce(ctx, r, nn)
		Expect(get(ctx, nn).Status.History).To(HaveLen(2), "an unchanged instance records no apply")
	})

	It("applies only the missing object and leaves a drifted one reported", func() {
		ctx := context.Background()
		nn := createPeriodicInstance(ctx, "restore-only-missing-mi")
		DeferCleanup(func() {
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "restore-kept", Namespace: periodicNamespace},
			}))).To(Succeed())
		})
		renderer := &callCountingRenderer{}
		renderer.result = &render.RenderResult{
			Resources: []*object.Resource{
				configMapResource("test-module", "hello"),
				configMapResource("restore-kept", "rendered"),
			},
			ModuleVersion:    stubModuleVersion,
			PlatformIdentity: stubPlatformIdentity,
			SkewPolicy:       stubSkewPolicy,
		}
		// The reconcile drops the resources of the result it is handed, so
		// each render gets a fresh list.
		fresh := func() {
			renderer.result.Resources = []*object.Resource{
				configMapResource("test-module", "hello"),
				configMapResource("restore-kept", "rendered"),
			}
		}
		r := periodicReconciler(renderer, 0)

		reconcileOnce(ctx, r, nn) // finalizer
		reconcileOnce(ctx, r, nn) // apply

		kept, err := configMap(ctx, "restore-kept")
		Expect(err).NotTo(HaveOccurred())
		kept.Data["message"] = "edited-by-hand"
		Expect(k8sClient.Update(ctx, kept)).To(Succeed())
		deleteConfigMap(ctx)

		fresh()
		reconcileOnce(ctx, r, nn)

		_, err = configMap(ctx, "test-module")
		Expect(err).NotTo(HaveOccurred(), "the deleted ConfigMap exists again")
		kept, err = configMap(ctx, "restore-kept")
		Expect(err).NotTo(HaveOccurred())
		Expect(kept.Data).To(HaveKeyWithValue("message", "edited-by-hand"), "the restore does not correct drift")

		drifted := apimeta.FindStatusCondition(get(ctx, nn).Status.Conditions, status.DriftedCondition)
		Expect(drifted).NotTo(BeNil(), "the drift found by the same reconcile stays reported")
		Expect(drifted.Status).To(Equal(metav1.ConditionTrue))
	})

	It("leaves a Job with a TTL absent, ends NoOp and reports it Missing", func() {
		ctx := context.Background()
		nn := createPeriodicInstance(ctx, "restore-ttl-job-mi")
		const jobName = "restore-ttl-job"
		DeferCleanup(func() {
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{Name: jobName, Namespace: periodicNamespace},
			}, client.PropagationPolicy(metav1.DeletePropagationBackground)))).To(Succeed())
		})
		renderer := &callCountingRenderer{}
		resources := func() []*object.Resource {
			return []*object.Resource{configMapResource("test-module", "hello"), jobResource(jobName)}
		}
		renderer.result = &render.RenderResult{
			Resources:        resources(),
			ModuleVersion:    stubModuleVersion,
			PlatformIdentity: stubPlatformIdentity,
			SkewPolicy:       stubSkewPolicy,
		}
		r := periodicReconciler(renderer, 0) // every reconcile renders

		reconcileOnce(ctx, r, nn) // finalizer
		reconcileOnce(ctx, r, nn) // apply
		applied := get(ctx, nn)
		Expect(applied.Status.History).To(HaveLen(1))

		By("the cluster removes the finished Job")
		jobKey := types.NamespacedName{Name: jobName, Namespace: periodicNamespace}
		Expect(k8sClient.Delete(ctx, &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{Name: jobName, Namespace: periodicNamespace},
		}, client.PropagationPolicy(metav1.DeletePropagationBackground))).To(Succeed())
		Eventually(func() bool {
			return apierrors.IsNotFound(k8sClient.Get(ctx, jobKey, &batchv1.Job{}))
		}, 5*time.Second, 50*time.Millisecond).Should(BeTrue())

		renderer.result.Resources = resources()
		calls := renderer.calls.Load()
		res := reconcileOnce(ctx, r, nn)

		Expect(renderer.calls.Load()).To(Equal(calls+1), "the reconcile rendered")
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, jobKey, &batchv1.Job{}))).To(BeTrue(), "the Job is not created again")
		after := get(ctx, nn)
		Expect(after.Status.History).To(HaveLen(1), "the reconcile ended NoOp")
		Expect(apimeta.IsStatusConditionTrue(after.Status.Conditions, status.ReadyCondition)).To(BeTrue())
		h := apimeta.FindStatusCondition(after.Status.Conditions, status.HealthyCondition)
		Expect(h).NotTo(BeNil())
		Expect(h.Status).To(Equal(metav1.ConditionFalse))
		Expect(h.Reason).To(Equal(status.NotRolledOutReason))
		Expect(h.Message).To(ContainSubstring("Job " + periodicNamespace + "/" + jobName + " (Missing)"))
		Expect(res.RequeueAfter).To(BeNumerically(">", 0))
		Expect(res.RequeueAfter).To(BeNumerically("<=", 2*time.Minute), "the missing Job keeps the health requeue")
	})

	It("records a failed restore as a failed apply and retries on the backoff", func() {
		ctx := context.Background()
		nn := createPeriodicInstance(ctx, "restore-fails-mi")
		r := periodicReconciler(&callCountingRenderer{}, 0)

		reconcileOnce(ctx, r, nn) // finalizer
		reconcileOnce(ctx, r, nn) // apply
		deleteConfigMap(ctx)

		// The dry-run of drift detection passes; every real apply fails.
		base, err := client.NewWithWatch(cfg, client.Options{})
		Expect(err).NotTo(HaveOccurred())
		failing := interceptor.NewClient(base, interceptor.Funcs{
			Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, p client.Patch, opts ...client.PatchOption) error {
				o := &client.PatchOptions{}
				o.ApplyOptions(opts)
				if len(o.DryRun) == 0 {
					return fmt.Errorf("injected apply failure")
				}
				return c.Patch(ctx, obj, p, opts...)
			},
		})
		r.ResourceManager = apply.NewResourceManager(failing, "opm-controller")
		res := reconcileOnce(ctx, r, nn)

		_, err = configMap(ctx, "test-module")
		Expect(apierrors.IsNotFound(err)).To(BeTrue(), "the restore failed")
		after := get(ctx, nn)
		ready := apimeta.FindStatusCondition(after.Status.Conditions, status.ReadyCondition)
		Expect(ready).NotTo(BeNil())
		Expect(ready.Status).To(Equal(metav1.ConditionFalse))
		Expect(ready.Reason).To(Equal(status.ApplyFailedReason))
		Expect(after.Status.NextRetryAt).NotTo(BeNil(), "a failed restore is a retry")
		Expect(res.RequeueAfter).To(Equal(opmreconcile.BackoffBaseDelay))

		By("the next reconcile restores once the apply works again")
		r.ResourceManager = apply.NewResourceManager(k8sClient, "opm-controller")
		reconcileOnce(ctx, r, nn)
		_, err = configMap(ctx, "test-module")
		Expect(err).NotTo(HaveOccurred())
		Expect(apimeta.IsStatusConditionTrue(get(ctx, nn).Status.Conditions, status.ReadyCondition)).To(BeTrue())
	})

	It("restores nothing while the render is skipped", func() {
		ctx := context.Background()
		createSkipPlatform(ctx)
		nn := createPeriodicInstance(ctx, "restore-skipped-mi")
		renderer := &callCountingRenderer{}
		r := periodicReconciler(renderer, 30*time.Minute)

		reconcileOnce(ctx, r, nn) // finalizer
		reconcileOnce(ctx, r, nn) // apply, records the key
		calls := renderer.calls.Load()

		deleteConfigMap(ctx)
		res := reconcileOnce(ctx, r, nn)

		Expect(renderer.calls.Load()).To(Equal(calls), "the render is skipped")
		_, err := configMap(ctx, "test-module")
		Expect(apierrors.IsNotFound(err)).To(BeTrue(), "a skipped render has nothing to restore from")
		Expect(res.RequeueAfter).To(BeNumerically("<=", 2*time.Minute), "the missing object keeps the health requeue")
		Expect(res.RequeueAfter).To(BeNumerically(">", 0))
	})
})
