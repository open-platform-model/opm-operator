package reconcile_test

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
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

var _ = Describe("Instance identities of a ModulePackage", func() {
	const sourceName = "identity-src"
	var (
		rec    *actionRecorder
		params *opmreconcile.ModulePackageParams
	)

	BeforeEach(func() {
		rec = &actionRecorder{}
		realClient, err := client.NewWithWatch(cfg, client.Options{Scheme: k8sClient.Scheme()})
		Expect(err).NotTo(HaveOccurred())
		params = &opmreconcile.ModulePackageParams{
			Client:          readySource(realClient, sourceName),
			ResourceManager: apply.NewResourceManager(k8sClient, "opm-controller"),
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

	packageStatus := func(nn types.NamespacedName) releasesv1alpha1.ModulePackageStatus {
		GinkgoHelper()
		var pkg releasesv1alpha1.ModulePackage
		Expect(k8sClient.Get(ctx, nn, &pkg)).To(Succeed())
		return pkg.Status
	}

	expectPackageIdentities := func(nn types.NamespacedName, current, previous string) {
		GinkgoHelper()
		st := packageStatus(nn)
		Expect(st.InstanceUUID).To(Equal(current), "status.instanceUUID")
		Expect(st.PreviousInstanceUUID).To(Equal(previous), "status.previousInstanceUUID")
	}

	// start creates a package and reconciles it to Ready with identity A and
	// the named ConfigMaps.
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
		params.Renderer = &stubPackageRenderer{result: identityRender(identityA, names...)}
		res, err := opmreconcile.ReconcileModulePackage(ctx, params, ctrl.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(ctrl.Result{Requeue: true}), "the first reconcile registers the finalizer")
		reconcilePackage(nn, identityRender(identityA, names...))
		rec.events = nil
		return nn
	}

	// forgetIdentity removes the recorded identity, as a package reconciled
	// by a release older than the field has none.
	forgetIdentity := func(nn types.NamespacedName) {
		GinkgoHelper()
		var pkg releasesv1alpha1.ModulePackage
		Expect(k8sClient.Get(ctx, nn, &pkg)).To(Succeed())
		pkg.Status.InstanceUUID = ""
		Expect(k8sClient.Status().Update(ctx, &pkg)).To(Succeed())
	}

	removePackage := func(nn types.NamespacedName) {
		GinkgoHelper()
		var pkg releasesv1alpha1.ModulePackage
		if err := k8sClient.Get(ctx, nn, &pkg); err != nil {
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
			return
		}
		pkg.Finalizers = nil
		Expect(k8sClient.Update(ctx, &pkg)).To(Succeed())
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &pkg))).To(Succeed())
	}

	deletePackage := func(nn types.NamespacedName) {
		GinkgoHelper()
		Expect(k8sClient.Delete(ctx, &releasesv1alpha1.ModulePackage{
			ObjectMeta: metav1.ObjectMeta{Name: nn.Name, Namespace: nn.Namespace},
		})).To(Succeed())
		_, err := opmreconcile.ReconcileModulePackage(ctx, params, ctrl.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())
		err = k8sClient.Get(ctx, nn, &releasesv1alpha1.ModulePackage{})
		Expect(apierrors.IsNotFound(err)).To(BeTrue(), "the finalizer must be removed, got %v", err)
	}

	It("records the identity of the instance it renders", func() {
		nn := start("pkg-id-first", "pif-app")
		DeferCleanup(func() { removeConfigMaps("pif-app"); removePackage(nn) })
		expectPackageIdentities(nn, identityA, "")
	})

	It("gains the field without an apply when it was reconciled before the field existed", func() {
		nn := start("pkg-id-gain", "pig-app")
		DeferCleanup(func() { removeConfigMaps("pig-app"); removePackage(nn) })
		forgetIdentity(nn)

		reconcilePackage(nn, identityRender(identityA, "pig-app"))

		Expect(rec.withReason(status.NoOpReason)).To(HaveLen(1))
		Expect(rec.withReason(status.AppliedReason)).To(BeEmpty())
		expectPackageIdentities(nn, identityA, "")
	})

	It("deletes its stale objects when the first render after the upgrade also changes the identity", func() {
		nn := start("pkg-id-upgrade", "piu-app", "piu-old")
		DeferCleanup(func() { removeConfigMaps("piu-app", "piu-old"); removePackage(nn) })
		forgetIdentity(nn)

		reconcilePackage(nn, identityRender(identityB, "piu-app"))

		Expect(configMapExists("piu-old")).To(BeFalse(), "deleted on its managed-by label, as before the field existed")
		expectPackageIdentities(nn, identityB, "")
	})

	It("keeps both identities until an identity change is settled, and deletes the stale objects", func() {
		nn := start("pkg-id-change", "pic-app", "pic-old")
		DeferCleanup(func() { removeConfigMaps("pic-app", "pic-old"); removePackage(nn) })

		reconcilePackage(nn, identityRender(identityB, "pic-app"))

		Expect(configMapExists("pic-old")).To(BeFalse())
		Expect(liveConfigMapLabels("pic-app")).To(HaveKeyWithValue(labels.ModuleInstanceUUID, identityB))
		expectPackageIdentities(nn, identityB, "")
	})

	It("does not prune another instance's object once an identity is recorded", func() {
		nn := start("pkg-id-other", "pio-app", "pio-other")
		DeferCleanup(func() { removeConfigMaps("pio-app", "pio-other"); removePackage(nn) })
		setConfigMapMeta("pio-other", func(cm *corev1.ConfigMap) { cm.Labels[labels.ModuleInstanceUUID] = identityX })

		reconcilePackage(nn, identityRender(identityA, "pio-app"))

		Expect(configMapExists("pio-other")).To(BeTrue())
		st := packageStatus(nn)
		Expect(inventoryNames(st.Inventory)).To(ConsistOf("ConfigMap/pio-app"))
		Expect(apimeta.FindStatusCondition(st.Conditions, status.ReadyCondition).Status).To(Equal(metav1.ConditionTrue))
		left := rec.withReason(status.LeftBehindReason)
		Expect(left).To(HaveLen(1))
		Expect(left[0].eventType).To(Equal(corev1.EventTypeWarning))
		Expect(left[0].action).To(Equal("Prune"))
		Expect(left[0].note).To(ContainSubstring("ConfigMap/default/pio-other belongs to module instance " + identityX))
	})

	It("refuses a third identity while a change is not settled", func() {
		nn := start("pkg-id-third", "pit-app")
		DeferCleanup(func() { removeConfigMaps("pit-app", "pit-new"); removePackage(nn) })
		var pkg releasesv1alpha1.ModulePackage
		Expect(k8sClient.Get(ctx, nn, &pkg)).To(Succeed())
		pkg.Status.InstanceUUID, pkg.Status.PreviousInstanceUUID = identityB, identityA
		Expect(k8sClient.Status().Update(ctx, &pkg)).To(Succeed())

		res := reconcilePackage(nn, identityRender(identityC, "pit-app", "pit-new"))
		reconcilePackage(nn, identityRender(identityC, "pit-app", "pit-new"))

		Expect(res.RequeueAfter).To(Equal(opmreconcile.StalledRecheckInterval))
		Expect(configMapExists("pit-new")).To(BeFalse())
		Expect(liveConfigMapLabels("pit-app")).To(HaveKeyWithValue(labels.ModuleInstanceUUID, identityA))
		expectPackageIdentities(nn, identityB, identityA)
		st := packageStatus(nn)
		rdy := apimeta.FindStatusCondition(st.Conditions, status.ReadyCondition)
		Expect(rdy.Reason).To(Equal(status.IdentityChangeUnsettledReason))
		stalled := apimeta.FindStatusCondition(st.Conditions, status.StalledCondition)
		Expect(stalled).NotTo(BeNil())
		Expect(stalled.Status).To(Equal(metav1.ConditionTrue))
		Expect(rec.withReason(status.IdentityChangeUnsettledReason)).To(HaveLen(1))
		Expect(rec.withReason(status.AppliedReason)).To(BeEmpty())
	})

	It("judges the deletion cleanup with the recorded identity", func() {
		nn := start("pkg-id-delete", "pid-own", "pid-other")
		DeferCleanup(func() { removeConfigMaps("pid-own", "pid-other"); removePackage(nn) })
		setConfigMapMeta("pid-other", func(cm *corev1.ConfigMap) { cm.Labels[labels.ModuleInstanceUUID] = identityX })

		deletePackage(nn)

		Expect(configMapExists("pid-own")).To(BeFalse())
		Expect(configMapExists("pid-other")).To(BeTrue())
		left := rec.withReason(status.LeftBehindReason)
		Expect(left).To(HaveLen(1))
		Expect(left[0].action).To(Equal("Delete"))
	})

	It("deletes a package with no recorded identity as before, and leaves an annotated object", func() {
		nn := start("pkg-id-none", "pin-plain", "pin-other", "pin-annotated")
		DeferCleanup(func() { removeConfigMaps("pin-plain", "pin-other", "pin-annotated"); removePackage(nn) })
		forgetIdentity(nn)
		setConfigMapMeta("pin-other", func(cm *corev1.ConfigMap) { cm.Labels[labels.ModuleInstanceUUID] = identityX })
		setConfigMapMeta("pin-annotated", func(cm *corev1.ConfigMap) {
			cm.Annotations = map[string]string{labels.AnnotationAdopt: identityA}
		})

		deletePackage(nn)

		Expect(configMapExists("pin-plain")).To(BeFalse())
		Expect(configMapExists("pin-other")).To(BeFalse(), "managed by OPM, whatever its UUID label")
		Expect(configMapExists("pin-annotated")).To(BeTrue(), "no identity is known to compare the annotation with")
		left := rec.withReason(status.LeftBehindReason)
		Expect(left).To(HaveLen(1))
		Expect(left[0].eventType).To(Equal(corev1.EventTypeWarning))
		Expect(left[0].note).To(ContainSubstring(
			"ConfigMap/default/pin-annotated is being adopted by module instance " + identityA))
	})
})
