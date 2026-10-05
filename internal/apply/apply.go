package apply

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/fluxcd/cli-utils/pkg/object"
	fluxssa "github.com/fluxcd/pkg/ssa"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// ApplyResult carries counts of apply outcomes.
type ApplyResult struct {
	// Created is the number of resources created (did not exist before).
	Created int

	// Updated is the number of resources updated (existed, fields changed).
	Updated int

	// Unchanged is the number of resources unchanged (existed, no field diff).
	Unchanged int
}

// The discovery retry's pacing. They are variables so the package's unit
// tests can shorten them; tests that shorten these must not call t.Parallel.
var (
	// discoveryRetryInterval is the wait between two staged apply attempts.
	discoveryRetryInterval = 500 * time.Millisecond

	// discoveryRetryTimeout is how long after the first retryable failure a
	// new attempt may still start. It does not limit how long an attempt runs.
	discoveryRetryTimeout = 10 * time.Second
)

// stagedApply runs one staged apply of a fixed resource set under ctx. It
// returns the change set of the objects applied up to a failure.
type stagedApply func(ctx context.Context) (*fluxssa.ChangeSet, error)

// Apply applies the given resources to the cluster using Server-Side Apply.
// Staging is handled by Flux's ApplyAllStaged: it applies cluster definitions
// (CRDs, Namespaces, ClusterRoles) and waits for them to become ready, then
// applies class definitions and waits for them, then any custom-stage kinds,
// then everything else.
//
// A CRD is ready once it reports Established, but API discovery can serve its
// kind a moment later, so a custom resource in the same set can fail with a
// no-match error. Apply then retries the whole staged apply every
// discoveryRetryInterval, starting no new attempt once discoveryRetryTimeout
// has passed since the first such failure, and never past ctx. Every attempt
// runs under ctx unchanged. Only a no-match for a kind (or, when discovery
// reports only the group, a group) that a CRD in resources defines is
// retried; every other error returns at once. When the retry gives up, Apply
// returns the last no-match error.
//
// When force is true, immutable field conflicts are resolved by recreating
// the object (maps to ApplyOptions.Force, not SSA field-ownership conflicts —
// Flux always applies with ForceOwnership).
//
// Returns an ApplyResult with counts, or an error on any apply failure. An
// object counts by the first attempt that created or configured it.
func Apply(
	ctx context.Context,
	rm *fluxssa.ResourceManager,
	resources []*unstructured.Unstructured,
	force bool,
) (*ApplyResult, error) {
	opts := fluxssa.DefaultApplyOptions()
	opts.Force = force

	return applyWithDiscoveryRetry(ctx, func(ctx context.Context) (*fluxssa.ChangeSet, error) {
		return rm.ApplyAllStaged(ctx, resources, opts)
	}, resources)
}

// applyWithDiscoveryRetry runs apply until it succeeds, fails with an error
// pendingCRDKind does not accept, or the retry window or ctx ends, and merges
// the counts of every attempt. See Apply.
func applyWithDiscoveryRetry(
	ctx context.Context,
	apply stagedApply,
	resources []*unstructured.Unstructured,
) (*ApplyResult, error) {
	ledger := actionLedger{}
	var pending error      // the last retryable no-match
	var deadline time.Time // no new attempt starts after it
	for {
		cs, err := apply(ctx)
		ledger.record(cs)
		if err == nil {
			return ledger.result(cs), nil
		}
		// An attempt that ctx cut short reports the context, not the cause, so
		// return the earlier no-match. A real error the attempt hit first,
		// such as a conflict once discovery served the kind, still returns.
		if pending != nil && ctx.Err() != nil &&
			(errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) ||
				pendingCRDKind(err, resources)) {
			return nil, fmt.Errorf("failed to apply resources: %w", pending)
		}
		if !pendingCRDKind(err, resources) {
			return nil, fmt.Errorf("failed to apply resources: %w", err)
		}
		pending = err
		if deadline.IsZero() {
			deadline = time.Now().Add(discoveryRetryTimeout)
		}
		logf.FromContext(ctx).V(1).Info("Waiting for the API server to serve a custom resource kind",
			"error", err.Error())

		timer := time.NewTimer(discoveryRetryInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, fmt.Errorf("failed to apply resources: %w", pending)
		case <-timer.C:
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("failed to apply resources: %w", pending)
		}
	}
}

// pendingCRDKind reports whether err only says that the API server does not
// serve yet a kind that a CustomResourceDefinition in resources defines.
// Discovery can lag a CRD's Established condition, so such an error is worth
// retrying. When discovery reports only the group (the aggregated discovery
// path), a group that a CRD in resources defines is enough.
func pendingCRDKind(err error, resources []*unstructured.Unstructured) bool {
	if !meta.IsNoMatchError(err) {
		return false
	}
	defined := crdKinds(resources)
	if len(defined) == 0 {
		return false
	}
	if kindErr, ok := errors.AsType[*meta.NoKindMatchError](err); ok {
		_, found := defined[kindErr.GroupKind]
		return found
	}
	if resErr, ok := errors.AsType[*meta.NoResourceMatchError](err); ok {
		for gk := range defined {
			if gk.Group == resErr.PartialResource.Group {
				return true
			}
		}
	}
	return false
}

// crdKinds returns the group and kind that each CustomResourceDefinition in
// resources defines. A CRD without spec.group or spec.names.kind defines
// nothing.
func crdKinds(resources []*unstructured.Unstructured) map[schema.GroupKind]struct{} {
	defined := map[schema.GroupKind]struct{}{}
	for _, obj := range resources {
		gvk := obj.GroupVersionKind()
		if gvk.Group != "apiextensions.k8s.io" || gvk.Kind != "CustomResourceDefinition" {
			continue
		}
		group, _, _ := unstructured.NestedString(obj.Object, "spec", "group")
		kind, _, _ := unstructured.NestedString(obj.Object, "spec", "names", "kind")
		if group == "" || kind == "" {
			continue
		}
		defined[schema.GroupKind{Group: group, Kind: kind}] = struct{}{}
	}
	return defined
}

// actionLedger remembers, per object, the first created or configured action
// any attempt reported for it, so a retry that sees the object unchanged
// still counts what this call did to it.
type actionLedger map[object.ObjMetadata]fluxssa.Action

// record notes the created and configured actions in cs. A nil cs is fine.
func (l actionLedger) record(cs *fluxssa.ChangeSet) {
	if cs == nil {
		return
	}
	for _, entry := range cs.Entries {
		if entry.Action != fluxssa.CreatedAction && entry.Action != fluxssa.ConfiguredAction {
			continue
		}
		if _, seen := l[entry.ObjMetadata]; !seen {
			l[entry.ObjMetadata] = entry.Action
		}
	}
}

// result counts the objects of the successful attempt's change set cs, each
// by its first created or configured action when it had one.
func (l actionLedger) result(cs *fluxssa.ChangeSet) *ApplyResult {
	result := &ApplyResult{}
	if cs == nil {
		return result
	}
	for _, entry := range cs.Entries {
		action := entry.Action
		if first, ok := l[entry.ObjMetadata]; ok {
			action = first
		}
		switch action {
		case fluxssa.CreatedAction:
			result.Created++
		case fluxssa.ConfiguredAction:
			result.Updated++
		case fluxssa.UnchangedAction:
			result.Unchanged++
		}
	}
	return result
}
