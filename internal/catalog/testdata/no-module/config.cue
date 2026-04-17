package catalog

import "opmodel.dev/opm/v1alpha1/providers@v1"

// Import requires module resolution — without cue.mod/module.cue this fails.
providers: {
	"kubernetes": {}
}
