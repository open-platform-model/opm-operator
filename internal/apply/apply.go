package apply

import (
	"context"
	"fmt"

	fluxssa "github.com/fluxcd/pkg/ssa"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// ApplyResult carries counts of apply outcomes.
type ApplyResult struct {
	// Created is the number of resources created (did not exist before).
	Created int

	// Updated is the number of resources updated (existed, fields changed).
	Updated int

	// Unchanged is the number of resources unchanged (existed, no field diff).
	Unchanged int
}

// Apply applies the given resources to the cluster using Server-Side Apply.
// Staging is handled by Flux's ApplyAllStaged, which applies cluster definitions
// (CRDs, Namespaces, ClusterRoles) first with readiness waits, then class
// definitions, then everything else. See docs/design/flux-ssa-staging.md.
//
// When force is true, immutable field conflicts are resolved by recreating
// the object (maps to ApplyOptions.Force, not SSA field-ownership conflicts —
// Flux always applies with ForceOwnership).
//
// Returns an ApplyResult with counts, or an error on any apply failure.
func Apply(
	ctx context.Context,
	rm *fluxssa.ResourceManager,
	resources []*unstructured.Unstructured,
	force bool,
) (*ApplyResult, error) {
	opts := fluxssa.DefaultApplyOptions()
	opts.Force = force

	changeSet, err := rm.ApplyAllStaged(ctx, resources, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to apply resources: %w", err)
	}

	result := &ApplyResult{}
	for _, entry := range changeSet.Entries {
		switch entry.Action {
		case fluxssa.CreatedAction:
			result.Created++
		case fluxssa.ConfiguredAction:
			result.Updated++
		case fluxssa.UnchangedAction:
			result.Unchanged++
		}
	}

	return result, nil
}
