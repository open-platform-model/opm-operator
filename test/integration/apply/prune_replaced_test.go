package apply_test

import (
	"context"
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	"github.com/open-platform-model/opm-operator/internal/apply"
)

// replaceBeforeDelete returns a client that deletes the named ConfigMap and
// creates it again, through the suite's client, just before it forwards the
// first DELETE of that name: the object the caller read is gone and another
// one holds its name.
func replaceBeforeDelete(name string) client.Client {
	realClient, err := client.NewWithWatch(cfg, client.Options{})
	Expect(err).NotTo(HaveOccurred())
	replaced := false
	return interceptor.NewClient(realClient, interceptor.Funcs{
		Delete: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
			if obj.GetName() == name && !replaced {
				replaced = true
				old := &corev1.ConfigMap{}
				Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(obj), old)).To(Succeed())
				Expect(k8sClient.Delete(ctx, old)).To(Succeed())
				Expect(k8sClient.Create(ctx, &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: obj.GetNamespace(), Labels: ownedLabels()},
					Data:       map[string]string{"generation": "second"},
				})).To(Succeed())
			}
			return c.Delete(ctx, obj, opts...)
		},
	})
}

var _ = Describe("Prune of an object replaced since the read", func() {
	It("leaves the new object, fails the entry with ErrReplaced and still prunes the rest", func() {
		for _, name := range []string{"prune-replaced-cm", "prune-replaced-other"} {
			Expect(k8sClient.Create(ctx, &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default", Labels: ownedLabels()},
				Data:       map[string]string{"generation": "first"},
			})).To(Succeed())
		}
		DeferCleanup(func() {
			_ = k8sClient.Delete(ctx, &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "prune-replaced-cm", Namespace: "default"},
			})
		})
		first := &corev1.ConfigMap{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: "default", Name: "prune-replaced-cm"}, first)).To(Succeed())

		stale := []releasesv1alpha1.InventoryEntry{
			{Kind: "ConfigMap", Version: "v1", Namespace: "default", Name: "prune-replaced-cm"},
			{Kind: "ConfigMap", Version: "v1", Namespace: "default", Name: "prune-replaced-other"},
		}
		result, err := apply.Prune(ctx, replaceBeforeDelete("prune-replaced-cm"),
			[]string{testOwnerUUID}, stale, apply.PruneOptions{})

		Expect(err).To(HaveOccurred())
		Expect(errors.Is(err, apply.ErrReplaced)).To(BeTrue(), "error: %v", err)
		Expect(err.Error()).To(ContainSubstring("prune-replaced-cm"))
		Expect(result.Deleted).To(Equal(1), "the other entry is still pruned and the replaced one is not counted")

		second := &corev1.ConfigMap{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: "default", Name: "prune-replaced-cm"}, second)).To(Succeed())
		Expect(second.UID).NotTo(Equal(first.UID))
		Expect(second.DeletionTimestamp).To(BeNil())
		Expect(second.Data).To(HaveKeyWithValue("generation", "second"))
	})
})
