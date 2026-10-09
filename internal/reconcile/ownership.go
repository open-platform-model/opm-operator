package reconcile

import (
	"context"
	"errors"

	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/fluxcd/pkg/runtime/conditions"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	"github.com/open-platform-model/opm-operator/internal/apply"
	"github.com/open-platform-model/opm-operator/internal/status"
)

// readySucceeded is the message of Ready=True after a reconcile that ended
// well. status.ReadyMessage adds the count of let-go objects to it.
const readySucceeded = "Reconciliation succeeded"

// guardedApply is what the apply guard means for one reconcile that renders.
type guardedApply struct {
	// err is the guard's failure: a read that failed, or no identity to ask
	// with. No verdict was reached, so nothing may be written.
	err error

	// allowed is the apply list from the guard on: what the verdict allows.
	// It is the list as handed in when the guard did not run or failed.
	allowed []*unstructured.Unstructured

	// takenIn are the allowed objects that exist outside the inventory, and
	// pins names them with the UID that was read.
	takenIn []*unstructured.Unstructured
	pins    []apply.Pin

	// letGo are the objects another instance adopted; refused are the ones
	// that refuse a reconcile that would write them.
	letGo, refused []apply.Judged

	// adopted is the number the Ready message states: len(letGo), or the
	// number stated before when the guard reached no verdict.
	adopted int
}

// guardApply runs the apply guard over applyList (0012:D8:R1): one live read
// per object through reader, the reader of the identity that applies, and the
// library's apply verdict with identity. identity is the instance's own (the
// render's, or the recorded one): never the earlier identity of an unsettled
// change, which another record can render, and never the list a prune judges
// with.
//
// identityErr is the error of building the client that applies. When it is
// set there is no reader and no guard runs: the caller reports the error and
// writes nothing.
func guardApply(
	ctx context.Context,
	identityErr error,
	reader client.Reader,
	applyList []*unstructured.Unstructured,
	inventory []releasesv1alpha1.InventoryEntry,
	identity string,
	adoptedAtStart int,
) guardedApply {
	unjudged := guardedApply{allowed: applyList, adopted: adoptedAtStart}
	if identityErr != nil {
		return unjudged
	}
	result, err := apply.Guard(ctx, reader, apply.GuardInput{
		Resources: applyList, Inventory: inventory, Identity: identity,
	})
	if err != nil {
		unjudged.err = err
		return unjudged
	}
	if len(result.LetGo) > 0 {
		logf.FromContext(ctx).Info("Rendered objects are adopted by another instance and are not applied",
			"objects", len(result.LetGo), "messages", judgedMessages(result.LetGo))
	}
	return guardedApply{
		allowed: result.Allowed,
		takenIn: result.TakenIn,
		pins:    result.Pins,
		letGo:   result.LetGo,
		refused: result.Refused,
		adopted: len(result.LetGo),
	}
}

// failsApply reports whether the guard's failure fails the reconcile as a
// failed apply: with changed digests the reconcile would write, and with no
// identity nothing can be judged at all. A failed read on unchanged digests
// does not: the reconcile writes nothing and reports a failed drift check.
func (g guardedApply) failsApply(digestsUnchanged bool) bool {
	return g.err != nil && (!digestsUnchanged || errors.Is(g.err, apply.ErrNoIdentity))
}

// changesInventory reports whether the verdict changes what the instance
// holds: an object to take in, or an inventoried object to let go.
func (g guardedApply) changesInventory() bool {
	if len(g.takenIn) > 0 {
		return true
	}
	for _, j := range g.letGo {
		if j.InInventory {
			return true
		}
	}
	return false
}

// refusedToWrite returns the refusals of the apply verdict that refuse this
// reconcile. With changed digests the reconcile would write every rendered
// object, so each refusal counts. With unchanged digests it writes nothing
// over an object of the inventory, so only an object that exists outside the
// inventory counts: one the instance renders and does not hold.
func refusedToWrite(refused []apply.Judged, digestsUnchanged bool) []apply.Judged {
	if !digestsUnchanged {
		return refused
	}
	var out []apply.Judged
	for _, j := range refused {
		if !j.InInventory {
			out = append(out, j)
		}
	}
	return out
}

// judgedMessages returns the library's message of each judged object.
func judgedMessages(judged []apply.Judged) []string {
	out := make([]string, 0, len(judged))
	for _, j := range judged {
		out = append(out, j.Message)
	}
	return out
}

// judgedObjects returns the rendered object of each judged one.
func judgedObjects(judged []apply.Judged) []*unstructured.Unstructured {
	out := make([]*unstructured.Unstructured, 0, len(judged))
	for _, j := range judged {
		out = append(out, j.Object)
	}
	return out
}

// withoutObjects returns list without the objects of drop, in order. Both
// hold objects of one render, so the pointers are compared.
func withoutObjects(list, drop []*unstructured.Unstructured) []*unstructured.Unstructured {
	if len(drop) == 0 {
		return list
	}
	dropped := make(map[*unstructured.Unstructured]struct{}, len(drop))
	for _, obj := range drop {
		dropped[obj] = struct{}{}
	}
	out := make([]*unstructured.Unstructured, 0, len(list))
	for _, obj := range list {
		if _, ok := dropped[obj]; !ok {
			out = append(out, obj)
		}
	}
	return out
}

// ownedObject is a ModuleInstance or a ModulePackage: what the ownership
// reports of a reconcile are written to.
type ownedObject interface {
	conditions.Setter
	runtime.Object
}

// refuseApply records a reconcile the apply verdict refused: Ready=False with
// reason ApplyRefused, not Stalled, and one Warning event. It returns the
// message, which carries the library's message of each refused object.
func refuseApply(ctx context.Context, recorder events.EventRecorder, obj ownedObject, refusing []apply.Judged) string {
	msg := status.ApplyRefusedNote(judgedMessages(refusing))
	logf.FromContext(ctx).Info("The apply verdict refused the reconcile", "objects", len(refusing))
	recorder.Eventf(obj, nil, corev1.EventTypeWarning, status.ApplyRefusedReason, "Apply", "%s", msg)
	status.MarkNotReady(obj, status.ApplyRefusedReason, "%s", msg)
	return msg
}

// adoptedBefore returns the number of let-go objects the Ready condition
// stated when the reconcile started. It is zero unless Ready was True: a
// refused or failed reconcile replaced the message, so the success after it
// reports the let-go objects once more.
func adoptedBefore(conds []metav1.Condition) int {
	ready := apimeta.FindStatusCondition(conds, status.ReadyCondition)
	if ready == nil || ready.Status != metav1.ConditionTrue {
		return 0
	}
	return status.AdoptedElsewhereCount(ready.Message)
}

// reportAdoptedElsewhere emits the one AdoptedElsewhere event of a reconcile
// that ends Ready=True, when it let objects go and their number differs from
// the one the Ready message stated before. The Ready message carries the
// state between two events.
func reportAdoptedElsewhere(recorder events.EventRecorder, obj runtime.Object, letGo []apply.Judged, before int) {
	if len(letGo) == 0 || len(letGo) == before {
		return
	}
	recorder.Eventf(obj, nil, corev1.EventTypeWarning, status.AdoptedElsewhereReason, "Apply",
		"%s", status.AdoptedElsewhereNote(judgedMessages(letGo)))
}
