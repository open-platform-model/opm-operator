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
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
)

func crdEntry(name string) releasesv1alpha1.InventoryEntry {
	return releasesv1alpha1.InventoryEntry{
		Group:   "apiextensions.k8s.io",
		Kind:    "CustomResourceDefinition",
		Name:    name,
		Version: "v1",
	}
}

func TestIsOwnInstance(t *testing.T) {
	tests := []struct {
		name       string
		objName    string
		namespace  string
		path       string
		entries    []releasesv1alpha1.InventoryEntry
		wantOwn    bool
		wantSignal string
	}{
		{
			name:       "fixed coordinates with any module path",
			objName:    "opm-operator",
			namespace:  "opm-operator-system",
			path:       "example.com/modules/anything@v3",
			wantOwn:    true,
			wantSignal: "name opm-operator in namespace opm-operator-system",
		},
		{
			name:       "operator module major v0 elsewhere",
			objName:    "team-ops",
			namespace:  "platform",
			path:       "opmodel.dev/modules/opm_operator@v0",
			wantOwn:    true,
			wantSignal: "module opmodel.dev/modules/opm_operator",
		},
		{
			name:       "operator module major v1 elsewhere",
			objName:    "team-ops",
			namespace:  "platform",
			path:       "opmodel.dev/modules/opm_operator@v1",
			wantOwn:    true,
			wantSignal: "module opmodel.dev/modules/opm_operator",
		},
		{
			name:       "operator module without major elsewhere",
			objName:    "team-ops",
			namespace:  "platform",
			path:       "opmodel.dev/modules/opm_operator",
			wantOwn:    true,
			wantSignal: "module opmodel.dev/modules/opm_operator",
		},
		{
			name:      "recorded operator CRD",
			objName:   "renamed",
			namespace: "platform",
			path:      "example.com/forks/operator@v0",
			entries: []releasesv1alpha1.InventoryEntry{
				{Kind: "ServiceAccount", Namespace: "platform", Name: "x", Version: "v1"},
				crdEntry("moduleinstances.opmodel.dev"),
			},
			wantOwn:    true,
			wantSignal: "inventory records CustomResourceDefinition moduleinstances.opmodel.dev",
		},
		{
			name:       "first matching signal wins",
			objName:    "opm-operator",
			namespace:  "opm-operator-system",
			path:       "opmodel.dev/modules/opm_operator@v0",
			entries:    []releasesv1alpha1.InventoryEntry{crdEntry("platforms.opmodel.dev")},
			wantOwn:    true,
			wantSignal: "name opm-operator in namespace opm-operator-system",
		},
		{
			name:      "similar names are not the operator's own instance",
			objName:   "opm-operator",
			namespace: "default",
			path:      "opmodel.dev/modules/opm_operator_dashboard@v0",
			entries: []releasesv1alpha1.InventoryEntry{
				crdEntry("widgets.example.opmodel.dev"),
				crdEntry("widgets.example.opmodel.dev.io"),
			},
			wantOwn: false,
		},
		{
			name:      "another name in the operator's namespace",
			objName:   "something",
			namespace: "opm-operator-system",
			path:      "example.com/modules/app@v0",
			wantOwn:   false,
		},
		{
			name:      "operator name in another namespace",
			objName:   "opm-operator",
			namespace: "default",
			path:      "example.com/modules/app@v0",
			wantOwn:   false,
		},
		{
			name:      "operator group on a non-CRD kind",
			objName:   "app",
			namespace: "default",
			path:      "example.com/modules/app@v0",
			entries: []releasesv1alpha1.InventoryEntry{
				{Group: "opmodel.dev", Kind: "ModuleInstance", Namespace: "default", Name: "moduleinstances.opmodel.dev"},
			},
			wantOwn: false,
		},
		{
			name:      "CRD name without a group part",
			objName:   "app",
			namespace: "default",
			path:      "example.com/modules/app@v0",
			entries:   []releasesv1alpha1.InventoryEntry{crdEntry("opmodel")},
			wantOwn:   false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mi := &releasesv1alpha1.ModuleInstance{
				ObjectMeta: metav1.ObjectMeta{Name: tt.objName, Namespace: tt.namespace},
				Spec: releasesv1alpha1.ModuleInstanceSpec{
					Module: releasesv1alpha1.ModuleReference{Path: tt.path, Version: "v0.1.0"},
				},
			}
			if tt.entries != nil {
				mi.Status.Inventory = &releasesv1alpha1.Inventory{Entries: tt.entries}
			}
			signal, own := isOwnInstance(mi)
			if own != tt.wantOwn {
				t.Fatalf("isOwnInstance() own = %v, want %v (signal %q)", own, tt.wantOwn, signal)
			}
			if signal != tt.wantSignal {
				t.Fatalf("isOwnInstance() signal = %q, want %q", signal, tt.wantSignal)
			}
		})
	}
}
