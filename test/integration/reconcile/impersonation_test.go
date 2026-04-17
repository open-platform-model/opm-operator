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
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	releasesv1alpha1 "github.com/open-platform-model/poc-controller/api/v1alpha1"
	"github.com/open-platform-model/poc-controller/internal/apply"
	opmreconcile "github.com/open-platform-model/poc-controller/internal/reconcile"
	"github.com/open-platform-model/poc-controller/internal/status"
)

func reconcileParamsWithConfig() *opmreconcile.ModuleReleaseParams {
	return &opmreconcile.ModuleReleaseParams{
		Client:          k8sClient,
		APIReader:       k8sClient,
		RestConfig:      cfg,
		Provider:        testProvider(),
		ResourceManager: apply.NewResourceManager(k8sClient, "opm-controller"),
		EventRecorder:   events.NewFakeRecorder(10),
		Renderer:        &stubRenderer{},
	}
}

var _ = Describe("ServiceAccount Impersonation", func() {
	Context("Reconcile with valid ServiceAccount", func() {
		It("should apply resources using impersonated identity", func() {
			mrName := "imp-valid-mr"
			saName := "deploy-sa"

			// Create SA and grant it permissions to manage ConfigMaps.
			sa := &corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name:      saName,
					Namespace: namespace,
				},
			}
			Expect(k8sClient.Create(ctx, sa)).To(Succeed())

			role := &rbacv1.ClusterRole{
				ObjectMeta: metav1.ObjectMeta{Name: "imp-test-role"},
				Rules: []rbacv1.PolicyRule{
					{
						APIGroups: []string{""},
						Resources: []string{"configmaps"},
						Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
					},
				},
			}
			Expect(k8sClient.Create(ctx, role)).To(Succeed())

			binding := &rbacv1.ClusterRoleBinding{
				ObjectMeta: metav1.ObjectMeta{Name: "imp-test-binding"},
				RoleRef: rbacv1.RoleRef{
					APIGroup: "rbac.authorization.k8s.io",
					Kind:     "ClusterRole",
					Name:     "imp-test-role",
				},
				Subjects: []rbacv1.Subject{
					{
						Kind:      "ServiceAccount",
						Name:      saName,
						Namespace: namespace,
					},
				},
			}
			Expect(k8sClient.Create(ctx, binding)).To(Succeed())

			// Create MR with serviceAccountName.
			mr := &releasesv1alpha1.ModuleRelease{
				ObjectMeta: metav1.ObjectMeta{
					Name:      mrName,
					Namespace: namespace,
				},
				Spec: releasesv1alpha1.ModuleReleaseSpec{
					Module: releasesv1alpha1.ModuleReference{
						Path:    "opmodel.dev/test/module",
						Version: "v0.1.0",
					},
					Prune:              true,
					ServiceAccountName: saName,
					Values:             &releasesv1alpha1.RawValues{},
				},
			}
			mr.Spec.Values.Raw = []byte(`{"message": "impersonated"}`)
			Expect(k8sClient.Create(ctx, mr)).To(Succeed())

			params := reconcileParamsWithConfig()
			nn := types.NamespacedName{Name: mrName, Namespace: namespace}
			ensureFinalizer(params, nn)

			// Reconcile — should succeed using impersonated client.
			// envtest admin user can impersonate any SA.
			result, err := opmreconcile.ReconcileModuleRelease(ctx, params, ctrl.Request{
				NamespacedName: nn,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(BeZero())

			var updated releasesv1alpha1.ModuleRelease
			Expect(k8sClient.Get(ctx, nn, &updated)).To(Succeed())

			// Ready=True
			ready := apimeta.FindStatusCondition(updated.Status.Conditions, status.ReadyCondition)
			Expect(ready).NotTo(BeNil())
			Expect(ready.Status).To(Equal(metav1.ConditionTrue))

			// Resources were created.
			Expect(updated.Status.Inventory).NotTo(BeNil())
			Expect(updated.Status.Inventory.Count).To(BeNumerically(">", 0))

			// Cleanup
			Expect(k8sClient.Delete(ctx, &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "test-module", Namespace: namespace},
			})).To(Succeed())
			Expect(k8sClient.Delete(ctx, &releasesv1alpha1.ModuleRelease{
				ObjectMeta: metav1.ObjectMeta{Name: mrName, Namespace: namespace},
			})).To(Succeed())
			Expect(k8sClient.Delete(ctx, &corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{Name: saName, Namespace: namespace},
			})).To(Succeed())
			Expect(k8sClient.Delete(ctx, &rbacv1.ClusterRoleBinding{
				ObjectMeta: metav1.ObjectMeta{Name: "imp-test-binding"},
			})).To(Succeed())
			Expect(k8sClient.Delete(ctx, &rbacv1.ClusterRole{
				ObjectMeta: metav1.ObjectMeta{Name: "imp-test-role"},
			})).To(Succeed())
		})
	})

	Context("Reconcile with missing ServiceAccount", func() {
		It("should stall with ImpersonationFailed when SA does not exist", func() {
			mrName := "imp-missing-mr"

			// Create MR with nonexistent SA.
			mr := &releasesv1alpha1.ModuleRelease{
				ObjectMeta: metav1.ObjectMeta{
					Name:      mrName,
					Namespace: namespace,
				},
				Spec: releasesv1alpha1.ModuleReleaseSpec{
					Module: releasesv1alpha1.ModuleReference{
						Path:    "opmodel.dev/test/module",
						Version: "v0.1.0",
					},
					Prune:              true,
					ServiceAccountName: "nonexistent-sa",
					Values:             &releasesv1alpha1.RawValues{},
				},
			}
			mr.Spec.Values.Raw = []byte(`{"message": "hello"}`)
			Expect(k8sClient.Create(ctx, mr)).To(Succeed())

			params := reconcileParamsWithConfig()
			nn := types.NamespacedName{Name: mrName, Namespace: namespace}
			ensureFinalizer(params, nn)

			result, err := opmreconcile.ReconcileModuleRelease(ctx, params, ctrl.Request{
				NamespacedName: nn,
			})
			Expect(err).NotTo(HaveOccurred(), "stalled errors return nil")
			Expect(result.RequeueAfter).To(Equal(30*time.Minute), "stalled requeues with safety interval")

			var updated releasesv1alpha1.ModuleRelease
			Expect(k8sClient.Get(ctx, nn, &updated)).To(Succeed())

			// Stalled=True with ImpersonationFailed
			stalled := apimeta.FindStatusCondition(updated.Status.Conditions, status.StalledCondition)
			Expect(stalled).NotTo(BeNil())
			Expect(stalled.Status).To(Equal(metav1.ConditionTrue))
			Expect(stalled.Reason).To(Equal(status.ImpersonationFailedReason))

			// Ready=False
			ready := apimeta.FindStatusCondition(updated.Status.Conditions, status.ReadyCondition)
			Expect(ready).NotTo(BeNil())
			Expect(ready.Status).To(Equal(metav1.ConditionFalse))

			// Message mentions the SA
			Expect(stalled.Message).To(ContainSubstring("nonexistent-sa"))

			// Cleanup
			Expect(k8sClient.Delete(ctx, &releasesv1alpha1.ModuleRelease{
				ObjectMeta: metav1.ObjectMeta{Name: mrName, Namespace: namespace},
			})).To(Succeed())
		})
	})

	Context("Reconcile with impersonation RBAC denial", func() {
		It("should stall when controller user lacks impersonate permission", func() {
			mrName := "imp-denied-mr"
			saName := "target-sa"

			// Create the target SA.
			sa := &corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name:      saName,
					Namespace: namespace,
				},
			}
			Expect(k8sClient.Create(ctx, sa)).To(Succeed())

			// Create a restricted user that can GET ServiceAccounts but NOT impersonate.
			restrictedUser, err := testEnv.AddUser(envtest.User{
				Name:   "restricted-controller",
				Groups: []string{"system:authenticated"},
			}, cfg)
			Expect(err).NotTo(HaveOccurred())

			// Grant the restricted user GET on ServiceAccounts (needed for SA verification).
			restrictedRole := &rbacv1.ClusterRole{
				ObjectMeta: metav1.ObjectMeta{Name: "imp-denied-role"},
				Rules: []rbacv1.PolicyRule{
					{
						APIGroups: []string{""},
						Resources: []string{"serviceaccounts"},
						Verbs:     []string{"get"},
					},
				},
			}
			Expect(k8sClient.Create(ctx, restrictedRole)).To(Succeed())

			restrictedBinding := &rbacv1.ClusterRoleBinding{
				ObjectMeta: metav1.ObjectMeta{Name: "imp-denied-binding"},
				RoleRef: rbacv1.RoleRef{
					APIGroup: "rbac.authorization.k8s.io",
					Kind:     "ClusterRole",
					Name:     "imp-denied-role",
				},
				Subjects: []rbacv1.Subject{
					{
						Kind: "User",
						Name: "restricted-controller",
					},
				},
			}
			Expect(k8sClient.Create(ctx, restrictedBinding)).To(Succeed())

			mr := &releasesv1alpha1.ModuleRelease{
				ObjectMeta: metav1.ObjectMeta{
					Name:      mrName,
					Namespace: namespace,
				},
				Spec: releasesv1alpha1.ModuleReleaseSpec{
					Module: releasesv1alpha1.ModuleReference{
						Path:    "opmodel.dev/test/module",
						Version: "v0.1.0",
					},
					Prune:              true,
					ServiceAccountName: saName,
					Values:             &releasesv1alpha1.RawValues{},
				},
			}
			mr.Spec.Values.Raw = []byte(`{"message": "denied"}`)
			Expect(k8sClient.Create(ctx, mr)).To(Succeed())

			// Use restricted user's config as RestConfig — impersonation will be denied.
			restrictedCfg := restrictedUser.Config()
			params := &opmreconcile.ModuleReleaseParams{
				Client:          k8sClient,
				APIReader:       k8sClient,
				RestConfig:      restrictedCfg,
				Provider:        testProvider(),
				ResourceManager: apply.NewResourceManager(k8sClient, "opm-controller"),
				EventRecorder:   events.NewFakeRecorder(10),
				Renderer:        &stubRenderer{},
			}

			nn := types.NamespacedName{Name: mrName, Namespace: namespace}
			ensureFinalizer(params, nn)

			result, reconcileErr := opmreconcile.ReconcileModuleRelease(ctx, params, ctrl.Request{
				NamespacedName: nn,
			})
			Expect(reconcileErr).NotTo(HaveOccurred(), "stalled errors return nil")
			Expect(result.RequeueAfter).To(Equal(30*time.Minute), "stalled requeues with safety interval")

			var updated releasesv1alpha1.ModuleRelease
			Expect(k8sClient.Get(ctx, nn, &updated)).To(Succeed())

			// Stalled=True with ImpersonationFailed
			stalled := apimeta.FindStatusCondition(updated.Status.Conditions, status.StalledCondition)
			Expect(stalled).NotTo(BeNil())
			Expect(stalled.Status).To(Equal(metav1.ConditionTrue))
			Expect(stalled.Reason).To(Equal(status.ImpersonationFailedReason))

			// Ready=False
			ready := apimeta.FindStatusCondition(updated.Status.Conditions, status.ReadyCondition)
			Expect(ready).NotTo(BeNil())
			Expect(ready.Status).To(Equal(metav1.ConditionFalse))

			// Error message mentions Forbidden
			Expect(stalled.Message).To(ContainSubstring("Forbidden"))

			// Cleanup
			Expect(k8sClient.Delete(ctx, &releasesv1alpha1.ModuleRelease{
				ObjectMeta: metav1.ObjectMeta{Name: mrName, Namespace: namespace},
			})).To(Succeed())
			Expect(k8sClient.Delete(ctx, &corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{Name: saName, Namespace: namespace},
			})).To(Succeed())
			Expect(k8sClient.Delete(ctx, &rbacv1.ClusterRoleBinding{
				ObjectMeta: metav1.ObjectMeta{Name: "imp-denied-binding"},
			})).To(Succeed())
			Expect(k8sClient.Delete(ctx, &rbacv1.ClusterRole{
				ObjectMeta: metav1.ObjectMeta{Name: "imp-denied-role"},
			})).To(Succeed())
		})
	})

	Context("Reconcile without serviceAccountName", func() {
		It("should use controller client when SA is not specified", func() {
			mrName := "imp-nosa-mr"

			createModuleRelease(mrName)

			// Use params with RestConfig set but no SA on the MR.
			params := reconcileParamsWithConfig()
			nn := types.NamespacedName{Name: mrName, Namespace: namespace}
			ensureFinalizer(params, nn)

			result, err := opmreconcile.ReconcileModuleRelease(ctx, params, ctrl.Request{
				NamespacedName: nn,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(BeZero())

			var updated releasesv1alpha1.ModuleRelease
			Expect(k8sClient.Get(ctx, nn, &updated)).To(Succeed())

			// Ready=True — normal reconcile without impersonation.
			ready := apimeta.FindStatusCondition(updated.Status.Conditions, status.ReadyCondition)
			Expect(ready).NotTo(BeNil())
			Expect(ready.Status).To(Equal(metav1.ConditionTrue))

			// No stalled condition.
			stalled := apimeta.FindStatusCondition(updated.Status.Conditions, status.StalledCondition)
			Expect(stalled).To(BeNil())

			// Cleanup
			Expect(k8sClient.Delete(ctx, &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "test-module", Namespace: namespace},
			})).To(Succeed())
			Expect(k8sClient.Delete(ctx, &releasesv1alpha1.ModuleRelease{
				ObjectMeta: metav1.ObjectMeta{Name: mrName, Namespace: namespace},
			})).To(Succeed())
		})
	})
})
