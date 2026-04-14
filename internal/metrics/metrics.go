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
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	// ReconcileTotal counts reconcile attempts by outcome.
	ReconcileTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "opm_controller_reconcile_total",
			Help: "Total number of reconcile attempts by outcome.",
		},
		[]string{"name", "namespace", "outcome"},
	)

	// ReconcileDuration observes reconcile duration in seconds.
	ReconcileDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "opm_controller_reconcile_duration_seconds",
			Help:    "Duration of reconcile attempts in seconds.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"name", "namespace"},
	)

	// ApplyResourcesTotal counts resources applied by action.
	ApplyResourcesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "opm_controller_apply_resources_total",
			Help: "Total number of resources applied by action.",
		},
		[]string{"name", "namespace", "action"},
	)

	// PruneResourcesTotal counts resources pruned.
	PruneResourcesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "opm_controller_prune_resources_total",
			Help: "Total number of resources pruned.",
		},
		[]string{"name", "namespace"},
	)

	// InventorySize reports the current inventory entry count.
	InventorySize = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "opm_controller_inventory_size",
			Help: "Current number of entries in the inventory.",
		},
		[]string{"name", "namespace"},
	)
)

func init() {
	metrics.Registry.MustRegister(
		ReconcileTotal,
		ReconcileDuration,
		ApplyResourcesTotal,
		PruneResourcesTotal,
		InventorySize,
	)
}

// RecordReconcile records the outcome and duration of a reconcile attempt.
func RecordReconcile(name, namespace, outcome string, duration time.Duration) {
	ReconcileTotal.WithLabelValues(name, namespace, outcome).Inc()
	ReconcileDuration.WithLabelValues(name, namespace).Observe(duration.Seconds())
}

// RecordApply records apply resource counts by action.
func RecordApply(name, namespace string, created, updated, unchanged int) {
	ApplyResourcesTotal.WithLabelValues(name, namespace, "created").Add(float64(created))
	ApplyResourcesTotal.WithLabelValues(name, namespace, "updated").Add(float64(updated))
	ApplyResourcesTotal.WithLabelValues(name, namespace, "unchanged").Add(float64(unchanged))
}

// RecordPrune records the number of resources pruned.
func RecordPrune(name, namespace string, deleted int) {
	PruneResourcesTotal.WithLabelValues(name, namespace).Add(float64(deleted))
}

// RecordDuration records only the reconcile duration without an outcome counter.
// Used for suspend and deletion paths that don't have a reconcile outcome.
func RecordDuration(name, namespace string, duration time.Duration) {
	ReconcileDuration.WithLabelValues(name, namespace).Observe(duration.Seconds())
}

// SetInventorySize sets the current inventory size gauge.
func SetInventorySize(name, namespace string, size int) {
	InventorySize.WithLabelValues(name, namespace).Set(float64(size))
}
