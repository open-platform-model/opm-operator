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

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	"github.com/open-platform-model/opm-operator/internal/apply"
)

// A prune that only kept PersistentVolumeClaims deleted nothing, so the
// reconcile did not prune. Every other successful prune keeps the outcome it
// had before claims were protected.
func TestPruneOutcome(t *testing.T) {
	claim := releasesv1alpha1.InventoryEntry{Kind: "PersistentVolumeClaim", Version: "v1", Namespace: "media", Name: "config"}
	tests := []struct {
		name   string
		result apply.PruneResult
		want   Outcome
	}{
		{"deleted", apply.PruneResult{Deleted: 2}, AppliedAndPruned},
		{"deleted and kept", apply.PruneResult{Deleted: 1, Kept: []releasesv1alpha1.InventoryEntry{claim}}, AppliedAndPruned},
		{"only kept", apply.PruneResult{Kept: []releasesv1alpha1.InventoryEntry{claim}}, Applied},
		{"only skipped", apply.PruneResult{Skipped: 1}, AppliedAndPruned},
		{"nothing left to do", apply.PruneResult{}, AppliedAndPruned},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := pruneOutcome(&tt.result); got != tt.want {
				t.Fatalf("pruneOutcome(%+v) = %v, want %v", tt.result, got, tt.want)
			}
		})
	}
}
