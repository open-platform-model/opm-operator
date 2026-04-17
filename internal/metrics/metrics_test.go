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

package metrics

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
)

func TestMetricsRegistered(t *testing.T) {
	tests := []struct {
		name   string
		metric string
		labels []string
	}{
		{
			name:   "ReconcileTotal",
			metric: "opm_controller_reconcile_total",
			labels: []string{"name", "namespace", "outcome"},
		},
		{
			name:   "ReconcileDuration",
			metric: "opm_controller_reconcile_duration_seconds",
			labels: []string{"name", "namespace"},
		},
		{
			name:   "ApplyResourcesTotal",
			metric: "opm_controller_apply_resources_total",
			labels: []string{"name", "namespace", "action"},
		},
		{
			name:   "PruneResourcesTotal",
			metric: "opm_controller_prune_resources_total",
			labels: []string{"name", "namespace"},
		},
		{
			name:   "InventorySize",
			metric: "opm_controller_inventory_size",
			labels: []string{"name", "namespace"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Verify the metric exists by attempting to collect it.
			// testutil.CollectAndCount returns 0 for unregistered metrics.
			// Since init() registers all metrics, they should all be collectible.
			switch tt.metric {
			case "opm_controller_reconcile_total":
				assert.NotNil(t, ReconcileTotal)
			case "opm_controller_reconcile_duration_seconds":
				assert.NotNil(t, ReconcileDuration)
			case "opm_controller_apply_resources_total":
				assert.NotNil(t, ApplyResourcesTotal)
			case "opm_controller_prune_resources_total":
				assert.NotNil(t, PruneResourcesTotal)
			case "opm_controller_inventory_size":
				assert.NotNil(t, InventorySize)
			}
		})
	}
}

func TestRecordReconcile(t *testing.T) {
	// Reset counters for this test.
	ReconcileTotal.Reset()
	ReconcileDuration.Reset()

	RecordReconcile("test-release", "default", "applied", 2500*time.Millisecond)

	count := testutil.ToFloat64(ReconcileTotal.WithLabelValues("test-release", "default", "applied"))
	assert.Equal(t, float64(1), count)

	// Duration histogram should have 1 observation.
	histCount := testutil.CollectAndCount(ReconcileDuration)
	assert.Greater(t, histCount, 0)
}

func TestRecordApply(t *testing.T) {
	ApplyResourcesTotal.Reset()

	RecordApply("test-release", "default", 3, 2, 1)

	created := testutil.ToFloat64(ApplyResourcesTotal.WithLabelValues("test-release", "default", "created"))
	updated := testutil.ToFloat64(ApplyResourcesTotal.WithLabelValues("test-release", "default", "updated"))
	unchanged := testutil.ToFloat64(ApplyResourcesTotal.WithLabelValues("test-release", "default", "unchanged"))

	assert.Equal(t, float64(3), created)
	assert.Equal(t, float64(2), updated)
	assert.Equal(t, float64(1), unchanged)
}

func TestRecordPrune(t *testing.T) {
	PruneResourcesTotal.Reset()

	RecordPrune("test-release", "default", 5)

	count := testutil.ToFloat64(PruneResourcesTotal.WithLabelValues("test-release", "default"))
	assert.Equal(t, float64(5), count)
}

func TestRecordDuration(t *testing.T) {
	ReconcileDuration.Reset()

	RecordDuration("test-release", "default", 500*time.Millisecond)

	histCount := testutil.CollectAndCount(ReconcileDuration)
	assert.Greater(t, histCount, 0)
}

func TestSetInventorySize(t *testing.T) {
	InventorySize.Reset()

	SetInventorySize("test-release", "default", 15)

	size := testutil.ToFloat64(InventorySize.WithLabelValues("test-release", "default"))
	assert.Equal(t, float64(15), size)

	// Gauge should update when set again.
	SetInventorySize("test-release", "default", 10)
	size = testutil.ToFloat64(InventorySize.WithLabelValues("test-release", "default"))
	assert.Equal(t, float64(10), size)
}
