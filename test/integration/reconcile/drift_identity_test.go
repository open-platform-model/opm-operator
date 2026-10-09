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
	"net/http"
	"strings"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	opmreconcile "github.com/open-platform-model/opm-operator/internal/reconcile"
	"github.com/open-platform-model/opm-operator/internal/status"
)

// recordedRequest is what dryRunRecorder keeps of one request.
type recordedRequest struct {
	method, path, dryRun, user string
}

// dryRunRecorder records every request sent through the rest config it
// wraps, with its Impersonate-User header. client-go puts
// Config.WrapTransport inside its impersonating round tripper, so the header
// read here is the one the API server receives.
type dryRunRecorder struct {
	mu       sync.Mutex
	requests []recordedRequest
}

func (r *dryRunRecorder) wrap(cfg *rest.Config) *rest.Config {
	out := rest.CopyConfig(cfg)
	out.Wrap(func(rt http.RoundTripper) http.RoundTripper {
		return roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			r.mu.Lock()
			r.requests = append(r.requests, recordedRequest{
				method: req.Method,
				path:   req.URL.Path,
				dryRun: req.URL.Query().Get("dryRun"),
				user:   req.Header.Get("Impersonate-User"),
			})
			r.mu.Unlock()
			return rt.RoundTrip(req)
		})
	})
	return out
}

// takeAll returns the recorded requests and forgets them.
func (r *dryRunRecorder) takeAll() []recordedRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.requests
	r.requests = nil
	return out
}

// take returns the users of the recorded server-side dry-run applies and
// forgets every recorded request.
func (r *dryRunRecorder) take() []string {
	var users []string
	for _, req := range r.takeAll() {
		if req.method == http.MethodPatch && req.dryRun == metav1.DryRunAll {
			users = append(users, req.user)
		}
	}
	return users
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// Drift detection runs as the identity that applies the instance
// (drift-detection, serviceaccount-impersonation): the dry-run is sent as the
// effective ServiceAccount, and a dry-run that identity may not send is
// stated on the Drifted condition.
var _ = Describe("Drift detection identity", func() {
	const cmName = "test-module"
	allVerbs := []string{"get", "list", "watch", "create", "update", "patch", "delete"}

	// setup creates a ServiceAccount that may manage ConfigMaps and an
	// instance that the returned params reconcile once, to Ready. specSA
	// decides whether the instance names the ServiceAccount or leaves it to
	// the manager's default.
	setup := func(
		prefix string, specSA bool,
	) (*opmreconcile.ModuleInstanceParams, *dryRunRecorder, types.NamespacedName, string) {
		GinkgoHelper()
		saName := prefix + "-sa"
		roleName := prefix + "-role"
		nn := types.NamespacedName{Name: prefix + "-mi", Namespace: namespace}

		Expect(k8sClient.Create(ctx, &corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{Name: saName, Namespace: namespace},
		})).To(Succeed())
		Expect(k8sClient.Create(ctx, &rbacv1.ClusterRole{
			ObjectMeta: metav1.ObjectMeta{Name: roleName},
			Rules:      []rbacv1.PolicyRule{{APIGroups: []string{""}, Resources: []string{"configmaps"}, Verbs: allVerbs}},
		})).To(Succeed())
		Expect(k8sClient.Create(ctx, &rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: roleName},
			RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: roleName},
			Subjects:   []rbacv1.Subject{{Kind: rbacv1.ServiceAccountKind, Name: saName, Namespace: namespace}},
		})).To(Succeed())
		DeferCleanup(func() {
			for _, obj := range []client.Object{
				&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: cmName, Namespace: namespace}},
				&releasesv1alpha1.ModuleInstance{ObjectMeta: metav1.ObjectMeta{Name: nn.Name, Namespace: namespace}},
				&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: saName, Namespace: namespace}},
				&rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: roleName}},
				&rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: roleName}},
			} {
				Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, obj))).To(Succeed())
			}
		})

		mi := &releasesv1alpha1.ModuleInstance{
			ObjectMeta: metav1.ObjectMeta{Name: nn.Name, Namespace: namespace},
			Spec: releasesv1alpha1.ModuleInstanceSpec{
				Module: releasesv1alpha1.ModuleReference{Path: "opmodel.dev/test/module", Version: "v0.1.0"},
			},
		}
		recorder := &dryRunRecorder{}
		params := reconcileParamsWithConfig()
		params.RestConfig = recorder.wrap(cfg)
		// The specs reconcile until the authorizer has seen a role change;
		// the default buffer of ten events would block the reconciler.
		params.EventRecorder = events.NewFakeRecorder(1024)
		if specSA {
			mi.Spec.ServiceAccountName = saName
		} else {
			params.DefaultServiceAccount = saName
		}
		Expect(k8sClient.Create(ctx, mi)).To(Succeed())
		ensureFinalizer(params, nn)

		_, err := opmreconcile.ReconcileModuleInstance(ctx, params, ctrl.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())
		var applied releasesv1alpha1.ModuleInstance
		Expect(k8sClient.Get(ctx, nn, &applied)).To(Succeed())
		Expect(apimeta.IsStatusConditionTrue(applied.Status.Conditions, status.ReadyCondition)).To(BeTrue())
		recorder.take()
		return params, recorder, nn, saName
	}

	instance := func(nn types.NamespacedName) *releasesv1alpha1.ModuleInstance {
		GinkgoHelper()
		var mi releasesv1alpha1.ModuleInstance
		Expect(k8sClient.Get(ctx, nn, &mi)).To(Succeed())
		return &mi
	}

	modifyConfigMap := func(message string) {
		GinkgoHelper()
		var cm corev1.ConfigMap
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: cmName, Namespace: namespace}, &cm)).To(Succeed())
		cm.Data["message"] = message
		Expect(k8sClient.Update(ctx, &cm)).To(Succeed())
	}

	setVerbs := func(roleName string, verbs []string) {
		GinkgoHelper()
		Eventually(func() error {
			var role rbacv1.ClusterRole
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: roleName}, &role); err != nil {
				return err
			}
			role.Rules[0].Verbs = verbs
			return k8sClient.Update(ctx, &role)
		}, 5*time.Second, 100*time.Millisecond).Should(Succeed())
	}

	DescribeTable("sends the dry-run as the effective ServiceAccount",
		func(prefix string, specSA bool) {
			params, recorder, nn, saName := setup(prefix, specSA)
			modifyConfigMap("drifted-by-hand")

			_, err := opmreconcile.ReconcileModuleInstance(ctx, params, ctrl.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			users := recorder.take()
			Expect(users).NotTo(BeEmpty(), "the dry-run of drift detection goes through the impersonated client")
			Expect(users).To(HaveEach("system:serviceaccount:" + namespace + ":" + saName))

			mi := instance(nn)
			drifted := apimeta.FindStatusCondition(mi.Status.Conditions, status.DriftedCondition)
			Expect(drifted).NotTo(BeNil())
			Expect(drifted.Status).To(Equal(metav1.ConditionTrue))
			Expect(drifted.Reason).To(Equal(status.DriftDetectedReason))
			Expect(apimeta.IsStatusConditionTrue(mi.Status.Conditions, status.ReadyCondition)).To(BeTrue())
		},
		Entry("named by spec.serviceAccountName", "drift-id-spec", true),
		Entry("named by --default-service-account", "drift-id-flag", false),
	)

	It("sends the dry-run as the controller when no ServiceAccount is effective", func() {
		nn := types.NamespacedName{Name: "drift-id-none-mi", Namespace: namespace}
		createModuleInstance(nn.Name)
		DeferCleanup(func() {
			for _, obj := range []client.Object{
				&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: cmName, Namespace: namespace}},
				&releasesv1alpha1.ModuleInstance{ObjectMeta: metav1.ObjectMeta{Name: nn.Name, Namespace: namespace}},
			} {
				Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, obj))).To(Succeed())
			}
		})
		recorder := &dryRunRecorder{}
		params := reconcileParamsWithConfig()
		params.RestConfig = recorder.wrap(cfg)
		ensureFinalizer(params, nn)
		_, err := opmreconcile.ReconcileModuleInstance(ctx, params, ctrl.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())
		modifyConfigMap("drifted-by-hand")

		_, err = opmreconcile.ReconcileModuleInstance(ctx, params, ctrl.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		// The rest config is used only to impersonate: nothing went through it.
		Expect(recorder.take()).To(BeEmpty())
		drifted := apimeta.FindStatusCondition(instance(nn).Status.Conditions, status.DriftedCondition)
		Expect(drifted).NotTo(BeNil())
		Expect(drifted.Status).To(Equal(metav1.ConditionTrue), "the controller's own client gave the verdict")
	})

	It("states a dry-run the ServiceAccount may not send, and the verdict returns with the verb", func() {
		params, _, nn, saName := setup("drift-id-forbidden", true)
		modifyConfigMap("drifted-while-forbidden")

		By("without patch, the dry-run is refused and Drifted is Unknown")
		setVerbs("drift-id-forbidden-role", []string{"get", "list", "watch", "create", "update", "delete"})
		// The authorizer sees the role change asynchronously; each NoOp
		// reconcile runs drift detection again.
		Eventually(func(g Gomega) {
			_, err := opmreconcile.ReconcileModuleInstance(ctx, params, ctrl.Request{NamespacedName: nn})
			g.Expect(err).NotTo(HaveOccurred())
			mi := instance(nn)
			drifted := apimeta.FindStatusCondition(mi.Status.Conditions, status.DriftedCondition)
			g.Expect(drifted).NotTo(BeNil())
			g.Expect(drifted.Status).To(Equal(metav1.ConditionUnknown))
			g.Expect(drifted.Reason).To(Equal(status.DriftCheckForbiddenReason))
			g.Expect(drifted.Message).To(ContainSubstring("system:serviceaccount:" + namespace + ":" + saName))
			g.Expect(strings.ToLower(drifted.Message)).To(ContainSubstring("forbidden"))
			g.Expect(mi.Status.FailureCounters).NotTo(BeNil())
			g.Expect(mi.Status.FailureCounters.Drift).To(BeNumerically(">", 0))
			g.Expect(apimeta.IsStatusConditionTrue(mi.Status.Conditions, status.ReadyCondition)).To(BeTrue(),
				"a refused drift check does not move Ready")
			g.Expect(apimeta.FindStatusCondition(mi.Status.Conditions, status.StalledCondition)).To(BeNil())
		}, 10*time.Second, 200*time.Millisecond).Should(Succeed())

		By("the object the ServiceAccount may not patch was left alone")
		var cm corev1.ConfigMap
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: cmName, Namespace: namespace}, &cm)).To(Succeed())
		Expect(cm.Data["message"]).To(Equal("drifted-while-forbidden"))

		By("with patch again, the dry-run gives its verdict")
		setVerbs("drift-id-forbidden-role", allVerbs)
		Eventually(func(g Gomega) {
			_, err := opmreconcile.ReconcileModuleInstance(ctx, params, ctrl.Request{NamespacedName: nn})
			g.Expect(err).NotTo(HaveOccurred())
			mi := instance(nn)
			drifted := apimeta.FindStatusCondition(mi.Status.Conditions, status.DriftedCondition)
			g.Expect(drifted).NotTo(BeNil())
			g.Expect(drifted.Status).To(Equal(metav1.ConditionTrue))
			g.Expect(drifted.Reason).To(Equal(status.DriftDetectedReason))
			g.Expect(mi.Status.FailureCounters.Drift).To(BeZero())
		}, 10*time.Second, 200*time.Millisecond).Should(Succeed())
	})

	// Flux's diff drops the error of its own read, so a ServiceAccount that
	// may patch and not get would be told that an object it never saw drifted.
	It("gives no verdict against an object the ServiceAccount may not read", func() {
		params, _, nn, saName := setup("drift-id-noget", true)

		setVerbs("drift-id-noget-role", []string{"list", "watch", "create", "update", "patch", "delete"})
		Eventually(func(g Gomega) {
			_, err := opmreconcile.ReconcileModuleInstance(ctx, params, ctrl.Request{NamespacedName: nn})
			g.Expect(err).NotTo(HaveOccurred())
			mi := instance(nn)
			drifted := apimeta.FindStatusCondition(mi.Status.Conditions, status.DriftedCondition)
			g.Expect(drifted).NotTo(BeNil())
			g.Expect(drifted.Status).To(Equal(metav1.ConditionUnknown))
			g.Expect(drifted.Reason).To(Equal(status.DriftCheckForbiddenReason))
			g.Expect(drifted.Message).To(ContainSubstring("system:serviceaccount:" + namespace + ":" + saName))
			g.Expect(drifted.Message).To(ContainSubstring("cannot get"))
			g.Expect(apimeta.IsStatusConditionTrue(mi.Status.Conditions, status.ReadyCondition)).To(BeTrue())
		}, 10*time.Second, 200*time.Millisecond).Should(Succeed())

		// Nothing drifted, and the authorizer has settled: the condition
		// must never have turned True on the way.
		Consistently(func(g Gomega) {
			_, err := opmreconcile.ReconcileModuleInstance(ctx, params, ctrl.Request{NamespacedName: nn})
			g.Expect(err).NotTo(HaveOccurred())
			drifted := apimeta.FindStatusCondition(instance(nn).Status.Conditions, status.DriftedCondition)
			g.Expect(drifted).NotTo(BeNil())
			g.Expect(drifted.Status).To(Equal(metav1.ConditionUnknown))
		}, time.Second, 200*time.Millisecond).Should(Succeed())
	})

	// The apply guard reads each object as the identity that applies. A
	// ServiceAccount that may patch and not get would otherwise write over an
	// object nobody judged.
	It("writes nothing over an object the ServiceAccount may not read, and stalls", func() {
		params, _, nn, saName := setup("own-noget", true)

		setVerbs("own-noget-role", []string{"list", "watch", "create", "update", "patch", "delete"})
		Eventually(func(g Gomega) {
			_, err := opmreconcile.ReconcileModuleInstance(ctx, params, ctrl.Request{NamespacedName: nn})
			g.Expect(err).NotTo(HaveOccurred())
			drifted := apimeta.FindStatusCondition(instance(nn).Status.Conditions, status.DriftedCondition)
			g.Expect(drifted).NotTo(BeNil())
			g.Expect(drifted.Reason).To(Equal(status.DriftCheckForbiddenReason))
		}, 10*time.Second, 200*time.Millisecond).Should(Succeed(), "the authorizer has seen the role change")

		By("a changed render")
		mi := instance(nn)
		mi.Spec.Values = &releasesv1alpha1.RawValues{}
		mi.Spec.Values.Raw = []byte(`{"message": "changed"}`)
		Expect(k8sClient.Update(ctx, mi)).To(Succeed())
		result, err := opmreconcile.ReconcileModuleInstance(ctx, params, ctrl.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(opmreconcile.StalledRecheckInterval))

		stalled := apimeta.FindStatusCondition(instance(nn).Status.Conditions, status.StalledCondition)
		Expect(stalled).NotTo(BeNil())
		Expect(stalled.Status).To(Equal(metav1.ConditionTrue))
		Expect(stalled.Reason).To(Equal(status.ImpersonationFailedReason))
		Expect(stalled.Message).To(ContainSubstring("system:serviceaccount:" + namespace + ":" + saName))
		Expect(stalled.Message).To(ContainSubstring("cannot get"))

		var cm corev1.ConfigMap
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: cmName, Namespace: namespace}, &cm)).To(Succeed())
		Expect(cm.Data["message"]).To(Equal("hello"), "nothing is written")
	})

	// The restore list comes from the dry-run, and the restore is an apply:
	// both must be the ServiceAccount's, or the operator would create a
	// tenant's objects with its own rights.
	It("restores a missing object as the ServiceAccount, on every request", func() {
		params, recorder, nn, saName := setup("drift-id-restore", true)
		Expect(k8sClient.Delete(ctx, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: cmName, Namespace: namespace},
		})).To(Succeed())

		_, err := opmreconcile.ReconcileModuleInstance(ctx, params, ctrl.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		var reads, dryRuns, writes int
		for _, req := range recorder.takeAll() {
			if !strings.HasSuffix(req.path, "/configmaps/"+cmName) {
				continue
			}
			Expect(req.user).To(Equal("system:serviceaccount:"+namespace+":"+saName),
				"%s %s dryRun=%q", req.method, req.path, req.dryRun)
			switch {
			case req.method == http.MethodGet:
				reads++
			case req.method == http.MethodPatch && req.dryRun != "":
				dryRuns++
			case req.method == http.MethodPatch:
				writes++
			}
		}
		Expect(reads).To(BeNumerically(">", 0), "the object is read through the impersonating config")
		Expect(dryRuns).To(BeNumerically(">", 0), "the dry-runs go through the impersonating config")
		Expect(writes).To(BeNumerically(">", 0), "the restore is applied through the impersonating config")

		var cm corev1.ConfigMap
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: cmName, Namespace: namespace}, &cm)).To(Succeed())
		Expect(apimeta.IsStatusConditionTrue(instance(nn).Status.Conditions, status.ReadyCondition)).To(BeTrue())
	})

	// The dry-run of an object that does not exist is authorized as a create.
	It("restores nothing the ServiceAccount may not create, and says so", func() {
		params, _, nn, saName := setup("drift-id-nocreate", true)
		cmKey := types.NamespacedName{Name: cmName, Namespace: namespace}

		setVerbs("drift-id-nocreate-role", []string{"get", "list", "watch", "update", "patch", "delete"})
		// The authorizer sees the role change asynchronously. Until it has, a
		// reconcile restores the object, so each try deletes it again.
		Eventually(func(g Gomega) {
			g.Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: cmName, Namespace: namespace},
			}))).To(Succeed())
			_, err := opmreconcile.ReconcileModuleInstance(ctx, params, ctrl.Request{NamespacedName: nn})
			g.Expect(err).NotTo(HaveOccurred())

			mi := instance(nn)
			drifted := apimeta.FindStatusCondition(mi.Status.Conditions, status.DriftedCondition)
			g.Expect(drifted).NotTo(BeNil())
			g.Expect(drifted.Status).To(Equal(metav1.ConditionUnknown))
			g.Expect(drifted.Reason).To(Equal(status.DriftCheckForbiddenReason))
			g.Expect(drifted.Message).To(ContainSubstring("system:serviceaccount:" + namespace + ":" + saName))
			g.Expect(drifted.Message).To(ContainSubstring("cannot create"))
			var cm corev1.ConfigMap
			g.Expect(apierrors.IsNotFound(k8sClient.Get(ctx, cmKey, &cm))).To(BeTrue(),
				"the operator must not create what the ServiceAccount may not")
			g.Expect(apimeta.IsStatusConditionTrue(mi.Status.Conditions, status.ReadyCondition)).To(BeTrue())
		}, 20*time.Second, 300*time.Millisecond).Should(Succeed())

		// With the refusal settled, a further reconcile still restores nothing.
		_, err := opmreconcile.ReconcileModuleInstance(ctx, params, ctrl.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())
		var cm corev1.ConfigMap
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, cmKey, &cm))).To(BeTrue())
	})

	It("sends no dry-run when the ServiceAccount is gone, and says so", func() {
		params, recorder, nn, saName := setup("drift-id-gone", true)
		modifyConfigMap("drifted-without-identity")
		Expect(k8sClient.Delete(ctx, &corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{Name: saName, Namespace: namespace},
		})).To(Succeed())

		_, err := opmreconcile.ReconcileModuleInstance(ctx, params, ctrl.Request{NamespacedName: nn})
		Expect(err).NotTo(HaveOccurred())

		Expect(recorder.take()).To(BeEmpty(), "no dry-run through the impersonating config")
		mi := instance(nn)
		drifted := apimeta.FindStatusCondition(mi.Status.Conditions, status.DriftedCondition)
		Expect(drifted).NotTo(BeNil(), "the operator's own identity must not give the verdict")
		Expect(drifted.Status).To(Equal(metav1.ConditionUnknown))
		Expect(drifted.Reason).To(Equal(status.ImpersonationFailedReason))
		Expect(drifted.Message).To(ContainSubstring("drift detection did not run"))
		Expect(drifted.Message).To(ContainSubstring(saName))
		Expect(mi.Status.FailureCounters).NotTo(BeNil())
		Expect(mi.Status.FailureCounters.Drift).To(BeNumerically(">", 0))
		Expect(apimeta.IsStatusConditionTrue(mi.Status.Conditions, status.ReadyCondition)).To(BeTrue(),
			"a reconcile with unchanged digests stays a NoOp")
	})
})
