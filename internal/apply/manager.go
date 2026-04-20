package apply

import (
	"github.com/fluxcd/cli-utils/pkg/kstatus/polling"
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
//
// A StatusPoller is wired in because fluxssa.ApplyAllStaged internally calls
// WaitForSet after applying cluster-scoped resources (CRDs, ClusterRoles,
// Namespaces); a nil poller there nil-derefs on the first module whose render
// contains any such resource.
func NewResourceManager(c client.Client, owner string) *fluxssa.ResourceManager {
	poller := polling.NewStatusPoller(c, c.RESTMapper(), polling.Options{})
	return fluxssa.NewResourceManager(c, poller, fluxssa.Owner{
		Field: FieldManager,
		Group: owner,
	})
}
