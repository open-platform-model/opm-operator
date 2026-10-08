package apply

import (
	"context"
	"errors"
	"fmt"
	"strings"

	ssaerrors "github.com/fluxcd/pkg/ssa/errors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const claimKind = "PersistentVolumeClaim"

// ClaimConflictError says that an apply left a PersistentVolumeClaim as it is
// where a forced recreate would have deleted it: the API server refuses the
// update, and the apply may not delete data.
type ClaimConflictError struct {
	// Namespace and Name identify the claim.
	Namespace string
	Name      string

	// Fields are the fields whose update the API server refused, as it
	// reports them. Empty when it named none.
	Fields []string

	// Cause is the API server's refusal. Nil when the claim was kept by the
	// resource manager's delete guard, which sees no refusal.
	Cause error
}

func (e *ClaimConflictError) Error() string {
	claim := fmt.Sprintf("%s %s/%s", claimKind, e.Namespace, e.Name)
	if e.Cause == nil {
		return claim + " was not deleted: the apply may not delete data"
	}
	return fmt.Sprintf("%s was not recreated, the apply may not delete data: %v", claim, e.Cause)
}

func (e *ClaimConflictError) Unwrap() error { return e.Cause }

// isCoreClaim reports whether group and kind name a PersistentVolumeClaim of
// the core API group, the one kind whose deletion also deletes user data.
func isCoreClaim(group, kind string) bool {
	return group == "" && kind == claimKind
}

// isClaimObject reports whether obj is a core PersistentVolumeClaim, typed or
// unstructured.
func isClaimObject(obj client.Object) bool {
	if _, ok := obj.(*corev1.PersistentVolumeClaim); ok {
		return true
	}
	gvk := obj.GetObjectKind().GroupVersionKind()
	return isCoreClaim(gvk.Group, gvk.Kind)
}

// checkClaims finds the first live PersistentVolumeClaim in resources that a
// forced apply would delete and recreate, and returns a *ClaimConflictError
// for it. It asks the API server with the dry-run the staged apply itself
// makes, and judges the answer as the staged apply does, so it refuses
// exactly the claims that would be deleted. It changes nothing.
//
// A claim that is not in the cluster has no data to protect. A dry-run error
// that would not lead to a recreate is left to the staged apply to report.
func checkClaims(ctx context.Context, c client.Client, resources []*unstructured.Unstructured) error {
	for _, obj := range resources {
		gvk := obj.GroupVersionKind()
		if !isCoreClaim(gvk.Group, gvk.Kind) {
			continue
		}
		live := &unstructured.Unstructured{}
		live.SetGroupVersionKind(gvk)
		if err := c.Get(ctx, client.ObjectKeyFromObject(obj), live); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return fmt.Errorf("failed to read %s %s/%s before a forced apply: %w",
				claimKind, obj.GetNamespace(), obj.GetName(), err)
		}
		err := c.Apply(ctx, client.ApplyConfigurationFromUnstructured(obj.DeepCopy()),
			client.DryRunAll, client.ForceOwnership, client.FieldOwner(FieldManager))
		if err != nil && ssaerrors.IsImmutableError(err) {
			return &ClaimConflictError{
				Namespace: obj.GetNamespace(),
				Name:      obj.GetName(),
				Fields:    refusedFields(err),
				Cause:     err,
			}
		}
	}
	return nil
}

// refusedFields returns the fields the API server names in the causes of a
// refusal, without duplicates and in its order.
func refusedFields(err error) []string {
	var statusErr apierrors.APIStatus
	if !errors.As(err, &statusErr) {
		return nil
	}
	details := statusErr.Status().Details
	if details == nil {
		return nil
	}
	var fields []string
	seen := map[string]struct{}{}
	for _, cause := range details.Causes {
		field := strings.TrimSpace(cause.Field)
		if field == "" {
			continue
		}
		if _, dup := seen[field]; dup {
			continue
		}
		seen[field] = struct{}{}
		fields = append(fields, field)
	}
	return fields
}

// claimDeletionKey marks a context under which the resource manager's client
// may delete PersistentVolumeClaims.
type claimDeletionKey struct{}

// allowClaimDeletion returns a context under which the resource manager's
// client deletes PersistentVolumeClaims. Only Apply calls it.
func allowClaimDeletion(ctx context.Context) context.Context {
	return context.WithValue(ctx, claimDeletionKey{}, true)
}

func claimDeletionAllowed(ctx context.Context) bool {
	allowed, _ := ctx.Value(claimDeletionKey{}).(bool)
	return allowed
}

// claimGuard is the client a resource manager works through. It refuses to
// delete a PersistentVolumeClaim unless the context allows it, so a forced
// recreate cannot delete a claim that changed after checkClaims read it, and
// a caller that sets no option cannot delete one at all.
type claimGuard struct {
	client.Client
}

func (g claimGuard) Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
	if isClaimObject(obj) && !claimDeletionAllowed(ctx) {
		return &ClaimConflictError{Namespace: obj.GetNamespace(), Name: obj.GetName()}
	}
	return g.Client.Delete(ctx, obj, opts...)
}

func (g claimGuard) DeleteAllOf(ctx context.Context, obj client.Object, opts ...client.DeleteAllOfOption) error {
	if isClaimObject(obj) && !claimDeletionAllowed(ctx) {
		return &ClaimConflictError{Namespace: obj.GetNamespace()}
	}
	return g.Client.DeleteAllOf(ctx, obj, opts...)
}
