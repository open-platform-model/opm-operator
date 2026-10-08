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
	"os"
	"path/filepath"
	"slices"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/yaml"

	"github.com/open-platform-model/opm-operator/internal/apply"
)

// The operator in these specs is a user that holds exactly the rules of the
// shipped config/rbac/role.yaml, so they fail when that role cannot do what
// the apply path needs, and when it can impersonate more than a
// ServiceAccount.
var _ = Describe("Impersonation under the shipped manager role", Ordered, ContinueOnFailure, func() {
	const (
		operatorUser = "shipped-role-operator"
		roleName     = "imp-shipped-manager-role"
		directNS     = "imp-shipped-direct"
		groupNS      = "imp-shipped-group"
		editRole     = "imp-shipped-configmap-editor"
	)

	var (
		shipped        rbacv1.ClusterRole
		operatorCfg    *rest.Config
		operatorClient client.Client
	)

	configMap := func(namespace, name string) *unstructured.Unstructured {
		return &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata":   map[string]any{"name": name, "namespace": namespace},
			"data":       map[string]any{"key": "value"},
		}}
	}

	// applyAs applies one ConfigMap through the client the reconciler builds
	// for the ServiceAccount, with the operator's own credentials underneath.
	applyAs := func(namespace, saName, cmName string) error {
		impClient, err := apply.NewImpersonatedClient(ctx, operatorCfg, operatorClient, scheme.Scheme, namespace, saName)
		Expect(err).NotTo(HaveOccurred())
		rm := apply.NewResourceManager(impClient, "opm-controller")
		_, err = apply.Apply(ctx, rm, []*unstructured.Unstructured{configMap(namespace, cmName)}, apply.ApplyOptions{})
		return err
	}

	BeforeAll(func() {
		raw, err := os.ReadFile(filepath.Join("..", "..", "..", "config", "rbac", "role.yaml"))
		Expect(err).NotTo(HaveOccurred())
		Expect(yaml.UnmarshalStrict(raw, &shipped)).To(Succeed())
		Expect(shipped.Rules).NotTo(BeEmpty())

		Expect(k8sClient.Create(ctx, &rbacv1.ClusterRole{
			ObjectMeta: metav1.ObjectMeta{Name: roleName},
			Rules:      shipped.Rules,
		})).To(Succeed())
		Expect(k8sClient.Create(ctx, &rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: roleName},
			RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: roleName},
			Subjects:   []rbacv1.Subject{{APIGroup: rbacv1.GroupName, Kind: rbacv1.UserKind, Name: operatorUser}},
		})).To(Succeed())

		user, err := testEnv.AddUser(envtest.User{Name: operatorUser}, cfg)
		Expect(err).NotTo(HaveOccurred())
		operatorCfg = user.Config()
		operatorClient, err = client.New(operatorCfg, client.Options{Scheme: scheme.Scheme})
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Create(ctx, &rbacv1.ClusterRole{
			ObjectMeta: metav1.ObjectMeta{Name: editRole},
			Rules: []rbacv1.PolicyRule{{
				APIGroups: []string{""},
				Resources: []string{"configmaps"},
				Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
			}},
		})).To(Succeed())

		for _, ns := range []string{directNS, groupNS} {
			Expect(k8sClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}})).To(Succeed())
		}
		for ns, names := range map[string][]string{directNS: {"bound-sa", "unbound-sa"}, groupNS: {"member-sa"}} {
			for _, name := range names {
				Expect(k8sClient.Create(ctx, &corev1.ServiceAccount{
					ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
				})).To(Succeed())
			}
		}

		// directNS: only bound-sa may edit ConfigMaps, by a binding to itself.
		Expect(k8sClient.Create(ctx, &rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: "direct", Namespace: directNS},
			RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: editRole},
			Subjects:   []rbacv1.Subject{{Kind: rbacv1.ServiceAccountKind, Name: "bound-sa", Namespace: directNS}},
		})).To(Succeed())
		// groupNS: every ServiceAccount of the namespace may, by its group.
		Expect(k8sClient.Create(ctx, &rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: "by-group", Namespace: groupNS},
			RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: editRole},
			Subjects: []rbacv1.Subject{{
				APIGroup: rbacv1.GroupName, Kind: rbacv1.GroupKind, Name: "system:serviceaccounts:" + groupNS,
			}},
		})).To(Succeed())
	})

	AfterAll(func() {
		for _, obj := range []client.Object{
			&rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: roleName}},
			&rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: roleName}},
			&rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: editRole}},
			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: directNS}},
			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: groupNS}},
		} {
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, obj))).To(Succeed())
		}
	})

	It("grants impersonate on serviceaccounts and on nothing else", func() {
		var impersonable []string
		for _, rule := range shipped.Rules {
			for _, verb := range rule.Verbs {
				if verb == "impersonate" || verb == rbacv1.VerbAll {
					impersonable = append(impersonable, rule.Resources...)
				}
			}
		}
		Expect(impersonable).To(ConsistOf("serviceaccounts"))
	})

	// The ServiceAccount watch of the ModuleInstance controller needs list and
	// watch; nothing in the operator writes a ServiceAccount.
	It("reads ServiceAccounts and writes none", func() {
		var verbs []string
		for _, rule := range shipped.Rules {
			if slices.Contains(rule.Resources, "serviceaccounts") {
				verbs = append(verbs, rule.Verbs...)
			}
		}
		Expect(verbs).To(ConsistOf("get", "impersonate", "list", "watch"))

		var list metav1.PartialObjectMetadataList
		list.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("ServiceAccountList"))
		Expect(operatorClient.List(ctx, &list)).To(Succeed())

		err := operatorClient.Create(ctx, &corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{Name: "never-created", Namespace: directNS},
		})
		Expect(apierrors.IsForbidden(err)).To(BeTrue(), "got %v", err)
	})

	It("applies as a ServiceAccount that a RoleBinding names", func() {
		Expect(applyAs(directNS, "bound-sa", "applied-directly")).To(Succeed())

		var cm corev1.ConfigMap
		Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: directNS, Name: "applied-directly"}, &cm)).To(Succeed())
	})

	It("applies as a ServiceAccount authorised only through system:serviceaccounts:<namespace>", func() {
		Expect(applyAs(groupNS, "member-sa", "applied-by-group")).To(Succeed())

		var cm corev1.ConfigMap
		Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: groupNS, Name: "applied-by-group"}, &cm)).To(Succeed())
	})

	It("is refused the apply when the ServiceAccount has no binding", func() {
		err := applyAs(directNS, "unbound-sa", "never-applied")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(
			`User "system:serviceaccount:` + directNS + `:unbound-sa" cannot`))

		var cm corev1.ConfigMap
		err = k8sClient.Get(ctx, client.ObjectKey{Namespace: directNS, Name: "never-applied"}, &cm)
		Expect(apierrors.IsNotFound(err)).To(BeTrue())
	})

	It("acts with the identity a ServiceAccount token would carry", func() {
		impClient, err := apply.NewImpersonatedClient(ctx, operatorCfg, operatorClient, scheme.Scheme, groupNS, "member-sa")
		Expect(err).NotTo(HaveOccurred())

		var review authenticationv1.SelfSubjectReview
		Expect(impClient.Create(ctx, &review)).To(Succeed())
		Expect(review.Status.UserInfo.Username).To(Equal("system:serviceaccount:" + groupNS + ":member-sa"))
		Expect(review.Status.UserInfo.Groups).To(ConsistOf(
			"system:serviceaccounts",
			"system:serviceaccounts:"+groupNS,
			"system:authenticated",
		))
	})

	DescribeTable("cannot impersonate anything but a ServiceAccount",
		func(imp rest.ImpersonationConfig) {
			impCfg := rest.CopyConfig(operatorCfg)
			impCfg.Impersonate = imp
			c, err := client.New(impCfg, client.Options{Scheme: scheme.Scheme})
			Expect(err).NotTo(HaveOccurred())

			var review authenticationv1.SelfSubjectReview
			err = c.Create(ctx, &review)
			Expect(apierrors.IsForbidden(err)).To(BeTrue(), "got %v", err)
		},
		Entry("a user", rest.ImpersonationConfig{UserName: "jane"}),
		Entry("a user with a group", rest.ImpersonationConfig{UserName: "jane", Groups: []string{"system:masters"}}),
		Entry("a ServiceAccount with a group it does not have", rest.ImpersonationConfig{
			UserName: "system:serviceaccount:" + groupNS + ":member-sa",
			Groups:   []string{"system:masters"},
		}),
	)
})
