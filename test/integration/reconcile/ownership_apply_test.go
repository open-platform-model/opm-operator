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
	"errors"
	"fmt"
	"reflect"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"cuelang.org/go/cue/cuecontext"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
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

// ownedRender renders one ConfigMap per name with data.payload, each carrying
// uuid as its instance identity. Another payload is a changed render.
func ownedRender(uuid, payload string, names ...string) *render.RenderResult {
	result := &render.RenderResult{}
	for _, name := range names {
		result.Resources = append(result.Resources, cueResource(fmt.Sprintf(`{
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
	data: payload: %q
}`, name, namespace, labels.ManagedBy, labels.ManagedByController, labels.ModuleInstanceUUID, uuid, payload)))
	}
	return result
}

// cueResource compiles src into one rendered resource.
func cueResource(src string) *object.Resource {
	v := cuecontext.New().CompileString(src)
	if v.Err() != nil {
		panic(fmt.Sprintf("compiling a rendered object: %v", v.Err()))
	}
	return &object.Resource{Value: v, Instance: "own", Component: "own", Transformer: "kubernetes#simple"}
}

// configMapKind is the kind the guard's reads are counted for.
var configMapKind = reflect.TypeFor[corev1.ConfigMap]().Name()

// ownershipFaults is a client that counts and can disturb the requests the
// apply guard and the apply send.
type ownershipFaults struct {
	failRead     bool // every GET of a ConfigMap fails
	configMapGet int  // GETs of ConfigMaps that reached the client
	applied      int  // server-side applies that are no dry run

	// replace names a ConfigMap that another writer deletes and creates
	// again, unlabelled, just before the replaceAt-th apply request for it
	// (dry runs counted) is forwarded.
	replace   string
	replaceAt int
}

func (f *ownershipFaults) client() client.WithWatch {
	realClient, err := client.NewWithWatch(cfg, client.Options{Scheme: k8sClient.Scheme()})
	Expect(err).NotTo(HaveOccurred())
	return interceptor.NewClient(realClient, interceptor.Funcs{
		Get: func(
			ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption,
		) error {
			if obj.GetObjectKind().GroupVersionKind().Kind == configMapKind {
				f.configMapGet++
				if f.failRead {
					return errors.New("injected read failure")
				}
			}
			return c.Get(ctx, key, obj, opts...)
		},
		Patch: func(
			ctx context.Context, c client.WithWatch, obj client.Object, p client.Patch, opts ...client.PatchOption,
		) error {
			po := &client.PatchOptions{}
			po.ApplyOptions(opts)
			if p.Type() == types.ApplyPatchType {
				if f.replace != "" && obj.GetName() == f.replace {
					f.replaceAt--
					if f.replaceAt == 0 {
						name := f.replace
						f.replace = ""
						removeConfigMaps(name)
						createLiveConfigMap(name, "", "", "")
					}
				}
				if len(po.DryRun) == 0 {
					f.applied++
				}
			}
			return c.Patch(ctx, obj, p, opts...)
		},
	})
}

var _ = Describe("Ownership guard on the apply of a ModuleInstance", func() {
	var (
		rec    *actionRecorder
		f      *ownershipFaults
		params *opmreconcile.ModuleInstanceParams
	)

	BeforeEach(func() {
		rec = &actionRecorder{}
		f = &ownershipFaults{}
		c := f.client()
		params = &opmreconcile.ModuleInstanceParams{
			Client:          c,
			APIReader:       c,
			ResourceManager: apply.NewResourceManager(c, "opm-controller"),
			EventRecorder:   rec,
		}
	})

	reconcileWith := func(nn types.NamespacedName, r *render.RenderResult) time.Duration {
		GinkgoHelper()
		params.Renderer = &stubRenderer{result: r}
		res, err := opmreconcile.ReconcileModuleInstance(ctx, params, ctrl.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())
		return res.RequeueAfter
	}

	// create makes an instance with its finalizer and nothing applied.
	create := func(name string, names ...string) types.NamespacedName {
		GinkgoHelper()
		createModuleInstance(name)
		nn := types.NamespacedName{Name: name, Namespace: namespace}
		params.Renderer = &stubRenderer{}
		ensureFinalizer(params, nn)
		DeferCleanup(func() { removeConfigMaps(names...); removeInstance(nn) })
		return nn
	}

	// start reconciles a new instance to Ready with identity A, payload v1
	// and the named ConfigMaps.
	start := func(name string, names ...string) types.NamespacedName {
		GinkgoHelper()
		nn := create(name, names...)
		reconcileWith(nn, ownedRender(identityA, "v1", names...))
		expectIdentities(nn, identityA, "")
		rec.events = nil
		return nn
	}

	cond := func(nn types.NamespacedName, conditionType string) *metav1.Condition {
		GinkgoHelper()
		st := instanceStatus(nn)
		return apimeta.FindStatusCondition(st.Conditions, conditionType)
	}

	expectRefused := func(nn types.NamespacedName, requeue time.Duration) *metav1.Condition {
		GinkgoHelper()
		ready := cond(nn, status.ReadyCondition)
		Expect(ready.Status).To(Equal(metav1.ConditionFalse))
		Expect(ready.Reason).To(Equal(status.ApplyRefusedReason))
		Expect(cond(nn, status.StalledCondition)).To(BeNil(), "a refusal is retried, not stalled")
		Expect(requeue).To(BeNumerically(">", 0))
		Expect(requeue).To(BeNumerically("<=", 5*time.Minute), "the bounded backoff")
		refusals := rec.withReason(status.ApplyRefusedReason)
		Expect(refusals).To(HaveLen(1))
		Expect(refusals[0].eventType).To(Equal(corev1.EventTypeWarning))
		Expect(refusals[0].action).To(Equal("Apply"))
		Expect(refusals[0].note).To(Equal(ready.Message))
		Expect(rec.withReason(status.AppliedReason)).To(BeEmpty())
		return ready
	}

	expectReady := func(nn types.NamespacedName) *metav1.Condition {
		GinkgoHelper()
		ready := cond(nn, status.ReadyCondition)
		Expect(ready.Status).To(Equal(metav1.ConditionTrue), "reason=%s message=%s", ready.Reason, ready.Message)
		return ready
	}

	payloadOf := func(name string) string {
		GinkgoHelper()
		return liveConfigMapNamed(name).Data["payload"]
	}

	adopt := func(name, uuid string) {
		GinkgoHelper()
		setConfigMapMeta(name, func(cm *corev1.ConfigMap) {
			if uuid == "" {
				cm.Annotations = nil
				return
			}
			cm.Annotations = map[string]string{labels.AnnotationAdopt: uuid}
		})
	}

	It("refuses the whole apply over an object OPM does not manage, and applies once it is adopted", func() {
		nn := create("own-foreign", "ownf-a", "ownf-b", "ownf-c")
		createLiveConfigMap("ownf-b", "", "", "")
		before := liveConfigMapNamed("ownf-b")

		desired := ownedRender(identityA, "v1", "ownf-a", "ownf-b", "ownf-c")
		ready := expectRefused(nn, reconcileWith(nn, desired))
		Expect(ready.Message).To(ContainSubstring("1 object(s)"))
		Expect(ready.Message).To(ContainSubstring("ConfigMap/" + namespace + "/ownf-b"))
		Expect(ready.Message).To(ContainSubstring(labels.AnnotationAdopt + "=" + identityA))

		Expect(configMapExists("ownf-a")).To(BeFalse(), "no other rendered object is created")
		Expect(configMapExists("ownf-c")).To(BeFalse(), "no other rendered object is created")
		after := liveConfigMapNamed("ownf-b")
		Expect(after.ResourceVersion).To(Equal(before.ResourceVersion), "the refused object is untouched")
		Expect(after.ManagedFields).To(Equal(before.ManagedFields))
		st := instanceStatus(nn)
		Expect(st.Inventory).To(BeNil())
		Expect(st.LastAppliedRenderDigest).To(BeEmpty())
		Expect(st.InstanceUUID).To(BeEmpty(), "a refusal stores no identity")
		Expect(st.FailureCounters.Apply).To(Equal(int64(1)), "the refusal counts as a failed apply")
		Expect(f.applied).To(BeZero())

		By("the user adopts the object and the retry runs")
		adopt("ownf-b", identityA)
		rec.events = nil
		reconcileWith(nn, desired)
		expectReady(nn)
		Expect(inventoryNames(instanceStatus(nn).Inventory)).To(ConsistOf(
			"ConfigMap/ownf-a", "ConfigMap/ownf-b", "ConfigMap/ownf-c"))
		Expect(liveConfigMapLabels("ownf-b")).To(HaveKeyWithValue(labels.ModuleInstanceUUID, identityA))
		Expect(payloadOf("ownf-b")).To(Equal("v1"))
		Expect(liveConfigMapNamed("ownf-b").UID).To(Equal(before.UID), "adopted, not recreated")
	})

	// A render with every component switched off holds no object, so it
	// carries no identity. There is nothing to judge and nothing to write.
	It("does not fail an empty render that carries no identity", func() {
		nn := create("own-empty")

		requeue := reconcileWith(nn, &render.RenderResult{})

		expectReady(nn)
		Expect(requeue).To(BeZero())
		Expect(rec.withReason(status.ApplyFailedReason)).To(BeEmpty())
		st := instanceStatus(nn)
		Expect(st.InstanceUUID).To(BeEmpty())
		Expect(st.FailureCounters.Apply).To(BeZero())

		By("and again, with unchanged digests")
		reconcileWith(nn, &render.RenderResult{})
		expectReady(nn)
		Expect(rec.withReason(status.ApplyFailedReason)).To(BeEmpty())
	})

	It("refuses over two objects of another instance and names both", func() {
		nn := create("own-other", "owno-a", "owno-b", "owno-c")
		createLiveConfigMap("owno-a", labels.ManagedByController, identityX, "")
		createLiveConfigMap("owno-b", labels.ManagedByController, identityX, "")
		before := liveConfigMapNamed("owno-a")

		ready := expectRefused(nn, reconcileWith(nn, ownedRender(identityA, "v1", "owno-a", "owno-b", "owno-c")))
		Expect(ready.Message).To(ContainSubstring("2 object(s)"))
		for _, name := range []string{"owno-a", "owno-b"} {
			Expect(ready.Message).To(ContainSubstring(
				"ConfigMap/" + namespace + "/" + name + " belongs to module instance " + identityX))
		}
		Expect(ready.Message).NotTo(MatchRegexp(`\d{4}:D\d+`), "no enhancement reference")

		Expect(configMapExists("owno-c")).To(BeFalse())
		after := liveConfigMapNamed("owno-a")
		Expect(after.ResourceVersion).To(Equal(before.ResourceVersion))
		Expect(after.Labels).To(HaveKeyWithValue(labels.ModuleInstanceUUID, identityX))
	})

	It("refuses a changed render over an inventoried object that is being deleted, and not an unchanged one", func() {
		nn := start("own-term", "ownt-a", "ownt-b")
		setConfigMapMeta("ownt-a", func(cm *corev1.ConfigMap) { cm.Finalizers = []string{"test.opmodel.dev/hold"} })
		Expect(k8sClient.Delete(ctx, liveConfigMapNamed("ownt-a"))).To(Succeed())
		DeferCleanup(func() {
			setConfigMapMeta("ownt-a", func(cm *corev1.ConfigMap) { cm.Finalizers = nil })
		})
		Expect(liveConfigMapNamed("ownt-a").DeletionTimestamp).NotTo(BeNil())

		By("unchanged digests: nothing is written over the object, so nothing is refused")
		reconcileWith(nn, ownedRender(identityA, "v1", "ownt-a", "ownt-b"))
		expectReady(nn)
		Expect(rec.withReason(status.ApplyRefusedReason)).To(BeEmpty())

		By("a changed render would write it")
		ready := expectRefused(nn, reconcileWith(nn, ownedRender(identityA, "v2", "ownt-a", "ownt-b")))
		Expect(ready.Message).To(ContainSubstring("ownt-a is being deleted"))
		Expect(payloadOf("ownt-b")).To(Equal("v1"), "nothing is written")
		Expect(inventoryNames(instanceStatus(nn).Inventory)).To(ConsistOf("ConfigMap/ownt-a", "ConfigMap/ownt-b"))
	})

	It("lets an object go that another instance adopted, counts it, and takes it back on unchanged digests", func() {
		nn := start("own-letgo", "ownl-a", "ownl-b", "ownl-c")
		names := []string{"ownl-a", "ownl-b", "ownl-c"}
		adopt("ownl-a", identityX)

		By("a changed render: the other objects are applied")
		reconcileWith(nn, ownedRender(identityA, "v2", names...))
		ready := expectReady(nn)
		Expect(ready.Message).To(Equal(
			"Reconciliation succeeded. 1 rendered object(s) are adopted by another instance and are not applied."))
		Expect(payloadOf("ownl-a")).To(Equal("v1"), "the adopted object keeps its content")
		Expect(payloadOf("ownl-b")).To(Equal("v2"))
		Expect(inventoryNames(instanceStatus(nn).Inventory)).To(ConsistOf("ConfigMap/ownl-b", "ConfigMap/ownl-c"))
		let := rec.withReason(status.AdoptedElsewhereReason)
		Expect(let).To(HaveLen(1))
		Expect(let[0].eventType).To(Equal(corev1.EventTypeWarning))
		Expect(let[0].action).To(Equal("Apply"))
		Expect(let[0].note).To(ContainSubstring("1 rendered object(s)"))
		Expect(let[0].note).To(ContainSubstring(
			"ConfigMap/" + namespace + "/ownl-a was adopted by module instance " + identityX))
		Expect(rec.withReason(status.ApplyRefusedReason)).To(BeEmpty())

		By("nothing changes: no event, the Ready message carries the state")
		rec.events = nil
		applied := f.applied
		reconcileWith(nn, ownedRender(identityA, "v2", names...))
		Expect(expectReady(nn).Message).To(ContainSubstring("1 rendered object(s) are adopted"))
		Expect(rec.withReason(status.AdoptedElsewhereReason)).To(BeEmpty())
		Expect(rec.withReason(status.NoOpReason)).To(HaveLen(1))
		Expect(f.applied).To(Equal(applied), "a no-op sends no apply")

		By("a second object is let go, with unchanged digests")
		adopt("ownl-b", identityX)
		reconcileWith(nn, ownedRender(identityA, "v2", names...))
		Expect(expectReady(nn).Message).To(ContainSubstring("2 rendered object(s) are adopted"))
		let = rec.withReason(status.AdoptedElsewhereReason)
		Expect(let).To(HaveLen(1))
		Expect(let[0].note).To(ContainSubstring("2 rendered object(s)"))
		Expect(inventoryNames(instanceStatus(nn).Inventory)).To(ConsistOf("ConfigMap/ownl-c"))
		Expect(f.applied).To(Equal(applied), "letting go sends no apply")

		By("both are annotated back: taken in by the restore step, nothing else is written")
		adopt("ownl-a", identityA)
		adopt("ownl-b", identityA)
		setConfigMapMeta("ownl-c", func(cm *corev1.ConfigMap) { cm.Data["payload"] = "by-hand" })
		untouched := liveConfigMapNamed("ownl-c").ResourceVersion
		rec.events = nil
		reconcileWith(nn, ownedRender(identityA, "v2", names...))
		Expect(expectReady(nn).Message).To(Equal("Reconciliation succeeded"))
		Expect(rec.withReason(status.AdoptedElsewhereReason)).To(BeEmpty())
		Expect(inventoryNames(instanceStatus(nn).Inventory)).To(ConsistOf(
			"ConfigMap/ownl-a", "ConfigMap/ownl-b", "ConfigMap/ownl-c"))
		Expect(payloadOf("ownl-a")).To(Equal("v2"), "the taken-in object has the rendered content")
		Expect(liveConfigMapNamed("ownl-c").ResourceVersion).To(Equal(untouched), "an inventoried object is not rewritten")
		drifted := cond(nn, status.DriftedCondition)
		Expect(drifted).NotTo(BeNil())
		Expect(drifted.Status).To(Equal(metav1.ConditionTrue))
		Expect(drifted.Message).To(ContainSubstring("1 resource(s)"),
			"Drifted names the modified object and not the taken-in ones")
		// ownl-b already holds the render, so the staged apply sends one write.
		Expect(f.applied).To(Equal(applied+1), "only a taken-in object is written")
	})

	It("lets an object go on unchanged digests without an apply, and neither reports nor restores it", func() {
		nn := start("own-noop", "ownn-a", "ownn-b")
		adopt("ownn-a", identityX)
		setConfigMapMeta("ownn-a", func(cm *corev1.ConfigMap) { cm.Data["payload"] = "the-adopter's" })
		appliedAt := instanceStatus(nn).LastAppliedAt
		applied := f.applied

		reconcileWith(nn, ownedRender(identityA, "v1", "ownn-a", "ownn-b"))
		expectReady(nn)
		st := instanceStatus(nn)
		Expect(inventoryNames(st.Inventory)).To(ConsistOf("ConfigMap/ownn-b"))
		Expect(st.LastAppliedAt).To(Equal(appliedAt), "no apply was sent")
		Expect(f.applied).To(Equal(applied))
		Expect(cond(nn, status.DriftedCondition)).To(BeNil(), "a let-go object is not drift")
		Expect(rec.withReason(status.AdoptedElsewhereReason)).To(HaveLen(1))

		By("the adopter deletes the object and creates it again")
		removeConfigMaps("ownn-a")
		createLiveConfigMap("ownn-a", labels.ManagedByController, identityX, identityX)
		before := liveConfigMapNamed("ownn-a").ResourceVersion
		reconcileWith(nn, ownedRender(identityA, "v1", "ownn-a", "ownn-b"))
		expectReady(nn)
		Expect(liveConfigMapNamed("ownn-a").ResourceVersion).To(Equal(before), "a let-go object is not written")
		Expect(f.applied).To(Equal(applied))
	})

	It("refuses on unchanged digests when a rendered object outside the inventory is another instance's", func() {
		nn := start("own-held", "ownh-a", "ownh-b")
		adopt("ownh-a", identityX)
		reconcileWith(nn, ownedRender(identityA, "v1", "ownh-a", "ownh-b"))
		expectReady(nn)
		Expect(inventoryNames(instanceStatus(nn).Inventory)).To(ConsistOf("ConfigMap/ownh-b"))

		By("the adopter applies the object and drops the annotation")
		setConfigMapMeta("ownh-a", func(cm *corev1.ConfigMap) {
			cm.Annotations = nil
			cm.Labels[labels.ModuleInstanceUUID] = identityX
		})
		before := liveConfigMapNamed("ownh-a").ResourceVersion
		applied := f.applied
		rec.events = nil

		ready := expectRefused(nn, reconcileWith(nn, ownedRender(identityA, "v1", "ownh-a", "ownh-b")))
		Expect(ready.Message).To(ContainSubstring("ownh-a belongs to module instance " + identityX))
		Expect(liveConfigMapNamed("ownh-a").ResourceVersion).To(Equal(before))
		Expect(f.applied).To(Equal(applied))
		Expect(inventoryNames(instanceStatus(nn).Inventory)).To(ConsistOf("ConfigMap/ownh-b"),
			"a refusal keeps the inventory")
	})

	It("does not ask with the earlier identity while an identity change is not settled", func() {
		nn := start("own-window", "ownw-app")
		DeferCleanup(func() { removeConfigMaps("ownw-kept") })
		// An object outside the inventory that carries the identity the
		// instance is leaving: its own leftover, or another record's.
		createLiveConfigMap("ownw-kept", labels.ManagedByController, identityA, "")
		before := liveConfigMapNamed("ownw-kept").ResourceVersion

		ready := expectRefused(nn, reconcileWith(nn, ownedRender(identityB, "v1", "ownw-app", "ownw-kept")))
		Expect(ready.Message).To(ContainSubstring("ownw-kept belongs to module instance " + identityA))
		Expect(ready.Message).To(ContainSubstring(labels.AnnotationAdopt + "=" + identityB))
		expectIdentities(nn, identityA, "")
		Expect(liveConfigMapNamed("ownw-kept").ResourceVersion).To(Equal(before))
		Expect(liveConfigMapLabels("ownw-app")).To(HaveKeyWithValue(labels.ModuleInstanceUUID, identityA),
			"nothing is relabelled")

		By("annotated for the new identity: taken in, and the change settles")
		adopt("ownw-kept", identityB)
		reconcileWith(nn, ownedRender(identityB, "v1", "ownw-app", "ownw-kept"))
		expectReady(nn)
		expectIdentities(nn, identityB, "")
		Expect(liveConfigMapLabels("ownw-kept")).To(HaveKeyWithValue(labels.ModuleInstanceUUID, identityB))
	})

	It("lets an object adopted under the earlier identity go at the first render under the new one", func() {
		nn := start("own-earlier", "owne-app", "owne-adopted")
		adopt("owne-adopted", identityA)
		before := liveConfigMapNamed("owne-adopted")

		reconcileWith(nn, ownedRender(identityB, "v1", "owne-app", "owne-adopted"))
		expectReady(nn)
		expectIdentities(nn, identityB, "")
		Expect(inventoryNames(instanceStatus(nn).Inventory)).To(ConsistOf("ConfigMap/owne-app"))
		after := liveConfigMapNamed("owne-adopted")
		Expect(after.ResourceVersion).To(Equal(before.ResourceVersion), "not written and not deleted")
		Expect(liveConfigMapLabels("owne-app")).To(HaveKeyWithValue(labels.ModuleInstanceUUID, identityB))
		let := rec.withReason(status.AdoptedElsewhereReason)
		Expect(let).To(HaveLen(1))
		Expect(let[0].note).To(ContainSubstring(labels.AnnotationAdopt+"="+identityB),
			"the event prints the annotation to set")
	})

	// The upgrade promise: what the instance already holds is judged by its
	// adopt annotation alone, whatever its labels say.
	It("applies an inventoried object that carries another instance's UUID label, and one without OPM labels", func() {
		nn := start("own-shared", "owns-shared", "owns-bare")
		setConfigMapMeta("owns-shared", func(cm *corev1.ConfigMap) { cm.Labels[labels.ModuleInstanceUUID] = identityX })
		setConfigMapMeta("owns-bare", func(cm *corev1.ConfigMap) { cm.Labels = nil })

		reconcileWith(nn, ownedRender(identityA, "v2", "owns-shared", "owns-bare"))
		expectReady(nn)
		Expect(rec.withReason(status.ApplyRefusedReason)).To(BeEmpty())
		Expect(payloadOf("owns-shared")).To(Equal("v2"))
		Expect(payloadOf("owns-bare")).To(Equal("v2"))
		Expect(liveConfigMapLabels("owns-bare")).To(HaveKeyWithValue(labels.ManagedBy, labels.ManagedByController))
		Expect(liveConfigMapLabels("owns-bare")).To(HaveKeyWithValue(labels.ModuleInstanceUUID, identityA))
	})

	// A shared Namespace is the most likely meeting of two instances, and it
	// is cluster-scoped: its inventory entry has no namespace, and it is
	// applied in the first stage of the staged apply.
	It("judges a Namespace two instances render: refused, taken in, and applied once both hold it", func() {
		const shared = "own-shared-ns"
		namespaceOf := func(uuid string) *object.Resource {
			return cueResource(fmt.Sprintf(`{
	apiVersion: "v1"
	kind:       "Namespace"
	metadata: {name: %q, labels: {%q: %q, %q: %q}}
}`, shared, labels.ManagedBy, labels.ManagedByController, labels.ModuleInstanceUUID, uuid))
		}
		renderOf := func(uuid, payload, cm string) *render.RenderResult {
			r := ownedRender(uuid, payload, cm)
			r.Resources = append(r.Resources, namespaceOf(uuid))
			return r
		}
		liveNamespace := func() *corev1.Namespace {
			GinkgoHelper()
			var ns corev1.Namespace
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: shared}, &ns)).To(Succeed())
			return &ns
		}
		DeferCleanup(func() {
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, liveNamespace()))).To(Succeed())
		})

		first := create("own-ns-first", "ownns-first")
		reconcileWith(first, renderOf(identityA, "v1", "ownns-first"))
		expectReady(first)
		Expect(inventoryNames(instanceStatus(first).Inventory)).To(ConsistOf("ConfigMap/ownns-first", "Namespace/"+shared))
		held := liveNamespace()

		By("a second instance meets the Namespace for the first time")
		second := create("own-ns-second", "ownns-second")
		rec.events = nil
		ready := expectRefused(second, reconcileWith(second, renderOf(identityB, "v1", "ownns-second")))
		Expect(ready.Message).To(ContainSubstring("Namespace/" + shared + " belongs to module instance " + identityA))
		Expect(liveNamespace().ResourceVersion).To(Equal(held.ResourceVersion))
		Expect(configMapExists("ownns-second")).To(BeFalse())

		By("annotated for the second instance: taken in, the same object")
		ns := liveNamespace()
		ns.Annotations = map[string]string{labels.AnnotationAdopt: identityB}
		Expect(k8sClient.Update(ctx, ns)).To(Succeed())
		reconcileWith(second, renderOf(identityB, "v1", "ownns-second"))
		expectReady(second)
		Expect(inventoryNames(instanceStatus(second).Inventory)).To(ConsistOf("ConfigMap/ownns-second", "Namespace/"+shared))
		taken := liveNamespace()
		Expect(taken.UID).To(Equal(held.UID))
		Expect(taken.Labels).To(HaveKeyWithValue(labels.ModuleInstanceUUID, identityB))

		By("both inventories list it and no annotation names an instance: both apply, neither is refused")
		taken.Annotations = nil
		Expect(k8sClient.Update(ctx, taken)).To(Succeed())
		rec.events = nil
		reconcileWith(first, renderOf(identityA, "v2", "ownns-first"))
		expectReady(first)
		Expect(liveNamespace().Labels).To(HaveKeyWithValue(labels.ModuleInstanceUUID, identityA), "the apply relabels it")
		reconcileWith(second, renderOf(identityB, "v2", "ownns-second"))
		expectReady(second)
		Expect(liveNamespace().Labels).To(HaveKeyWithValue(labels.ModuleInstanceUUID, identityB))
		Expect(rec.withReason(status.ApplyRefusedReason)).To(BeEmpty())
		Expect(inventoryNames(instanceStatus(first).Inventory)).To(ContainElement("Namespace/" + shared))
		Expect(inventoryNames(instanceStatus(second).Inventory)).To(ContainElement("Namespace/" + shared))
	})

	It("writes nothing when an object cannot be read", func() {
		nn := start("own-read", "ownr-a", "ownr-b")
		removeConfigMaps("ownr-b")
		applied := f.applied

		By("unchanged digests: reported as a failed drift check, nothing restored")
		f.failRead = true
		reconcileWith(nn, ownedRender(identityA, "v1", "ownr-a", "ownr-b"))
		f.failRead = false
		expectReady(nn)
		st := instanceStatus(nn)
		Expect(st.FailureCounters.Drift).To(Equal(int64(1)))
		Expect(inventoryNames(st.Inventory)).To(ConsistOf("ConfigMap/ownr-a", "ConfigMap/ownr-b"))
		Expect(configMapExists("ownr-b")).To(BeFalse(), "nothing is restored on a failed read")
		Expect(f.applied).To(Equal(applied))

		By("a changed render fails as a failed apply")
		f.failRead = true
		requeue := reconcileWith(nn, ownedRender(identityA, "v2", "ownr-a", "ownr-b"))
		f.failRead = false
		ready := cond(nn, status.ReadyCondition)
		Expect(ready.Status).To(Equal(metav1.ConditionFalse))
		Expect(ready.Reason).To(Equal(status.ApplyFailedReason))
		Expect(ready.Message).To(ContainSubstring("reading ConfigMap/" + namespace + "/ownr-a before the apply"))
		Expect(cond(nn, status.StalledCondition)).To(BeNil())
		Expect(requeue).To(BeNumerically(">", 0))
		Expect(payloadOf("ownr-a")).To(Equal("v1"))
		Expect(configMapExists("ownr-b")).To(BeFalse())
		Expect(f.applied).To(Equal(applied))
		Expect(instanceStatus(nn).FailureCounters.Apply).To(Equal(int64(1)))
	})

	It("sends one read of its own per object before the dry-run", func() {
		names := []string{"ownc-a", "ownc-b", "ownc-c"}
		nn := start("own-count", names...)
		// The guard reads through the uncached reader; the resource manager,
		// whose diff reads each object once, works through another client.
		diffs := &ownershipFaults{}
		dc := diffs.client()
		params.Client, params.ResourceManager = dc, apply.NewResourceManager(dc, "opm-controller")

		f.configMapGet = 0
		reconcileWith(nn, ownedRender(identityA, "v1", names...))
		expectReady(nn)
		// The same reader serves the health judgement of the inventory, one
		// read per entry, as before the guard.
		Expect(f.configMapGet).To(Equal(2*len(names)), "the guard reads each object once")
		Expect(diffs.configMapGet).To(Equal(len(names)), "drift detection sends no read of its own beside the diff's")
	})

	It("counts a custom resource whose kind a CRD of the same render defines as an object that does not exist", func() {
		nn := create("own-crd", "ownk-cm")
		crd := cueResource(`{
	apiVersion: "apiextensions.k8s.io/v1"
	kind:       "CustomResourceDefinition"
	metadata: {
		name: "gadgets.ownership.test.opmodel.dev"
		labels: {"app.kubernetes.io/managed-by": "opm-controller", "module-instance.opmodel.dev/uuid": "` + identityA + `"}
	}
	spec: {
		group: "ownership.test.opmodel.dev"
		scope: "Namespaced"
		names: {plural: "gadgets", singular: "gadget", kind: "Gadget", listKind: "GadgetList"}
		versions: [{
			name: "v1", served: true, storage: true
			schema: openAPIV3Schema: {type: "object", "x-kubernetes-preserve-unknown-fields": true}
		}]
	}
}`)
		gadget := cueResource(`{
	apiVersion: "ownership.test.opmodel.dev/v1"
	kind:       "Gadget"
	metadata: {
		name: "ownk-gadget", namespace: "` + namespace + `"
		labels: {"app.kubernetes.io/managed-by": "opm-controller", "module-instance.opmodel.dev/uuid": "` + identityA + `"}
	}
	spec: size: 1
}`)
		DeferCleanup(func() {
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &apiextensionsv1.CustomResourceDefinition{
				ObjectMeta: metav1.ObjectMeta{Name: "gadgets.ownership.test.opmodel.dev"},
			}))).To(Succeed())
		})

		desired := ownedRender(identityA, "v1", "ownk-cm")
		desired.Resources = append(desired.Resources, crd, gadget)
		reconcileWith(nn, desired)
		expectReady(nn)
		live := &unstructured.Unstructured{}
		live.SetAPIVersion("ownership.test.opmodel.dev/v1")
		live.SetKind("Gadget")
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Name: "ownk-gadget", Namespace: namespace}, live)
		}, 10*time.Second, 200*time.Millisecond).Should(Succeed())
	})

	Describe("a taken-in object", func() {
		const svcName = "own-taken-svc"

		service := func(clusterIP string) *object.Resource {
			return cueResource(fmt.Sprintf(`{
	apiVersion: "v1"
	kind:       "Service"
	metadata: {
		name: %q, namespace: %q
		labels: {%q: %q, %q: %q}
	}
	spec: {
		clusterIP: %q
		ports: [{port: 80, protocol: "TCP", targetPort: 80}]
	}
}`, svcName, namespace, labels.ManagedBy, labels.ManagedByController, labels.ModuleInstanceUUID, identityA, clusterIP))
		}

		It("is never deleted and created again under forceConflicts", func() {
			nn := create("own-force", "ownfc-cm")
			var mi releasesv1alpha1.ModuleInstance
			Expect(k8sClient.Get(ctx, nn, &mi)).To(Succeed())
			mi.Spec.Rollout = &releasesv1alpha1.RolloutSpec{ForceConflicts: true}
			Expect(k8sClient.Update(ctx, &mi)).To(Succeed())

			live := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name: svcName, Namespace: namespace,
					Annotations: map[string]string{labels.AnnotationAdopt: identityA},
				},
				Spec: corev1.ServiceSpec{
					ClusterIP: "10.0.0.211",
					Ports:     []corev1.ServicePort{{Port: 80, Protocol: corev1.ProtocolTCP}},
				},
			}
			Expect(k8sClient.Create(ctx, live)).To(Succeed())
			DeferCleanup(func() { Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, live))).To(Succeed()) })

			desired := ownedRender(identityA, "v1", "ownfc-cm")
			desired.Resources = append(desired.Resources, service("10.0.0.212"))
			requeue := reconcileWith(nn, desired)

			ready := cond(nn, status.ReadyCondition)
			Expect(ready.Status).To(Equal(metav1.ConditionFalse))
			Expect(ready.Reason).To(Equal(status.ApplyFailedReason))
			Expect(ready.Message).To(ContainSubstring("Service/" + namespace + "/" + svcName))
			Expect(ready.Message).To(ContainSubstring("spec.clusterIP"))
			Expect(ready.Message).To(ContainSubstring("does not delete and create again an object it is taking in"))
			Expect(requeue).To(BeNumerically(">", 0))

			var after corev1.Service
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(live), &after)).To(Succeed())
			Expect(after.UID).To(Equal(live.UID), "the Service is not recreated")
			Expect(after.ResourceVersion).To(Equal(live.ResourceVersion))
			Expect(configMapExists("ownfc-cm")).To(BeFalse(), "nothing is written")
			Expect(f.applied).To(BeZero())
		})

		// The second lock: the apply acts on the object the guard read. An
		// object another writer puts under the name after that read is
		// neither written over nor deleted.
		DescribeTable("is the object that was read, or the apply fails",
			func(instance, cm string, force bool, replaceAt int, want string) {
				nn := create(instance, cm)
				if force {
					var mi releasesv1alpha1.ModuleInstance
					Expect(k8sClient.Get(ctx, nn, &mi)).To(Succeed())
					mi.Spec.Rollout = &releasesv1alpha1.RolloutSpec{ForceConflicts: true}
					Expect(k8sClient.Update(ctx, &mi)).To(Succeed())
				}
				createLiveConfigMap(cm, "", "", identityA)
				read := liveConfigMapNamed(cm)

				f.replace, f.replaceAt = cm, replaceAt
				reconcileWith(nn, ownedRender(identityA, "v1", cm))

				Expect(f.replace).To(BeEmpty(), "the object was replaced during the apply")
				ready := cond(nn, status.ReadyCondition)
				Expect(ready.Status).To(Equal(metav1.ConditionFalse))
				Expect(ready.Reason).To(Equal(status.ApplyFailedReason))
				Expect(ready.Message).To(ContainSubstring(want))
				replaced := liveConfigMapNamed(cm)
				Expect(replaced.UID).NotTo(Equal(read.UID))
				Expect(replaced.Labels).To(BeEmpty(), "the object that took the name is not written")
				Expect(replaced.Data).To(BeEmpty())
				Expect(replaced.DeletionTimestamp).To(BeNil())
				Expect(instanceStatus(nn).Inventory).To(BeNil())
			},
			Entry("replaced before the staged apply reads it", "own-pin", "ownp-cm", false, 1, "ownp-cm"),
			Entry("replaced before the write itself", "own-pin-late", "ownpl-cm", false, 2, "ownpl-cm"),
			Entry("replaced under forceConflicts, after the check", "own-pin-force", "ownpf-cm", true, 1, "was not deleted"),
			Entry("replaced under forceConflicts, before the write itself",
				"own-pin-force-late", "ownpfl-cm", true, 2, "ownpfl-cm"),
		)
	})
})
