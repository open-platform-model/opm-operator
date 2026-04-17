package core

// OPM Kubernetes label keys applied to all managed resources.
const (
	// LabelManagedBy is the standard Kubernetes label indicating the manager.
	LabelManagedBy = "app.kubernetes.io/managed-by"

	// LabelManagedByValue is the runtime-owned value for the CLI actor.
	// The controller uses LabelManagedByControllerValue instead.
	LabelManagedByValue = "opm-cli"

	// LabelManagedByControllerValue is the runtime-owned value for the controller actor.
	LabelManagedByControllerValue = "opm-controller"

	// LabelManagedByLegacyValue is the legacy value used before runtime-owned
	// labels were introduced. Recognized for backward compatibility during
	// transition but no longer stamped on new resources.
	LabelManagedByLegacyValue = "open-platform-model"

	// LabelComponent is the OPM infrastructure label that categorizes the type
	// of OPM-managed object (e.g., "inventory"). Distinct from component names
	// set by CUE transformers on application resources.
	LabelComponent = "opmodel.dev/component"

	// LabelComponentName is the label injected by the CUE catalog on all application
	// resources to record which component produced them. Value is the component name.
	// Set by module.cue in the v1alpha1 catalog:
	//   labels: "component.opmodel.dev/name": name
	// Used by inventory to track provenance for component-rename safety checks.
	LabelComponentName = "component.opmodel.dev/name"

	// LabelModuleReleaseName is the release name label.
	LabelModuleReleaseName = "module-release.opmodel.dev/name"

	// LabelModuleReleaseNamespace is the release namespace label.
	LabelModuleReleaseNamespace = "module-release.opmodel.dev/namespace"

	// LabelModuleReleaseUUID is the release identity UUID label for resource discovery.
	LabelModuleReleaseUUID = "module-release.opmodel.dev/uuid"
)

// IsOPMManagedBy reports whether a managed-by label value identifies any OPM
// runtime actor. This accepts the current CLI and controller values as well as
// the legacy value for backward compatibility with resources applied before
// runtime-owned labels were introduced.
func IsOPMManagedBy(value string) bool {
	switch value {
	case LabelManagedByValue, LabelManagedByControllerValue, LabelManagedByLegacyValue:
		return true
	default:
		return false
	}
}
