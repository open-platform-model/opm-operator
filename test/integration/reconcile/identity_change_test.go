package reconcile_test

import (
	"context"
	"errors"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"cuelang.org/go/cue/cuecontext"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/open-platform-model/library/opm/k8s/labels"
	"github.com/open-platform-model/library/opm/k8s/object"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	"github.com/open-platform-model/opm-operator/internal/apply"
	opmreconcile "github.com/open-platform-model/opm-operator/internal/reconcile"
	"github.com/open-platform-model/opm-operator/internal/render"
	"github.com/open-platform-model/opm-operator/internal/status"
)

// Three instance identities. A render carries one of them as the UUID label
// of every object; a render that carries another one is what a changed
// spec.module.path produces.
const (
	identityA = "00000000-0000-0000-0000-00000000000a"
	identityB = "00000000-0000-0000-0000-00000000000b"
	identityC = "00000000-0000-0000-0000-00000000000c"
	identityX = "00000000-0000-0000-0000-0000000000ff"
)

// identityRender renders one ConfigMap per name, each carrying uuid as its
// instance identity.
func identityRender(uuid string, names ...string) *render.RenderResult {
	cueCtx := cuecontext.New()
	result := &render.RenderResult{}
	for _, name := range names {
		cm := cueCtx.CompileString(fmt.Sprintf(`{
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
	data: name: %q
}`, name, namespace, labels.ManagedBy, labels.ManagedByController, labels.ModuleInstanceUUID, uuid, name))
		if cm.Err() != nil {
			panic(fmt.Sprintf("compiling identity ConfigMap: %v", cm.Err()))
		}
		result.Resources = append(result.Resources, &object.Resource{
			Value: cm, Instance: name, Component: name, Transformer: "kubernetes#simple",
		})
	}
	return result
}

// faults is a client whose deletes, applies and status writes a test can make
// fail. The zero value fails nothing.
type faults struct {
	failDelete string // name of the object whose DELETE fails
	// replaceOnDelete names a ConfigMap that is deleted and created again
	// just before its first DELETE is forwarded.
	replaceOnDelete string
	failApply       bool // every server-side apply that is no dry run fails
	failStatus      bool // every status write fails
	applied         int  // server-side applies that reached the API server
	deleteCalls     int
}

func (f *faults) client() client.WithWatch {
	realClient, err := client.NewWithWatch(cfg, client.Options{Scheme: k8sClient.Scheme()})
	Expect(err).NotTo(HaveOccurred())
	return interceptor.NewClient(realClient, interceptor.Funcs{
		Delete: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
			f.deleteCalls++
			if f.failDelete != "" && obj.GetName() == f.failDelete {
				return errors.New("injected delete failure")
			}
			if f.replaceOnDelete != "" && obj.GetName() == f.replaceOnDelete {
				// Another writer deletes the object and creates it again
				// between the prune's read and its DELETE.
				name := f.replaceOnDelete
				f.replaceOnDelete = ""
				removeConfigMaps(name)
				createLiveConfigMap(name, labels.ManagedByController, identityA, "")
			}
			return c.Delete(ctx, obj, opts...)
		},
		Patch: func(
			ctx context.Context, c client.WithWatch, obj client.Object, p client.Patch, opts ...client.PatchOption,
		) error {
			po := &client.PatchOptions{}
			po.ApplyOptions(opts)
			if p.Type() == types.ApplyPatchType && len(po.DryRun) == 0 {
				if f.failApply {
					return errors.New("injected apply failure")
				}
				f.applied++
			}
			return c.Patch(ctx, obj, p, opts...)
		},
		SubResourcePatch: func(
			ctx context.Context, c client.Client, sub string, obj client.Object, p client.Patch,
			opts ...client.SubResourcePatchOption,
		) error {
			if f.failStatus && sub == "status" {
				return errors.New("injected status failure")
			}
			return c.SubResource(sub).Patch(ctx, obj, p, opts...)
		},
	})
}

// panicOnApplied is a recorder that panics when the reconcile reports its
// apply: after the identities were stored and the objects written.
type panicOnApplied struct{ actionRecorder }

func (r *panicOnApplied) Eventf(a, b runtime.Object, eventType, reason, action, note string, args ...any) {
	if reason == status.AppliedReason {
		panic("injected panic after the apply")
	}
	r.actionRecorder.Eventf(a, b, eventType, reason, action, note, args...)
}

func liveConfigMapNamed(name string) *corev1.ConfigMap {
	GinkgoHelper()
	cm := &corev1.ConfigMap{}
	Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, cm)).To(Succeed())
	return cm
}

func liveConfigMapLabels(name string) map[string]string {
	GinkgoHelper()
	return liveConfigMapNamed(name).Labels
}

// createLiveConfigMap creates a ConfigMap as another writer would: with the
// given managed-by value and identity (each left out when empty) and adopt
// annotation.
func createLiveConfigMap(name, managedBy, uuid, adopt string) {
	GinkgoHelper()
	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: map[string]string{}}}
	if managedBy != "" {
		cm.Labels[labels.ManagedBy] = managedBy
	}
	if uuid != "" {
		cm.Labels[labels.ModuleInstanceUUID] = uuid
	}
	if adopt != "" {
		cm.Annotations = map[string]string{labels.AnnotationAdopt: adopt}
	}
	Expect(k8sClient.Create(ctx, cm)).To(Succeed())
}

func setConfigMapMeta(name string, mutate func(*corev1.ConfigMap)) {
	GinkgoHelper()
	var cm corev1.ConfigMap
	Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, &cm)).To(Succeed())
	mutate(&cm)
	Expect(k8sClient.Update(ctx, &cm)).To(Succeed())
}

func instanceStatus(nn types.NamespacedName) releasesv1alpha1.ModuleInstanceStatus {
	GinkgoHelper()
	var mi releasesv1alpha1.ModuleInstance
	Expect(k8sClient.Get(ctx, nn, &mi)).To(Succeed())
	return mi.Status
}

func expectIdentities(nn types.NamespacedName, current, previous string) {
	GinkgoHelper()
	st := instanceStatus(nn)
	Expect(st.InstanceUUID).To(Equal(current), "status.instanceUUID")
	Expect(st.PreviousInstanceUUID).To(Equal(previous), "status.previousInstanceUUID")
}

func removeInstance(nn types.NamespacedName) {
	GinkgoHelper()
	var mi releasesv1alpha1.ModuleInstance
	if err := k8sClient.Get(ctx, nn, &mi); err != nil {
		Expect(apierrors.IsNotFound(err)).To(BeTrue())
		return
	}
	mi.Finalizers = nil
	Expect(k8sClient.Update(ctx, &mi)).To(Succeed())
	Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &mi))).To(Succeed())
}

func instanceGone(nn types.NamespacedName) bool {
	GinkgoHelper()
	err := k8sClient.Get(ctx, nn, &releasesv1alpha1.ModuleInstance{})
	return apierrors.IsNotFound(err)
}

var _ = Describe("Instance identities of a ModuleInstance", func() {
	var (
		rec    *actionRecorder
		f      *faults
		params *opmreconcile.ModuleInstanceParams
	)

	BeforeEach(func() {
		rec = &actionRecorder{}
		f = &faults{}
		c := f.client()
		params = &opmreconcile.ModuleInstanceParams{
			Client:          c,
			ResourceManager: apply.NewResourceManager(c, "opm-controller"),
			EventRecorder:   rec,
		}
	})

	// start creates an instance and reconciles it to Ready with identity A
	// and the named ConfigMaps.
	start := func(name string, names ...string) types.NamespacedName {
		GinkgoHelper()
		createModuleInstance(name)
		nn := types.NamespacedName{Name: name, Namespace: namespace}
		params.Renderer = &stubRenderer{result: identityRender(identityA, names...)}
		ensureFinalizer(params, nn)
		Expect(reconcileInstance(params, nn)).To(Succeed())
		expectIdentities(nn, identityA, "")
		rec.events = nil
		return nn
	}

	reconcileWith := func(nn types.NamespacedName, r *render.RenderResult) (time.Duration, error) {
		GinkgoHelper()
		params.Renderer = &stubRenderer{result: r}
		res, err := opmreconcile.ReconcileModuleInstance(ctx, params, ctrl.Request{NamespacedName: nn})
		return res.RequeueAfter, err
	}

	ready := func(nn types.NamespacedName) *metav1.Condition {
		GinkgoHelper()
		st := instanceStatus(nn)
		return apimeta.FindStatusCondition(st.Conditions, status.ReadyCondition)
	}

	It("records the identity on the first reconcile", func() {
		nn := start("id-first", "idf-app")
		DeferCleanup(func() { removeConfigMaps("idf-app"); removeInstance(nn) })
		Expect(ready(nn).Status).To(Equal(metav1.ConditionTrue))
	})

	It("deletes the stale objects of the earlier identity after an identity change", func() {
		nn := start("id-change", "idc-app", "idc-old")
		DeferCleanup(func() { removeConfigMaps("idc-app", "idc-old"); removeInstance(nn) })

		_, err := reconcileWith(nn, identityRender(identityB, "idc-app"))
		Expect(err).NotTo(HaveOccurred())

		expectConfigMapGone("idc-old", "it carries the earlier identity: the instance's own")
		Expect(liveConfigMapLabels("idc-app")).To(HaveKeyWithValue(labels.ModuleInstanceUUID, identityB))
		expectIdentities(nn, identityB, "")
		Expect(inventoryNames(instanceStatus(nn).Inventory)).To(ConsistOf("ConfigMap/idc-app"))
		Expect(rec.withReason(status.LeftBehindReason)).To(BeEmpty())
	})

	It("leaves nothing orphaned when the prune after an identity change fails", func() {
		nn := start("id-retry", "idr-app", "idr-old", "idr-older")
		DeferCleanup(func() { removeConfigMaps("idr-app", "idr-old", "idr-older"); removeInstance(nn) })
		before := instanceStatus(nn).Inventory.DeepCopy()

		f.failDelete = "idr-old"
		_, err := reconcileWith(nn, identityRender(identityB, "idr-app"))
		Expect(err).NotTo(HaveOccurred())

		expectIdentities(nn, identityB, identityA)
		Expect(instanceStatus(nn).Inventory.Entries).To(Equal(before.Entries), "a failed prune keeps the inventory")
		Expect(ready(nn).Reason).To(Equal(status.PruneFailedReason))
		Expect(configMapExists("idr-old")).To(BeTrue())
		Expect(rec.withReason(status.LeftBehindReason)).To(BeEmpty(), "a failed prune reports nothing as left behind")

		f.failDelete = ""
		_, err = reconcileWith(nn, identityRender(identityB, "idr-app"))
		Expect(err).NotTo(HaveOccurred())

		expectConfigMapGone("idr-old")
		expectConfigMapGone("idr-older")
		expectIdentities(nn, identityB, "")
		Expect(ready(nn).Status).To(Equal(metav1.ConditionTrue))
	})

	It("stores both identities before an apply that then fails", func() {
		nn := start("id-applyfail", "idaf-app")
		DeferCleanup(func() { removeConfigMaps("idaf-app"); removeInstance(nn) })
		before := instanceStatus(nn).Inventory.DeepCopy()

		f.failApply = true
		_, err := reconcileWith(nn, identityRender(identityB, "idaf-app"))
		Expect(err).NotTo(HaveOccurred())

		expectIdentities(nn, identityB, identityA)
		Expect(instanceStatus(nn).Inventory.Entries).To(Equal(before.Entries))
		Expect(ready(nn).Status).To(Equal(metav1.ConditionFalse))
	})

	It("keeps both identities when the apply succeeds and the reconcile is refused afterwards", func() {
		providerName := "id-refused"
		claimName := namespace + "." + providerName
		consumer := demandContracts("id-refused-consumer", contractBackup)
		storeClaim(claimName, providerName, contractStorage, contractBackup)
		createModuleInstance(providerName)
		nn := types.NamespacedName{Name: providerName, Namespace: namespace}
		DeferCleanup(func() {
			removeConfigMaps("idrf-marker")
			cleanupShrinkFixtures(claimName, providerName, nn, consumer)
		})
		params.APIReader = k8sClient

		first := providerRenderResult(claimName, providerName, contractStorage, contractBackup)
		params.Renderer = &stubRenderer{result: first}
		ensureFinalizer(params, nn)
		Expect(reconcileInstance(params, nn)).To(Succeed())
		expectIdentities(nn, stubInstanceUUID, "")
		inventory := instanceStatus(nn).Inventory.DeepCopy()

		By("a render of another identity that also shrinks the claim: applied, then refused")
		shrinking := providerRenderResult(claimName, providerName, contractStorage)
		shrinking.Resources = append(identityRender(identityB, "idrf-marker").Resources, shrinking.Resources...)
		requeue, err := reconcileWith(nn, shrinking)
		Expect(err).NotTo(HaveOccurred())

		Expect(requeue).To(BeNumerically(">", 0))
		Expect(ready(nn).Reason).To(Equal(status.DependentsRemainReason))
		Expect(configMapExists("idrf-marker")).To(BeTrue(), "the apply ran")
		expectIdentities(nn, identityB, stubInstanceUUID)
		Expect(instanceStatus(nn).Inventory.Entries).To(Equal(inventory.Entries))
	})

	It("keeps both identities when the reconcile panics after they were stored", func() {
		nn := start("id-panic", "idp-app")
		DeferCleanup(func() { removeConfigMaps("idp-app"); removeInstance(nn) })

		params.EventRecorder = &panicOnApplied{}
		params.Renderer = &stubRenderer{result: identityRender(identityB, "idp-app")}
		Expect(func() {
			_, _ = opmreconcile.ReconcileModuleInstance(ctx, params, ctrl.Request{NamespacedName: nn})
		}).To(PanicWith("injected panic after the apply"))

		expectIdentities(nn, identityB, identityA)
		Expect(ready(nn).Reason).To(Equal(status.ReconcilePanicReason))
		Expect(liveConfigMapLabels("idp-app")).To(HaveKeyWithValue(labels.ModuleInstanceUUID, identityB))
	})

	It("applies nothing when the identities cannot be stored", func() {
		nn := start("id-nostore", "idn-app")
		DeferCleanup(func() { removeConfigMaps("idn-app", "idn-new"); removeInstance(nn) })

		f.failStatus = true
		f.applied = 0
		requeue, err := reconcileWith(nn, identityRender(identityB, "idn-app", "idn-new"))
		Expect(err).NotTo(HaveOccurred())

		Expect(requeue).To(BeNumerically(">", 0), "the reconcile is retried")
		Expect(f.applied).To(BeZero(), "no object of the render is applied")
		expectConfigMapGone("idn-new")
		Expect(liveConfigMapLabels("idn-app")).To(HaveKeyWithValue(labels.ModuleInstanceUUID, identityA))
		expectIdentities(nn, identityA, "")

		f.failStatus = false
		_, err = reconcileWith(nn, identityRender(identityB, "idn-app", "idn-new"))
		Expect(err).NotTo(HaveOccurred())
		expectIdentities(nn, identityB, "")
		Expect(configMapExists("idn-new")).To(BeTrue())
	})

	It("swaps the identities when the user goes back before the change is settled", func() {
		nn := start("id-back", "idb-app", "idb-old")
		DeferCleanup(func() { removeConfigMaps("idb-app", "idb-old"); removeInstance(nn) })

		f.failDelete = "idb-old"
		_, err := reconcileWith(nn, identityRender(identityB, "idb-app"))
		Expect(err).NotTo(HaveOccurred())
		expectIdentities(nn, identityB, identityA)

		By("going back to A while the apply fails: the swap is stored before the apply")
		f.failApply = true
		_, err = reconcileWith(nn, identityRender(identityA, "idb-app", "idb-old"))
		Expect(err).NotTo(HaveOccurred())
		expectIdentities(nn, identityA, identityB)

		By("the retry relabels back and settles")
		f.failApply, f.failDelete = false, ""
		_, err = reconcileWith(nn, identityRender(identityA, "idb-app", "idb-old"))
		Expect(err).NotTo(HaveOccurred())
		expectIdentities(nn, identityA, "")
		Expect(liveConfigMapLabels("idb-app")).To(HaveKeyWithValue(labels.ModuleInstanceUUID, identityA))
		Expect(ready(nn).Status).To(Equal(metav1.ConditionTrue))
	})

	It("deletes a stale object that an unsettled apply already relabelled", func() {
		nn := start("id-relabelled", "idl-app", "idl-keep", "idl-old")
		DeferCleanup(func() { removeConfigMaps("idl-app", "idl-keep", "idl-old"); removeInstance(nn) })

		f.failDelete = "idl-old"
		_, err := reconcileWith(nn, identityRender(identityB, "idl-app", "idl-keep"))
		Expect(err).NotTo(HaveOccurred())
		expectIdentities(nn, identityB, identityA)
		Expect(liveConfigMapLabels("idl-app")).To(HaveKeyWithValue(labels.ModuleInstanceUUID, identityB))

		By("a later render of B drops the relabelled idl-app")
		f.failDelete = ""
		_, err = reconcileWith(nn, identityRender(identityB, "idl-keep"))
		Expect(err).NotTo(HaveOccurred())

		expectConfigMapGone("idl-app", "labelled B, the instance's own")
		expectConfigMapGone("idl-old", "labelled A, the instance's own")
		expectIdentities(nn, identityB, "")
	})

	It("gains the field without an apply when it was recorded before the field existed", func() {
		nn := start("id-gain", "idg-app")
		DeferCleanup(func() { removeConfigMaps("idg-app"); removeInstance(nn) })

		var mi releasesv1alpha1.ModuleInstance
		Expect(k8sClient.Get(ctx, nn, &mi)).To(Succeed())
		mi.Status.InstanceUUID = ""
		Expect(k8sClient.Status().Update(ctx, &mi)).To(Succeed())
		expectIdentities(nn, "", "")

		f.applied = 0
		_, err := reconcileWith(nn, identityRender(identityA, "idg-app"))
		Expect(err).NotTo(HaveOccurred())

		Expect(rec.withReason(status.NoOpReason)).To(HaveLen(1))
		Expect(f.applied).To(BeZero())
		expectIdentities(nn, identityA, "")
	})

	Context("no recorded identity", func() {
		// unrecorded reconciles an instance to Ready and then removes the
		// recorded identity, as an inventory written by a release older than
		// the field has none.
		unrecorded := func(name string, names ...string) types.NamespacedName {
			GinkgoHelper()
			nn := start(name, names...)
			var mi releasesv1alpha1.ModuleInstance
			Expect(k8sClient.Get(ctx, nn, &mi)).To(Succeed())
			mi.Status.InstanceUUID = ""
			Expect(k8sClient.Status().Update(ctx, &mi)).To(Succeed())
			return nn
		}

		It("leaves a stale object that carries another instance's UUID, and deletes one annotated for this instance", func() {
			nn := unrecorded("id-none", "idz-app", "idz-earlier", "idz-mine", "idz-theirs")
			DeferCleanup(func() {
				removeConfigMaps("idz-app", "idz-earlier", "idz-mine", "idz-theirs")
				removeInstance(nn)
			})
			setConfigMapMeta("idz-earlier", func(cm *corev1.ConfigMap) { cm.Labels[labels.ModuleInstanceUUID] = identityX })
			setConfigMapMeta("idz-mine", func(cm *corev1.ConfigMap) {
				delete(cm.Labels, labels.ModuleInstanceUUID)
				cm.Annotations = map[string]string{labels.AnnotationAdopt: identityB}
			})
			setConfigMapMeta("idz-theirs", func(cm *corev1.ConfigMap) {
				delete(cm.Labels, labels.ModuleInstanceUUID)
				cm.Annotations = map[string]string{labels.AnnotationAdopt: identityC}
			})

			_, err := reconcileWith(nn, identityRender(identityB, "idz-app"))
			Expect(err).NotTo(HaveOccurred())

			Expect(configMapExists("idz-earlier")).To(BeTrue(), "another instance's UUID label: left, as before")
			expectConfigMapGone("idz-mine", "annotated for the render's identity")
			Expect(configMapExists("idz-theirs")).To(BeTrue(), "annotated for another instance")
			expectIdentities(nn, identityB, "")
			Expect(inventoryNames(instanceStatus(nn).Inventory)).To(ConsistOf("ConfigMap/idz-app"))
			left := rec.withReason(status.LeftBehindReason)
			Expect(left).To(HaveLen(1))
			Expect(left[0].eventType).To(Equal(corev1.EventTypeWarning))
			Expect(left[0].note).To(HavePrefix("Left 2 object(s) in the cluster: "))
			Expect(left[0].note).To(ContainSubstring(
				"ConfigMap/default/idz-earlier belongs to module instance " + identityX))
			Expect(left[0].note).To(ContainSubstring(
				"ConfigMap/default/idz-theirs is being adopted by module instance " + identityC))
		})
	})

	Context("a third identity before the first change is settled", func() {
		unsettled := func(name, app, old string) types.NamespacedName {
			GinkgoHelper()
			nn := start(name, app, old)
			f.failDelete = old
			_, err := reconcileWith(nn, identityRender(identityB, app))
			Expect(err).NotTo(HaveOccurred())
			expectIdentities(nn, identityB, identityA)
			f.failDelete = ""
			rec.events = nil
			return nn
		}

		It("is refused with IdentityChangeUnsettled, once, and clears when the earlier path is restored", func() {
			nn := unsettled("id-third", "idt-app", "idt-old")
			DeferCleanup(func() { removeConfigMaps("idt-app", "idt-old", "idt-new"); removeInstance(nn) })
			inventory := instanceStatus(nn).Inventory.DeepCopy()

			f.applied, f.deleteCalls = 0, 0
			requeue, err := reconcileWith(nn, identityRender(identityC, "idt-app", "idt-new"))
			Expect(err).NotTo(HaveOccurred())
			_, err = reconcileWith(nn, identityRender(identityC, "idt-app", "idt-new"))
			Expect(err).NotTo(HaveOccurred())

			Expect(requeue).To(Equal(opmreconcile.StalledRecheckInterval))
			Expect(f.applied).To(BeZero(), "nothing is applied")
			Expect(f.deleteCalls).To(BeZero(), "nothing is deleted")
			expectConfigMapGone("idt-new")
			Expect(configMapExists("idt-old")).To(BeTrue())
			expectIdentities(nn, identityB, identityA)
			st := instanceStatus(nn)
			Expect(st.Inventory.Entries).To(Equal(inventory.Entries))
			rdy := apimeta.FindStatusCondition(st.Conditions, status.ReadyCondition)
			Expect(rdy.Status).To(Equal(metav1.ConditionFalse))
			Expect(rdy.Reason).To(Equal(status.IdentityChangeUnsettledReason))
			Expect(rdy.Message).To(ContainSubstring("Restore the earlier module path"))
			stalled := apimeta.FindStatusCondition(st.Conditions, status.StalledCondition)
			Expect(stalled).NotTo(BeNil())
			Expect(stalled.Status).To(Equal(metav1.ConditionTrue))
			Expect(stalled.Reason).To(Equal(status.IdentityChangeUnsettledReason))

			refused := rec.withReason(status.IdentityChangeUnsettledReason)
			Expect(refused).To(HaveLen(1), "one event over two reconciles")
			Expect(refused[0].eventType).To(Equal(corev1.EventTypeWarning))
			Expect(refused[0].action).To(Equal("Reconcile"))
			Expect(refused[0].note).To(Equal(rdy.Message))

			By("restoring the earlier path settles the change")
			_, err = reconcileWith(nn, identityRender(identityB, "idt-app"))
			Expect(err).NotTo(HaveOccurred())
			expectIdentities(nn, identityB, "")
			Expect(ready(nn).Status).To(Equal(metav1.ConditionTrue))
			expectConfigMapGone("idt-old")
		})

		It("does not stop the deletion cleanup, which accepts both identities", func() {
			nn := unsettled("id-third-del", "idtd-app", "idtd-old")
			DeferCleanup(func() { removeConfigMaps("idtd-app", "idtd-old"); removeInstance(nn) })
			_, err := reconcileWith(nn, identityRender(identityC, "idtd-app"))
			Expect(err).NotTo(HaveOccurred())
			Expect(ready(nn).Reason).To(Equal(status.IdentityChangeUnsettledReason))

			Expect(k8sClient.Delete(ctx, &releasesv1alpha1.ModuleInstance{
				ObjectMeta: metav1.ObjectMeta{Name: nn.Name, Namespace: nn.Namespace},
			})).To(Succeed())
			Expect(reconcileInstance(params, nn)).To(Succeed())

			expectConfigMapGone("idtd-app", "relabelled to B")
			expectConfigMapGone("idtd-old", "still labelled A")
			Expect(instanceGone(nn)).To(BeTrue())
		})
	})

	Context("deletion cleanup", func() {
		deleteInstance := func(nn types.NamespacedName) error {
			GinkgoHelper()
			Expect(k8sClient.Delete(ctx, &releasesv1alpha1.ModuleInstance{
				ObjectMeta: metav1.ObjectMeta{Name: nn.Name, Namespace: nn.Namespace},
			})).To(Succeed())
			return reconcileInstance(params, nn)
		}

		It("removes the live workload when the instance is deleted before an identity change is settled", func() {
			nn := start("id-window", "idw-app", "idw-old")
			DeferCleanup(func() { removeConfigMaps("idw-app", "idw-old"); removeInstance(nn) })
			f.failDelete = "idw-old"
			_, err := reconcileWith(nn, identityRender(identityB, "idw-app"))
			Expect(err).NotTo(HaveOccurred())
			expectIdentities(nn, identityB, identityA)
			Expect(liveConfigMapLabels("idw-app")).To(HaveKeyWithValue(labels.ModuleInstanceUUID, identityB))
			Expect(liveConfigMapLabels("idw-old")).To(HaveKeyWithValue(labels.ModuleInstanceUUID, identityA))
			f.failDelete = ""
			rec.events = nil

			Expect(deleteInstance(nn)).To(Succeed())

			expectConfigMapGone("idw-app", "the live workload, relabelled to the new identity")
			expectConfigMapGone("idw-old", "not yet relabelled")
			Expect(instanceGone(nn)).To(BeTrue())
			Expect(rec.withReason(status.LeftBehindReason)).To(BeEmpty())
		})

		It("leaves another instance's, an adopted and a foreign object, and reports them once", func() {
			nn := start("id-del-left", "iddl-own", "iddl-other", "iddl-adopted", "iddl-foreign")
			DeferCleanup(func() {
				removeConfigMaps("iddl-own", "iddl-other", "iddl-adopted", "iddl-foreign")
				removeInstance(nn)
			})
			setConfigMapMeta("iddl-other", func(cm *corev1.ConfigMap) { cm.Labels[labels.ModuleInstanceUUID] = identityX })
			setConfigMapMeta("iddl-adopted", func(cm *corev1.ConfigMap) {
				cm.Annotations = map[string]string{labels.AnnotationAdopt: identityX}
			})
			setConfigMapMeta("iddl-foreign", func(cm *corev1.ConfigMap) { cm.Labels[labels.ManagedBy] = "helm" })

			Expect(deleteInstance(nn)).To(Succeed())

			expectConfigMapGone("iddl-own")
			for _, name := range []string{"iddl-other", "iddl-adopted", "iddl-foreign"} {
				Expect(configMapExists(name)).To(BeTrue(), name)
			}
			Expect(instanceGone(nn)).To(BeTrue(), "a skipped object does not hold the finalizer")
			left := rec.withReason(status.LeftBehindReason)
			Expect(left).To(HaveLen(1))
			Expect(left[0].eventType).To(Equal(corev1.EventTypeWarning))
			Expect(left[0].action).To(Equal("Delete"))
			Expect(left[0].note).To(HavePrefix("Left 3 object(s) in the cluster: "))
			Expect(left[0].note).To(ContainSubstring("ConfigMap/default/iddl-other belongs to module instance " + identityX))
			Expect(left[0].note).To(ContainSubstring(
				"ConfigMap/default/iddl-adopted is being adopted by module instance " + identityX))
			Expect(left[0].note).To(ContainSubstring("ConfigMap/default/iddl-foreign is not managed by OPM"))
		})

		It("holds the finalizer for one more attempt when an object was replaced since the read", func() {
			nn := start("id-del-replaced", "iddr-own")
			DeferCleanup(func() { removeConfigMaps("iddr-own"); removeInstance(nn) })
			first := liveConfigMapNamed("iddr-own").UID

			f.replaceOnDelete = "iddr-own"
			err := deleteInstance(nn)

			Expect(errors.Is(err, apply.ErrReplaced)).To(BeTrue(), "error: %v", err)
			Expect(instanceGone(nn)).To(BeFalse(), "a delete refused on its precondition holds the finalizer")
			second := liveConfigMapNamed("iddr-own")
			Expect(second.UID).NotTo(Equal(first), "the object that now holds the name")
			Expect(second.DeletionTimestamp).To(BeNil(), "is not deleted by that cleanup")

			By("the next cleanup reads the object that now holds the name and judges it")
			Expect(reconcileInstance(params, nn)).To(Succeed())
			expectConfigMapGone("iddr-own")
			Expect(instanceGone(nn)).To(BeTrue())
		})

		It("holds the finalizer when a delete fails and reports nothing as left behind", func() {
			nn := start("id-del-fail", "iddf-own", "iddf-other")
			DeferCleanup(func() { removeConfigMaps("iddf-own", "iddf-other"); removeInstance(nn) })
			setConfigMapMeta("iddf-other", func(cm *corev1.ConfigMap) { cm.Labels[labels.ModuleInstanceUUID] = identityX })

			f.failDelete = "iddf-own"
			Expect(deleteInstance(nn)).NotTo(Succeed())
			Expect(instanceGone(nn)).To(BeFalse(), "a failed delete holds the finalizer")
			Expect(rec.withReason(status.LeftBehindReason)).To(BeEmpty())

			f.failDelete = ""
			Expect(reconcileInstance(params, nn)).To(Succeed())
			Expect(instanceGone(nn)).To(BeTrue())
			Expect(rec.withReason(status.LeftBehindReason)).To(HaveLen(1), "the cleanup that removes the finalizer reports")
		})
	})

	Context("objects the stale prune leaves behind", func() {
		It("leaves a foreign object under a recorded name, drops it from the inventory and stays Ready", func() {
			nn := start("id-foreign", "idfo-app", "idfo-gone")
			DeferCleanup(func() { removeConfigMaps("idfo-app", "idfo-gone"); removeInstance(nn) })

			By("another writer replaces the object under the recorded name")
			removeConfigMaps("idfo-gone")
			createLiveConfigMap("idfo-gone", "", "", "")

			_, err := reconcileWith(nn, identityRender(identityA, "idfo-app"))
			Expect(err).NotTo(HaveOccurred())

			Expect(configMapExists("idfo-gone")).To(BeTrue())
			Expect(inventoryNames(instanceStatus(nn).Inventory)).To(ConsistOf("ConfigMap/idfo-app"))
			Expect(ready(nn).Status).To(Equal(metav1.ConditionTrue))
			left := rec.withReason(status.LeftBehindReason)
			Expect(left).To(HaveLen(1))
			Expect(left[0].eventType).To(Equal(corev1.EventTypeWarning))
			Expect(left[0].action).To(Equal("Prune"))
			Expect(left[0].note).To(ContainSubstring("ConfigMap/default/idfo-gone is not managed by OPM; left in place"))
		})

		It("leaves an object adopted by another instance and drops it from the inventory", func() {
			nn := start("id-adopted", "idad-app", "idad-shared")
			DeferCleanup(func() { removeConfigMaps("idad-app", "idad-shared"); removeInstance(nn) })
			setConfigMapMeta("idad-shared", func(cm *corev1.ConfigMap) {
				cm.Annotations = map[string]string{labels.AnnotationAdopt: identityX}
			})

			_, err := reconcileWith(nn, identityRender(identityA, "idad-app"))
			Expect(err).NotTo(HaveOccurred())

			Expect(configMapExists("idad-shared")).To(BeTrue(), "an object adopted elsewhere is never deleted")
			Expect(inventoryNames(instanceStatus(nn).Inventory)).To(ConsistOf("ConfigMap/idad-app"))
			Expect(ready(nn).Status).To(Equal(metav1.ConditionTrue))
			left := rec.withReason(status.LeftBehindReason)
			Expect(left).To(HaveLen(1))
			Expect(left[0].eventType).To(Equal(corev1.EventTypeWarning))
			Expect(left[0].note).To(ContainSubstring("is being adopted by module instance " + identityX))
		})

		It("reports nothing when the prune deleted every stale object", func() {
			nn := start("id-clean", "idcl-app", "idcl-old")
			DeferCleanup(func() { removeConfigMaps("idcl-app", "idcl-old"); removeInstance(nn) })

			_, err := reconcileWith(nn, identityRender(identityA, "idcl-app"))
			Expect(err).NotTo(HaveOccurred())

			expectConfigMapGone("idcl-old")
			Expect(rec.withReason(status.LeftBehindReason)).To(BeEmpty())
		})
	})
})
