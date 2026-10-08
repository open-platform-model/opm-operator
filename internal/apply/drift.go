package apply

import (
	"context"
	"fmt"

	fluxssa "github.com/fluxcd/pkg/ssa"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// DriftedResource identifies a single resource that has drifted from desired state.
type DriftedResource struct {
	Group     string
	Kind      string
	Namespace string
	Name      string
}

// DriftResult holds the outcome of drift detection across a resource set.
type DriftResult struct {
	// Drifted is true if any resource has drifted from desired state.
	Drifted bool

	// Resources lists the resources that have drifted.
	Resources []DriftedResource

	// Missing lists the given resources that do not exist on the cluster:
	// the dry-run would create them. A missing resource is not drift; the
	// caller decides whether to create it again.
	Missing []*unstructured.Unstructured
}

// DetectDrift performs SSA dry-run diffs for each resource and returns which
// resources have drifted from desired state. Uses Flux's ResourceManager.Diff
// which performs a server-side apply dry-run and compares the result.
//
// A resource is considered drifted when the dry-run result differs from the
// desired state (Flux returns ConfiguredAction). Resources that don't exist
// yet (CreatedAction) are not considered drifted and are returned in Missing;
// unchanged resources are neither.
//
// Returns an error only when the dry-run API call itself fails (transient).
// Drift detection results are not errors — drift is an expected operational signal.
func DetectDrift(
	ctx context.Context,
	rm *fluxssa.ResourceManager,
	resources []*unstructured.Unstructured,
) (*DriftResult, error) {
	log := logf.FromContext(ctx)
	result := &DriftResult{}
	opts := fluxssa.DefaultDiffOptions()

	for _, resource := range resources {
		entry, _, _, err := rm.Diff(ctx, resource, opts)
		if err != nil {
			return nil, fmt.Errorf("dry-run diff for %s/%s %s: %w",
				resource.GetNamespace(), resource.GetName(), resource.GetKind(), err)
		}

		if entry.Action == fluxssa.CreatedAction {
			result.Missing = append(result.Missing, resource)
			continue
		}

		if entry.Action == fluxssa.ConfiguredAction {
			gvk := resource.GroupVersionKind()
			drifted := DriftedResource{
				Group:     gvk.Group,
				Kind:      gvk.Kind,
				Namespace: resource.GetNamespace(),
				Name:      resource.GetName(),
			}
			result.Resources = append(result.Resources, drifted)
			log.V(1).Info("Drift detected",
				"kind", gvk.Kind, "namespace", resource.GetNamespace(), "name", resource.GetName())
		}
	}

	result.Drifted = len(result.Resources) > 0
	return result, nil
}

// Restorable returns the missing resources that a reconcile with unchanged
// digests creates again. A Job whose spec sets ttlSecondsAfterFinished is
// left out: the cluster deletes it after it finished, so its absence is the
// expected state, and creating it again would run it again. Expired returns
// the ones left out.
func Restorable(missing []*unstructured.Unstructured) []*unstructured.Unstructured {
	var out []*unstructured.Unstructured
	for _, obj := range missing {
		if expiresAfterFinish(obj) {
			continue
		}
		out = append(out, obj)
	}
	return out
}

// Expired returns the missing resources that Restorable leaves out: the Jobs
// that set ttlSecondsAfterFinished. With unchanged digests such a Job counts
// as finished and removed by the cluster. Nothing records that it completed,
// so a Job deleted by hand before it ran is returned too.
func Expired(missing []*unstructured.Unstructured) []*unstructured.Unstructured {
	var out []*unstructured.Unstructured
	for _, obj := range missing {
		if expiresAfterFinish(obj) {
			out = append(out, obj)
		}
	}
	return out
}

// expiresAfterFinish reports whether obj is a batch Job that sets
// spec.ttlSecondsAfterFinished.
func expiresAfterFinish(obj *unstructured.Unstructured) bool {
	gvk := obj.GroupVersionKind()
	if gvk.Group != "batch" || gvk.Kind != "Job" {
		return false
	}
	_, found, _ := unstructured.NestedFieldNoCopy(obj.Object, "spec", "ttlSecondsAfterFinished")
	return found
}
