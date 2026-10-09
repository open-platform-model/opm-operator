package apply

import (
	"context"
	"errors"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/open-platform-model/library/opm/k8s/ownership"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
)

// GuardInput is what one reconcile hands to the apply guard.
type GuardInput struct {
	// Resources is the apply list: the rendered set without withheld objects.
	Resources []*unstructured.Unstructured

	// Inventory is status.inventory as read at the start of the reconcile.
	Inventory []releasesv1alpha1.InventoryEntry

	// Identity is the instance's identity: the render's, or the recorded one
	// when the render carries none. Never the earlier identity of an
	// unsettled change, which another record can render, and never empty:
	// Guard returns ErrNoIdentity for an empty identity.
	Identity string
}

// Judged is one object the apply verdict did not allow, with the library's
// reason and message.
type Judged struct {
	Object  *unstructured.Unstructured
	Refuse  ownership.ApplyRefusal
	Message string

	// InInventory is what the guard handed to the verdict.
	InInventory bool
}

// GuardResult sorts an apply list by the apply verdict.
type GuardResult struct {
	// Allowed are the objects the verdict lets the instance apply, in the
	// order of GuardInput.Resources.
	Allowed []*unstructured.Unstructured

	// TakenIn are the allowed objects that exist and are not in the
	// inventory: applying one brings it under the instance.
	TakenIn []*unstructured.Unstructured

	// Pins names each object of TakenIn with the UID the guard read, in the
	// order of TakenIn. An apply that is handed them (ApplyOptions.TakenIn)
	// writes that object and no other of the same name.
	Pins []Pin

	// LetGo are the objects whose adopt annotation names another instance.
	// They are not applied and the reconcile goes on.
	LetGo []Judged

	// Refused are the objects that are being deleted, are not managed by
	// OPM, or belong to another instance. A refused object exists.
	Refused []Judged
}

// ErrNoIdentity is the guard's refusal to judge without an instance identity.
// The library answers an empty identity with "apply" for an inventoried
// object whose adopt annotation names another instance, so asking without
// one could write over an object that must be let go.
var ErrNoIdentity = errors.New("the apply verdict cannot be asked without an instance identity")

// GuardReadError is a read of a live object that failed for another reason
// than that the object does not exist. No verdict was reached for any object.
type GuardReadError struct {
	Object ownership.Object
	Err    error
}

// Error names the object and, when the API server gave one, the reason of
// the refusal (Forbidden): the text of a refused discovery request does not
// say it.
func (e *GuardReadError) Error() string {
	if reason := apierrors.ReasonForError(e.Err); reason != metav1.StatusReasonUnknown {
		return fmt.Sprintf("reading %s before the apply (%s): %v", e.Object, reason, e.Err)
	}
	return fmt.Sprintf("reading %s before the apply: %v", e.Object, e.Err)
}

func (e *GuardReadError) Unwrap() error { return e.Err }

// Guard reads every object of in.Resources through c and asks the library's
// apply verdict (opm/k8s/ownership) for it, once, with in.Identity. It writes
// nothing and decides ownership with no comparison of its own. Admit is never
// set: it is for the operator install only.
//
// c must read live, as the identity that applies: a refusal names the owner
// of an object, and a cached read could miss a new adopt annotation.
//
// An object that does not exist is allowed. So is one whose kind the API
// server does not serve yet while a CustomResourceDefinition of in.Resources
// defines it: such an object cannot exist. Every other failed read returns a
// *GuardReadError and no result.
func Guard(ctx context.Context, c client.Reader, in GuardInput) (*GuardResult, error) {
	if in.Identity == "" {
		return nil, ErrNoIdentity
	}

	type key struct{ group, kind, namespace, name string }
	inventoried := make(map[key]struct{}, len(in.Inventory))
	for _, e := range in.Inventory {
		inventoried[key{e.Group, e.Kind, e.Namespace, e.Name}] = struct{}{}
	}

	result := &GuardResult{}
	for _, resource := range in.Resources {
		gvk := resource.GroupVersionKind()
		obj := ownership.Object{
			Group: gvk.Group, Kind: gvk.Kind,
			Namespace: resource.GetNamespace(), Name: resource.GetName(),
		}

		live := &unstructured.Unstructured{}
		live.SetGroupVersionKind(gvk)
		switch err := c.Get(ctx, client.ObjectKeyFromObject(resource), live); {
		case err == nil:
		case apierrors.IsNotFound(err), pendingCRDKind(err, in.Resources):
			live = nil
		default:
			return nil, &GuardReadError{Object: obj, Err: err}
		}

		_, inInventory := inventoried[key{obj.Group, obj.Kind, obj.Namespace, obj.Name}]
		verdict := ownership.CanApply(ownership.ApplyInput{
			Object:       obj,
			Live:         live,
			InInventory:  inInventory,
			InstanceUUID: in.Identity,
		})
		switch {
		case verdict.Allowed():
			result.Allowed = append(result.Allowed, resource)
			if live != nil && !inInventory {
				result.TakenIn = append(result.TakenIn, resource)
				result.Pins = append(result.Pins, Pin{Object: obj, UID: live.GetUID()})
			}
		case verdict.Refuse == ownership.RefuseAdoptedElsewhere:
			result.LetGo = append(result.LetGo, judged(resource, verdict, inInventory))
		default:
			result.Refused = append(result.Refused, judged(resource, verdict, inInventory))
		}
	}
	return result, nil
}

func judged(resource *unstructured.Unstructured, verdict ownership.ApplyVerdict, inInventory bool) Judged {
	return Judged{Object: resource, Refuse: verdict.Refuse, Message: verdict.Message, InInventory: inInventory}
}
