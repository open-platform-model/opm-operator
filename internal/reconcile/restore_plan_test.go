package reconcile

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestPlanRestore(t *testing.T) {
	object := func(apiVersion, kind, name string, spec map[string]any) *unstructured.Unstructured {
		u := &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": apiVersion,
			"kind":       kind,
			"metadata":   map[string]any{"name": name, "namespace": "default"},
		}}
		if spec != nil {
			u.Object["spec"] = spec
		}
		return u
	}
	configMap := object("v1", "ConfigMap", "settings", nil)
	expiringJob := object("batch/v1", "Job", "migrate", map[string]any{"ttlSecondsAfterFinished": int64(300)})
	rendered := []*unstructured.Unstructured{configMap, expiringJob}

	tests := []struct {
		name          string
		isNoOp        bool
		missing       []*unstructured.Unstructured
		takenIn       []*unstructured.Unstructured
		wantApply     []*unstructured.Unstructured
		wantNoOp      bool
		wantRestoring bool
	}{
		{
			name:      "changed digests apply the rendered set whatever is missing",
			isNoOp:    false,
			missing:   []*unstructured.Unstructured{configMap},
			wantApply: rendered,
		},
		{
			name:      "unchanged digests with nothing missing stay a no-op",
			isNoOp:    true,
			wantApply: rendered,
			wantNoOp:  true,
		},
		{
			name:      "a finished Job with a TTL alone leaves the no-op",
			isNoOp:    true,
			missing:   []*unstructured.Unstructured{expiringJob},
			wantApply: rendered,
			wantNoOp:  true,
		},
		{
			name:          "unchanged digests with a missing object restore only that object",
			isNoOp:        true,
			missing:       []*unstructured.Unstructured{configMap, expiringJob},
			wantApply:     []*unstructured.Unstructured{configMap},
			wantRestoring: true,
		},
		{
			name:          "unchanged digests with a taken-in object apply only that object",
			isNoOp:        true,
			takenIn:       []*unstructured.Unstructured{configMap},
			wantApply:     []*unstructured.Unstructured{configMap},
			wantRestoring: true,
		},
		{
			name:      "changed digests apply the rendered set whatever is taken in",
			isNoOp:    false,
			takenIn:   []*unstructured.Unstructured{configMap},
			wantApply: rendered,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			toApply, noOp, restoring := planRestore(context.Background(), tt.isNoOp, tt.missing, tt.takenIn, rendered)
			if noOp != tt.wantNoOp || restoring != tt.wantRestoring {
				t.Errorf("planRestore noOp, restoring = %v, %v, want %v, %v", noOp, restoring, tt.wantNoOp, tt.wantRestoring)
			}
			if len(toApply) != len(tt.wantApply) {
				t.Fatalf("planRestore applies %d objects, want %d", len(toApply), len(tt.wantApply))
			}
			for i := range tt.wantApply {
				if toApply[i] != tt.wantApply[i] {
					t.Errorf("planRestore applies %s at %d, want %s", toApply[i].GetName(), i, tt.wantApply[i].GetName())
				}
			}
		})
	}
}
