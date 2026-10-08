package reconcile

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
)

func renderedObject(apiVersion, kind, name string, spec map[string]any) *unstructured.Unstructured {
	u := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": apiVersion,
		"kind":       kind,
		"metadata":   map[string]any{"name": name, "namespace": "apps"},
	}}
	if spec != nil {
		u.Object["spec"] = spec
	}
	return u
}

func TestExpiredJobs(t *testing.T) {
	configMap := renderedObject("v1", "ConfigMap", "settings", nil)
	expiring := renderedObject("batch/v1", "Job", "migrate", map[string]any{"ttlSecondsAfterFinished": int64(100)})
	plain := renderedObject("batch/v1", "Job", "once", nil)
	missing := []*unstructured.Unstructured{configMap, expiring, plain}

	got := expiredJobs(true, missing)
	if len(got) != 1 || got[0] != expiring {
		t.Errorf("expiredJobs with unchanged digests = %v, want only the Job with a TTL", got)
	}
	if got := expiredJobs(false, missing); len(got) != 0 {
		t.Errorf("expiredJobs with changed digests = %d objects, want none: the apply creates them", len(got))
	}
}

func TestWithoutEntries(t *testing.T) {
	job := func(name string) releasesv1alpha1.InventoryEntry {
		return releasesv1alpha1.InventoryEntry{Group: "batch", Kind: "Job", Namespace: "apps", Name: name, Version: "v1"}
	}
	entries := []releasesv1alpha1.InventoryEntry{entry("ConfigMap", "migrate"), job("migrate"), entry("Deployment", "web"), job("once")}
	expired := []*unstructured.Unstructured{
		renderedObject("batch/v1", "Job", "migrate", map[string]any{"ttlSecondsAfterFinished": int64(100)}),
	}

	got := withoutEntries(entries, expired)
	want := []releasesv1alpha1.InventoryEntry{entry("ConfigMap", "migrate"), entry("Deployment", "web"), job("once")}
	if len(got) != len(want) {
		t.Fatalf("withoutEntries kept %d entries, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("withoutEntries()[%d] = %v, want %v", i, got[i], want[i])
		}
	}
	if len(entries) != 4 {
		t.Errorf("withoutEntries changed its input")
	}
	if same := withoutEntries(entries, nil); len(same) != len(entries) {
		t.Errorf("withoutEntries with nothing to drop kept %d entries, want %d", len(same), len(entries))
	}
}

func TestForgetExpiredJobs(t *testing.T) {
	jobEntry := releasesv1alpha1.InventoryEntry{Group: "batch", Kind: "Job", Namespace: "apps", Name: "migrate", Version: "v1"}
	expired := []*unstructured.Unstructured{
		renderedObject("batch/v1", "Job", "migrate", map[string]any{"ttlSecondsAfterFinished": int64(100)}),
	}
	newInstance := func() *releasesv1alpha1.ModuleInstance {
		return &releasesv1alpha1.ModuleInstance{Status: releasesv1alpha1.ModuleInstanceStatus{
			Inventory: &releasesv1alpha1.Inventory{
				Revision: 3,
				Digest:   "sha256:rendered",
				Count:    2,
				Entries:  []releasesv1alpha1.InventoryEntry{entry("ConfigMap", "settings"), jobEntry},
			},
		}}
	}

	mi := newInstance()
	forgetExpiredJobs(context.Background(), mi, expired)
	inv := mi.Status.Inventory
	if len(inv.Entries) != 1 || inv.Entries[0] != entry("ConfigMap", "settings") || inv.Count != 1 {
		t.Errorf("inventory after the removal = %d entries (count %d), want the ConfigMap only", len(inv.Entries), inv.Count)
	}
	if inv.Digest != "sha256:rendered" || inv.Revision != 3 {
		t.Errorf("digest, revision = %q, %d, want them unchanged", inv.Digest, inv.Revision)
	}

	untouched := newInstance()
	forgetExpiredJobs(context.Background(), untouched, nil)
	if len(untouched.Status.Inventory.Entries) != 2 || untouched.Status.Inventory.Count != 2 {
		t.Errorf("an inventory with no expired Job was changed")
	}

	forgetExpiredJobs(context.Background(), &releasesv1alpha1.ModuleInstance{}, expired) // no inventory: no panic
}

// The verdict says when a Job entry was read as missing, and only then: the
// skip path renders on it.
func TestJudgeHealthReportsAnAbsentJob(t *testing.T) {
	jobEntry := releasesv1alpha1.InventoryEntry{Group: "batch", Kind: "Job", Namespace: "apps", Name: "migrate", Version: "v1"}
	otherJob := releasesv1alpha1.InventoryEntry{Group: "example.com", Kind: "Job", Namespace: "apps", Name: "other", Version: "v1"}
	present := &healthReader{objs: map[string]map[string]any{"ConfigMap apps/cfg": configMap("cfg")}}

	tests := []struct {
		name    string
		entries []releasesv1alpha1.InventoryEntry
		want    bool
	}{
		{"an absent batch Job", []releasesv1alpha1.InventoryEntry{entry("ConfigMap", "cfg"), jobEntry}, true},
		{"an absent ConfigMap", []releasesv1alpha1.InventoryEntry{entry("ConfigMap", "gone")}, false},
		{"an absent Job of another group", []releasesv1alpha1.InventoryEntry{otherJob}, false},
		{"nothing absent", []releasesv1alpha1.InventoryEntry{entry("ConfigMap", "cfg")}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := judgeHealth(context.Background(), present, tt.entries)
			if v.absentJob != tt.want {
				t.Errorf("absentJob = %v, want %v (verdict %s %s)", v.absentJob, tt.want, v.status, v.reason)
			}
		})
	}

	// The absent Job is still a missing object to the verdict itself.
	v := judgeHealth(context.Background(), present, []releasesv1alpha1.InventoryEntry{jobEntry})
	if v.status != metav1.ConditionFalse {
		t.Errorf("an absent Job entry judged %s, want False", v.status)
	}
}
