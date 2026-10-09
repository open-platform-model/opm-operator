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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	"github.com/open-platform-model/opm-operator/internal/apply"
	opmreconcile "github.com/open-platform-model/opm-operator/internal/reconcile"
	"github.com/open-platform-model/opm-operator/internal/render"
	opmsource "github.com/open-platform-model/opm-operator/internal/source"
	"github.com/open-platform-model/opm-operator/internal/status"
)

// waitSubject is one ModuleInstance or ModulePackage whose deletion the specs
// below drive, so each spec runs for both kinds with one body.
type waitSubject struct {
	nn  types.NamespacedName
	rec *actionRecorder
	// apply reconciles the object with the given render.
	apply func(*render.RenderResult)
	// reconcile runs one reconcile of the object as it stands.
	reconcile func() (ctrl.Result, error)
	// object reads the object; nil when it is gone.
	object func() client.Object
	// conditions and inventory read the status of obj.
	conditions func(obj client.Object) []metav1.Condition
	// setInventory appends an entry to status.inventory.
	addInventory func(releasesv1alpha1.InventoryEntry)
	setWait      func(opmreconcile.DeletionWait)
}

func (s *waitSubject) requestDelete() {
	GinkgoHelper()
	Expect(k8sClient.Delete(ctx, s.object())).To(Succeed())
	s.rec.events = nil
}

func (s *waitSubject) gone() bool {
	GinkgoHelper()
	return s.object() == nil
}

func (s *waitSubject) ready() *metav1.Condition {
	GinkgoHelper()
	obj := s.object()
	Expect(obj).NotTo(BeNil(), "the deleting object must still exist")
	return apimeta.FindStatusCondition(s.conditions(obj), status.ReadyCondition)
}

func (s *waitSubject) condition(kind string) *metav1.Condition {
	GinkgoHelper()
	return apimeta.FindStatusCondition(s.conditions(s.object()), kind)
}

func (s *waitSubject) mustReconcile() ctrl.Result {
	GinkgoHelper()
	result, err := s.reconcile()
	Expect(err).NotTo(HaveOccurred())
	return result
}

// reconcileUntilGone reconciles until the finalizer is removed, as the
// requeue of a waiting deletion would.
func (s *waitSubject) reconcileUntilGone() {
	GinkgoHelper()
	Eventually(func(g Gomega) {
		_, err := s.reconcile()
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(s.object()).To(BeNil(), "the deleting object must be gone")
	}, 10*time.Second, 50*time.Millisecond).Should(Succeed())
}

// forceRemove removes the object whatever its finalizer, after a spec.
func (s *waitSubject) forceRemove() {
	GinkgoHelper()
	obj := s.object()
	if obj == nil {
		return
	}
	obj.SetFinalizers(nil)
	Expect(k8sClient.Update(ctx, obj)).To(Succeed())
	Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, obj))).To(Succeed())
}

func newInstanceSubject(name, serviceAccount string) *waitSubject {
	GinkgoHelper()
	rec := &actionRecorder{}
	params := reconcileParamsWithConfig()
	params.EventRecorder = rec
	nn := types.NamespacedName{Name: name, Namespace: namespace}
	Expect(k8sClient.Create(ctx, &releasesv1alpha1.ModuleInstance{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: releasesv1alpha1.ModuleInstanceSpec{
			Module:             releasesv1alpha1.ModuleReference{Path: "opmodel.dev/test/module", Version: "v0.1.0"},
			Prune:              true,
			ServiceAccountName: serviceAccount,
		},
	})).To(Succeed())
	ensureFinalizer(params, nn)
	s := &waitSubject{nn: nn, rec: rec}
	s.reconcile = func() (ctrl.Result, error) {
		return opmreconcile.ReconcileModuleInstance(ctx, params, ctrl.Request{NamespacedName: nn})
	}
	s.apply = func(r *render.RenderResult) {
		GinkgoHelper()
		params.Renderer = &stubRenderer{result: r}
		s.mustReconcile()
	}
	s.object = func() client.Object {
		GinkgoHelper()
		var mi releasesv1alpha1.ModuleInstance
		if err := k8sClient.Get(ctx, nn, &mi); err != nil {
			Expect(apierrors.IsNotFound(err)).To(BeTrue(), "reading the ModuleInstance: %v", err)
			return nil
		}
		return &mi
	}
	s.conditions = func(obj client.Object) []metav1.Condition {
		return obj.(*releasesv1alpha1.ModuleInstance).Status.Conditions
	}
	s.addInventory = func(entry releasesv1alpha1.InventoryEntry) {
		GinkgoHelper()
		mi := s.object().(*releasesv1alpha1.ModuleInstance)
		mi.Status.Inventory.Entries = append(mi.Status.Inventory.Entries, entry)
		Expect(k8sClient.Status().Update(ctx, mi)).To(Succeed())
	}
	s.setWait = func(w opmreconcile.DeletionWait) { params.DeletionWait = w }
	return s
}

func newPackageSubject(name, serviceAccount string) *waitSubject {
	GinkgoHelper()
	const sourceName = "deletion-wait-src"
	rec := &actionRecorder{}
	realClient, err := client.NewWithWatch(cfg, client.Options{Scheme: k8sClient.Scheme()})
	Expect(err).NotTo(HaveOccurred())
	params := &opmreconcile.ModulePackageParams{
		Client:          readySource(realClient, sourceName),
		APIReader:       k8sClient,
		RestConfig:      cfg,
		ResourceManager: apply.NewResourceManager(k8sClient, "opm-controller"),
		EventRecorder:   rec,
		Fetcher:         packageDirFetcher{path: "releases/app"},
		Renderer:        &stubPackageRenderer{result: renderOf(nil, nil)},
	}
	nn := types.NamespacedName{Name: name, Namespace: namespace}
	Expect(k8sClient.Create(ctx, &releasesv1alpha1.ModulePackage{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: releasesv1alpha1.ModulePackageSpec{
			SourceRef:          releasesv1alpha1.SourceReference{Kind: opmsource.SourceKindOCIRepository, Name: sourceName},
			Path:               "releases/app",
			Interval:           metav1.Duration{Duration: time.Minute},
			Prune:              true,
			ServiceAccountName: serviceAccount,
		},
	})).To(Succeed())
	s := &waitSubject{nn: nn, rec: rec}
	s.reconcile = func() (ctrl.Result, error) {
		return opmreconcile.ReconcileModulePackage(ctx, params, ctrl.Request{NamespacedName: nn})
	}
	Expect(s.mustReconcile()).To(Equal(ctrl.Result{Requeue: true}), "the first reconcile registers the finalizer")
	s.apply = func(r *render.RenderResult) {
		GinkgoHelper()
		params.Renderer = &stubPackageRenderer{result: r}
		s.mustReconcile()
	}
	s.object = func() client.Object {
		GinkgoHelper()
		var pkg releasesv1alpha1.ModulePackage
		if err := k8sClient.Get(ctx, nn, &pkg); err != nil {
			Expect(apierrors.IsNotFound(err)).To(BeTrue(), "reading the ModulePackage: %v", err)
			return nil
		}
		return &pkg
	}
	s.conditions = func(obj client.Object) []metav1.Condition {
		return obj.(*releasesv1alpha1.ModulePackage).Status.Conditions
	}
	s.addInventory = func(entry releasesv1alpha1.InventoryEntry) {
		GinkgoHelper()
		pkg := s.object().(*releasesv1alpha1.ModulePackage)
		pkg.Status.Inventory.Entries = append(pkg.Status.Inventory.Entries, entry)
		Expect(k8sClient.Status().Update(ctx, pkg)).To(Succeed())
	}
	s.setWait = func(w opmreconcile.DeletionWait) { params.DeletionWait = w }
	return s
}

// tenant is a ServiceAccount with a Role in the test namespace.
type tenant struct {
	name    string
	role    *rbacv1.Role
	binding *rbacv1.RoleBinding
}

var allVerbs = []string{"get", "list", "watch", "create", "update", "patch", "delete"}

// newTenant creates a ServiceAccount that may do deploymentVerbs on
// Deployments and everything on ConfigMaps and PersistentVolumeClaims.
func newTenant(name string, deploymentVerbs []string) *tenant {
	GinkgoHelper()
	sa := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace}}
	Expect(k8sClient.Create(ctx, sa)).To(Succeed())
	t := &tenant{name: name}
	t.role = &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Rules: []rbacv1.PolicyRule{
			{APIGroups: []string{""}, Resources: []string{"configmaps", "persistentvolumeclaims"}, Verbs: allVerbs},
			{APIGroups: []string{"apps"}, Resources: []string{"deployments"}, Verbs: deploymentVerbs},
		},
	}
	Expect(k8sClient.Create(ctx, t.role)).To(Succeed())
	t.binding = &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: name},
		Subjects:   []rbacv1.Subject{{Kind: rbacv1.ServiceAccountKind, Name: name, Namespace: namespace}},
	}
	Expect(k8sClient.Create(ctx, t.binding)).To(Succeed())
	DeferCleanup(func() {
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, t.binding))).To(Succeed())
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, t.role))).To(Succeed())
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, sa))).To(Succeed())
	})
	return t
}

func (t *tenant) deleteServiceAccount() {
	GinkgoHelper()
	Expect(k8sClient.Delete(ctx, &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{Name: t.name, Namespace: namespace},
	})).To(Succeed())
}

// client acts as the tenant, as the cleanup does.
func (t *tenant) client() client.Client {
	GinkgoHelper()
	c, err := apply.NewImpersonatedClient(ctx, cfg, k8sClient, k8sClient.Scheme(), namespace, t.name)
	Expect(err).NotTo(HaveOccurred())
	return c
}

// expectForbidden waits until the API server refuses probe as the tenant: a
// change of RBAC reaches the authorizer a moment after the write.
func (t *tenant) expectForbidden(what string, probe func(client.Client) error) {
	GinkgoHelper()
	c := t.client()
	Eventually(func() bool { return apierrors.IsForbidden(probe(c)) }, 10*time.Second, 50*time.Millisecond).
		Should(BeTrue(), "the tenant must be forbidden to "+what)
}

func readDeployment(name string) func(client.Client) error {
	return func(c client.Client) error {
		return c.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, &appsv1.Deployment{})
	}
}

func deleteDeploymentDryRun(name string) func(client.Client) error {
	return func(c client.Client) error {
		return c.Delete(ctx, &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace}},
			client.DryRunAll)
	}
}

var _ = Describe("Deletion cleanup waits for its deleted objects", func() {
	kinds := []struct {
		name string
		// short keeps object names of the two kinds apart.
		short string
		new   func(name, serviceAccount string) *waitSubject
	}{
		{"ModuleInstance", "mi", newInstanceSubject},
		{"ModulePackage", "pkg", newPackageSubject},
	}

	removeDeployment := func(name string) {
		GinkgoHelper()
		deployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace}}
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, deployment))).To(Succeed())
		expectGone(client.ObjectKeyFromObject(deployment), &appsv1.Deployment{})
	}

	// waiting creates a subject with a ConfigMap and a Deployment, pauses the
	// collector so the Deployment stays terminating as one whose Pods have
	// not gone, deletes the subject and runs the first cleanup reconcile.
	waiting := func(prefix, serviceAccount string, newSubject func(string, string) *waitSubject) (*waitSubject, func()) {
		GinkgoHelper()
		s := newSubject(prefix, serviceAccount)
		DeferCleanup(func() {
			removeDeployment(prefix + "-web")
			removeConfigMaps(prefix + "-config")
			s.forceRemove()
		})
		s.apply(withDeployment(prefix+"-config", prefix+"-web"))
		resume := gc.Pause(namespace)
		DeferCleanup(resume)
		s.requestDelete()
		result := s.mustReconcile()
		Expect(result.RequeueAfter).To(Equal(time.Second), "a young wait is rechecked after the minimum interval")
		expectTerminatingInForeground(prefix + "-web")
		return s, resume
	}

	expectInProgress := func(s *waitSubject, deployment string) {
		GinkgoHelper()
		ready := s.ready()
		Expect(ready).NotTo(BeNil())
		Expect(ready.Status).To(Equal(metav1.ConditionFalse))
		Expect(ready.Reason).To(Equal(status.DeletionInProgressReason))
		Expect(ready.Message).To(ContainSubstring(
			"Deployment/" + namespace + "/" + deployment + " (waits for its dependents to be deleted)"))
		Expect(s.object().GetFinalizers()).To(ContainElement(opmreconcile.FinalizerName))
	}

	elevenMinutesAhead := opmreconcile.DeletionWait{Now: func() time.Time { return time.Now().Add(11 * time.Minute) }}

	for _, kind := range kinds {
		Context(kind.name, func() {
			It("keeps the finalizer with DeletionInProgress until the deleted objects are gone", func() {
				prefix := "dw-wait-" + kind.short
				s, resume := waiting(prefix, "", kind.new)

				expectInProgress(s, prefix+"-web")
				reconciling := s.condition(status.ReconcilingCondition)
				Expect(reconciling).NotTo(BeNil())
				Expect(reconciling.Status).To(Equal(metav1.ConditionTrue))
				Expect(s.condition(status.StalledCondition)).To(BeNil(), "waiting is not stalled")

				By("three reconciles while it still exists emit one event")
				s.mustReconcile()
				s.mustReconcile()
				expectInProgress(s, prefix+"-web")
				events := s.rec.withReason(status.DeletionInProgressReason)
				Expect(events).To(HaveLen(1))
				Expect(events[0].eventType).To(Equal(corev1.EventTypeNormal))
				Expect(events[0].action).To(Equal("Delete"))
				Expect(events[0].note).To(Equal(s.ready().Message))

				By("the Deployment goes")
				resume()
				expectGone(types.NamespacedName{Name: prefix + "-web", Namespace: namespace}, &appsv1.Deployment{})
				// The ConfigMap's repeated delete set its foregroundDeletion
				// finalizer again, so the collector may need one more sweep.
				s.reconcileUntilGone()
				Expect(s.rec.withReason(status.DeletionUnconfirmedReason)).To(BeEmpty(), "a confirmed deletion")
			})

			It("reports DeletionBlocked through the injected clock, and completes when the object goes", func() {
				prefix := "dw-block-" + kind.short
				s, resume := waiting(prefix, "", kind.new)
				expectInProgress(s, prefix+"-web")

				By("the controller's clock is 11 minutes past the deletionTimestamp")
				s.setWait(elevenMinutesAhead)
				Expect(s.mustReconcile().RequeueAfter).To(Equal(time.Minute))
				Expect(s.mustReconcile().RequeueAfter).To(Equal(time.Minute))

				ready := s.ready()
				Expect(ready.Status).To(Equal(metav1.ConditionFalse))
				Expect(ready.Reason).To(Equal(status.DeletionBlockedReason))
				Expect(ready.Message).To(ContainSubstring("Deployment/" + namespace + "/" + prefix + "-web"))
				Expect(ready.Message).To(ContainSubstring("spec.prune=false"))
				stalled := s.condition(status.StalledCondition)
				Expect(stalled).NotTo(BeNil())
				Expect(stalled.Status).To(Equal(metav1.ConditionTrue))
				Expect(s.object().GetFinalizers()).To(ContainElement(opmreconcile.FinalizerName), "it never gives up")
				blocked := s.rec.withReason(status.DeletionBlockedReason)
				Expect(blocked).To(HaveLen(1))
				Expect(blocked[0].eventType).To(Equal(corev1.EventTypeWarning))
				Expect(blocked[0].note).To(ContainSubstring(prefix + "-web"))

				resume()
				expectGone(types.NamespacedName{Name: prefix + "-web", Namespace: namespace}, &appsv1.Deployment{})
				s.reconcileUntilGone()
			})

			for _, from := range []string{status.DeletionInProgressReason, status.DeletionBlockedReason} {
				It("releases with DeletionUnconfirmed when the ServiceAccount is deleted during the wait, from "+from, func() {
					prefix := "dw-sa-" + kind.short
					if from == status.DeletionBlockedReason {
						prefix = "dw-sab-" + kind.short
					}
					who := newTenant(prefix, allVerbs)
					s, _ := waiting(prefix, who.name, kind.new)
					if from == status.DeletionBlockedReason {
						s.setWait(elevenMinutesAhead)
						s.mustReconcile()
					}
					Expect(s.ready().Reason).To(Equal(from))
					s.rec.events = nil

					who.deleteServiceAccount()
					Expect(s.mustReconcile()).To(Equal(ctrl.Result{}))

					Expect(s.gone()).To(BeTrue(), "every delete was sent, so the lost identity holds nothing")
					unconfirmed := s.rec.withReason(status.DeletionUnconfirmedReason)
					Expect(unconfirmed).To(HaveLen(1))
					Expect(unconfirmed[0].eventType).To(Equal(corev1.EventTypeWarning))
					Expect(unconfirmed[0].action).To(Equal("Delete"))
					Expect(unconfirmed[0].note).To(ContainSubstring("2 object(s)"))
					Expect(unconfirmed[0].note).To(ContainSubstring(`ServiceAccount "` + namespace + "/" + who.name + `" is missing`))
					Expect(s.rec.withReason(status.DeletionSAMissingReason)).To(BeEmpty())
					expectTerminatingInForeground(prefix + "-web")
				})
			}

			// Only a ServiceAccount that is gone releases a waiting deletion.
			// When its rights go and the ServiceAccount stays, the deletion
			// holds, says why at once, is DeletionBlocked after the
			// threshold, and spec.prune=false is the way out.
			It("holds when the RoleBinding is deleted during the wait, and names the way out", func() {
				prefix := "dw-rbac-" + kind.short
				who := newTenant(prefix, allVerbs)
				s, _ := waiting(prefix, who.name, kind.new)
				expectInProgress(s, prefix+"-web")
				s.rec.events = nil

				Expect(k8sClient.Delete(ctx, who.binding)).To(Succeed())
				who.expectForbidden("read", readDeployment(prefix+"-web"))
				_, err := s.reconcile()
				Expect(err).To(HaveOccurred(), "the failed recheck is retried")

				Expect(s.gone()).To(BeFalse(), "a Forbidden answer does not show that the ServiceAccount is gone")
				ready := s.ready()
				Expect(ready.Reason).To(Equal(status.DeletionInProgressReason), "the wait reason is the record and stays")
				Expect(ready.Message).To(ContainSubstring("could not be checked"))
				Expect(ready.Message).To(ContainSubstring("Deployment " + namespace + "/" + prefix + "-web"))
				Expect(s.rec.withReason(status.DeletionUnconfirmedReason)).To(BeEmpty())
				Expect(s.rec.withReason(status.ImpersonationFailedReason)).To(BeEmpty())

				By("the deletion is 11 minutes old by the controller's clock")
				s.setWait(elevenMinutesAhead)
				_, err = s.reconcile()
				Expect(err).To(HaveOccurred())
				ready = s.ready()
				Expect(ready.Reason).To(Equal(status.DeletionBlockedReason))
				Expect(ready.Message).To(ContainSubstring("spec.prune=false"))
				Expect(s.rec.withReason(status.DeletionBlockedReason)).To(HaveLen(1))
				Expect(s.object().GetFinalizers()).To(ContainElement(opmreconcile.FinalizerName))

				By("the way out: spec.prune is set to false")
				obj := s.object()
				switch o := obj.(type) {
				case *releasesv1alpha1.ModuleInstance:
					o.Spec.Prune = false
				case *releasesv1alpha1.ModulePackage:
					o.Spec.Prune = false
				}
				Expect(k8sClient.Update(ctx, obj)).To(Succeed())
				Expect(s.mustReconcile()).To(Equal(ctrl.Result{}))
				Expect(s.gone()).To(BeTrue())
			})

			It("stalls with DeletionSAMissing when the ServiceAccount goes after a cleanup with a failed step", func() {
				prefix := "dw-early-" + kind.short
				who := newTenant(prefix, []string{"get", "list", "watch", "create", "update", "patch"})
				s := kind.new(prefix, who.name)
				DeferCleanup(func() {
					removeDeployment(prefix + "-web")
					removeConfigMaps(prefix + "-config")
					s.forceRemove()
				})
				s.apply(withDeployment(prefix+"-config", prefix+"-web"))
				s.requestDelete()

				By("the first cleanup deletes the ConfigMap and is forbidden to delete the Deployment")
				s.mustReconcile()
				Expect(s.ready().Reason).To(Equal(status.ImpersonationFailedReason), "no wait reason is recorded")
				expectConfigMapGone(prefix + "-config")

				who.deleteServiceAccount()
				Expect(s.mustReconcile().RequeueAfter).To(Equal(30 * time.Minute))
				Expect(s.ready().Reason).To(Equal(status.DeletionSAMissingReason))
				Expect(s.object().GetFinalizers()).To(ContainElement(opmreconcile.FinalizerName))
				Expect(s.rec.withReason(status.DeletionUnconfirmedReason)).To(BeEmpty())
			})

			It("holds for a readable terminating object whose repeated delete is forbidden", func() {
				prefix := "dw-nodel-" + kind.short
				who := newTenant(prefix, allVerbs)
				s, _ := waiting(prefix, who.name, kind.new)
				expectInProgress(s, prefix+"-web")
				s.rec.events = nil

				who.role.Rules[1].Verbs = []string{"get"}
				Expect(k8sClient.Update(ctx, who.role)).To(Succeed())
				who.expectForbidden("delete", deleteDeploymentDryRun(prefix+"-web"))
				Expect(readDeployment(prefix+"-web")(who.client())).To(Succeed(), "the tenant can still read")

				_, err := s.reconcile()
				Expect(err).To(HaveOccurred(), "the failed recheck is retried")
				ready := s.ready()
				Expect(ready.Reason).To(Equal(status.DeletionInProgressReason))
				Expect(ready.Message).To(ContainSubstring("delete Deployment " + namespace + "/" + prefix + "-web"))
				Expect(s.object().GetFinalizers()).To(ContainElement(opmreconcile.FinalizerName))
				Expect(s.rec.withReason(status.ImpersonationFailedReason)).To(BeEmpty())
				Expect(s.rec.withReason(status.DeletionUnconfirmedReason)).To(BeEmpty())
			})

			It("reports kept claims and left objects once, however long the wait", func() {
				prefix := "dw-once-" + kind.short
				s := kind.new(prefix, "")
				DeferCleanup(func() {
					removeDeployment(prefix + "-web")
					removeClaims(prefix + "-data")
					s.forceRemove()
				})
				r := withDeployment(prefix+"-config", prefix+"-web")
				r.Resources = append(r.Resources, claimResources(prefix+"-data")...)
				s.apply(r)
				// A kind OPM never deletes: left behind without a read.
				s.addInventory(releasesv1alpha1.InventoryEntry{Version: "v1", Kind: "Namespace", Name: namespace})

				resume := gc.Pause(namespace)
				DeferCleanup(resume)
				s.requestDelete()
				for range 4 {
					s.mustReconcile()
				}
				expectInProgress(s, prefix+"-web")
				Expect(s.ready().Message).NotTo(ContainSubstring(prefix+"-data"), "a kept claim is not waited for")
				resume()
				s.reconcileUntilGone()

				for _, reason := range []string{status.ClaimsKeptReason, status.LeftBehindReason, status.DeletionInProgressReason} {
					events := s.rec.withReason(reason)
					Expect(events).To(HaveLen(1), "reason %s", reason)
					Expect(events[0].action).To(Equal("Delete"))
				}
				expectClaimUntouched(prefix + "-data")
			})

			It("releases a waiting deletion when spec.prune is set to false", func() {
				prefix := "dw-prune-" + kind.short
				s, _ := waiting(prefix, "", kind.new)
				expectInProgress(s, prefix+"-web")

				obj := s.object()
				switch o := obj.(type) {
				case *releasesv1alpha1.ModuleInstance:
					o.Spec.Prune = false
				case *releasesv1alpha1.ModulePackage:
					o.Spec.Prune = false
				}
				Expect(k8sClient.Update(ctx, obj)).To(Succeed())

				Expect(s.mustReconcile()).To(Equal(ctrl.Result{}))
				Expect(s.gone()).To(BeTrue())
				expectTerminatingInForeground(prefix + "-web")
			})

			It("ignores a wait reason that another client wrote on a live object", func() {
				prefix := "dw-live-" + kind.short
				s := kind.new(prefix, "")
				DeferCleanup(func() {
					removeConfigMaps(prefix+"-a", prefix+"-b")
					s.forceRemove()
				})
				s.apply(namedConfigMapRenderResult(prefix + "-a"))

				obj := s.object()
				forged := metav1.Condition{
					Type: status.ReadyCondition, Status: metav1.ConditionFalse, Reason: status.DeletionInProgressReason,
					Message: "written by the test", LastTransitionTime: metav1.Now(),
				}
				switch o := obj.(type) {
				case *releasesv1alpha1.ModuleInstance:
					apimeta.SetStatusCondition(&o.Status.Conditions, forged)
				case *releasesv1alpha1.ModulePackage:
					apimeta.SetStatusCondition(&o.Status.Conditions, forged)
				}
				Expect(k8sClient.Status().Update(ctx, obj)).To(Succeed())
				Expect(s.ready().Reason).To(Equal(status.DeletionInProgressReason))

				By("a reconcile renders and applies as usual and replaces the condition")
				s.apply(namedConfigMapRenderResult(prefix+"-a", prefix+"-b"))
				Expect(configMapExists(prefix + "-b")).To(BeTrue())
				ready := s.ready()
				Expect(ready.Status).To(Equal(metav1.ConditionTrue), "reason=%s message=%s", ready.Reason, ready.Message)
				Expect(s.object().GetFinalizers()).To(ContainElement(opmreconcile.FinalizerName))
			})

			// The operator sends no delete for a kept claim, also when the
			// claim names a deleted object as its owner. What happens next is
			// the garbage collector's: in a cluster it deletes the dependents
			// of an owner that is deleted, with Foreground and with Background
			// alike, so a kept claim that carries an owner reference to a
			// deleted object IS collected there. envtest runs no garbage
			// collector and the collector helper deletes no dependent, so here
			// the claim stays. This spec pins the operator's side only.
			It("sends no delete for a kept claim whose owner is deleted with Foreground", func() {
				prefix := "dw-owned-" + kind.short
				s := kind.new(prefix, "")
				DeferCleanup(func() {
					removeDeployment(prefix + "-web")
					removeClaims(prefix + "-data")
					removeConfigMaps(prefix + "-config")
					s.forceRemove()
				})
				r := withDeployment(prefix+"-config", prefix+"-web")
				r.Resources = append(r.Resources, claimResources(prefix+"-data")...)
				s.apply(r)

				var owner appsv1.Deployment
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: prefix + "-web", Namespace: namespace}, &owner)).To(Succeed())
				var claim corev1.PersistentVolumeClaim
				claimKey := types.NamespacedName{Name: prefix + "-data", Namespace: namespace}
				Expect(k8sClient.Get(ctx, claimKey, &claim)).To(Succeed())
				block := true
				claim.OwnerReferences = []metav1.OwnerReference{{
					APIVersion: "apps/v1", Kind: "Deployment", Name: owner.Name, UID: owner.UID, BlockOwnerDeletion: &block,
				}}
				Expect(k8sClient.Update(ctx, &claim)).To(Succeed())

				s.requestDelete()
				s.reconcileUntilGone()

				expectGone(client.ObjectKeyFromObject(&owner), &appsv1.Deployment{})
				expectClaimUntouched(prefix + "-data")
				kept := s.rec.withReason(status.ClaimsKeptReason)
				Expect(kept).To(HaveLen(1))
				Expect(kept[0].note).To(ContainSubstring(namespace + "/" + prefix + "-data"))
			})
		})
	}
})
