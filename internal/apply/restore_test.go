package apply

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestRestorable(t *testing.T) {
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

	configMap := object("v1", "ConfigMap", "cm", nil)
	deployment := object("apps/v1", "Deployment", "web", map[string]any{"replicas": int64(1)})
	plainJob := object("batch/v1", "Job", "once", map[string]any{"backoffLimit": int64(1)})
	expiringJob := object("batch/v1", "Job", "migrate", map[string]any{"ttlSecondsAfterFinished": int64(300)})
	zeroTTLJob := object("batch/v1", "Job", "cleanup", map[string]any{"ttlSecondsAfterFinished": int64(0)})
	// A kind named Job in another group has its own meaning of the field.
	otherJob := object("example.com/v1", "Job", "other", map[string]any{"ttlSecondsAfterFinished": int64(300)})

	got := Restorable([]*unstructured.Unstructured{configMap, expiringJob, deployment, zeroTTLJob, plainJob, otherJob})
	want := []*unstructured.Unstructured{configMap, deployment, plainJob, otherJob}
	if len(got) != len(want) {
		t.Fatalf("Restorable returned %d objects, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Restorable()[%d] = %s %s, want %s %s",
				i, got[i].GetKind(), got[i].GetName(), want[i].GetKind(), want[i].GetName())
		}
	}

	if out := Restorable(nil); len(out) != 0 {
		t.Errorf("Restorable(nil) = %d objects, want none", len(out))
	}
}
