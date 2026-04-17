package apply

import (
	fluxssa "github.com/fluxcd/pkg/ssa"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// FieldManager is the SSA field manager name used by the controller.
	// Distinguishes from "opm-cli", "kubectl", "helm", etc.
	// From docs/design/ssa-ownership-and-drift-policy.md.
	FieldManager = "opm-controller"
)

// NewResourceManager constructs a Flux SSA ResourceManager with the opm-controller
// field manager. The owner string is used for SSA ownership labels.
func NewResourceManager(c client.Client, owner string) *fluxssa.ResourceManager {
	return fluxssa.NewResourceManager(c, nil, fluxssa.Owner{
		Field: FieldManager,
		Group: owner,
	})
}
