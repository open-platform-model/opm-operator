package apply

import (
	"context"
	"errors"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/open-platform-model/library/opm/k8s/ownership"
)

func takeInObject(kind, name string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetAPIVersion("v1")
	u.SetKind(kind)
	u.SetNamespace("team-a")
	u.SetName(name)
	return u
}

// A taken-in object is sent with the UID the guard read, on a copy; every
// other object, and a pin without a UID, is sent as rendered.
func TestPinTakenInSetsTheUIDThatWasRead(t *testing.T) {
	taken, other, blank := takeInObject("ConfigMap", "taken"), takeInObject("ConfigMap", "other"), takeInObject("Secret", "taken")
	pins := []Pin{
		{Object: ownership.Object{Kind: "ConfigMap", Namespace: "team-a", Name: "taken"}, UID: types.UID("uid-1")},
		{Object: ownership.Object{Kind: "Secret", Namespace: "team-a", Name: "taken"}},
	}

	out := pinTakenIn([]*unstructured.Unstructured{taken, other, blank}, pins)

	if len(out) != 3 {
		t.Fatalf("pinTakenIn returned %d objects, want 3", len(out))
	}
	if got := out[0].GetUID(); got != "uid-1" {
		t.Errorf("the taken-in object carries UID %q, want uid-1", got)
	}
	if taken.GetUID() != "" {
		t.Errorf("the rendered object was changed: UID %q", taken.GetUID())
	}
	if out[1] != other || out[2] != blank {
		t.Errorf("an object without a pinned UID was copied or changed")
	}
}

// The second lock: the resource manager's client refuses to delete an object
// the apply takes in, whatever its UID, and deletes every other object.
func TestDeleteGuardKeepsATakenInObject(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	taken := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "taken", UID: "uid-new"}}
	other := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "other", UID: "uid-other"}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(taken, other).Build()
	guard := deleteGuard{c}
	ctx := keepTakenIn(context.Background(), []Pin{{
		Object: ownership.Object{Kind: "ConfigMap", Namespace: "team-a", Name: "taken"}, UID: "uid-read",
	}})

	live := takeInObject("ConfigMap", "taken")
	live.SetUID("uid-new")
	err := guard.Delete(ctx, live)
	conflict, ok := errors.AsType[*TakenInConflictError](err)
	if !ok {
		t.Fatalf("Delete of a taken-in object = %v, want a *TakenInConflictError", err)
	}
	if !strings.Contains(conflict.Error(), "ConfigMap/team-a/taken was not deleted") {
		t.Errorf("the refusal does not name the object: %q", conflict.Error())
	}
	if err := c.Get(ctx, client.ObjectKeyFromObject(taken), &corev1.ConfigMap{}); err != nil {
		t.Fatalf("the taken-in object is gone: %v", err)
	}

	rest := takeInObject("ConfigMap", "other")
	rest.SetUID("uid-other")
	if err := guard.Delete(ctx, rest); err != nil {
		t.Fatalf("Delete of another object = %v, want nil", err)
	}
	if err := guard.Delete(context.Background(), live); err != nil {
		t.Fatalf("Delete outside an apply that takes the object in = %v, want nil", err)
	}
}

// The message names the object, the refused fields and the way out.
func TestTakenInConflictErrorNamesTheObjectAndTheFields(t *testing.T) {
	err := &TakenInConflictError{
		Object: ownership.Object{Kind: "Service", Namespace: "team-a", Name: "web"},
		Fields: []string{"spec.clusterIP"},
		Cause:  errors.New("field is immutable"),
	}
	for _, want := range []string{"Service/team-a/web", "spec.clusterIP", "field is immutable", "delete it"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("message %q does not contain %q", err.Error(), want)
		}
	}
}
