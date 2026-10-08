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
	"errors"

	fluxssa "github.com/fluxcd/pkg/ssa"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/open-platform-model/opm-operator/internal/apply"
)

// desiredClaim is a PersistentVolumeClaim as a render holds it.
// storageClassName is immutable once the claim exists.
func desiredClaim(name, storageClass string, lbls map[string]any) *unstructured.Unstructured {
	metadata := map[string]any{"name": name, "namespace": "default"}
	if lbls != nil {
		metadata["labels"] = lbls
	}
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "PersistentVolumeClaim",
		"metadata":   metadata,
		"spec": map[string]any{
			"accessModes":      []any{"ReadWriteOnce"},
			"storageClassName": storageClass,
			"resources":        map[string]any{"requests": map[string]any{"storage": "1Gi"}},
		},
	}}
}

// desiredImmutableConfigMap is a ConfigMap whose data the API server refuses
// to change.
func desiredImmutableConfigMap(name, value string) *unstructured.Unstructured {
	cm := newUnstructuredConfigMap(name, map[string]string{"key": value})
	_ = unstructured.SetNestedField(cm.Object, true, "immutable")
	return cm
}

// applyClaim applies a claim of storage class "fast" for the first time and returns it as the
// cluster holds it. envtest runs no controller that clears the
// pvc-protection finalizer, so the helper strips it: a later delete then
// completes, as it does on a cluster once no pod mounts the claim. The
// cleanup removes whatever claim of that name is left.
func applyClaim(rm *fluxssa.ResourceManager, name string) *corev1.PersistentVolumeClaim {
	GinkgoHelper()
	_, err := apply.Apply(ctx, rm,
		[]*unstructured.Unstructured{desiredClaim(name, "fast", nil)}, apply.ApplyOptions{})
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(func() {
		var left corev1.PersistentVolumeClaim
		if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: "default", Name: name}, &left); err != nil {
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
			return
		}
		left.Finalizers = nil
		Expect(k8sClient.Update(ctx, &left)).To(Succeed())
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &left))).To(Succeed())
	})

	live := liveClaim(name)
	live.Finalizers = nil
	Expect(k8sClient.Update(ctx, live)).To(Succeed())
	return liveClaim(name)
}

func liveClaim(name string) *corev1.PersistentVolumeClaim {
	GinkgoHelper()
	var live corev1.PersistentVolumeClaim
	Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: "default", Name: name}, &live)).To(Succeed())
	return &live
}

func liveConfigMap(name string) *corev1.ConfigMap {
	GinkgoHelper()
	var live corev1.ConfigMap
	Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: "default", Name: name}, &live)).To(Succeed())
	return &live
}

func removeConfigMap(name string) {
	GinkgoHelper()
	Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, newUnstructuredConfigMap(name, nil)))).To(Succeed())
}

// expectClaimKept asserts that the claim is the object it was: same UID, same
// storage class, and no delete reached it.
func expectClaimKept(before *corev1.PersistentVolumeClaim) {
	GinkgoHelper()
	after := liveClaim(before.Name)
	Expect(after.UID).To(Equal(before.UID), "the claim must not be recreated")
	Expect(after.DeletionTimestamp.IsZero()).To(BeTrue(), "no delete may reach the claim")
	Expect(after.Spec.StorageClassName).To(Equal(before.Spec.StorageClassName))
}

var _ = Describe("Apply with force and PersistentVolumeClaims", func() {
	var rm *fluxssa.ResourceManager

	BeforeEach(func() {
		rm = apply.NewResourceManager(k8sClient, "test-owner")
	})

	It("keeps a claim with a changed immutable field and applies nothing", func() {
		before := applyClaim(rm, "force-keep-pvc")
		DeferCleanup(removeConfigMap, "force-keep-cm")
		_, err := apply.Apply(ctx, rm, []*unstructured.Unstructured{
			newUnstructuredConfigMap("force-keep-cm", map[string]string{"key": "old"}),
		}, apply.ApplyOptions{})
		Expect(err).NotTo(HaveOccurred())

		_, err = apply.Apply(ctx, rm, []*unstructured.Unstructured{
			newUnstructuredConfigMap("force-keep-cm", map[string]string{"key": "new"}),
			newUnstructuredConfigMap("force-keep-new-cm", map[string]string{"key": "new"}),
			desiredClaim("force-keep-pvc", "slow", nil),
		}, apply.ApplyOptions{Force: true})
		DeferCleanup(removeConfigMap, "force-keep-new-cm")

		conflict, ok := errors.AsType[*apply.ClaimConflictError](err)
		Expect(ok).To(BeTrue(), "want a ClaimConflictError, got %v", err)
		Expect(conflict.Namespace).To(Equal("default"))
		Expect(conflict.Name).To(Equal("force-keep-pvc"))
		Expect(conflict.Fields).To(ConsistOf("spec"))
		Expect(apierrors.IsInvalid(conflict.Cause)).To(BeTrue(), "the cause is the API server's refusal")

		expectClaimKept(before)
		Expect(liveConfigMap("force-keep-cm").Data).To(HaveKeyWithValue("key", "old"),
			"no other object of the set may be applied")
		err = k8sClient.Get(ctx, types.NamespacedName{Namespace: "default", Name: "force-keep-new-cm"}, &corev1.ConfigMap{})
		Expect(apierrors.IsNotFound(err)).To(BeTrue(), "no other object of the set may be created")
	})

	It("does not delete another refused object of the set before it stops on the claim", func() {
		before := applyClaim(rm, "force-order-pvc")
		DeferCleanup(removeConfigMap, "force-order-cm")
		_, err := apply.Apply(ctx, rm, []*unstructured.Unstructured{
			desiredImmutableConfigMap("force-order-cm", "old"),
		}, apply.ApplyOptions{})
		Expect(err).NotTo(HaveOccurred())
		cmBefore := liveConfigMap("force-order-cm")

		_, err = apply.Apply(ctx, rm, []*unstructured.Unstructured{
			desiredImmutableConfigMap("force-order-cm", "new"),
			desiredClaim("force-order-pvc", "slow", nil),
		}, apply.ApplyOptions{Force: true})
		_, ok := errors.AsType[*apply.ClaimConflictError](err)
		Expect(ok).To(BeTrue(), "want a ClaimConflictError, got %v", err)

		expectClaimKept(before)
		Expect(liveConfigMap("force-order-cm").UID).To(Equal(cmBefore.UID),
			"the immutable ConfigMap must not be deleted when the apply stops on the claim")
	})

	It("recreates the claim when DeleteData is set", func() {
		before := applyClaim(rm, "force-delete-pvc")

		result, err := apply.Apply(ctx, rm, []*unstructured.Unstructured{
			desiredClaim("force-delete-pvc", "slow", nil),
		}, apply.ApplyOptions{Force: true, DeleteData: true})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Created).To(Equal(1))

		after := liveClaim("force-delete-pvc")
		Expect(after.UID).NotTo(Equal(before.UID), "the claim must be a new object")
		Expect(after.Spec.StorageClassName).To(HaveValue(Equal("slow")))
	})

	It("recreates another kind as before", func() {
		DeferCleanup(removeConfigMap, "force-other-cm")
		_, err := apply.Apply(ctx, rm, []*unstructured.Unstructured{
			desiredImmutableConfigMap("force-other-cm", "old"),
		}, apply.ApplyOptions{})
		Expect(err).NotTo(HaveOccurred())
		before := liveConfigMap("force-other-cm")

		_, err = apply.Apply(ctx, rm, []*unstructured.Unstructured{
			desiredImmutableConfigMap("force-other-cm", "new"),
		}, apply.ApplyOptions{Force: true})
		Expect(err).NotTo(HaveOccurred())

		after := liveConfigMap("force-other-cm")
		Expect(after.UID).NotTo(Equal(before.UID), "the ConfigMap must be a new object")
		Expect(after.Data).To(HaveKeyWithValue("key", "new"))
	})

	It("applies a claim change that needs no recreate", func() {
		before := applyClaim(rm, "force-label-pvc")

		result, err := apply.Apply(ctx, rm, []*unstructured.Unstructured{
			desiredClaim("force-label-pvc", "fast", map[string]any{"tier": "data"}),
		}, apply.ApplyOptions{Force: true})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Updated).To(Equal(1))

		after := liveClaim("force-label-pvc")
		Expect(after.UID).To(Equal(before.UID))
		Expect(after.Labels).To(HaveKeyWithValue("tier", "data"))
	})

	It("fails on a refused claim without force, as before, and keeps it", func() {
		before := applyClaim(rm, "noforce-pvc")

		_, err := apply.Apply(ctx, rm, []*unstructured.Unstructured{
			desiredClaim("noforce-pvc", "slow", nil),
		}, apply.ApplyOptions{})
		Expect(err).To(HaveOccurred())
		_, ok := errors.AsType[*apply.ClaimConflictError](err)
		Expect(ok).To(BeFalse(), "an apply without force reports the refusal as it did")
		expectClaimKept(before)
	})

	// The check and the staged apply are two requests. This spec hides the
	// claim from the check, as if it appeared just after, so the staged
	// apply meets the refusal itself and reaches for the delete.
	It("refuses the delete itself when the claim appears after the check", func() {
		before := applyClaim(rm, "force-race-pvc")

		hidden := false
		racing, err := client.NewWithWatch(cfg, client.Options{Scheme: k8sClient.Scheme()})
		Expect(err).NotTo(HaveOccurred())
		raceRM := apply.NewResourceManager(interceptor.NewClient(racing, interceptor.Funcs{
			Get: func(
				ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption,
			) error {
				if !hidden && key.Name == "force-race-pvc" {
					hidden = true
					return apierrors.NewNotFound(corev1.Resource("persistentvolumeclaims"), key.Name)
				}
				return c.Get(ctx, key, obj, opts...)
			},
		}), "opm-controller")

		_, err = apply.Apply(ctx, raceRM, []*unstructured.Unstructured{
			desiredClaim("force-race-pvc", "slow", nil),
		}, apply.ApplyOptions{Force: true})
		Expect(hidden).To(BeTrue(), "the check must have read the claim")
		conflict, ok := errors.AsType[*apply.ClaimConflictError](err)
		Expect(ok).To(BeTrue(), "want a ClaimConflictError from the delete guard, got %v", err)
		Expect(conflict.Name).To(Equal("force-race-pvc"))
		expectClaimKept(before)
	})

	It("refuses a claim delete through the resource manager's client", func() {
		before := applyClaim(rm, "guard-pvc")

		err := rm.Client().Delete(ctx, desiredClaim("guard-pvc", "fast", nil))
		_, ok := errors.AsType[*apply.ClaimConflictError](err)
		Expect(ok).To(BeTrue(), "want a ClaimConflictError, got %v", err)
		expectClaimKept(before)
	})
})
