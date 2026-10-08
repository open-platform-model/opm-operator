package apply

import (
	"context"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func unstructuredOf(group, kind, name string) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{Group: group, Version: "v1", Kind: kind})
	obj.SetNamespace("default")
	obj.SetName(name)
	return obj
}

// The resource manager's client deletes a PersistentVolumeClaim only under a
// context that Apply marked, and every other kind always.
func TestClaimGuardDelete(t *testing.T) {
	typedClaim := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "typed", Namespace: "default"}}
	tests := []struct {
		name    string
		obj     client.Object
		allowed bool
		refused bool
	}{
		{"unstructured claim", unstructuredOf("", "PersistentVolumeClaim", "data"), false, true},
		{"typed claim", typedClaim, false, true},
		{"claim under a context that allows it", unstructuredOf("", "PersistentVolumeClaim", "data"), true, false},
		{"same kind in another group", unstructuredOf("example.com", "PersistentVolumeClaim", "data"), false, false},
		{"config map", unstructuredOf("", "ConfigMap", "settings"), false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deletes := 0
			inner := countingDeletes{Client: fake.NewClientBuilder().Build(), deletes: &deletes}
			ctx := context.Background()
			if tt.allowed {
				ctx = allowClaimDeletion(ctx)
			}

			err := deleteGuard{inner}.Delete(ctx, tt.obj)
			conflict, isConflict := errors.AsType[*ClaimConflictError](err)
			if isConflict != tt.refused {
				t.Fatalf("Delete error = %v, refused = %v, want refused = %v", err, isConflict, tt.refused)
			}
			if tt.refused {
				if deletes != 0 {
					t.Fatalf("a refused delete reached the API %d time(s)", deletes)
				}
				if conflict.Namespace != "default" || conflict.Name != tt.obj.GetName() {
					t.Fatalf("the error names %s/%s, want default/%s", conflict.Namespace, conflict.Name, tt.obj.GetName())
				}
				return
			}
			if deletes != 1 {
				t.Fatalf("the delete reached the API %d time(s), want 1", deletes)
			}
		})
	}
}

// A delete of a collection cannot name the objects that were read, so the
// resource manager's client refuses it for every kind, also under a context
// that allows the deletion of claims.
func TestDeleteGuardRefusesDeleteAllOf(t *testing.T) {
	deletes := 0
	guard := deleteGuard{countingDeletes{Client: fake.NewClientBuilder().Build(), deletes: &deletes}}

	for _, tt := range []struct {
		name string
		ctx  context.Context
		obj  client.Object
	}{
		{"claims", context.Background(), &corev1.PersistentVolumeClaim{}},
		{"claims under a context that allows their deletion", allowClaimDeletion(context.Background()), &corev1.PersistentVolumeClaim{}},
		{"config maps", context.Background(), &corev1.ConfigMap{}},
		{"an unstructured kind", context.Background(), unstructuredOf("example.com", "Widget", "")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := guard.DeleteAllOf(tt.ctx, tt.obj, client.InNamespace("default"))
			if !errors.Is(err, ErrCollectionDelete) {
				t.Fatalf("DeleteAllOf = %v, want ErrCollectionDelete", err)
			}
		})
	}
	if deletes != 0 {
		t.Fatalf("a refused DeleteAllOf reached the API %d time(s)", deletes)
	}
}

// The guard sends a delete with a precondition on the UID of the object it
// is handed, after the claim rule, and without one for an object that has no
// UID. A Conflict answer is the refusal on that precondition.
func TestDeleteGuardUIDPrecondition(t *testing.T) {
	withUID := func(obj *unstructured.Unstructured, uid string) *unstructured.Unstructured {
		obj.SetUID(types.UID(uid))
		return obj
	}
	tests := []struct {
		name    string
		obj     client.Object
		ctx     context.Context
		wantUID string
	}{
		{"config map", withUID(unstructuredOf("", "ConfigMap", "settings"), "u-1"), context.Background(), "u-1"},
		{"claim under a context that allows it", withUID(unstructuredOf("", "PersistentVolumeClaim", "data"), "u-2"),
			allowClaimDeletion(context.Background()), "u-2"},
		{"object without a UID", unstructuredOf("", "ConfigMap", "settings"), context.Background(), ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got *client.DeleteOptions
			inner := interceptor.NewClient(fake.NewClientBuilder().Build(), interceptor.Funcs{
				Delete: func(_ context.Context, _ client.WithWatch, _ client.Object, opts ...client.DeleteOption) error {
					got = (&client.DeleteOptions{}).ApplyOptions(opts)
					return nil
				},
			})
			// A caller's own precondition never replaces the guard's.
			other := types.UID("someone-else")
			if err := (deleteGuard{inner}).Delete(tt.ctx, tt.obj, client.Preconditions{UID: &other},
				client.PropagationPolicy(metav1.DeletePropagationBackground)); err != nil {
				t.Fatalf("Delete: %v", err)
			}
			if got == nil {
				t.Fatal("the delete did not reach the API")
			}
			if got.PropagationPolicy == nil || *got.PropagationPolicy != metav1.DeletePropagationBackground {
				t.Fatalf("the caller's propagation policy was lost: %+v", got)
			}
			if tt.wantUID == "" {
				if got.Preconditions == nil || got.Preconditions.UID == nil || *got.Preconditions.UID != other {
					t.Fatalf("preconditions = %+v, want the caller's options unchanged", got.Preconditions)
				}
				return
			}
			if got.Preconditions == nil || got.Preconditions.UID == nil || string(*got.Preconditions.UID) != tt.wantUID {
				t.Fatalf("preconditions = %+v, want UID %s", got.Preconditions, tt.wantUID)
			}
		})
	}

	t.Run("a Conflict answer is ErrReplaced", func(t *testing.T) {
		inner := interceptor.NewClient(fake.NewClientBuilder().Build(), interceptor.Funcs{
			Delete: func(context.Context, client.WithWatch, client.Object, ...client.DeleteOption) error {
				return apierrors.NewConflict(schema.GroupResource{Resource: "configmaps"}, "settings", errors.New("precondition failed"))
			},
		})
		err := deleteGuard{inner}.Delete(context.Background(), withUID(unstructuredOf("", "ConfigMap", "settings"), "u-1"))
		if !errors.Is(err, ErrReplaced) || !apierrors.IsConflict(err) {
			t.Fatalf("Delete = %v, want ErrReplaced with the API status", err)
		}
	})
}

// countingDeletes counts the delete calls that reach it and deletes nothing.
type countingDeletes struct {
	client.Client
	deletes *int
}

func (c countingDeletes) Delete(context.Context, client.Object, ...client.DeleteOption) error {
	*c.deletes++
	return nil
}

func (c countingDeletes) DeleteAllOf(context.Context, client.Object, ...client.DeleteAllOfOption) error {
	*c.deletes++
	return nil
}

func TestRefusedFields(t *testing.T) {
	invalid := apierrors.NewInvalid(schema.GroupKind{Kind: "PersistentVolumeClaim"}, "data", field.ErrorList{
		field.Forbidden(field.NewPath("spec"), "spec is immutable after creation"),
		field.Forbidden(field.NewPath("spec"), "again"),
		field.Invalid(field.NewPath("metadata", "name"), "x", "bad"),
	})
	tests := []struct {
		name string
		err  error
		want []string
	}{
		{"an invalid error names its fields once, in order", invalid, []string{"spec", "metadata.name"}},
		{"a wrapped invalid error", errors.Join(errors.New("context"), invalid), []string{"spec", "metadata.name"}},
		{"a conflict names no field", apierrors.NewConflict(schema.GroupResource{Resource: "persistentvolumeclaims"}, "data", errors.New("changed")), nil},
		{"a plain error", errors.New("field is immutable"), nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := refusedFields(tt.err)
			if len(got) != len(tt.want) {
				t.Fatalf("refusedFields = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("refusedFields = %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func TestClaimConflictErrorUnwrapsTheRefusal(t *testing.T) {
	cause := apierrors.NewInvalid(schema.GroupKind{Kind: "PersistentVolumeClaim"}, "data", nil)
	err := error(&ClaimConflictError{Namespace: "media", Name: "data", Cause: cause})
	if !apierrors.IsInvalid(err) {
		t.Fatalf("the conflict does not unwrap to the API server's refusal: %v", err)
	}
	if guard := (&ClaimConflictError{Namespace: "media", Name: "data"}); guard.Unwrap() != nil || guard.Error() == "" {
		t.Fatalf("a conflict without a cause must still describe itself: %q", guard.Error())
	}
}
