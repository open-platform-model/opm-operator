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
}

// DetectDrift performs SSA dry-run diffs for each resource and returns which
// resources have drifted from desired state. Uses Flux's ResourceManager.Diff
// which performs a server-side apply dry-run and compares the result.
//
// A resource is considered drifted when the dry-run result differs from the
// desired state (Flux returns ConfiguredAction). Resources that don't exist
// yet (CreatedAction) or are unchanged are not considered drifted.
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
