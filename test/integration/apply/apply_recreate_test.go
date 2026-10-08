package apply_test

import (
	"context"
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/open-platform-model/opm-operator/internal/apply"
)

var _ = Describe("Deletes of the resource manager", func() {
	It("recreates exactly the object that was read: the DELETE names its UID", func() {
		var sent []types.UID
		realClient, err := client.NewWithWatch(cfg, client.Options{})
		Expect(err).NotTo(HaveOccurred())
		recording := interceptor.NewClient(realClient, interceptor.Funcs{
			Delete: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
				do := (&client.DeleteOptions{}).ApplyOptions(opts)
				if do.Preconditions != nil && do.Preconditions.UID != nil {
					sent = append(sent, *do.Preconditions.UID)
				} else {
					sent = append(sent, "")
				}
				return c.Delete(ctx, obj, opts...)
			},
		})
		rm := apply.NewResourceManager(recording, "opm-controller")
		DeferCleanup(removeConfigMap, "recreate-uid-cm")

		_, err = apply.Apply(ctx, rm, []*unstructured.Unstructured{
			desiredImmutableConfigMap("recreate-uid-cm", "old"),
		}, apply.ApplyOptions{})
		Expect(err).NotTo(HaveOccurred())
		before := liveConfigMap("recreate-uid-cm")

		_, err = apply.Apply(ctx, rm, []*unstructured.Unstructured{
			desiredImmutableConfigMap("recreate-uid-cm", "new"),
		}, apply.ApplyOptions{Force: true})
		Expect(err).NotTo(HaveOccurred())

		Expect(sent).To(Equal([]types.UID{before.UID}), "one DELETE, with the UID of the object that was read")
		after := liveConfigMap("recreate-uid-cm")
		Expect(after.UID).NotTo(Equal(before.UID))
		Expect(after.Data).To(HaveKeyWithValue("key", "new"))
	})

	It("does not delete an object that was replaced since it was read", func() {
		rm := apply.NewResourceManager(k8sClient, "opm-controller")
		DeferCleanup(removeConfigMap, "recreate-replaced-cm")
		create := func(value string) *corev1.ConfigMap {
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "recreate-replaced-cm", Namespace: "default"},
				Data:       map[string]string{"key": value},
			}
			Expect(k8sClient.Create(ctx, cm)).To(Succeed())
			return cm
		}
		read := create("first")
		Expect(k8sClient.Delete(ctx, read.DeepCopy())).To(Succeed())
		second := create("second")
		Expect(second.UID).NotTo(Equal(read.UID))

		err := rm.Client().Delete(ctx, read)

		Expect(errors.Is(err, apply.ErrReplaced)).To(BeTrue(), "error: %v", err)
		Expect(err.Error()).To(ContainSubstring("replaced since it was read"))
		live := liveConfigMap("recreate-replaced-cm")
		Expect(live.UID).To(Equal(second.UID))
		Expect(live.DeletionTimestamp).To(BeNil())
	})

	It("refuses a delete of a collection", func() {
		rm := apply.NewResourceManager(k8sClient, "opm-controller")
		DeferCleanup(removeConfigMap, "recreate-collection-cm")
		Expect(k8sClient.Create(ctx, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name: "recreate-collection-cm", Namespace: "default", Labels: map[string]string{"collection": "yes"},
			},
		})).To(Succeed())

		err := rm.Client().DeleteAllOf(ctx, &corev1.ConfigMap{},
			client.InNamespace("default"), client.MatchingLabels{"collection": "yes"})

		Expect(errors.Is(err, apply.ErrCollectionDelete)).To(BeTrue(), "error: %v", err)
		Expect(liveConfigMap("recreate-collection-cm").DeletionTimestamp).To(BeNil())
	})
})
