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

// jobResource is a rendered Job named name. With ttl it sets
// ttlSecondsAfterFinished, as the opm catalog's Job transformer does.
func jobResource(name string, ttl bool) *object.Resource {
	ttlField := ""
	if ttl {
		ttlField = "ttlSecondsAfterFinished: 100"
	}
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
		%s
		template: spec: {
			restartPolicy: "Never"
			containers: [{name: "run", image: "busybox"}]
		}
	}
}`, name, periodicNamespace,
		labels.ManagedBy, labels.ManagedByController,
		labels.ModuleInstanceNamespace, periodicNamespace,
		ttlField))
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

	// jobInstance creates an instance whose render is the ConfigMap
	// "test-module" and the Job jobName, and returns its renderer. The
	// reconcile drops the resources of the result it is handed, so fresh
	// gives the next render a new list.
	jobInstance := func(ctx context.Context, name, jobName string, ttl bool) (nn types.NamespacedName, renderer *callCountingRenderer, fresh func()) {
		nn = createPeriodicInstance(ctx, name)
		DeferCleanup(func() {
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{Name: jobName, Namespace: periodicNamespace},
			}, client.PropagationPolicy(metav1.DeletePropagationBackground)))).To(Succeed())
		})
		renderer = &callCountingRenderer{}
		renderer.result = &render.RenderResult{
			ModuleVersion:    stubModuleVersion,
			PlatformIdentity: stubPlatformIdentity,
			SkewPolicy:       stubSkewPolicy,
		}
		fresh = func() {
			renderer.result.Resources = []*object.Resource{configMapResource("test-module", "hello"), jobResource(jobName, ttl)}
		}
		fresh()
		return nn, renderer, fresh
	}

	jobExists := func(ctx context.Context, jobName string) bool {
		err := k8sClient.Get(ctx, types.NamespacedName{Name: jobName, Namespace: periodicNamespace}, &batchv1.Job{})
		if err != nil {
			Expect(apierrors.IsNotFound(err)).To(BeTrue(), "reading the Job: %v", err)
		}
		return err == nil
	}

	// removeJob removes the Job as the cluster's TTL controller, or a user,
	// would. envtest runs no Job and no TTL controller.
	removeJob := func(ctx context.Context, jobName string) {
		Expect(k8sClient.Delete(ctx, &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{Name: jobName, Namespace: periodicNamespace},
		}, client.PropagationPolicy(metav1.DeletePropagationBackground))).To(Succeed())
		Eventually(func() bool { return jobExists(ctx, jobName) }, 5*time.Second, 50*time.Millisecond).Should(BeFalse())
	}

	// completeJob writes the status the Job controller writes when the Job
	// finished well.
	completeJob := func(ctx context.Context, jobName string) {
		var job batchv1.Job
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: jobName, Namespace: periodicNamespace}, &job)).To(Succeed())
		now := metav1.Now()
		job.Status.StartTime = &now
		job.Status.CompletionTime = &now
		job.Status.Succeeded = 1
		job.Status.Conditions = []batchv1.JobCondition{
			{Type: batchv1.JobSuccessCriteriaMet, Status: corev1.ConditionTrue, Reason: "CompletionsReached", LastProbeTime: now, LastTransitionTime: now},
			{Type: batchv1.JobComplete, Status: corev1.ConditionTrue, Reason: "CompletionsReached", LastProbeTime: now, LastTransitionTime: now},
		}
		Expect(k8sClient.Status().Update(ctx, &job)).To(Succeed())
	}

	listsJob := func(mi *releasesv1alpha1.ModuleInstance, jobName string) bool {
		for _, e := range mi.Status.Inventory.Entries {
			if e.Group == "batch" && e.Kind == "Job" && e.Name == jobName {
				return true
			}
		}
		return false
	}

	healthy := func(mi *releasesv1alpha1.ModuleInstance) *metav1.Condition {
		h := apimeta.FindStatusCondition(mi.Status.Conditions, status.HealthyCondition)
		Expect(h).NotTo(BeNil())
		return h
	}

	It("keeps an instance healthy when its completed Job with a TTL expired", func() {
		ctx := context.Background()
		createSkipPlatform(ctx)
		const jobName = "restore-ttl-complete-job"
		nn, renderer, fresh := jobInstance(ctx, "restore-ttl-complete-mi", jobName, true)
		r := periodicReconciler(renderer, 30*time.Minute) // the render skip is on

		reconcileOnce(ctx, r, nn) // finalizer
		reconcileOnce(ctx, r, nn) // apply, records the key
		applied := get(ctx, nn)
		Expect(listsJob(applied, jobName)).To(BeTrue())
		Expect(healthy(applied).Status).To(Equal(metav1.ConditionFalse), "the Job has not finished")

		By("the Job completes and a skipped reconcile sees it")
		completeJob(ctx, jobName)
		calls := renderer.calls.Load()
		res := reconcileOnce(ctx, r, nn)
		Expect(renderer.calls.Load()).To(Equal(calls), "the render is skipped")
		Expect(healthy(get(ctx, nn)).Status).To(Equal(metav1.ConditionTrue))
		Expect(res.RequeueAfter).To(beThePeriodicRequeue())

		By("the cluster removes the finished Job")
		removeJob(ctx, jobName)
		fresh()
		res = reconcileOnce(ctx, r, nn)

		Expect(renderer.calls.Load()).To(Equal(calls+1), "an absent inventory Job makes the reconcile render")
		Expect(jobExists(ctx, jobName)).To(BeFalse(), "the Job is not created again")
		after := get(ctx, nn)
		Expect(after.Status.History).To(HaveLen(1), "the reconcile ended NoOp")
		Expect(listsJob(after, jobName)).To(BeFalse(), "the expired Job left the inventory")
		Expect(after.Status.Inventory.Count).To(Equal(int64(1)))
		Expect(after.Status.Inventory.Digest).To(Equal(applied.Status.Inventory.Digest), "the digest stays that of the rendered set")
		Expect(after.Status.Inventory.Revision).To(Equal(applied.Status.Inventory.Revision))
		Expect(apimeta.IsStatusConditionTrue(after.Status.Conditions, status.ReadyCondition)).To(BeTrue())
		h := healthy(after)
		Expect(h.Status).To(Equal(metav1.ConditionTrue))
		Expect(h.Reason).To(Equal(status.RolledOutReason))
		Expect(h.Message).To(Equal("1/1 objects ready"))
		Expect(res.RequeueAfter).To(beThePeriodicRequeue(), "the instance is back on its interval")

		By("the next periodic reconcile skips its render and writes nothing")
		counting, writes := patchCountingClient()
		r.Client = counting
		res = reconcileOnce(ctx, r, nn)
		Expect(renderer.calls.Load()).To(Equal(calls+1), "one render per expired Job")
		Expect(writes.Load()).To(BeZero())
		Expect(res.RequeueAfter).To(beThePeriodicRequeue())
		Expect(healthy(get(ctx, nn)).Status).To(Equal(metav1.ConditionTrue))
	})

	It("treats a Job with a TTL that was removed before it ran as finished", func() {
		ctx := context.Background()
		const jobName = "restore-ttl-job"
		nn, renderer, fresh := jobInstance(ctx, "restore-ttl-job-mi", jobName, true)
		r := periodicReconciler(renderer, 0) // every reconcile renders

		reconcileOnce(ctx, r, nn) // finalizer
		reconcileOnce(ctx, r, nn) // apply
		applied := get(ctx, nn)
		Expect(applied.Status.History).To(HaveLen(1))

		By("the Job is removed before any Pod of it ran")
		removeJob(ctx, jobName)
		fresh()
		res := reconcileOnce(ctx, r, nn)

		// Nothing records that a Job completed, so this Job reads as one
		// that finished and expired.
		Expect(jobExists(ctx, jobName)).To(BeFalse(), "the Job is not created again")
		after := get(ctx, nn)
		Expect(after.Status.History).To(HaveLen(1), "the reconcile ended NoOp")
		Expect(listsJob(after, jobName)).To(BeFalse())
		h := healthy(after)
		Expect(h.Status).To(Equal(metav1.ConditionTrue))
		Expect(h.Reason).To(Equal(status.RolledOutReason))
		Expect(res.RequeueAfter).To(beThePeriodicRequeue())

		By("the next render with unchanged digests is still a NoOp")
		fresh()
		reconcileOnce(ctx, r, nn)
		Expect(jobExists(ctx, jobName)).To(BeFalse())
		Expect(get(ctx, nn).Status.History).To(HaveLen(1), "the shortened inventory does not cause an apply")
	})

	It("treats a Job with a TTL that failed and expired as finished too", func() {
		ctx := context.Background()
		createSkipPlatform(ctx)
		const jobName = "restore-ttl-failed-job"
		nn, renderer, fresh := jobInstance(ctx, "restore-ttl-failed-mi", jobName, true)
		r := periodicReconciler(renderer, 30*time.Minute)

		reconcileOnce(ctx, r, nn) // finalizer
		reconcileOnce(ctx, r, nn) // apply, records the key

		By("the Job fails and a skipped reconcile reports it")
		var job batchv1.Job
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: jobName, Namespace: periodicNamespace}, &job)).To(Succeed())
		now := metav1.Now()
		job.Status.StartTime = &now
		job.Status.Failed = 1
		job.Status.Conditions = []batchv1.JobCondition{
			{Type: batchv1.JobFailureTarget, Status: corev1.ConditionTrue, Reason: "BackoffLimitExceeded", LastProbeTime: now, LastTransitionTime: now},
			{Type: batchv1.JobFailed, Status: corev1.ConditionTrue, Reason: "BackoffLimitExceeded", LastProbeTime: now, LastTransitionTime: now},
		}
		Expect(k8sClient.Status().Update(ctx, &job)).To(Succeed())
		reconcileOnce(ctx, r, nn)
		h := healthy(get(ctx, nn))
		Expect(h.Status).To(Equal(metav1.ConditionFalse))
		Expect(h.Message).To(ContainSubstring("Job " + periodicNamespace + "/" + jobName))

		By("the cluster removes the failed Job after its TTL")
		removeJob(ctx, jobName)
		fresh()
		reconcileOnce(ctx, r, nn)

		// The operator records no outcome of a Job, so the failure is no
		// longer reported once the Job is gone. The Job's own events and
		// logs are the record of it.
		Expect(jobExists(ctx, jobName)).To(BeFalse(), "a failed Job is not run again")
		after := get(ctx, nn)
		Expect(listsJob(after, jobName)).To(BeFalse())
		Expect(healthy(after).Status).To(Equal(metav1.ConditionTrue))
	})

	It("reads an instance whose only object was an expired Job as HealthUnknown with an empty inventory", func() {
		ctx := context.Background()
		const jobName = "restore-only-job"
		nn := createPeriodicInstance(ctx, "restore-only-job-mi")
		DeferCleanup(func() {
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{Name: jobName, Namespace: periodicNamespace},
			}, client.PropagationPolicy(metav1.DeletePropagationBackground)))).To(Succeed())
		})
		renderer := &callCountingRenderer{}
		renderer.result = &render.RenderResult{
			Resources:        []*object.Resource{jobResource(jobName, true)},
			ModuleVersion:    stubModuleVersion,
			PlatformIdentity: stubPlatformIdentity,
			SkewPolicy:       stubSkewPolicy,
		}
		r := periodicReconciler(renderer, 0)

		reconcileOnce(ctx, r, nn) // finalizer
		reconcileOnce(ctx, r, nn) // apply
		removeJob(ctx, jobName)
		renderer.result.Resources = []*object.Resource{jobResource(jobName, true)}
		res := reconcileOnce(ctx, r, nn)

		Expect(jobExists(ctx, jobName)).To(BeFalse())
		after := get(ctx, nn)
		Expect(after.Status.Inventory.Entries).To(BeEmpty())
		Expect(after.Status.Inventory.Count).To(BeZero())
		h := healthy(after)
		Expect(h.Status).To(Equal(metav1.ConditionUnknown), "nothing is left to judge")
		Expect(h.Reason).To(Equal(status.HealthUnknownReason))
		Expect(h.Message).To(ContainSubstring("the inventory is empty"))
		Expect(apimeta.IsStatusConditionTrue(after.Status.Conditions, status.ReadyCondition)).To(BeTrue())
		Expect(res.RequeueAfter).To(beThePeriodicRequeue(), "an empty inventory asks for no health requeue")
	})

	It("creates a missing Job without a TTL again, without waiting for the drift render interval", func() {
		ctx := context.Background()
		createSkipPlatform(ctx)
		const jobName = "restore-plain-job"
		nn, renderer, fresh := jobInstance(ctx, "restore-plain-job-mi", jobName, false)
		r := periodicReconciler(renderer, 30*time.Minute)

		reconcileOnce(ctx, r, nn) // finalizer
		reconcileOnce(ctx, r, nn) // apply, records the key
		calls := renderer.calls.Load()

		removeJob(ctx, jobName)
		fresh()
		res := reconcileOnce(ctx, r, nn)

		Expect(renderer.calls.Load()).To(Equal(calls+1), "an absent inventory Job makes the reconcile render")
		Expect(jobExists(ctx, jobName)).To(BeTrue(), "the Job without a TTL is created again")
		after := get(ctx, nn)
		Expect(after.Status.History).To(HaveLen(2), "the restore is recorded as an apply")
		Expect(listsJob(after, jobName)).To(BeTrue())
		h := healthy(after)
		Expect(h.Status).To(Equal(metav1.ConditionFalse), "the new Job has not finished")
		Expect(h.Message).NotTo(ContainSubstring("Missing"))
		Expect(res.RequeueAfter).To(BeNumerically(">", 0))
		Expect(res.RequeueAfter).To(BeNumerically("<=", 2*time.Minute))
	})

	It("restores a deleted object beside an expired Job and records the inventory without the Job", func() {
		ctx := context.Background()
		const jobName = "restore-beside-job"
		nn, renderer, fresh := jobInstance(ctx, "restore-beside-job-mi", jobName, true)
		r := periodicReconciler(renderer, 0)

		reconcileOnce(ctx, r, nn) // finalizer
		reconcileOnce(ctx, r, nn) // apply
		applied := get(ctx, nn)

		removeJob(ctx, jobName)
		deleteConfigMap(ctx)
		fresh()
		res := reconcileOnce(ctx, r, nn)

		_, err := configMap(ctx, "test-module")
		Expect(err).NotTo(HaveOccurred(), "the deleted ConfigMap exists again")
		Expect(jobExists(ctx, jobName)).To(BeFalse(), "the expired Job is not created again")
		after := get(ctx, nn)
		Expect(after.Status.History).To(HaveLen(2), "the restore is recorded as an apply")
		Expect(listsJob(after, jobName)).To(BeFalse())
		Expect(after.Status.Inventory.Count).To(Equal(int64(1)))
		Expect(after.Status.Inventory.Digest).To(Equal(applied.Status.Inventory.Digest))
		Expect(after.Status.Inventory.Revision).To(Equal(applied.Status.Inventory.Revision + 1))
		Expect(healthy(after).Status).To(Equal(metav1.ConditionTrue))
		Expect(res.RequeueAfter).To(beThePeriodicRequeue())

		fresh()
		reconcileOnce(ctx, r, nn)
		Expect(get(ctx, nn).Status.History).To(HaveLen(2), "the next reconcile is a NoOp")
	})

	It("reports an absent Job as Missing while drift detection fails, and renders for it once", func() {
		ctx := context.Background()
		createSkipPlatform(ctx)
		const jobName = "restore-ttl-nodrift-job"
		nn, renderer, fresh := jobInstance(ctx, "restore-ttl-nodrift-mi", jobName, true)
		r := periodicReconciler(renderer, 30*time.Minute)

		reconcileOnce(ctx, r, nn) // finalizer
		reconcileOnce(ctx, r, nn) // apply, records the key
		calls := renderer.calls.Load()
		removeJob(ctx, jobName)

		// Every dry-run fails, so the missing set is unknown.
		base, err := client.NewWithWatch(cfg, client.Options{})
		Expect(err).NotTo(HaveOccurred())
		noDryRun := interceptor.NewClient(base, interceptor.Funcs{
			Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, p client.Patch, opts ...client.PatchOption) error {
				o := &client.PatchOptions{}
				o.ApplyOptions(opts)
				if len(o.DryRun) > 0 {
					return fmt.Errorf("injected dry-run failure")
				}
				return c.Patch(ctx, obj, p, opts...)
			},
		})
		r.ResourceManager = apply.NewResourceManager(noDryRun, "opm-controller")

		fresh()
		res := reconcileOnce(ctx, r, nn)
		Expect(renderer.calls.Load()).To(Equal(calls+1), "the absent Job makes the reconcile render")
		after := get(ctx, nn)
		Expect(after.Status.FailureCounters.Drift).To(Equal(int64(1)))
		Expect(listsJob(after, jobName)).To(BeTrue(), "an unknown missing set removes nothing")
		h := healthy(after)
		Expect(h.Status).To(Equal(metav1.ConditionFalse))
		Expect(h.Reason).To(Equal(status.NotRolledOutReason))
		Expect(h.Message).To(ContainSubstring("Job " + periodicNamespace + "/" + jobName + " (Missing)"))
		Expect(res.RequeueAfter).To(BeNumerically(">", 0))
		Expect(res.RequeueAfter).To(BeNumerically("<=", 2*time.Minute))

		By("the health requeue does not render again")
		res = reconcileOnce(ctx, r, nn)
		Expect(renderer.calls.Load()).To(Equal(calls+1), "a failed drift detection bounds the renders")
		Expect(healthy(get(ctx, nn)).Status).To(Equal(metav1.ConditionFalse))
		Expect(res.RequeueAfter).To(BeNumerically("<=", 2*time.Minute))

		By("the next render with a working dry-run classifies the Job")
		rendering := periodicReconciler(renderer, 0)
		fresh()
		res = reconcileOnce(ctx, rendering, nn)
		Expect(jobExists(ctx, jobName)).To(BeFalse())
		recovered := get(ctx, nn)
		Expect(listsJob(recovered, jobName)).To(BeFalse())
		Expect(healthy(recovered).Status).To(Equal(metav1.ConditionTrue))
		Expect(res.RequeueAfter).To(beThePeriodicRequeue())
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
