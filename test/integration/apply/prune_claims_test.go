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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/open-platform-model/library/opm/k8s/labels"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	"github.com/open-platform-model/opm-operator/internal/apply"
)

// createClaim creates a PersistentVolumeClaim in the default namespace and
// registers its cleanup. The API server puts the pvc-protection finalizer on
// every claim and envtest runs no controller that removes it, so the cleanup
// strips the finalizer before it deletes.
func createClaim(name string, lbls map[string]string) {
	GinkgoHelper()
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default", Labels: lbls},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("1Gi")},
			},
		},
	}
	Expect(k8sClient.Create(ctx, pvc)).To(Succeed())
	DeferCleanup(func() {
		var live corev1.PersistentVolumeClaim
		if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(pvc), &live); err != nil {
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
			return
		}
		live.Finalizers = nil
		Expect(k8sClient.Update(ctx, &live)).To(Succeed())
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &live))).To(Succeed())
	})
}

// claimDeleteRequested reports whether a delete reached the claim: it is
// gone, or it carries a deletion timestamp and waits on its finalizer.
func claimDeleteRequested(name string) bool {
	GinkgoHelper()
	var live corev1.PersistentVolumeClaim
	err := k8sClient.Get(ctx, types.NamespacedName{Namespace: "default", Name: name}, &live)
	if apierrors.IsNotFound(err) {
		return true
	}
	Expect(err).NotTo(HaveOccurred())
	return !live.DeletionTimestamp.IsZero()
}

func claimEntry(name string) releasesv1alpha1.InventoryEntry {
	return releasesv1alpha1.InventoryEntry{Kind: "PersistentVolumeClaim", Version: "v1", Namespace: "default", Name: name}
}

var _ = Describe("Prune and PersistentVolumeClaims", func() {
	It("keeps a claim by default and deletes every other stale resource", func() {
		createClaim("claims-default-pvc", ownedLabels())
		cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
			Name: "claims-default-cm", Namespace: "default", Labels: ownedLabels(),
		}}
		Expect(k8sClient.Create(ctx, cm)).To(Succeed())

		stale := []releasesv1alpha1.InventoryEntry{
			claimEntry("claims-default-pvc"),
			{Kind: "ConfigMap", Version: "v1", Namespace: "default", Name: "claims-default-cm"},
		}
		result, err := apply.Prune(ctx, k8sClient, []string{testOwnerUUID}, stale, apply.PruneOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Deleted).To(Equal(1))
		Expect(result.Skipped).To(Equal(0))
		Expect(result.Kept).To(Equal([]releasesv1alpha1.InventoryEntry{claimEntry("claims-default-pvc")}))

		Expect(claimDeleteRequested("claims-default-pvc")).To(BeFalse(), "the claim must be untouched")
		expectGone(client.ObjectKeyFromObject(cm), &corev1.ConfigMap{}, "the ConfigMap must be pruned as before")
	})

	It("deletes a claim when DeleteData is set", func() {
		createClaim("claims-delete-pvc", ownedLabels())

		result, err := apply.Prune(ctx, k8sClient, []string{testOwnerUUID},
			[]releasesv1alpha1.InventoryEntry{claimEntry("claims-delete-pvc")}, apply.PruneOptions{DeleteData: true})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Deleted).To(Equal(1))
		Expect(result.Kept).To(BeEmpty())
		Expect(claimDeleteRequested("claims-delete-pvc")).To(BeTrue())
	})

	It("does not report a claim that is already gone as kept", func() {
		result, err := apply.Prune(ctx, k8sClient, []string{testOwnerUUID},
			[]releasesv1alpha1.InventoryEntry{claimEntry("claims-absent-pvc")}, apply.PruneOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Kept).To(BeEmpty())
		Expect(result.Deleted).To(Equal(0))
		Expect(result.Skipped).To(Equal(0))
	})

	It("skips a claim another instance owns and does not report it as kept", func() {
		createClaim("claims-foreign-pvc", map[string]string{
			labels.ManagedBy:          labels.ManagedByController,
			labels.ModuleInstanceUUID: "00000000-0000-0000-0000-0000000000bb",
		})

		for _, opts := range []apply.PruneOptions{{}, {DeleteData: true}} {
			result, err := apply.Prune(ctx, k8sClient, []string{testOwnerUUID},
				[]releasesv1alpha1.InventoryEntry{claimEntry("claims-foreign-pvc")}, opts)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Skipped).To(Equal(1))
			Expect(result.Kept).To(BeEmpty())
		}
		Expect(claimDeleteRequested("claims-foreign-pvc")).To(BeFalse())
	})

	It("keeps a claim it cannot read without failing, and fails when it was asked to delete it", func() {
		createClaim("claims-unreadable-pvc", ownedLabels())
		realClient, err := client.NewWithWatch(cfg, client.Options{})
		Expect(err).NotTo(HaveOccurred())
		unreadable := interceptor.NewClient(realClient, interceptor.Funcs{
			Get: func(
				ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption,
			) error {
				if key.Name == "claims-unreadable-pvc" {
					return fmt.Errorf("injected read failure")
				}
				return c.Get(ctx, key, obj, opts...)
			},
		})
		stale := []releasesv1alpha1.InventoryEntry{claimEntry("claims-unreadable-pvc")}

		result, err := apply.Prune(ctx, unreadable, []string{testOwnerUUID}, stale, apply.PruneOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Kept).To(Equal(stale))

		result, err = apply.Prune(ctx, unreadable, []string{testOwnerUUID}, stale, apply.PruneOptions{DeleteData: true})
		Expect(err).To(MatchError(ContainSubstring("injected read failure")))
		Expect(result.Kept).To(BeEmpty())
		Expect(claimDeleteRequested("claims-unreadable-pvc")).To(BeFalse())
	})
})
