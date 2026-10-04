/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package reconcile

import (
	"strings"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
)

// The coordinates every CLI install records the operator's own instance
// under, and the module path the operator module is published under.
const (
	ownInstanceName       = "opm-operator"
	ownInstanceNamespace  = "opm-operator-system"
	ownInstanceModulePath = "opmodel.dev/modules/opm_operator"
)

// isOwnInstance reports whether mi deploys this operator, and which signal
// matched. It reads the stored object only (no render, no registry pull),
// because the refusal must be decided before the cleanup finalizer is
// registered. Signals are tried in order and the first match names the
// instance in the refusal message:
//
//  1. the fixed coordinates opm-operator in opm-operator-system;
//  2. a module path of the operator module, in any major;
//  3. a recorded inventory holding a CustomResourceDefinition of the
//     operator's own API group, which catches a renamed or forked copy
//     of the module once anything has recorded what it deployed.
func isOwnInstance(mi *releasesv1alpha1.ModuleInstance) (string, bool) {
	if mi.Name == ownInstanceName && mi.Namespace == ownInstanceNamespace {
		return "name " + ownInstanceName + " in namespace " + ownInstanceNamespace, true
	}

	path, _, _ := strings.Cut(mi.Spec.Module.Path, "@")
	if path == ownInstanceModulePath {
		return "module " + ownInstanceModulePath, true
	}

	if inv := mi.Status.Inventory; inv != nil {
		for _, e := range inv.Entries {
			if isOwnCRD(e) {
				return "inventory records CustomResourceDefinition " + e.Name, true
			}
		}
	}
	return "", false
}

// isOwnCRD reports whether an inventory entry is a CustomResourceDefinition
// of the operator's API group: the part of the CRD name after its first "."
// must equal the group exactly, so widgets.example.opmodel.dev does not match
// where a suffix check on ".opmodel.dev" would.
func isOwnCRD(e releasesv1alpha1.InventoryEntry) bool {
	if e.Group != "apiextensions.k8s.io" || e.Kind != "CustomResourceDefinition" {
		return false
	}
	_, group, ok := strings.Cut(e.Name, ".")
	return ok && group == releasesv1alpha1.GroupVersion.Group
}
