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

package apply

import (
	"testing"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
)

// Only a PersistentVolumeClaim of the core API group is a data claim. A kind
// of the same name in another group, and every other storage kind, is pruned
// like any resource.
func TestIsDataClaim(t *testing.T) {
	tests := []struct {
		name  string
		entry releasesv1alpha1.InventoryEntry
		want  bool
	}{
		{"core claim", releasesv1alpha1.InventoryEntry{Kind: "PersistentVolumeClaim", Version: "v1"}, true},
		{"same kind in another group", releasesv1alpha1.InventoryEntry{Group: "example.com", Kind: "PersistentVolumeClaim", Version: "v1"}, false},
		{"persistent volume", releasesv1alpha1.InventoryEntry{Kind: "PersistentVolume", Version: "v1"}, false},
		{"volume snapshot", releasesv1alpha1.InventoryEntry{Group: "snapshot.storage.k8s.io", Kind: "VolumeSnapshot", Version: "v1"}, false},
		{"config map", releasesv1alpha1.InventoryEntry{Kind: "ConfigMap", Version: "v1"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isDataClaim(tt.entry); got != tt.want {
				t.Fatalf("isDataClaim(%+v) = %v, want %v", tt.entry, got, tt.want)
			}
		})
	}
}
