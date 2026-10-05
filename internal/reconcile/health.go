package reconcile

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/fluxcd/pkg/runtime/conditions"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/open-platform-model/library/opm/k8s/health"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	"github.com/open-platform-model/opm-operator/internal/status"
)

// The Healthy condition is judged here and only here. Every readiness rule
// is the library's opm/k8s/health: this file fetches the inventory's objects,
// hands them to it, and maps its verdicts to a condition and a requeue. It
// reads no field of a fetched object itself (0012:D3:R6), which
// TestHealthJudgesOnlyThroughTheLibrary pins.

const (
	// healthReadParallelism bounds the inventory reads in flight, so a large
	// inventory neither holds a worker for N serial round trips nor floods
	// the API server.
	healthReadParallelism = 8

	// healthRequeueFloor and healthRequeueCeiling bound the requeue of an
	// instance that has not rolled out yet.
	healthRequeueFloor   = 5 * time.Second
	healthRequeueCeiling = 2 * time.Minute

	// healthMessageObjects is how many not-ready objects a message names.
	healthMessageObjects = 5
)

// healthReadTimeout is the one deadline every read of a judgement shares. A
// variable so tests can shorten it.
var healthReadTimeout = 30 * time.Second

// healthRequeueClass says how a verdict requeues.
type healthRequeueClass int

const (
	// healthNoRequeue: rolled out, or nothing to judge.
	healthNoRequeue healthRequeueClass = iota
	// healthBackoff: not rolled out yet, or not readable; ask again soon.
	healthBackoff
	// healthStalled: a Deployment is past its progress deadline; ask again
	// at the stalled recheck interval.
	healthStalled
)

// healthVerdict is one judgement of an inventory: the Healthy condition it
// sets and how the reconcile requeues for it.
type healthVerdict struct {
	status  metav1.ConditionStatus
	reason  string
	message string
	requeue healthRequeueClass
}

// healthRead is the result of reading one inventory entry.
type healthRead struct {
	obj     *unstructured.Unstructured
	missing bool
	err     error
}

// judgeHealth reads every entry through r, uncached and in parallel under one
// deadline, and judges the result with the library's health package. The
// verdict depends only on what was read, in inventory order, so judging the
// same cluster state twice gives the same verdict and the same message.
func judgeHealth(ctx context.Context, r client.Reader, entries []releasesv1alpha1.InventoryEntry) healthVerdict {
	reads := readEntries(ctx, r, entries)

	var (
		statuses   []health.Status
		stalled    []string
		notReady   []string
		absent     int
		unreadable []string
	)
	for i, rd := range reads {
		name := describeEntry(entries[i])
		switch {
		case rd.missing:
			absent++
			notReady = append(notReady, fmt.Sprintf("%s (%s)", name, health.Missing))
		case rd.err != nil:
			unreadable = append(unreadable, fmt.Sprintf("reading %s: %v", name, rd.err))
		default:
			s := health.Evaluate(rd.obj)
			statuses = append(statuses, s)
			switch {
			case health.ProgressDeadlineExceeded(rd.obj):
				stalled = append(stalled, fmt.Sprintf("%s (%s)", name, s))
			case !health.IsHealthy(s):
				notReady = append(notReady, fmt.Sprintf("%s (%s)", name, s))
			}
		}
	}

	agg, ready, total := health.Aggregate(statuses, absent+len(unreadable))
	counts := fmt.Sprintf("%d/%d objects ready", ready, total)

	switch {
	case len(stalled) > 0:
		return healthVerdict{metav1.ConditionFalse, status.ProgressDeadlineExceededReason,
			counts + ": " + nameObjects(append(stalled, notReady...)), healthStalled}
	case len(notReady) > 0:
		return healthVerdict{metav1.ConditionFalse, status.NotRolledOutReason,
			counts + ": " + nameObjects(notReady), healthBackoff}
	case len(unreadable) > 0:
		return healthVerdict{metav1.ConditionUnknown, status.HealthUnknownReason,
			counts + ": " + unreadable[0], healthBackoff}
	case agg == health.Unknown:
		return healthVerdict{metav1.ConditionUnknown, status.HealthUnknownReason,
			counts + ": the inventory is empty", healthNoRequeue}
	default:
		return healthVerdict{metav1.ConditionTrue, status.RolledOutReason, counts, healthNoRequeue}
	}
}

// unreadableVerdict is the verdict when the reader itself could not be built,
// for example because the impersonated ServiceAccount is gone. Nothing was
// read, so every entry counts as not ready.
func unreadableVerdict(entries []releasesv1alpha1.InventoryEntry, err error) healthVerdict {
	_, ready, total := health.Aggregate(nil, len(entries))
	return healthVerdict{metav1.ConditionUnknown, status.HealthUnknownReason,
		fmt.Sprintf("%d/%d objects ready: building the health reader: %v", ready, total, err), healthBackoff}
}

// readEntries reads every entry, at most healthReadParallelism at a time, all
// under one healthReadTimeout deadline, and returns the results in entry
// order. A not-found read, and a read whose kind the API server no longer
// serves, is missing: neither object can exist. Every other error, the
// deadline included, is unreadable.
func readEntries(ctx context.Context, r client.Reader, entries []releasesv1alpha1.InventoryEntry) []healthRead {
	ctx, cancel := context.WithTimeout(ctx, healthReadTimeout)
	defer cancel()

	reads := make([]healthRead, len(entries))
	slots := make(chan struct{}, healthReadParallelism)
	var wg sync.WaitGroup
	for i := range entries {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			select {
			case slots <- struct{}{}:
			case <-ctx.Done():
				reads[i] = healthRead{err: ctx.Err()}
				return
			}
			defer func() { <-slots }()
			reads[i] = readEntry(ctx, r, entries[i])
		}(i)
	}
	wg.Wait()
	return reads
}

// readEntry reads one entry as an unstructured object of its recorded group,
// version and kind.
func readEntry(ctx context.Context, r client.Reader, e releasesv1alpha1.InventoryEntry) healthRead {
	if e.Version == "" {
		return healthRead{err: fmt.Errorf("the inventory entry records no API version")}
	}
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{Group: e.Group, Version: e.Version, Kind: e.Kind})
	err := r.Get(ctx, client.ObjectKey{Namespace: e.Namespace, Name: e.Name}, obj)
	switch {
	case err == nil:
		return healthRead{obj: obj}
	case apierrors.IsNotFound(err), apimeta.IsNoMatchError(err):
		return healthRead{missing: true}
	default:
		return healthRead{err: err}
	}
}

// describeEntry names an entry as "Kind namespace/name", or "Kind name" for a
// cluster-scoped object.
func describeEntry(e releasesv1alpha1.InventoryEntry) string {
	if e.Namespace == "" {
		return e.Kind + " " + e.Name
	}
	return e.Kind + " " + e.Namespace + "/" + e.Name
}

// nameObjects joins the first healthMessageObjects names and says how many
// more there are.
func nameObjects(names []string) string {
	if len(names) <= healthMessageObjects {
		return strings.Join(names, ", ")
	}
	return fmt.Sprintf("%s, and %d more",
		strings.Join(names[:healthMessageObjects], ", "), len(names)-healthMessageObjects)
}

// healthRequeue is how long a reconcile waits before judging again. A verdict
// that is not rolled out yet asks again after half the time since the last
// apply, between healthRequeueFloor and healthRequeueCeiling: quickly just
// after an apply, and settling at the ceiling for a long rollout. It needs no
// counter, so it survives a restart. A stalled Deployment waits the stalled
// recheck interval, and a rolled-out or empty inventory does not requeue.
func healthRequeue(v healthVerdict, lastAppliedAt *metav1.Time, now time.Time) time.Duration {
	switch v.requeue {
	case healthStalled:
		return StalledRecheckInterval
	case healthBackoff:
		if lastAppliedAt == nil || lastAppliedAt.After(now) {
			return healthRequeueFloor
		}
		return min(max(now.Sub(lastAppliedAt.Time)/2, healthRequeueFloor), healthRequeueCeiling)
	default:
		return 0
	}
}

// packageRequeue is a ModulePackage's requeue: its interval, or the health
// requeue when that is sooner.
func packageRequeue(healthAfter, interval time.Duration) time.Duration {
	if healthAfter > 0 && healthAfter < interval {
		return healthAfter
	}
	return interval
}

// applyHealth records a verdict as the Healthy condition.
func applyHealth(obj conditions.Setter, v healthVerdict) {
	status.MarkHealthy(obj, v.status, v.reason, "%s", v.message)
}

// managerReader is the reader for objects the manager applied as itself: its
// uncached API reader. The manager's client caches typed reads, and a cached
// read may show the rollout before the apply. Tests that wire no API reader
// get the client they wired, which in envtest is uncached.
func managerReader(apiReader client.Reader, c client.Client) client.Reader {
	if apiReader != nil {
		return apiReader
	}
	return c
}

// inventoryEntries returns the entries of inv, nil for no inventory.
func inventoryEntries(inv *releasesv1alpha1.Inventory) []releasesv1alpha1.InventoryEntry {
	if inv == nil {
		return nil
	}
	return inv.Entries
}
