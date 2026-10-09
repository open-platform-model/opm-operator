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

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/open-platform-model/library/opm/k8s/labels"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	"github.com/open-platform-model/opm-operator/internal/apply"
	opmreconcile "github.com/open-platform-model/opm-operator/internal/reconcile"
	"github.com/open-platform-model/opm-operator/internal/render"
	opmsource "github.com/open-platform-model/opm-operator/internal/source"
	"github.com/open-platform-model/opm-operator/internal/status"
)

var _ = Describe("Ownership guard on the apply of a ModulePackage", func() {
	const sourceName = "ownership-src"
	var (
		rec    *actionRecorder
		f      *ownershipFaults
		params *opmreconcile.ModulePackageParams
	)

	BeforeEach(func() {
		rec = &actionRecorder{}
		f = &ownershipFaults{}
		c := readySource(f.client(), sourceName)
		params = &opmreconcile.ModulePackageParams{
			Client:          c,
			APIReader:       c,
			RestConfig:      cfg,
			ResourceManager: apply.NewResourceManager(c, "opm-controller"),
			EventRecorder:   rec,
			Fetcher:         packageDirFetcher{path: "releases/app"},
		}
	})

	reconcilePackage := func(nn types.NamespacedName, r *render.RenderResult) ctrl.Result {
		GinkgoHelper()
		params.Renderer = &stubPackageRenderer{result: r}
		res, err := opmreconcile.ReconcileModulePackage(ctx, params, ctrl.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())
		return res
	}

	packageOf := func(nn types.NamespacedName) *releasesv1alpha1.ModulePackage {
		GinkgoHelper()
		var pkg releasesv1alpha1.ModulePackage
		Expect(k8sClient.Get(ctx, nn, &pkg)).To(Succeed())
		return &pkg
	}

	cond := func(nn types.NamespacedName, conditionType string) *metav1.Condition {
		GinkgoHelper()
		return apimeta.FindStatusCondition(packageOf(nn).Status.Conditions, conditionType)
	}

	expectReady := func(nn types.NamespacedName) *metav1.Condition {
		GinkgoHelper()
		ready := cond(nn, status.ReadyCondition)
		Expect(ready.Status).To(Equal(metav1.ConditionTrue), "reason=%s message=%s", ready.Reason, ready.Message)
		return ready
	}

	// start creates a package and reconciles it to Ready with identity A,
	// payload v1 and the named ConfigMaps.
	start := func(name string, names ...string) types.NamespacedName {
		GinkgoHelper()
		pkg := &releasesv1alpha1.ModulePackage{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
			Spec: releasesv1alpha1.ModulePackageSpec{
				SourceRef: releasesv1alpha1.SourceReference{Kind: opmsource.SourceKindOCIRepository, Name: sourceName},
				Path:      "releases/app",
				Interval:  metav1.Duration{Duration: time.Minute},
				Prune:     true,
			},
		}
		Expect(k8sClient.Create(ctx, pkg)).To(Succeed())
		nn := types.NamespacedName{Name: name, Namespace: namespace}
		DeferCleanup(func() {
			removeConfigMaps(names...)
			var pkg releasesv1alpha1.ModulePackage
			if err := k8sClient.Get(ctx, nn, &pkg); err != nil {
				Expect(apierrors.IsNotFound(err)).To(BeTrue())
				return
			}
			pkg.Finalizers = nil
			Expect(k8sClient.Update(ctx, &pkg)).To(Succeed())
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &pkg))).To(Succeed())
		})
		res := reconcilePackage(nn, ownedRender(identityA, "v1", names...))
		Expect(res).To(Equal(ctrl.Result{Requeue: true}), "the first reconcile registers the finalizer")
		reconcilePackage(nn, ownedRender(identityA, "v1", names...))
		expectReady(nn)
		Expect(packageOf(nn).Status.InstanceUUID).To(Equal(identityA))
		rec.events = nil
		return nn
	}

	adopt := func(name, uuid string) {
		GinkgoHelper()
		setConfigMapMeta(name, func(cm *corev1.ConfigMap) {
			cm.Annotations = map[string]string{labels.AnnotationAdopt: uuid}
		})
	}

	It("is refused over another instance's object, and adopts it once it is annotated", func() {
		nn := start("pkg-own-refused", "por-app", "por-held", "por-new")
		removeConfigMaps("por-held", "por-new")
		// A package whose inventory does not list the object another
		// instance holds: the first render that names it meets it.
		reconcilePackage(nn, ownedRender(identityA, "v1", "por-app"))
		expectReady(nn)
		createLiveConfigMap("por-held", labels.ManagedByController, identityX, "")
		before := liveConfigMapNamed("por-held").ResourceVersion
		applied := f.applied
		rec.events = nil

		res := reconcilePackage(nn, ownedRender(identityA, "v1", "por-app", "por-held", "por-new"))

		ready := cond(nn, status.ReadyCondition)
		Expect(ready.Status).To(Equal(metav1.ConditionFalse))
		Expect(ready.Reason).To(Equal(status.ApplyRefusedReason))
		Expect(ready.Message).To(ContainSubstring("por-held belongs to module instance " + identityX))
		Expect(ready.Message).To(ContainSubstring(labels.AnnotationAdopt + "=" + identityA))
		Expect(cond(nn, status.StalledCondition)).To(BeNil())
		Expect(res.RequeueAfter).To(BeNumerically(">", 0))
		Expect(res.RequeueAfter).To(BeNumerically("<=", 5*time.Minute))
		refusals := rec.withReason(status.ApplyRefusedReason)
		Expect(refusals).To(HaveLen(1))
		Expect(refusals[0].action).To(Equal("Apply"))
		Expect(rec.withReason(status.AppliedReason)).To(BeEmpty())
		Expect(liveConfigMapNamed("por-held").ResourceVersion).To(Equal(before), "the refused object is untouched")
		Expect(configMapExists("por-new")).To(BeFalse(), "no other rendered object is created")
		Expect(f.applied).To(Equal(applied))
		Expect(inventoryNames(packageOf(nn).Status.Inventory)).To(ConsistOf("ConfigMap/por-app"))
		Expect(packageOf(nn).Status.FailureCounters.Apply).To(Equal(int64(1)))

		By("the object is annotated with the package's status.instanceUUID and the retry runs")
		adopt("por-held", packageOf(nn).Status.InstanceUUID)
		reconcilePackage(nn, ownedRender(identityA, "v1", "por-app", "por-held", "por-new"))
		expectReady(nn)
		Expect(inventoryNames(packageOf(nn).Status.Inventory)).To(ConsistOf(
			"ConfigMap/por-app", "ConfigMap/por-held", "ConfigMap/por-new"))
		Expect(liveConfigMapLabels("por-held")).To(HaveKeyWithValue(labels.ModuleInstanceUUID, identityA))
	})

	// The prune of a package with nothing recorded also judges with no
	// identity. The apply guard never does: with no identity the verdict
	// would apply over an inventoried object another instance adopted.
	It("lets an adopted object go when no identity is recorded, asking with the render's identity", func() {
		nn := start("pkg-own-noid", "pon-app", "pon-adopted")
		pkg := packageOf(nn)
		pkg.Status.InstanceUUID = ""
		Expect(k8sClient.Status().Update(ctx, pkg)).To(Succeed())
		adopt("pon-adopted", identityX)
		before := liveConfigMapNamed("pon-adopted").ResourceVersion

		reconcilePackage(nn, ownedRender(identityA, "v1", "pon-app", "pon-adopted"))

		ready := expectReady(nn)
		Expect(ready.Message).To(ContainSubstring("1 rendered object(s) are adopted by another instance"))
		Expect(liveConfigMapNamed("pon-adopted").ResourceVersion).To(Equal(before), "the adopted object is not written")
		st := packageOf(nn).Status
		Expect(inventoryNames(st.Inventory)).To(ConsistOf("ConfigMap/pon-app"))
		Expect(st.InstanceUUID).To(Equal(identityA))
		let := rec.withReason(status.AdoptedElsewhereReason)
		Expect(let).To(HaveLen(1))
		Expect(let[0].note).To(ContainSubstring(labels.AnnotationAdopt+"="+identityA),
			"the verdict was asked with the render's identity")
	})

	It("lets an adopted object go on matching digests, is then a no-op, and takes the object back", func() {
		nn := start("pkg-own-letgo", "pol-app", "pol-adopted")
		adopt("pol-adopted", identityX)
		before := liveConfigMapNamed("pol-adopted").ResourceVersion

		reconcilePackage(nn, ownedRender(identityA, "v1", "pol-app", "pol-adopted"))
		Expect(expectReady(nn).Message).To(ContainSubstring("1 rendered object(s) are adopted"))
		Expect(liveConfigMapNamed("pol-adopted").ResourceVersion).To(Equal(before))
		Expect(inventoryNames(packageOf(nn).Status.Inventory)).To(ConsistOf("ConfigMap/pol-app"))
		Expect(rec.withReason(status.AdoptedElsewhereReason)).To(HaveLen(1))

		By("the next reconcile with matching digests is a no-op and reports nothing new")
		rec.events = nil
		applied := f.applied
		reconcilePackage(nn, ownedRender(identityA, "v1", "pol-app", "pol-adopted"))
		Expect(expectReady(nn).Message).To(ContainSubstring("1 rendered object(s) are adopted"))
		Expect(rec.withReason(status.NoOpReason)).To(HaveLen(1))
		Expect(rec.withReason(status.AdoptedElsewhereReason)).To(BeEmpty())
		Expect(f.applied).To(Equal(applied))

		By("annotated back for the package: taken in on matching digests")
		adopt("pol-adopted", identityA)
		setConfigMapMeta("pol-adopted", func(cm *corev1.ConfigMap) { cm.Data["payload"] = "the-adopter's" })
		reconcilePackage(nn, ownedRender(identityA, "v1", "pol-app", "pol-adopted"))
		Expect(expectReady(nn).Message).To(Equal("Reconciliation succeeded"))
		Expect(inventoryNames(packageOf(nn).Status.Inventory)).To(ConsistOf("ConfigMap/pol-app", "ConfigMap/pol-adopted"))
		Expect(liveConfigMapNamed("pol-adopted").Data["payload"]).To(Equal("v1"))
	})

	// A package that applies on matching digests applies the rendered set:
	// it has no restore step. So it would write an inventoried object that
	// is being deleted, and that refuses it until the object is gone.
	It("refuses a take-in on matching digests while an inventoried object is being deleted", func() {
		nn := start("pkg-own-term", "pot-app", "pot-held", "pot-taken")
		names := []string{"pot-app", "pot-held", "pot-taken"}
		adopt("pot-taken", identityX)
		reconcilePackage(nn, ownedRender(identityA, "v1", names...))
		expectReady(nn)
		Expect(inventoryNames(packageOf(nn).Status.Inventory)).To(ConsistOf("ConfigMap/pot-app", "ConfigMap/pot-held"))

		setConfigMapMeta("pot-held", func(cm *corev1.ConfigMap) { cm.Finalizers = []string{"test.opmodel.dev/hold"} })
		Expect(k8sClient.Delete(ctx, liveConfigMapNamed("pot-held"))).To(Succeed())
		released := false
		release := func() {
			if !released {
				released = true
				setConfigMapMeta("pot-held", func(cm *corev1.ConfigMap) { cm.Finalizers = nil })
			}
		}
		DeferCleanup(release)

		By("a no-op writes nothing over the object, so nothing is refused")
		rec.events = nil
		reconcilePackage(nn, ownedRender(identityA, "v1", names...))
		expectReady(nn)
		Expect(rec.withReason(status.NoOpReason)).To(HaveLen(1))
		Expect(rec.withReason(status.ApplyRefusedReason)).To(BeEmpty())

		By("an object to take in makes the reconcile an apply of the rendered set")
		adopt("pot-taken", identityA)
		setConfigMapMeta("pot-app", func(cm *corev1.ConfigMap) { cm.Data["payload"] = "by-hand" })
		taken := liveConfigMapNamed("pot-taken").ResourceVersion
		edited := liveConfigMapNamed("pot-app").ResourceVersion
		applied := f.applied
		rec.events = nil
		res := reconcilePackage(nn, ownedRender(identityA, "v1", names...))

		ready := cond(nn, status.ReadyCondition)
		Expect(ready.Status).To(Equal(metav1.ConditionFalse))
		Expect(ready.Reason).To(Equal(status.ApplyRefusedReason))
		Expect(ready.Message).To(ContainSubstring("pot-held is being deleted"))
		Expect(cond(nn, status.StalledCondition)).To(BeNil())
		Expect(res.RequeueAfter).To(BeNumerically(">", 0))
		Expect(rec.withReason(status.ApplyRefusedReason)).To(HaveLen(1))
		Expect(f.applied).To(Equal(applied), "nothing is written")
		Expect(liveConfigMapNamed("pot-taken").ResourceVersion).To(Equal(taken))
		Expect(liveConfigMapNamed("pot-app").ResourceVersion).To(Equal(edited))
		Expect(inventoryNames(packageOf(nn).Status.Inventory)).To(ConsistOf("ConfigMap/pot-app", "ConfigMap/pot-held"))

		By("the object is gone: the retry applies the whole render")
		release()
		Expect(configMapExists("pot-held")).To(BeFalse())
		reconcilePackage(nn, ownedRender(identityA, "v1", names...))
		Expect(expectReady(nn).Message).To(Equal("Reconciliation succeeded"))
		Expect(inventoryNames(packageOf(nn).Status.Inventory)).To(ConsistOf(
			"ConfigMap/pot-app", "ConfigMap/pot-held", "ConfigMap/pot-taken"))
		Expect(configMapExists("pot-held")).To(BeTrue(), "the deleted object is created again")
	})

	It("fails with matching digests when an object cannot be read, and keeps the inventory", func() {
		nn := start("pkg-own-read", "pord-app")

		f.failRead = true
		res := reconcilePackage(nn, ownedRender(identityA, "v1", "pord-app"))
		f.failRead = false

		ready := cond(nn, status.ReadyCondition)
		Expect(ready.Status).To(Equal(metav1.ConditionFalse))
		Expect(ready.Reason).To(Equal(status.ApplyFailedReason))
		Expect(ready.Message).To(ContainSubstring("reading ConfigMap/" + namespace + "/pord-app before the apply"))
		Expect(cond(nn, status.StalledCondition)).To(BeNil())
		Expect(res.RequeueAfter).To(BeNumerically(">", 0))
		Expect(rec.withReason(status.NoOpReason)).To(BeEmpty())
		Expect(inventoryNames(packageOf(nn).Status.Inventory)).To(ConsistOf("ConfigMap/pord-app"))
	})

	It("stalls with matching digests when its ServiceAccount is gone", func() {
		nn := start("pkg-own-nosa", "pos-app")
		pkg := packageOf(nn)
		pkg.Spec.ServiceAccountName = "pkg-own-gone-sa"
		Expect(k8sClient.Update(ctx, pkg)).To(Succeed())

		reconcilePackage(nn, ownedRender(identityA, "v1", "pos-app"))

		stalled := cond(nn, status.StalledCondition)
		Expect(stalled).NotTo(BeNil())
		Expect(stalled.Status).To(Equal(metav1.ConditionTrue))
		Expect(stalled.Reason).To(Equal(status.ImpersonationFailedReason))
		Expect(rec.withReason(status.NoOpReason)).To(BeEmpty(), "not a no-op")
		Expect(inventoryNames(packageOf(nn).Status.Inventory)).To(ConsistOf("ConfigMap/pos-app"))
	})

	It("stalls with matching digests when its ServiceAccount may not read a rendered kind", func() {
		nn := start("pkg-own-noget", "pog-app")
		const saName, roleName = "pkg-own-noget-sa", "pkg-own-noget-role"
		sa := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: saName, Namespace: namespace}}
		role := &rbacv1.ClusterRole{
			ObjectMeta: metav1.ObjectMeta{Name: roleName},
			Rules: []rbacv1.PolicyRule{{
				APIGroups: []string{""}, Resources: []string{"configmaps"},
				Verbs: []string{"list", "watch", "create", "update", "patch", "delete"},
			}},
		}
		binding := &rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: roleName},
			RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: roleName},
			Subjects:   []rbacv1.Subject{{Kind: rbacv1.ServiceAccountKind, Name: saName, Namespace: namespace}},
		}
		for _, obj := range []client.Object{sa, role, binding} {
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
		}
		DeferCleanup(func() {
			for _, obj := range []client.Object{binding, role, sa} {
				Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, obj))).To(Succeed())
			}
		})
		pkg := packageOf(nn)
		pkg.Spec.ServiceAccountName = saName
		Expect(k8sClient.Update(ctx, pkg)).To(Succeed())
		before := liveConfigMapNamed("pog-app").ResourceVersion

		reconcilePackage(nn, ownedRender(identityA, "v1", "pog-app"))

		stalled := cond(nn, status.StalledCondition)
		Expect(stalled).NotTo(BeNil())
		Expect(stalled.Status).To(Equal(metav1.ConditionTrue))
		Expect(stalled.Reason).To(Equal(status.ImpersonationFailedReason))
		Expect(stalled.Message).To(ContainSubstring("system:serviceaccount:" + namespace + ":" + saName))
		Expect(stalled.Message).To(ContainSubstring("cannot get"))
		Expect(liveConfigMapNamed("pog-app").ResourceVersion).To(Equal(before), "nothing is written")
		Expect(inventoryNames(packageOf(nn).Status.Inventory)).To(ConsistOf("ConfigMap/pog-app"))
	})
})
