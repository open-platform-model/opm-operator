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
	"context"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/open-platform-model/opm-operator/internal/shrink"
)

// ClaimShrinkDecider judges whether one rendered resource must be withheld
// from apply. The reconcile orchestrates the refusal; it does not implement
// the decision (Principle II).
type ClaimShrinkDecider interface {
	Decide(ctx context.Context, rendered *unstructured.Unstructured) (shrink.Decision, error)
}

// shrinkDecider builds the decision seam from what the reconcile already
// holds, so the guard cannot be left unwired.
//
// The stored claim is read uncached, because the comparison against it is the
// whole verdict. APIReader is absent only in tests that construct params by
// hand; the cached client stands in there, and the integration suite's client
// is a direct one anyway.
func shrinkDecider(params *ModuleInstanceParams) *shrink.Decider {
	reader := params.APIReader
	if reader == nil {
		reader = params.Client
	}
	return &shrink.Decider{Client: params.Client, APIReader: reader}
}

// withholdRefused splits the rendered resources into the list that may be
// applied and the refusals that kept the rest out of it.
//
// The inventory is deliberately not derived from the returned list: the
// operator still owns a resource it declined to update, and dropping it from
// the inventory would mark it stale and prune the very object the refusal
// exists to protect (verified in withhold_invariant_test.go).
func withholdRefused(
	ctx context.Context,
	decider ClaimShrinkDecider,
	resources []*unstructured.Unstructured,
) ([]*unstructured.Unstructured, []shrink.Decision, error) {
	var (
		refused  []shrink.Decision
		withheld []int
	)

	for i, resource := range resources {
		decision, err := decider.Decide(ctx, resource)
		if err != nil {
			return nil, nil, err
		}
		if !decision.Refused() {
			continue
		}
		refused = append(refused, decision)
		withheld = append(withheld, i)
	}

	// Nothing withheld: the caller's slice is the apply list, so the common
	// reconcile copies nothing.
	if len(refused) == 0 {
		return resources, nil, nil
	}

	applyList := make([]*unstructured.Unstructured, 0, len(resources)-len(withheld))
	next := 0
	for i, resource := range resources {
		if next < len(withheld) && withheld[next] == i {
			next++
			continue
		}
		applyList = append(applyList, resource)
	}

	return applyList, refused, nil
}

// refusalMessage states what was refused in the blocked delete's wording: the
// claim, the contracts it would stop providing, and how many instances still
// demand them. The dependents are named too, because the operator's next
// action is to move them off the contract or to keep it.
func refusalMessage(decisions []shrink.Decision) string {
	parts := make([]string, 0, len(decisions))
	for _, decision := range decisions {
		parts = append(parts, fmt.Sprintf(
			"claim %s would stop providing %s, which %d module instance(s) still demand (%s)",
			decision.Claim,
			strings.Join(decision.Dropped, ", "),
			len(decision.Dependents),
			strings.Join(decision.Dependents, ", "),
		))
	}
	return fmt.Sprintf(
		"Upgrade blocked: %s; move them off the contract or keep it in the provider's catalog",
		strings.Join(parts, "; "))
}
