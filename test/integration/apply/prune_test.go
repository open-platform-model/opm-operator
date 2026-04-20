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

package apply_test

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	"github.com/open-platform-model/opm-operator/internal/apply"
	"github.com/open-platform-model/opm-operator/pkg/core"
)

const testOwnerUUID = "00000000-0000-0000-0000-0000000000aa"

// ownedLabels returns the label set that the prune ownership guard expects on
// a ConfigMap that is OPM-managed and belongs to the release identified by
// testOwnerUUID.
func ownedLabels() map[string]string {
	return map[string]string{
		core.LabelManagedBy:         core.LabelManagedByControllerValue,
		core.LabelModuleReleaseUUID: testOwnerUUID,
	}
}

var _ = Describe("Prune", func() {
	Context("When stale set contains a ConfigMap", func() {
		It("should delete the stale ConfigMap from the cluster", func() {
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "prune-test-cm",
					Namespace: "default",
					Labels:    ownedLabels(),
				},
				Data: map[string]string{"key": "value"},
			}

			By("creating the ConfigMap in the cluster")
			Expect(k8sClient.Create(ctx, cm)).To(Succeed())

			stale := []releasesv1alpha1.InventoryEntry{{
				Kind:      "ConfigMap",
				Version:   "v1",
				Namespace: "default",
				Name:      "prune-test-cm",
			}}

			By("pruning the stale set")
			result, err := apply.Prune(ctx, k8sClient, testOwnerUUID, stale)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Deleted).To(Equal(1))
			Expect(result.Skipped).To(Equal(0))

			By("verifying the ConfigMap no longer exists")
			fetched := &corev1.ConfigMap{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Namespace: "default",
				Name:      "prune-test-cm",
			}, fetched)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not found"))
		})
	})

	Context("When stale set contains a Namespace", func() {
		It("should skip the Namespace due to safety exclusion", func() {
			stale := []releasesv1alpha1.InventoryEntry{{
				Kind:    "Namespace",
				Version: "v1",
				Name:    "prune-test-ns",
			}}

			result, err := apply.Prune(ctx, k8sClient, testOwnerUUID, stale)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Deleted).To(Equal(0))
			Expect(result.Skipped).To(Equal(1))
		})
	})

	Context("When stale resource is already deleted", func() {
		It("should not error for a missing resource", func() {
			stale := []releasesv1alpha1.InventoryEntry{{
				Kind:      "ConfigMap",
				Version:   "v1",
				Namespace: "default",
				Name:      "prune-already-gone-cm",
			}}

			result, err := apply.Prune(ctx, k8sClient, testOwnerUUID, stale)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Deleted).To(Equal(0))
			Expect(result.Skipped).To(Equal(0))
		})
	})

	Context("When stale set is empty", func() {
		It("should be a no-op and return zero counts", func() {
			result, err := apply.Prune(ctx, k8sClient, testOwnerUUID, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Deleted).To(Equal(0))
			Expect(result.Skipped).To(Equal(0))
		})
	})

	Context("When stale set contains a CustomResourceDefinition", func() {
		It("should skip the CRD due to safety exclusion", func() {
			stale := []releasesv1alpha1.InventoryEntry{{
				Group:   "apiextensions.k8s.io",
				Kind:    "CustomResourceDefinition",
				Version: "v1",
				Name:    "prune-test-crd.example.com",
			}}

			result, err := apply.Prune(ctx, k8sClient, testOwnerUUID, stale)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Deleted).To(Equal(0))
			Expect(result.Skipped).To(Equal(1))
		})
	})

	Context("When stale set contains both pruneable and excluded resources", func() {
		It("should delete the pruneable resource and skip the excluded one", func() {
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "prune-mixed-cm",
					Namespace: "default",
					Labels:    ownedLabels(),
				},
				Data: map[string]string{"key": "value"},
			}

			By("creating the ConfigMap in the cluster")
			Expect(k8sClient.Create(ctx, cm)).To(Succeed())

			stale := []releasesv1alpha1.InventoryEntry{
				{
					Kind:      "ConfigMap",
					Version:   "v1",
					Namespace: "default",
					Name:      "prune-mixed-cm",
				},
				{
					Kind:    "Namespace",
					Version: "v1",
					Name:    "prune-mixed-ns",
				},
			}

			By("pruning the mixed stale set")
			result, err := apply.Prune(ctx, k8sClient, testOwnerUUID, stale)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Deleted).To(Equal(1))
			Expect(result.Skipped).To(Equal(1))

			By("verifying the ConfigMap no longer exists")
			fetched := &corev1.ConfigMap{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Namespace: "default",
				Name:      "prune-mixed-cm",
			}, fetched)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not found"))
		})
	})

	Context("When one delete fails and another succeeds", func() {
		It("should continue deleting remaining resources and return the error (fail-slow)", func() {
			cmOK := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "prune-failslow-ok",
					Namespace: "default",
					Labels:    ownedLabels(),
				},
				Data: map[string]string{"key": "ok"},
			}
			cmFail := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "prune-failslow-fail",
					Namespace: "default",
					Labels:    ownedLabels(),
				},
				Data: map[string]string{"key": "fail"},
			}

			By("creating both ConfigMaps in the cluster")
			Expect(k8sClient.Create(ctx, cmOK)).To(Succeed())
			Expect(k8sClient.Create(ctx, cmFail)).To(Succeed())

			By("constructing an interceptor client that fails on one specific resource")
			realClient, err := client.NewWithWatch(cfg, client.Options{})
			Expect(err).NotTo(HaveOccurred())

			failingClient := interceptor.NewClient(realClient, interceptor.Funcs{
				Delete: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
					if obj.GetName() == "prune-failslow-fail" {
						return fmt.Errorf("injected delete failure")
					}
					return c.Delete(ctx, obj, opts...)
				},
			})

			stale := []releasesv1alpha1.InventoryEntry{
				{
					Kind:      "ConfigMap",
					Version:   "v1",
					Namespace: "default",
					Name:      "prune-failslow-ok",
				},
				{
					Kind:      "ConfigMap",
					Version:   "v1",
					Namespace: "default",
					Name:      "prune-failslow-fail",
				},
			}

			By("pruning with the failing client")
			result, err := apply.Prune(ctx, failingClient, testOwnerUUID, stale)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("injected delete failure"))
			Expect(result.Deleted).To(Equal(1))
			Expect(result.Skipped).To(Equal(0))

			By("verifying the successful resource was deleted")
			fetched := &corev1.ConfigMap{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Namespace: "default",
				Name:      "prune-failslow-ok",
			}, fetched)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not found"))

			By("verifying the failed resource still exists")
			fetched = &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Namespace: "default",
				Name:      "prune-failslow-fail",
			}, fetched)).To(Succeed())
		})
	})

	Context("When live resource is missing the OPM managed-by label", func() {
		It("should skip the resource (foreign-owned) and increment Skipped", func() {
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "prune-foreign-cm",
					Namespace: "default",
				},
				Data: map[string]string{"key": "foreign"},
			}
			Expect(k8sClient.Create(ctx, cm)).To(Succeed())

			stale := []releasesv1alpha1.InventoryEntry{{
				Kind:      "ConfigMap",
				Version:   "v1",
				Namespace: "default",
				Name:      "prune-foreign-cm",
			}}

			result, err := apply.Prune(ctx, k8sClient, testOwnerUUID, stale)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Deleted).To(Equal(0))
			Expect(result.Skipped).To(Equal(1))

			By("verifying the foreign ConfigMap still exists")
			fetched := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Namespace: "default",
				Name:      "prune-foreign-cm",
			}, fetched)).To(Succeed())

			// Cleanup
			Expect(k8sClient.Delete(ctx, cm)).To(Succeed())
		})
	})

	Context("When live resource's release UUID disagrees with ownerUUID", func() {
		It("should skip the resource and increment Skipped", func() {
			const otherUUID = "00000000-0000-0000-0000-0000000000bb"
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "prune-cross-mr-cm",
					Namespace: "default",
					Labels: map[string]string{
						core.LabelManagedBy:         core.LabelManagedByControllerValue,
						core.LabelModuleReleaseUUID: otherUUID,
					},
				},
				Data: map[string]string{"key": "cross"},
			}
			Expect(k8sClient.Create(ctx, cm)).To(Succeed())

			stale := []releasesv1alpha1.InventoryEntry{{
				Kind:      "ConfigMap",
				Version:   "v1",
				Namespace: "default",
				Name:      "prune-cross-mr-cm",
			}}

			result, err := apply.Prune(ctx, k8sClient, testOwnerUUID, stale)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Deleted).To(Equal(0))
			Expect(result.Skipped).To(Equal(1))

			By("verifying the other-MR ConfigMap still exists")
			fetched := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Namespace: "default",
				Name:      "prune-cross-mr-cm",
			}, fetched)).To(Succeed())

			// Cleanup
			Expect(k8sClient.Delete(ctx, cm)).To(Succeed())
		})
	})

	Context("When live resource's release UUID matches ownerUUID", func() {
		It("should delete the resource", func() {
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "prune-owned-cm",
					Namespace: "default",
					Labels:    ownedLabels(),
				},
				Data: map[string]string{"key": "owned"},
			}
			Expect(k8sClient.Create(ctx, cm)).To(Succeed())

			stale := []releasesv1alpha1.InventoryEntry{{
				Kind:      "ConfigMap",
				Version:   "v1",
				Namespace: "default",
				Name:      "prune-owned-cm",
			}}

			result, err := apply.Prune(ctx, k8sClient, testOwnerUUID, stale)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Deleted).To(Equal(1))
			Expect(result.Skipped).To(Equal(0))

			By("verifying the owned ConfigMap no longer exists")
			fetched := &corev1.ConfigMap{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Namespace: "default",
				Name:      "prune-owned-cm",
			}, fetched)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not found"))
		})
	})

	Context("When live resource has legacy managed-by but no UUID label", func() {
		It("should delete the resource (legacy fallback tolerated)", func() {
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "prune-legacy-cm",
					Namespace: "default",
					Labels: map[string]string{
						core.LabelManagedBy: core.LabelManagedByLegacyValue,
					},
				},
				Data: map[string]string{"key": "legacy"},
			}
			Expect(k8sClient.Create(ctx, cm)).To(Succeed())

			stale := []releasesv1alpha1.InventoryEntry{{
				Kind:      "ConfigMap",
				Version:   "v1",
				Namespace: "default",
				Name:      "prune-legacy-cm",
			}}

			result, err := apply.Prune(ctx, k8sClient, testOwnerUUID, stale)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Deleted).To(Equal(1))
			Expect(result.Skipped).To(Equal(0))
		})
	})
})
