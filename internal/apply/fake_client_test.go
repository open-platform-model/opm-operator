package apply

import (
	"context"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// The fake client of this package's unit tests acts on neither option of a
// planned delete: it removes the object at once under Foreground propagation
// and under a UID precondition that does not match. A unit test can therefore
// assert only the options a delete was sent with (through an interceptor);
// that the API server refuses a replaced object, and that a Foreground delete
// leaves the object terminating, is proven against envtest
// (test/integration/apply). This test fails when the fake client learns
// either, so the unit tests that rely on it are looked at again.
func TestFakeClientIgnoresDeleteOptions(t *testing.T) {
	ctx := context.Background()
	key := client.ObjectKeyFromObject(own("cm", idA).object())

	t.Run("a UID precondition that does not match", func(t *testing.T) {
		c := fake.NewClientBuilder().WithObjects(own("cm", idA).object()).Build()
		other := types.UID("another-uid")
		if err := c.Delete(ctx, own("cm", idA).object(), client.Preconditions{UID: &other}); err != nil {
			t.Fatalf("Delete = %v, want the fake client to ignore the precondition", err)
		}
		if err := c.Get(ctx, key, own("cm", idA).object()); !apierrors.IsNotFound(err) {
			t.Fatalf("Get = %v, want NotFound: the fake client deleted the object", err)
		}
	})

	t.Run("Foreground propagation", func(t *testing.T) {
		c := fake.NewClientBuilder().WithObjects(own("cm", idA).object()).Build()
		foreground := client.PropagationPolicy(metav1.DeletePropagationForeground)
		if err := c.Delete(ctx, own("cm", idA).object(), foreground); err != nil {
			t.Fatalf("Delete = %v", err)
		}
		if err := c.Get(ctx, key, own("cm", idA).object()); !apierrors.IsNotFound(err) {
			t.Fatalf("Get = %v, want NotFound: the fake client keeps no terminating object", err)
		}
	})
}
