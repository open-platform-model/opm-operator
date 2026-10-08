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
	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
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

			err := claimGuard{inner}.Delete(ctx, tt.obj)
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

func TestClaimGuardDeleteAllOf(t *testing.T) {
	deletes := 0
	guard := claimGuard{countingDeletes{Client: fake.NewClientBuilder().Build(), deletes: &deletes}}

	err := guard.DeleteAllOf(context.Background(), &corev1.PersistentVolumeClaim{}, client.InNamespace("default"))
	if _, ok := errors.AsType[*ClaimConflictError](err); !ok {
		t.Fatalf("DeleteAllOf of claims = %v, want a ClaimConflictError", err)
	}
	if deletes != 0 {
		t.Fatalf("a refused DeleteAllOf reached the API %d time(s)", deletes)
	}
	if err := guard.DeleteAllOf(allowClaimDeletion(context.Background()),
		&corev1.PersistentVolumeClaim{}, client.InNamespace("default")); err != nil {
		t.Fatalf("DeleteAllOf under a context that allows it: %v", err)
	}
	if err := guard.DeleteAllOf(context.Background(), &corev1.ConfigMap{}, client.InNamespace("default")); err != nil {
		t.Fatalf("DeleteAllOf of another kind: %v", err)
	}
	if deletes != 2 {
		t.Fatalf("the allowed calls reached the API %d time(s), want 2", deletes)
	}
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
