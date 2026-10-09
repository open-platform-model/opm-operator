package apply

import (
	"context"
	"fmt"
	"strings"

	ssaerrors "github.com/fluxcd/pkg/ssa/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/open-platform-model/library/opm/k8s/ownership"
)

// Pin names a live object the apply guard allowed and that is not in the
// inventory, with the UID the guard read: the object an apply takes in.
type Pin struct {
	Object ownership.Object
	UID    types.UID
}

// TakenInConflictError says that an apply left an object it takes in as it
// is where a forced recreate would have deleted it. A user hands an object
// over with the adopt annotation so that it need not be deleted, so OPM never
// deletes and creates it again.
type TakenInConflictError struct {
	Object ownership.Object

	// Fields are the fields whose update the API server refused, as it
	// reports them. Empty when it named none.
	Fields []string

	// Cause is the API server's refusal. Nil when the object was kept by the
	// resource manager's delete guard, which sees no refusal.
	Cause error
}

func (e *TakenInConflictError) Error() string {
	const rule = "OPM does not delete and create again an object it is taking in"
	if e.Cause == nil {
		return fmt.Sprintf("%s was not deleted: %s", e.Object, rule)
	}
	fields := ""
	if len(e.Fields) > 0 {
		fields = " of " + strings.Join(e.Fields, ", ")
	}
	return fmt.Sprintf("%s exists and the API server refuses the update%s: %s. "+
		"Change the object so that the update is accepted, or delete it: %v", e.Object, fields, rule, e.Cause)
}

func (e *TakenInConflictError) Unwrap() error { return e.Cause }

func objectOf(obj client.Object) ownership.Object {
	gvk := obj.GetObjectKind().GroupVersionKind()
	return ownership.Object{Group: gvk.Group, Kind: gvk.Kind, Namespace: obj.GetNamespace(), Name: obj.GetName()}
}

// pinTakenIn returns resources with the UID of its pin set on a copy of each
// taken-in object. The API server refuses a write that names another UID
// than the live object's, and a create that names one, so the apply reaches
// the object the guard judged or nothing. An object without a pin, and a pin
// without a UID, are left as they are.
func pinTakenIn(resources []*unstructured.Unstructured, pins []Pin) []*unstructured.Unstructured {
	uids := make(map[ownership.Object]types.UID, len(pins))
	for _, pin := range pins {
		uids[pin.Object] = pin.UID
	}
	out := make([]*unstructured.Unstructured, 0, len(resources))
	for _, resource := range resources {
		uid, taken := uids[objectOf(resource)]
		if !taken || uid == "" {
			out = append(out, resource)
			continue
		}
		pinned := resource.DeepCopy()
		pinned.SetUID(uid)
		out = append(out, pinned)
	}
	return out
}

// checkTakenIn finds the first taken-in object of resources that a forced
// apply would delete and recreate, and returns a *TakenInConflictError for
// it. It sends the dry-run the staged apply itself makes and judges the
// answer as the staged apply does (checkClaims), so every Invalid or Conflict
// answer counts: the safe side. It changes nothing.
func checkTakenIn(ctx context.Context, c client.Client, resources []*unstructured.Unstructured, pins []Pin) error {
	taken := make(map[ownership.Object]struct{}, len(pins))
	for _, pin := range pins {
		taken[pin.Object] = struct{}{}
	}
	for _, resource := range resources {
		obj := objectOf(resource)
		if _, ok := taken[obj]; !ok {
			continue
		}
		err := c.Apply(ctx, client.ApplyConfigurationFromUnstructured(resource.DeepCopy()),
			client.DryRunAll, client.ForceOwnership, client.FieldOwner(FieldManager))
		if err != nil && ssaerrors.IsImmutableError(err) {
			return &TakenInConflictError{Object: obj, Fields: refusedFields(err), Cause: err}
		}
	}
	return nil
}

// takenInKey marks a context with the objects the apply takes in.
type takenInKey struct{}

// keepTakenIn returns a context under which the resource manager's client
// refuses to delete the objects of pins. Only Apply calls it.
func keepTakenIn(ctx context.Context, pins []Pin) context.Context {
	kept := make(map[ownership.Object]Pin, len(pins))
	for _, pin := range pins {
		kept[pin.Object] = pin
	}
	return context.WithValue(ctx, takenInKey{}, kept)
}

// takenInPin reports whether the apply of ctx takes obj in. The match is by
// group, kind, namespace and name, not by UID: an object created under the
// name since the guard's read is not the instance's to delete either.
func takenInPin(ctx context.Context, obj client.Object) (Pin, bool) {
	kept, _ := ctx.Value(takenInKey{}).(map[ownership.Object]Pin)
	pin, ok := kept[objectOf(obj)]
	return pin, ok
}
